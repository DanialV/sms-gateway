package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"sms-gateway/internal/adapter/httpapi"
	"sms-gateway/internal/adapter/logging"
	"sms-gateway/internal/adapter/metrics"
	"sms-gateway/internal/adapter/mysql"
	"sms-gateway/internal/adapter/redis"
	"sms-gateway/internal/app/receiver"
)

func main() {
	if err := run(); err != nil {
		slog.Error("receiver stopped", "err", err)
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
	ids, err := httpapi.NewIDCodec(cfg.idAlphabet, uint8(cfg.idLength))
	if err != nil {
		return err
	}
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
	go func() {
		if err := metrics.Serve(ctx, cfg.metricsAddr, reg); err != nil {
			log.Error("metrics endpoint stopped", "err", err)
		}
	}()

	svc := receiver.NewService(mysql.NewReceiverStore(db), redis.NewScheduler(rdb), cfg.policies, log)
	server := httpapi.NewServer(svc, ids, metrics.NewHTTP(reg), log)
	log.Info("receiver listening", "addr", cfg.httpAddr, "metrics", cfg.metricsAddr)
	return server.Run(ctx, cfg.httpAddr)
}
