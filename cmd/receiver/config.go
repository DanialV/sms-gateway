package main

import (
	"errors"
	"math"
	"time"

	"sms-gateway/internal/adapter/logging"
	"sms-gateway/internal/app/receiver"
	"sms-gateway/internal/domain"
	"sms-gateway/internal/env"
)

type config struct {
	httpAddr    string
	metricsAddr string
	mysqlDSN    string
	mysqlConns  int
	redisAddr   string
	policies    map[domain.Lane]receiver.LanePolicy
	idAlphabet  string
	idLength    int
	logging     logging.Config
}

func loadConfig() (config, error) {
	var vars env.Reader
	cfg := config{
		httpAddr:    vars.String("HTTP_ADDR", ":8080"),
		metricsAddr: vars.String("METRICS_ADDR", ":9100"),
		mysqlDSN:    vars.String("MYSQL_DSN", "sms:sms@tcp(127.0.0.1:3307)/sms?parseTime=true&loc=UTC"),
		mysqlConns:  vars.Int("MYSQL_MAX_CONNS", 50),
		redisAddr:   vars.String("REDIS_ADDR", "127.0.0.1:6379"),
		policies: map[domain.Lane]receiver.LanePolicy{
			domain.LaneNormal:  {Price: vars.Int64("PRICE_NORMAL", 1), MaxTTL: vars.Duration("MAX_TTL_NORMAL", 24*time.Hour)},
			domain.LaneExpress: {Price: vars.Int64("PRICE_EXPRESS", 1), MaxTTL: vars.Duration("MAX_TTL_EXPRESS", 2*time.Minute)},
		},
		idAlphabet: vars.String("PUBLIC_ID_ALPHABET", "BG2WJknpz7slOYihdvbarqtEgIA8VC1Pf63UmTwS5HLQFoKMZc04exX9NRjDyu"),
		idLength:   vars.Int("PUBLIC_ID_MIN_LENGTH", 10),
		logging: logging.Config{
			Service:     "receiver",
			SentryDSN:   vars.String("SENTRY_DSN", ""),
			Environment: vars.String("ENVIRONMENT", "development"),
		},
	}
	return cfg, errors.Join(vars.Err(), cfg.validate())
}

func (c config) validate() error {
	if c.idLength < 0 || c.idLength > math.MaxUint8 {
		return errors.New("PUBLIC_ID_MIN_LENGTH must be between 0 and 255")
	}
	for lane, policy := range c.policies {
		if policy.Price <= 0 || policy.MaxTTL <= 0 {
			return errors.New("price and max TTL must be positive for lane " + string(lane))
		}
	}
	return nil
}
