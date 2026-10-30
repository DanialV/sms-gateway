package receiver

import (
	"context"
	"time"

	"github.com/google/uuid"

	"sms-gateway/internal/domain"
)

type Store interface {
	CreateCustomer(ctx context.Context, customer domain.Customer) (domain.Customer, error)
	Customer(ctx context.Context, id uint32) (domain.Customer, error)
	Accept(ctx context.Context, msg domain.Message) (domain.Message, error)
	TopUp(ctx context.Context, customerID uint32, amount int64) (domain.Wallet, error)
	Wallet(ctx context.Context, customerID uint32) (domain.Wallet, error)
	Message(ctx context.Context, customerID uint32, id uint64) (domain.Message, error)
	MessageByClientID(ctx context.Context, customerID uint32, clientID uuid.UUID) (domain.Message, error)
	Messages(ctx context.Context, query MessageQuery) ([]domain.Message, error)
}

type Scheduler interface {
	Add(ctx context.Context, lane domain.Lane, customerIDs ...uint32) error
}

type MessageQuery struct {
	CustomerID uint32
	Status     domain.Status
	From       time.Time
	To         time.Time
	Cursor     uint64
	Limit      int
}
