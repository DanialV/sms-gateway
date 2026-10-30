package operator

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	"sms-gateway/internal/domain"
)

var errUnavailable = errors.New("operator temporarily unavailable")

type SimulatorConfig struct {
	MinLatency  time.Duration
	MaxLatency  time.Duration
	TimeoutRate float64
	RejectRate  float64
	FailureRate float64
}

type Simulator struct {
	cfg SimulatorConfig
}

func NewSimulator(cfg SimulatorConfig) *Simulator {
	return &Simulator{cfg: cfg}
}

func (s *Simulator) Send(ctx context.Context, msg domain.Message) (string, error) {
	roll := rand.Float64()
	if roll < s.cfg.TimeoutRate {
		<-ctx.Done()
		return "", ctx.Err()
	}
	if err := wait(ctx, s.latency()); err != nil {
		return "", err
	}
	switch roll -= s.cfg.TimeoutRate; {
	case roll < s.cfg.RejectRate:
		return "", fmt.Errorf("%w: invalid destination", domain.ErrRejected)
	case roll < s.cfg.RejectRate+s.cfg.FailureRate:
		return "", errUnavailable
	}
	return "sim-" + strconv.FormatUint(msg.ID, 10), nil
}

func (s *Simulator) latency() time.Duration {
	spread := s.cfg.MaxLatency - s.cfg.MinLatency
	if spread <= 0 {
		return s.cfg.MinLatency
	}
	return s.cfg.MinLatency + rand.N(spread)
}

func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
