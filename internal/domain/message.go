package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type Lane string

const (
	LaneNormal  Lane = "normal"
	LaneExpress Lane = "express"
)

var Lanes = []Lane{LaneExpress, LaneNormal}

func LaneFor(express bool) Lane {
	if express {
		return LaneExpress
	}
	return LaneNormal
}

type Status string

const (
	StatusPending Status = "PENDING"
	StatusSending Status = "SENDING"
	StatusSent    Status = "SENT"
	StatusFailed  Status = "FAILED"
	StatusExpired Status = "EXPIRED"
)

func ParseStatus(raw string) (Status, error) {
	switch status := Status(strings.ToUpper(raw)); status {
	case StatusPending, StatusSending, StatusSent, StatusFailed, StatusExpired:
		return status, nil
	}
	return "", Invalid("status", "must be one of PENDING, SENDING, SENT, FAILED, EXPIRED")
}

type Message struct {
	ID          uint64
	CustomerID  uint32
	ClientID    uuid.UUID
	Lane        Lane
	Phone       string
	Body        string
	Price       int64
	Status      Status
	Attempt     int
	CreatedAt   time.Time
	ExpiresAt   time.Time
	SentAt      *time.Time
	OperatorRef string
	LastError   string
}

func (m Message) IsExpired(now time.Time) bool {
	return !now.Before(m.ExpiresAt)
}
