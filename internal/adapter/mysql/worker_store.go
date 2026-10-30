package mysql

import (
	"context"
	"database/sql"
	"maps"
	"slices"
	"time"

	"sms-gateway/internal/app/worker"
	"sms-gateway/internal/domain"
)

type WorkerStore struct {
	db *sql.DB
}

func NewWorkerStore(db *sql.DB) *WorkerStore {
	return &WorkerStore{db: db}
}

func (s *WorkerStore) Claim(ctx context.Context, req worker.ClaimRequest) ([]domain.Message, error) {
	var claimed []domain.Message
	err := withTx(ctx, s.db, func(tx *sql.Tx) (err error) {
		claimed, err = selectClaimable(ctx, tx, req)
		if err != nil || len(claimed) == 0 {
			return err
		}
		return markSending(ctx, tx, claimed, req)
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (s *WorkerStore) NextDue(ctx context.Context, lane domain.Lane, customerID uint32) (time.Time, bool, error) {
	var due sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT MIN(available_at) FROM messages WHERE customer_id = ? AND lane = ? AND available_at IS NOT NULL`,
		customerID, lane).Scan(&due)
	return due.Time, due.Valid, err
}

func (s *WorkerStore) Complete(ctx context.Context, batch []worker.Settlement) ([]uint64, error) {
	var stale []uint64
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		stale = nil
		holds := map[uint32]holdChange{}
		for _, item := range batch {
			applied, err := record(ctx, tx, item)
			if err != nil {
				return err
			}
			if !applied {
				stale = append(stale, item.Message.ID)
				continue
			}
			addHoldChange(holds, item)
		}
		return settleHolds(ctx, tx, holds)
	})
	if err != nil {
		return nil, err
	}
	return stale, nil
}

func (s *WorkerStore) Backlog(ctx context.Context, lane domain.Lane) (int64, time.Time, error) {
	var count int64
	var oldest sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), MIN(available_at) FROM messages WHERE lane = ? AND available_at IS NOT NULL`, lane).
		Scan(&count, &oldest)
	return count, oldest.Time, err
}

func (s *WorkerStore) PendingCustomers(ctx context.Context, lane domain.Lane) ([]uint32, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT customer_id FROM messages WHERE lane = ? AND available_at IS NOT NULL`, lane)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uint32
	for rows.Next() {
		var id uint32
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func selectClaimable(ctx context.Context, tx *sql.Tx, req worker.ClaimRequest) ([]domain.Message, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT `+messageColumns+` FROM messages
		WHERE customer_id = ? AND lane = ? AND available_at <= ?
		ORDER BY available_at
		LIMIT ?
		FOR UPDATE SKIP LOCKED`,
		req.CustomerID, req.Lane, req.Now, req.Limit)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

// available_at doubles as the lease: nobody can claim these rows again until it passes.
func markSending(ctx context.Context, tx *sql.Tx, batch []domain.Message, req worker.ClaimRequest) error {
	args := []any{req.LeaseUntil, req.Now}
	for i := range batch {
		batch[i].Status = domain.StatusSending
		batch[i].Attempt++
		args = append(args, batch[i].ID)
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE messages SET status = 'SENDING', attempt = attempt + 1, available_at = ?, updated_at = ?
		WHERE id IN (`+placeholders(len(batch))+`)`, args...)
	return err
}

type holdChange struct {
	charged  int64
	released int64
}

// Both updates match only the attempt we claimed, so a late result from an expired lease is dropped.
func record(ctx context.Context, tx *sql.Tx, item worker.Settlement) (bool, error) {
	if item.Outcome.Status == domain.StatusPending {
		return reschedule(ctx, tx, item.Message, item.Outcome)
	}
	return finish(ctx, tx, item.Message, item.Outcome)
}

func reschedule(ctx context.Context, tx *sql.Tx, msg domain.Message, out worker.Outcome) (bool, error) {
	return applied(tx.ExecContext(ctx, `
		UPDATE messages SET status = 'PENDING', available_at = ?, last_error = ?, updated_at = ?
		WHERE id = ? AND attempt = ? AND status = 'SENDING'`,
		out.RetryAt, nullable(out.Reason), out.At, msg.ID, msg.Attempt))
}

func finish(ctx context.Context, tx *sql.Tx, msg domain.Message, out worker.Outcome) (bool, error) {
	var sentAt any
	if out.Status == domain.StatusSent {
		sentAt = out.At
	}
	return applied(tx.ExecContext(ctx, `
		UPDATE messages
		SET status = ?, available_at = NULL, operator_ref = ?, last_error = ?, sent_at = ?, updated_at = ?
		WHERE id = ? AND attempt = ? AND status = 'SENDING'`,
		out.Status, nullable(out.OperatorRef), nullable(out.Reason), sentAt, out.At, msg.ID, msg.Attempt))
}

func addHoldChange(holds map[uint32]holdChange, item worker.Settlement) {
	if item.Outcome.Status == domain.StatusPending {
		return
	}
	change := holds[item.Message.CustomerID]
	change.released += item.Message.Price
	if item.Outcome.Status == domain.StatusSent {
		change.charged += item.Message.Price
	}
	holds[item.Message.CustomerID] = change
}

// Wallets are updated in id order so two workers settling at once can't deadlock.
func settleHolds(ctx context.Context, tx *sql.Tx, holds map[uint32]holdChange) error {
	for _, customerID := range slices.Sorted(maps.Keys(holds)) {
		change := holds[customerID]
		_, err := tx.ExecContext(ctx,
			`UPDATE wallets SET balance = balance - ?, blocked = blocked - ? WHERE customer_id = ?`,
			change.charged, change.released, customerID)
		if err != nil {
			return err
		}
	}
	return nil
}

func applied(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected == 1, err
}
