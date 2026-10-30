package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"sms-gateway/internal/domain"
)

var (
	now     = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	discard = slog.New(slog.NewTextHandler(io.Discard, nil))
)

type fakeQueue struct {
	batch    []domain.Message
	due      time.Time
	settled  [][]Settlement
	outcomes []Outcome
}

func (f *fakeQueue) Claim(context.Context, ClaimRequest) ([]domain.Message, error) {
	batch := f.batch
	f.batch = nil
	return batch, nil
}

func (f *fakeQueue) NextDue(context.Context, domain.Lane, uint32) (time.Time, bool, error) {
	return f.due, !f.due.IsZero(), nil
}

func (f *fakeQueue) Complete(_ context.Context, batch []Settlement) ([]uint64, error) {
	f.settled = append(f.settled, batch)
	for _, item := range batch {
		f.outcomes = append(f.outcomes, item.Outcome)
	}
	return nil, nil
}

type fakeScheduler struct {
	removed []uint32
	added   map[domain.Lane][]uint32
	delayed map[uint32]time.Time
	woken   []domain.Lane
}

func newFakeScheduler() *fakeScheduler {
	return &fakeScheduler{added: map[domain.Lane][]uint32{}, delayed: map[uint32]time.Time{}}
}

func (f *fakeScheduler) Next(context.Context, domain.Lane) (uint32, bool, error) {
	return 42, true, nil
}

func (f *fakeScheduler) Add(_ context.Context, lane domain.Lane, ids ...uint32) error {
	f.added[lane] = append(f.added[lane], ids...)
	return nil
}

func (f *fakeScheduler) Remove(_ context.Context, _ domain.Lane, id uint32) error {
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeScheduler) Delay(_ context.Context, _ domain.Lane, id uint32, until time.Time) error {
	if current, ok := f.delayed[id]; !ok || until.Before(current) {
		f.delayed[id] = until
	}
	return nil
}

func (f *fakeScheduler) WakeDue(_ context.Context, lane domain.Lane, _ time.Time) error {
	f.woken = append(f.woken, lane)
	return nil
}

type fakeOperator struct {
	err   error
	calls int
}

func (f *fakeOperator) Send(context.Context, domain.Message) (string, error) {
	f.calls++
	return "ref-1", f.err
}

type noLimit struct{}

func (noLimit) Wait(context.Context, domain.Lane) error { return nil }

func newTestDispatcher(queue *fakeQueue, scheduler *fakeScheduler, op *fakeOperator) *Dispatcher {
	cfg := LaneConfig{
		Lane: domain.LaneNormal, Concurrency: 1, BatchSize: 10, Lease: time.Minute,
		SendTimeout: time.Second, RetryBase: time.Second, RetryMax: time.Minute,
	}
	d := NewDispatcher(cfg, queue, scheduler, op, noLimit{}, discard)
	d.now = func() time.Time { return now }
	return d
}

func claimedMessage(expiresIn time.Duration) domain.Message {
	return domain.Message{ID: 1, CustomerID: 42, Lane: domain.LaneNormal, Price: 1, Attempt: 1, ExpiresAt: now.Add(expiresIn)}
}

func TestTickRecordsOperatorOutcome(t *testing.T) {
	cases := map[string]struct {
		operatorErr error
		expiresIn   time.Duration
		status      domain.Status
		calls       int
	}{
		"delivered":     {status: domain.StatusSent, expiresIn: time.Hour, calls: 1},
		"rejected":      {operatorErr: fmt.Errorf("%w: bad number", domain.ErrRejected), status: domain.StatusFailed, expiresIn: time.Hour, calls: 1},
		"transient":     {operatorErr: errors.New("timeout"), status: domain.StatusPending, expiresIn: time.Hour, calls: 1},
		"already stale": {status: domain.StatusExpired, expiresIn: 0, calls: 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			queue := &fakeQueue{batch: []domain.Message{claimedMessage(tc.expiresIn)}}
			op := &fakeOperator{err: tc.operatorErr}
			if _, err := newTestDispatcher(queue, newFakeScheduler(), op).tick(context.Background()); err != nil {
				t.Fatalf("tick: %v", err)
			}
			if len(queue.outcomes) != 1 || queue.outcomes[0].Status != tc.status {
				t.Fatalf("expected %s, got %+v", tc.status, queue.outcomes)
			}
			if op.calls != tc.calls {
				t.Fatalf("expected %d operator calls, got %d", tc.calls, op.calls)
			}
		})
	}
}

func TestBatchIsSettledInOneCall(t *testing.T) {
	batch := []domain.Message{claimedMessage(time.Hour), claimedMessage(time.Hour), claimedMessage(time.Hour)}
	queue, op := &fakeQueue{batch: batch}, &fakeOperator{}
	if _, err := newTestDispatcher(queue, newFakeScheduler(), op).tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if op.calls != 3 || len(queue.settled) != 1 || len(queue.settled[0]) != 3 {
		t.Fatalf("operator calls=%d, settle calls=%d; want 3 sends settled in 1 call", op.calls, len(queue.settled))
	}
}

func TestRetryIsBackedOffAndClampedToExpiry(t *testing.T) {
	queue := &fakeQueue{batch: []domain.Message{claimedMessage(500 * time.Millisecond)}}
	d := newTestDispatcher(queue, newFakeScheduler(), &fakeOperator{err: errors.New("unavailable")})
	if _, err := d.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got := queue.outcomes[0].RetryAt; !got.Equal(now.Add(500 * time.Millisecond)) {
		t.Fatalf("retry must not be scheduled past expiry, got %v", got)
	}
}

func TestEmptyClaimRemovesCustomerUntilNextDue(t *testing.T) {
	later := now.Add(5 * time.Second)
	cases := map[string]struct {
		due     time.Time
		readded bool
		delayed time.Time
	}{
		"nothing unfinished": {},
		"due now":            {due: now, readded: true},
		"due later":          {due: later, delayed: later},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			scheduler := newFakeScheduler()
			d := newTestDispatcher(&fakeQueue{due: tc.due}, scheduler, &fakeOperator{})
			if _, err := d.tick(context.Background()); err != nil {
				t.Fatalf("tick: %v", err)
			}
			if len(scheduler.removed) != 1 {
				t.Fatalf("customer must be removed, got %v", scheduler.removed)
			}
			if readded := len(scheduler.added[domain.LaneNormal]) == 1; readded != tc.readded {
				t.Fatalf("re-added=%v, want %v", readded, tc.readded)
			}
			if got := scheduler.delayed[42]; !got.Equal(tc.delayed) {
				t.Fatalf("delayed until %v, want %v", got, tc.delayed)
			}
		})
	}
}

func TestRetryDelaysCustomerUntilEarliestRetry(t *testing.T) {
	first, third := claimedMessage(time.Hour), claimedMessage(time.Hour)
	third.Attempt = 3
	scheduler := newFakeScheduler()
	d := newTestDispatcher(&fakeQueue{batch: []domain.Message{third, first}}, scheduler, &fakeOperator{err: errors.New("unavailable")})
	if _, err := d.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got := scheduler.delayed[42]; !got.Equal(now.Add(time.Second)) {
		t.Fatalf("delayed until %v, want the first retry at %v", got, now.Add(time.Second))
	}
}

func TestFinishedBatchSetsNoWakeUp(t *testing.T) {
	scheduler := newFakeScheduler()
	d := newTestDispatcher(&fakeQueue{batch: []domain.Message{claimedMessage(time.Hour)}}, scheduler, &fakeOperator{})
	if _, err := d.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(scheduler.delayed) != 0 {
		t.Fatalf("a sent message must not set a wake-up, got %v", scheduler.delayed)
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	d := newTestDispatcher(&fakeQueue{}, newFakeScheduler(), &fakeOperator{})
	want := map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 4: 8 * time.Second, 10: time.Minute, 100: time.Minute}
	for attempt, expected := range want {
		if got := d.backoff(attempt); got != expected {
			t.Errorf("attempt %d: got %v, want %v", attempt, got, expected)
		}
	}
}
