package receiver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"sms-gateway/internal/domain"
)

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

type LanePolicy struct {
	Price  int64
	MaxTTL time.Duration
}

type SendCommand struct {
	CustomerID uint32
	ClientID   uuid.UUID
	Phone      string
	Text       string
	Express    bool
	TTL        time.Duration
}

type Service struct {
	store     Store
	scheduler Scheduler
	policies  map[domain.Lane]LanePolicy
	now       func() time.Time
	log       *slog.Logger
}

func NewService(store Store, scheduler Scheduler, policies map[domain.Lane]LanePolicy, log *slog.Logger) *Service {
	return &Service{
		store:     store,
		scheduler: scheduler,
		policies:  policies,
		now:       func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) },
		log:       log,
	}
}

func (s *Service) CreateCustomer(ctx context.Context, name string) (domain.Customer, error) {
	if err := domain.ValidateCustomerName(name); err != nil {
		return domain.Customer{}, err
	}
	return s.store.CreateCustomer(ctx, domain.Customer{Name: name, CreatedAt: s.now()})
}

func (s *Service) Customer(ctx context.Context, id uint32) (domain.Customer, error) {
	if err := domain.ValidateCustomer(id); err != nil {
		return domain.Customer{}, err
	}
	return s.store.Customer(ctx, id)
}

func (s *Service) Send(ctx context.Context, cmd SendCommand) (domain.Message, error) {
	if err := requireClientID(cmd.ClientID); err != nil {
		return domain.Message{}, err
	}
	msg, err := s.newMessage(cmd)
	if err != nil {
		return domain.Message{}, err
	}
	stored, err := s.store.Accept(ctx, msg)
	if err != nil {
		return domain.Message{}, err
	}
	if err := s.scheduler.Add(ctx, stored.Lane, stored.CustomerID); err != nil {
		s.log.WarnContext(ctx, "scheduling deferred to resync", "customer_id", stored.CustomerID, "err", err)
	}
	return stored, nil
}

func (s *Service) TopUp(ctx context.Context, customerID uint32, amount int64) (domain.Wallet, error) {
	if err := domain.ValidateCustomer(customerID); err != nil {
		return domain.Wallet{}, err
	}
	if amount <= 0 {
		return domain.Wallet{}, domain.Invalid("amount", "must be positive")
	}
	return s.store.TopUp(ctx, customerID, amount)
}

func (s *Service) Balance(ctx context.Context, customerID uint32) (domain.Wallet, error) {
	if err := domain.ValidateCustomer(customerID); err != nil {
		return domain.Wallet{}, err
	}
	return s.store.Wallet(ctx, customerID)
}

func (s *Service) Message(ctx context.Context, customerID uint32, id uint64) (domain.Message, error) {
	if err := domain.ValidateCustomer(customerID); err != nil {
		return domain.Message{}, err
	}
	return s.store.Message(ctx, customerID, id)
}

func (s *Service) MessageByClientID(ctx context.Context, customerID uint32, clientID uuid.UUID) (domain.Message, error) {
	if err := domain.ValidateCustomer(customerID); err != nil {
		return domain.Message{}, err
	}
	if err := requireClientID(clientID); err != nil {
		return domain.Message{}, err
	}
	return s.store.MessageByClientID(ctx, customerID, clientID)
}

func (s *Service) Messages(ctx context.Context, query MessageQuery) ([]domain.Message, uint64, error) {
	if err := domain.ValidateCustomer(query.CustomerID); err != nil {
		return nil, 0, err
	}
	query.Limit = pageSize(query.Limit)
	msgs, err := s.store.Messages(ctx, query)
	if err != nil || len(msgs) < query.Limit {
		return msgs, 0, err
	}
	return msgs, msgs[len(msgs)-1].ID, nil
}

func (s *Service) newMessage(cmd SendCommand) (domain.Message, error) {
	lane := domain.LaneFor(cmd.Express)
	policy := s.policies[lane]
	ttl, err := policy.ttl(cmd.TTL)
	if err != nil {
		return domain.Message{}, err
	}
	draft := domain.Draft{CustomerID: cmd.CustomerID, Phone: cmd.Phone, Body: cmd.Text, TTL: ttl}
	if err := draft.Validate(); err != nil {
		return domain.Message{}, err
	}
	now := s.now()
	return domain.Message{
		CustomerID: draft.CustomerID,
		ClientID:   cmd.ClientID,
		Lane:       lane,
		Phone:      draft.Phone,
		Body:       draft.Body,
		Price:      policy.Price,
		Status:     domain.StatusPending,
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
	}, nil
}

func (p LanePolicy) ttl(requested time.Duration) (time.Duration, error) {
	if requested == 0 {
		return p.MaxTTL, nil
	}
	if requested > p.MaxTTL {
		return 0, domain.Invalid("ttl_seconds", fmt.Sprintf("must not exceed %.0f", p.MaxTTL.Seconds()))
	}
	return requested, nil
}

func requireClientID(id uuid.UUID) error {
	if id == uuid.Nil {
		return domain.Invalid("client_id", "must not be the nil UUID")
	}
	return nil
}

func pageSize(requested int) int {
	if requested <= 0 {
		return defaultPageSize
	}
	return min(requested, maxPageSize)
}
