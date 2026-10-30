package main

import (
	"errors"
	"strings"
	"time"

	"sms-gateway/internal/adapter/logging"
	"sms-gateway/internal/adapter/operator"
	"sms-gateway/internal/app/worker"
	"sms-gateway/internal/domain"
	"sms-gateway/internal/env"
)

type config struct {
	metricsAddr     string
	mysqlDSN        string
	mysqlConns      int
	redisAddr       string
	lanes           []worker.LaneConfig
	resyncInterval  time.Duration
	wakeInterval    time.Duration
	ratePerSecond   int
	expressReserved int
	simulator       operator.SimulatorConfig
	logging         logging.Config
}

func loadConfig() (config, error) {
	var vars env.Reader
	cfg := config{
		metricsAddr: vars.String("METRICS_ADDR", ":9100"),
		mysqlDSN:    vars.String("MYSQL_DSN", "sms:sms@tcp(127.0.0.1:3307)/sms?parseTime=true&loc=UTC"),
		mysqlConns:  vars.Int("MYSQL_MAX_CONNS", 50),
		redisAddr:   vars.String("REDIS_ADDR", "127.0.0.1:6379"),
		lanes: []worker.LaneConfig{
			laneConfig(&vars, worker.LaneConfig{
				Lane: domain.LaneExpress, Concurrency: 32, BatchSize: 1, Lease: 15 * time.Second,
				SendTimeout: 3 * time.Second, RetryBase: 200 * time.Millisecond, RetryMax: 2 * time.Second,
			}),
			laneConfig(&vars, worker.LaneConfig{
				Lane: domain.LaneNormal, Concurrency: 64, BatchSize: 10, Lease: time.Minute,
				SendTimeout: 5 * time.Second, RetryBase: time.Second, RetryMax: time.Minute,
			}),
		},
		resyncInterval:  vars.Duration("RESYNC_INTERVAL", time.Minute),
		wakeInterval:    vars.Duration("WAKE_INTERVAL", 100*time.Millisecond),
		ratePerSecond:   vars.Int("OPERATOR_RATE_LIMIT", 1000),
		expressReserved: vars.Int("EXPRESS_RESERVED_PERCENT", 20),
		simulator: operator.SimulatorConfig{
			MinLatency:  vars.Duration("SIM_MIN_LATENCY", 20*time.Millisecond),
			MaxLatency:  vars.Duration("SIM_MAX_LATENCY", 150*time.Millisecond),
			TimeoutRate: vars.Float("SIM_TIMEOUT_RATE", 0.01),
			RejectRate:  vars.Float("SIM_REJECT_RATE", 0.01),
			FailureRate: vars.Float("SIM_FAILURE_RATE", 0.02),
		},
		logging: logging.Config{
			Service:     "worker",
			SentryDSN:   vars.String("SENTRY_DSN", ""),
			Environment: vars.String("ENVIRONMENT", "development"),
		},
	}
	return cfg, errors.Join(vars.Err(), cfg.validate())
}

func laneConfig(vars *env.Reader, defaults worker.LaneConfig) worker.LaneConfig {
	prefix := strings.ToUpper(string(defaults.Lane)) + "_"
	return worker.LaneConfig{
		Lane:        defaults.Lane,
		Concurrency: vars.Int(prefix+"CONCURRENCY", defaults.Concurrency),
		BatchSize:   vars.Int(prefix+"BATCH_SIZE", defaults.BatchSize),
		Lease:       vars.Duration(prefix+"LEASE", defaults.Lease),
		SendTimeout: vars.Duration(prefix+"SEND_TIMEOUT", defaults.SendTimeout),
		RetryBase:   vars.Duration(prefix+"RETRY_BASE", defaults.RetryBase),
		RetryMax:    vars.Duration(prefix+"RETRY_MAX", defaults.RetryMax),
		IdleWait:    vars.Duration(prefix+"IDLE_WAIT", 50*time.Millisecond),
	}
}

func (c config) validate() error {
	if c.expressReserved < 0 || c.expressReserved > 100 {
		return errors.New("EXPRESS_RESERVED_PERCENT must be between 0 and 100")
	}
	if c.resyncInterval <= 0 || c.wakeInterval <= 0 {
		return errors.New("RESYNC_INTERVAL and WAKE_INTERVAL must be positive")
	}
	for _, lane := range c.lanes {
		if lane.Concurrency <= 0 || lane.BatchSize <= 0 {
			return errors.New("concurrency and batch size must be positive for lane " + string(lane.Lane))
		}
		if lane.Lease <= time.Duration(lane.BatchSize)*lane.SendTimeout {
			return errors.New("lease must be longer than batch size × send timeout for lane " + string(lane.Lane))
		}
	}
	return nil
}
