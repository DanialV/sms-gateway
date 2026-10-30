package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"sms-gateway/internal/app/worker"
	"sms-gateway/internal/domain"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not set")
	}
	db, err := Open(context.Background(), dsn, 100)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

const unknownCustomer = math.MaxUint32

func fundedCustomer(t *testing.T, store *ReceiverStore, balance int64) uint32 {
	t.Helper()
	id := newCustomer(t, store)
	if _, err := store.TopUp(context.Background(), id, balance); err != nil {
		t.Fatalf("top up: %v", err)
	}
	return id
}

func newCustomer(t *testing.T, store *ReceiverStore) uint32 {
	t.Helper()
	customer, err := store.CreateCustomer(context.Background(),
		domain.Customer{Name: "test", CreatedAt: time.Now().UTC().Truncate(time.Millisecond)})
	if err != nil || customer.ID == 0 {
		t.Fatalf("create customer: %+v, %v", customer, err)
	}
	return customer.ID
}

func TestCreateCustomerStartsWithAnEmptyWallet(t *testing.T) {
	store := NewReceiverStore(openTestDB(t))
	ctx := context.Background()
	first, second := newCustomer(t, store), newCustomer(t, store)
	if second <= first {
		t.Fatalf("ids must be generated in order, got %d then %d", first, second)
	}
	if wallet, err := store.Wallet(ctx, second); err != nil || wallet.Balance != 0 || wallet.Blocked != 0 {
		t.Fatalf("new wallet = %+v, %v; want an empty wallet", wallet, err)
	}
	if customer, err := store.Customer(ctx, second); err != nil || customer.Name != "test" {
		t.Fatalf("customer = %+v, %v", customer, err)
	}
}

func TestUnknownOrBrokeCustomersCannotSend(t *testing.T) {
	store := NewReceiverStore(openTestDB(t))
	ctx := context.Background()
	broke := newCustomer(t, store)

	cases := map[string]struct {
		customer uint32
		want     error
	}{
		"unknown":   {customer: unknownCustomer, want: domain.ErrNotFound},
		"no credit": {customer: broke, want: domain.ErrInsufficientBalance},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Accept(ctx, newMessage(tc.customer)); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := store.TopUp(ctx, unknownCustomer, 5); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("top-up for an unknown customer: got %v, want not found", err)
	}
	if _, err := store.Customer(ctx, unknownCustomer); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown customer lookup: got %v, want not found", err)
	}
}

func newMessage(customerID uint32) domain.Message {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return domain.Message{
		CustomerID: customerID, ClientID: uuid.New(), Lane: domain.LaneNormal,
		Phone: "+989121234567", Body: "hello", Price: 1, Status: domain.StatusPending,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
}

func TestAcceptNeverOverdraws(t *testing.T) {
	store := NewReceiverStore(openTestDB(t))
	customer := fundedCustomer(t, store, 100)

	var accepted, rejected atomic.Int64
	var wg sync.WaitGroup
	for range 300 {
		wg.Go(func() {
			_, err := store.Accept(context.Background(), newMessage(customer))
			switch {
			case err == nil:
				accepted.Add(1)
			case errors.Is(err, domain.ErrInsufficientBalance):
				rejected.Add(1)
			default:
				t.Errorf("accept: %v", err)
			}
		})
	}
	wg.Wait()

	wallet, _ := store.Wallet(context.Background(), customer)
	if accepted.Load() != 100 || rejected.Load() != 200 || wallet.Blocked != 100 || wallet.Available() != 0 {
		t.Fatalf("accepted=%d rejected=%d wallet=%+v", accepted.Load(), rejected.Load(), wallet)
	}
}

func TestAcceptReplaysTheSameClientID(t *testing.T) {
	store := NewReceiverStore(openTestDB(t))
	customer := fundedCustomer(t, store, 1)
	ctx := context.Background()

	original := newMessage(customer)
	first, err := store.Accept(ctx, original)
	if err != nil {
		t.Fatalf("first accept: %v", err)
	}
	different := original
	different.Body = "a different body"
	for _, retry := range []domain.Message{original, different} {
		replayed, err := store.Accept(ctx, retry)
		if err != nil || replayed.ID != first.ID || replayed.Body != first.Body {
			t.Fatalf("replay returned %+v, %v; want the original message %d", replayed, err, first.ID)
		}
	}
	if wallet, _ := store.Wallet(ctx, customer); wallet.Blocked != 1 || wallet.Available() != 0 {
		t.Fatalf("replays must not hold again, wallet=%+v", wallet)
	}
}

func TestMessageByClientID(t *testing.T) {
	store := NewReceiverStore(openTestDB(t))
	customer := fundedCustomer(t, store, 10)
	ctx := context.Background()

	sent, err := store.Accept(ctx, newMessage(customer))
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	found, err := store.MessageByClientID(ctx, customer, sent.ClientID)
	if err != nil || found.ID != sent.ID || found.ClientID != sent.ClientID {
		t.Fatalf("lookup returned %+v, %v; want message %d", found, err, sent.ID)
	}
	if _, err := store.MessageByClientID(ctx, customer+1, sent.ClientID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another customer must not see the message, got %v", err)
	}
	if _, err := store.MessageByClientID(ctx, customer, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown client id must be not found, got %v", err)
	}
}

func TestCompleteSettlesHoldOnce(t *testing.T) {
	cases := map[domain.Status]int64{domain.StatusSent: 9, domain.StatusFailed: 10, domain.StatusExpired: 10}
	for status, wantBalance := range cases {
		t.Run(string(status), func(t *testing.T) {
			db := openTestDB(t)
			receivers, workers := NewReceiverStore(db), NewWorkerStore(db)
			customer := fundedCustomer(t, receivers, 10)
			ctx := context.Background()
			if _, err := receivers.Accept(ctx, newMessage(customer)); err != nil {
				t.Fatalf("accept: %v", err)
			}

			batch := settle(claim(t, workers, customer, 1), status)
			if stale, err := workers.Complete(ctx, batch); err != nil || len(stale) != 0 {
				t.Fatalf("complete: stale=%v err=%v", stale, err)
			}
			if stale, err := workers.Complete(ctx, batch); err != nil || len(stale) != 1 {
				t.Fatalf("second completion must be fenced, stale=%v err=%v", stale, err)
			}

			wallet, _ := receivers.Wallet(ctx, customer)
			if wallet.Balance != wantBalance || wallet.Blocked != 0 {
				t.Fatalf("wallet=%+v, want balance %d and nothing blocked", wallet, wantBalance)
			}
		})
	}
}

func TestCompleteSettlesMixedBatchWithOneWalletUpdate(t *testing.T) {
	db := openTestDB(t)
	receivers, workers := NewReceiverStore(db), NewWorkerStore(db)
	customer := fundedCustomer(t, receivers, 10)
	ctx := context.Background()
	for range 4 {
		if _, err := receivers.Accept(ctx, newMessage(customer)); err != nil {
			t.Fatalf("accept: %v", err)
		}
	}

	batch := settle(claim(t, workers, customer, 4),
		domain.StatusSent, domain.StatusSent, domain.StatusFailed, domain.StatusPending)
	if stale, err := workers.Complete(ctx, batch); err != nil || len(stale) != 0 {
		t.Fatalf("complete: stale=%v err=%v", stale, err)
	}
	wallet, _ := receivers.Wallet(ctx, customer)
	if wallet.Balance != 8 || wallet.Blocked != 1 {
		t.Fatalf("wallet=%+v, want balance 8 (2 sent) and 1 still blocked (the retry)", wallet)
	}

	if stale, err := workers.Complete(ctx, batch); err != nil || len(stale) != 4 {
		t.Fatalf("replaying the batch must be fenced for every message, stale=%v err=%v", stale, err)
	}
	if again, _ := receivers.Wallet(ctx, customer); again != wallet {
		t.Fatalf("a fenced replay changed the wallet: %+v -> %+v", wallet, again)
	}
}

func TestNextDueAndPendingCustomersFollowTheRetry(t *testing.T) {
	db := openTestDB(t)
	receivers, workers := NewReceiverStore(db), NewWorkerStore(db)
	customer := fundedCustomer(t, receivers, 10)
	ctx := context.Background()
	if _, ok, err := workers.NextDue(ctx, domain.LaneNormal, customer); err != nil || ok {
		t.Fatalf("a customer without messages has nothing due, got ok=%v err=%v", ok, err)
	}
	if _, err := receivers.Accept(ctx, newMessage(customer)); err != nil {
		t.Fatalf("accept: %v", err)
	}

	batch := settle(claim(t, workers, customer, 1), domain.StatusPending)
	if _, err := workers.Complete(ctx, batch); err != nil {
		t.Fatalf("complete: %v", err)
	}
	due, ok, err := workers.NextDue(ctx, domain.LaneNormal, customer)
	if err != nil || !ok || !due.Equal(batch[0].Outcome.RetryAt) {
		t.Fatalf("next due = %v, %v, %v; want the retry time %v", due, ok, err, batch[0].Outcome.RetryAt)
	}
	if _, ok, _ := workers.NextDue(ctx, domain.LaneExpress, customer); ok {
		t.Fatal("the express lane has nothing due for this customer")
	}

	normal, err := workers.PendingCustomers(ctx, domain.LaneNormal)
	if err != nil || !slices.Contains(normal, customer) {
		t.Fatalf("normal lane must list the customer, got err=%v", err)
	}
	if express, _ := workers.PendingCustomers(ctx, domain.LaneExpress); slices.Contains(express, customer) {
		t.Fatal("express lane must not list a customer with only normal messages")
	}
}

func settle(batch []domain.Message, statuses ...domain.Status) []worker.Settlement {
	now := time.Now().UTC().Truncate(time.Millisecond)
	settlements := make([]worker.Settlement, len(batch))
	for i, msg := range batch {
		status := statuses[min(i, len(statuses)-1)]
		settlements[i] = worker.Settlement{Message: msg, Outcome: worker.Outcome{Status: status, At: now, RetryAt: now.Add(time.Minute)}}
	}
	return settlements
}

func claim(t *testing.T, store *WorkerStore, customer uint32, want int) []domain.Message {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	batch, err := store.Claim(context.Background(), worker.ClaimRequest{
		Lane: domain.LaneNormal, CustomerID: customer, Limit: 10, Now: now, LeaseUntil: now.Add(time.Minute),
	})
	if err != nil || len(batch) != want || batch[0].Attempt != 1 {
		t.Fatalf("claim: %v, %+v", err, batch)
	}
	return batch
}
