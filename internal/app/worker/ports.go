package worker

import (
	"context"
	"time"

	"sms-gateway/internal/domain"
)

type Queue interface {
	Claim(ctx context.Context, req ClaimRequest) ([]domain.Message, error)
	NextDue(ctx context.Context, lane domain.Lane, customerID uint32) (due time.Time, ok bool, err error)
	Complete(ctx context.Context, batch []Settlement) (stale []uint64, err error)
}

type PendingFinder interface {
	PendingCustomers(ctx context.Context, lane domain.Lane) ([]uint32, error)
}

type Scheduler interface {
	Next(ctx context.Context, lane domain.Lane) (customerID uint32, ok bool, err error)
	Add(ctx context.Context, lane domain.Lane, customerIDs ...uint32) error
	Remove(ctx context.Context, lane domain.Lane, customerID uint32) error
	Delay(ctx context.Context, lane domain.Lane, customerID uint32, until time.Time) error
	WakeDue(ctx context.Context, lane domain.Lane, now time.Time) error
}

type Operator interface {
	Send(ctx context.Context, msg domain.Message) (ref string, err error)
}

type Limiter interface {
	Wait(ctx context.Context, lane domain.Lane) error
}

type ClaimRequest struct {
	Lane       domain.Lane
	CustomerID uint32
	Limit      int
	Now        time.Time
	LeaseUntil time.Time
}

type Outcome struct {
	Status      domain.Status
	OperatorRef string
	Reason      string
	RetryAt     time.Time
	At          time.Time
}

type Settlement struct {
	Message domain.Message
	Outcome Outcome
}
