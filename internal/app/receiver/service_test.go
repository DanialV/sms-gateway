package receiver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"sms-gateway/internal/domain"
)

var now = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

type fakeStore struct {
	Store
	accepted []domain.Message
	err      error
}

func (f *fakeStore) Accept(_ context.Context, msg domain.Message) (domain.Message, error) {
	if f.err != nil {
		return domain.Message{}, f.err
	}
	msg.ID = uint64(len(f.accepted) + 1)
	f.accepted = append(f.accepted, msg)
	return msg, nil
}

type fakeScheduler struct {
	added []uint32
	err   error
}

func (f *fakeScheduler) Add(_ context.Context, _ domain.Lane, ids ...uint32) error {
	f.added = append(f.added, ids...)
	return f.err
}

func newTestService(store *fakeStore, scheduler *fakeScheduler) *Service {
	svc := NewService(store, scheduler, map[domain.Lane]LanePolicy{
		domain.LaneNormal:  {Price: 1, MaxTTL: time.Hour},
		domain.LaneExpress: {Price: 3, MaxTTL: 2 * time.Minute},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.now = func() time.Time { return now }
	return svc
}

func validCommand() SendCommand {
	return SendCommand{
		CustomerID: 7, ClientID: uuid.MustParse("0b6f2c4e-8d1a-4f3b-9c2e-5a7d1e9f0b34"),
		Phone: "+989121234567", Text: "code 1234", Express: true,
	}
}

func TestSendAcceptsAndSchedules(t *testing.T) {
	store, scheduler := &fakeStore{}, &fakeScheduler{}
	msg, err := newTestService(store, scheduler).Send(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if msg.Lane != domain.LaneExpress || msg.Price != 3 || !msg.ExpiresAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("express policy not applied: %+v", msg)
	}
	if len(scheduler.added) != 1 || scheduler.added[0] != 7 {
		t.Fatalf("customer not scheduled: %v", scheduler.added)
	}
}

func TestSendSucceedsWhenSchedulingFails(t *testing.T) {
	store, scheduler := &fakeStore{}, &fakeScheduler{err: errors.New("redis down")}
	if _, err := newTestService(store, scheduler).Send(context.Background(), validCommand()); err != nil {
		t.Fatalf("send must not fail after the message is stored: %v", err)
	}
	if len(store.accepted) != 1 {
		t.Fatal("message was not stored")
	}
}

func TestSendRejectsInvalidInputBeforeStoring(t *testing.T) {
	cases := map[string]func(*SendCommand){
		"nil client id": func(c *SendCommand) { c.ClientID = uuid.Nil },
		"ttl too long":  func(c *SendCommand) { c.TTL = time.Hour },
		"bad phone":     func(c *SendCommand) { c.Phone = "abc" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{}
			cmd := validCommand()
			mutate(&cmd)
			_, err := newTestService(store, &fakeScheduler{}).Send(context.Background(), cmd)
			var invalid *domain.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected validation error, got %v", err)
			}
			if len(store.accepted) != 0 {
				t.Fatal("invalid message reached the store")
			}
		})
	}
}

func TestSendPassesStoreErrorsThrough(t *testing.T) {
	store := &fakeStore{err: domain.ErrInsufficientBalance}
	scheduler := &fakeScheduler{}
	_, err := newTestService(store, scheduler).Send(context.Background(), validCommand())
	if !errors.Is(err, domain.ErrInsufficientBalance) {
		t.Fatalf("expected insufficient balance, got %v", err)
	}
	if len(scheduler.added) != 0 {
		t.Fatal("rejected message must not be scheduled")
	}
}
