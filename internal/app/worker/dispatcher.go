package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"sms-gateway/internal/domain"
)

const maxBackoffShift = 16

type LaneConfig struct {
	Lane        domain.Lane
	Concurrency int
	BatchSize   int
	Lease       time.Duration
	SendTimeout time.Duration
	RetryBase   time.Duration
	RetryMax    time.Duration
	IdleWait    time.Duration
}

type Dispatcher struct {
	cfg       LaneConfig
	queue     Queue
	scheduler Scheduler
	operator  Operator
	limiter   Limiter
	now       func() time.Time
	log       *slog.Logger
}

func NewDispatcher(cfg LaneConfig, queue Queue, scheduler Scheduler, operator Operator, limiter Limiter, log *slog.Logger) *Dispatcher {
	return &Dispatcher{
		cfg:       cfg,
		queue:     queue,
		scheduler: scheduler,
		operator:  operator,
		limiter:   limiter,
		now:       func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) },
		log:       log.With("lane", cfg.Lane),
	}
}

func (d *Dispatcher) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range d.cfg.Concurrency {
		wg.Go(func() { d.loop(ctx) })
	}
	wg.Wait()
}

func (d *Dispatcher) loop(ctx context.Context) {
	for ctx.Err() == nil {
		var busy bool
		err := recovered(func() (err error) {
			busy, err = d.tick(ctx)
			return err
		})
		if err != nil {
			d.log.ErrorContext(ctx, "dispatch failed", "err", err)
		}
		if !busy || err != nil {
			sleep(ctx, d.cfg.IdleWait)
		}
	}
}

func (d *Dispatcher) tick(ctx context.Context) (bool, error) {
	customerID, ok, err := d.scheduler.Next(ctx, d.cfg.Lane)
	if err != nil || !ok {
		return false, err
	}
	now := d.now()
	batch, err := d.queue.Claim(ctx, ClaimRequest{
		Lane:       d.cfg.Lane,
		CustomerID: customerID,
		Limit:      d.cfg.BatchSize,
		Now:        now,
		LeaseUntil: now.Add(d.cfg.Lease),
	})
	if err != nil {
		return false, err
	}
	if len(batch) == 0 {
		return true, d.removeFromRingIfIdle(ctx, customerID)
	}
	d.deliver(context.WithoutCancel(ctx), batch)
	return true, nil
}

// Remove first, then check: a message that arrives in between is either seen here or re-adds the customer itself.
func (d *Dispatcher) removeFromRingIfIdle(ctx context.Context, customerID uint32) error {
	if err := d.scheduler.Remove(ctx, d.cfg.Lane, customerID); err != nil {
		return err
	}
	due, ok, err := d.queue.NextDue(ctx, d.cfg.Lane, customerID)
	if err != nil || !ok {
		return err
	}
	if due.After(d.now()) {
		return d.scheduler.Delay(ctx, d.cfg.Lane, customerID, due)
	}
	return d.scheduler.Add(ctx, d.cfg.Lane, customerID)
}

func (d *Dispatcher) deliver(ctx context.Context, batch []domain.Message) {
	settlements := make([]Settlement, 0, len(batch))
	for _, msg := range batch {
		settlements = append(settlements, Settlement{Message: msg, Outcome: d.send(ctx, msg)})
	}
	stale, err := d.queue.Complete(ctx, settlements)
	if err != nil {
		d.log.ErrorContext(ctx, "recording outcomes failed", "messages", len(batch), "err", err)
		return
	}
	for _, id := range stale {
		d.log.WarnContext(ctx, "outcome discarded, message was reclaimed", "message_id", id)
	}
	if err := d.delayUntilRetry(ctx, settlements); err != nil {
		d.log.WarnContext(ctx, "retry wake-up deferred to resync", "customer_id", batch[0].CustomerID, "err", err)
	}
}

func (d *Dispatcher) delayUntilRetry(ctx context.Context, settlements []Settlement) error {
	var due time.Time
	for _, s := range settlements {
		if s.Outcome.Status == domain.StatusPending && (due.IsZero() || s.Outcome.RetryAt.Before(due)) {
			due = s.Outcome.RetryAt
		}
	}
	if due.IsZero() {
		return nil
	}
	return d.scheduler.Delay(ctx, d.cfg.Lane, settlements[0].Message.CustomerID, due)
}

func (d *Dispatcher) send(ctx context.Context, msg domain.Message) Outcome {
	if msg.IsExpired(d.now()) {
		return d.final(domain.StatusExpired, "expired before delivery")
	}
	if err := d.limiter.Wait(ctx, msg.Lane); err != nil {
		return d.retry(msg, err)
	}
	sendCtx, cancel := context.WithTimeout(ctx, d.cfg.SendTimeout)
	defer cancel()
	ref, err := d.operator.Send(sendCtx, msg)
	switch {
	case err == nil:
		return Outcome{Status: domain.StatusSent, OperatorRef: ref, At: d.now()}
	case errors.Is(err, domain.ErrRejected):
		return d.final(domain.StatusFailed, err.Error())
	default:
		return d.retry(msg, err)
	}
}

func (d *Dispatcher) final(status domain.Status, reason string) Outcome {
	return Outcome{Status: status, Reason: reason, At: d.now()}
}

func (d *Dispatcher) retry(msg domain.Message, cause error) Outcome {
	now := d.now()
	retryAt := now.Add(d.backoff(msg.Attempt))
	if retryAt.After(msg.ExpiresAt) {
		retryAt = msg.ExpiresAt
	}
	return Outcome{Status: domain.StatusPending, Reason: cause.Error(), RetryAt: retryAt, At: now}
}

func (d *Dispatcher) backoff(attempt int) time.Duration {
	shift := min(max(attempt-1, 0), maxBackoffShift)
	return min(d.cfg.RetryBase<<shift, d.cfg.RetryMax)
}

func sleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
