package worker

import (
	"context"
	"log/slog"
	"time"

	"sms-gateway/internal/domain"
)

type Waker struct {
	scheduler Scheduler
	interval  time.Duration
	now       func() time.Time
	log       *slog.Logger
}

func NewWaker(scheduler Scheduler, interval time.Duration, log *slog.Logger) *Waker {
	return &Waker{scheduler: scheduler, interval: interval, now: time.Now, log: log}
}

func (w *Waker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := recovered(func() error { return w.Wake(ctx) }); err != nil && ctx.Err() == nil {
			w.log.ErrorContext(ctx, "waking delayed customers failed", "err", err)
		}
	}
}

func (w *Waker) Wake(ctx context.Context) error {
	now := w.now()
	for _, lane := range domain.Lanes {
		if err := w.scheduler.WakeDue(ctx, lane, now); err != nil {
			return err
		}
	}
	return nil
}
