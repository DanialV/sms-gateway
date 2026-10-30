package mysql

import (
	"database/sql"
	"errors"

	"sms-gateway/internal/domain"
)

const messageColumns = `id, customer_id, client_id, lane, phone, body, price, status, attempt,
	created_at, expires_at, sent_at, operator_ref, last_error`

type scanner interface {
	Scan(dest ...any) error
}

func scanMessage(row scanner) (domain.Message, error) {
	var (
		msg         domain.Message
		sentAt      sql.NullTime
		operatorRef sql.NullString
		lastError   sql.NullString
	)
	err := row.Scan(&msg.ID, &msg.CustomerID, &msg.ClientID, &msg.Lane, &msg.Phone, &msg.Body, &msg.Price, &msg.Status,
		&msg.Attempt, &msg.CreatedAt, &msg.ExpiresAt, &sentAt, &operatorRef, &lastError)
	if sentAt.Valid {
		msg.SentAt = &sentAt.Time
	}
	msg.OperatorRef, msg.LastError = operatorRef.String, lastError.String
	return msg, err
}

func oneMessage(row scanner) (domain.Message, error) {
	msg, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Message{}, domain.ErrNotFound
	}
	return msg, err
}

func scanMessages(rows *sql.Rows) ([]domain.Message, error) {
	defer rows.Close()
	var msgs []domain.Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, msg)
	}
	return msgs, rows.Err()
}
