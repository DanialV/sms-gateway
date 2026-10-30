package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"

	"sms-gateway/internal/app/receiver"
	"sms-gateway/internal/domain"
)

var errClientIDExists = errors.New("client id exists")

type ReceiverStore struct {
	db *sql.DB
}

func NewReceiverStore(db *sql.DB) *ReceiverStore {
	return &ReceiverStore{db: db}
}

func (s *ReceiverStore) CreateCustomer(ctx context.Context, customer domain.Customer) (domain.Customer, error) {
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO customers (name, created_at) VALUES (?, ?)`, customer.Name, customer.CreatedAt)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		customer.ID = uint32(id)
		_, err = tx.ExecContext(ctx, `INSERT INTO wallets (customer_id) VALUES (?)`, customer.ID)
		return err
	})
	if err != nil {
		return domain.Customer{}, err
	}
	return customer, nil
}

func (s *ReceiverStore) Customer(ctx context.Context, id uint32) (domain.Customer, error) {
	customer := domain.Customer{ID: id}
	err := s.db.QueryRowContext(ctx, `SELECT name, created_at FROM customers WHERE id = ?`, id).
		Scan(&customer.Name, &customer.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Customer{}, domain.ErrNotFound
	}
	return customer, err
}

// Insert before the hold so a retried client_id hits the unique key and never touches the wallet.
func (s *ReceiverStore) Accept(ctx context.Context, msg domain.Message) (domain.Message, error) {
	err := withTx(ctx, s.db, func(tx *sql.Tx) (err error) {
		if msg.ID, err = insertMessage(ctx, tx, msg); err != nil {
			return err
		}
		return hold(ctx, tx, msg.CustomerID, msg.Price)
	})
	if errors.Is(err, errClientIDExists) {
		return s.MessageByClientID(ctx, msg.CustomerID, msg.ClientID)
	}
	if err != nil {
		return domain.Message{}, err
	}
	return msg, nil
}

func (s *ReceiverStore) TopUp(ctx context.Context, customerID uint32, amount int64) (domain.Wallet, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE wallets SET balance = balance + ? WHERE customer_id = ?`, amount, customerID)
	if err != nil {
		return domain.Wallet{}, err
	}
	if err := requireRow(res, domain.ErrNotFound); err != nil {
		return domain.Wallet{}, err
	}
	return s.Wallet(ctx, customerID)
}

func (s *ReceiverStore) Wallet(ctx context.Context, customerID uint32) (domain.Wallet, error) {
	wallet := domain.Wallet{CustomerID: customerID}
	err := s.db.QueryRowContext(ctx, `SELECT balance, blocked FROM wallets WHERE customer_id = ?`, customerID).
		Scan(&wallet.Balance, &wallet.Blocked)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Wallet{}, domain.ErrNotFound
	}
	return wallet, err
}

func (s *ReceiverStore) Message(ctx context.Context, customerID uint32, id uint64) (domain.Message, error) {
	return oneMessage(s.db.QueryRowContext(ctx,
		`SELECT `+messageColumns+` FROM messages WHERE id = ? AND customer_id = ?`, id, customerID))
}

func (s *ReceiverStore) MessageByClientID(ctx context.Context, customerID uint32, clientID uuid.UUID) (domain.Message, error) {
	return oneMessage(s.db.QueryRowContext(ctx,
		`SELECT `+messageColumns+` FROM messages WHERE customer_id = ? AND client_id = ?`, customerID, clientID[:]))
}

func (s *ReceiverStore) Messages(ctx context.Context, query receiver.MessageQuery) ([]domain.Message, error) {
	where, args := reportFilter(query)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+messageColumns+` FROM messages WHERE `+where+` ORDER BY id DESC LIMIT ?`,
		append(args, query.Limit)...)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

// One conditional update: the row lock serializes concurrent sends, so the balance can't go below zero.
func hold(ctx context.Context, tx *sql.Tx, customerID uint32, amount int64) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE wallets SET blocked = blocked + ? WHERE customer_id = ? AND balance - blocked >= ?`,
		amount, customerID, amount)
	if err != nil {
		return err
	}
	if held, err := applied(res, nil); err != nil || held {
		return err
	}
	return whyNoHold(ctx, tx, customerID)
}

func whyNoHold(ctx context.Context, tx *sql.Tx, customerID uint32) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM customers WHERE id = ?)`, customerID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domain.ErrNotFound
	}
	return domain.ErrInsufficientBalance
}

func insertMessage(ctx context.Context, tx *sql.Tx, msg domain.Message) (uint64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO messages (customer_id, client_id, lane, phone, body, price, status,
			attempt, available_at, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		msg.CustomerID, msg.ClientID[:], msg.Lane, msg.Phone, msg.Body, msg.Price, msg.Status,
		msg.CreatedAt, msg.ExpiresAt, msg.CreatedAt, msg.CreatedAt)
	if hasCode(err, codeDuplicateEntry) {
		return 0, errClientIDExists
	}
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return uint64(id), err
}

func reportFilter(query receiver.MessageQuery) (string, []any) {
	conds := []string{"customer_id = ?"}
	args := []any{query.CustomerID}
	add := func(cond string, arg any) {
		conds = append(conds, cond)
		args = append(args, arg)
	}
	if query.Status != "" {
		add("status = ?", query.Status)
	}
	if !query.From.IsZero() {
		add("created_at >= ?", query.From)
	}
	if !query.To.IsZero() {
		add("created_at < ?", query.To)
	}
	if query.Cursor != 0 {
		add("id < ?", query.Cursor)
	}
	return strings.Join(conds, " AND "), args
}
