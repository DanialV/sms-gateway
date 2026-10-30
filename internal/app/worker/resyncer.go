package worker

import (
	"context"
	"log/slog"
	"time"

	"sms-gateway/internal/domain"
)

type Resyncer struct {
	pending   PendingFinder
	scheduler Scheduler
	interval  time.Duration
	log       *slog.Logger
}

func NewResyncer(pending PendingFinder, scheduler Scheduler, interval time.Duration, log *slog.Logger) *Resyncer {
	return &Resyncer{pending: pending, scheduler: scheduler, interval: interval, log: log}
}

func (r *Resyncer) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		if err := recovered(func() error { return r.Resync(ctx) }); err != nil && ctx.Err() == nil {
			r.log.ErrorContext(ctx, "resync failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Resyncer) Resync(ctx context.Context) error {
	for _, lane := range domain.Lanes {
		ids, err := r.pending.PendingCustomers(ctx, lane)
		if err != nil {
			return err
		}
		if err := r.scheduler.Add(ctx, lane, ids...); err != nil {
			return err
		}
	}
	return nil
}
