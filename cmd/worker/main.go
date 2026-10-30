package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"sms-gateway/internal/adapter/logging"
	"sms-gateway/internal/adapter/metrics"
	"sms-gateway/internal/adapter/mysql"
	"sms-gateway/internal/adapter/operator"
	"sms-gateway/internal/adapter/redis"
	"sms-gateway/internal/app/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	log, flush, err := logging.New(cfg.logging)
	if err != nil {
		return err
	}
	defer flush()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := mysql.Open(ctx, cfg.mysqlDSN, cfg.mysqlConns)
	if err != nil {
		return err
	}
	defer db.Close()
	rdb := redis.NewClient(cfg.redisAddr)
	defer rdb.Close()

	reg := metrics.NewRegistry(db)
	store := mysql.NewWorkerStore(db)
	scheduler := redis.NewScheduler(rdb)
	queue := metrics.InstrumentQueue(store, reg)
	limiter := metrics.InstrumentLimiter(redis.NewLimiter(rdb, cfg.ratePerSecond, cfg.expressReserved), reg)
	sender := metrics.InstrumentOperator(operator.NewSimulator(cfg.simulator), reg)
	metrics.RegisterBacklog(reg, store, scheduler, log)

	var wg sync.WaitGroup
	wg.Go(func() {
		if err := metrics.Serve(ctx, cfg.metricsAddr, reg); err != nil {
			log.Error("metrics endpoint stopped", "err", err)
		}
	})
	wg.Go(func() { worker.NewResyncer(store, scheduler, cfg.resyncInterval, log).Run(ctx) })
	wg.Go(func() { worker.NewWaker(scheduler, cfg.wakeInterval, log).Run(ctx) })
	for _, lane := range cfg.lanes {
		dispatcher := worker.NewDispatcher(lane, queue, scheduler, sender, limiter, log)
		wg.Go(func() { dispatcher.Run(ctx) })
	}
	log.Info("worker started", "lanes", len(cfg.lanes), "metrics", cfg.metricsAddr)
	wg.Wait()
	log.Info("worker drained")
	return nil
}
