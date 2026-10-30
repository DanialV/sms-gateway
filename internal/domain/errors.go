package domain

import "errors"

var (
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrNotFound            = errors.New("not found")
	ErrRejected            = errors.New("operator rejected the message")
)

type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return e.Field + " " + e.Reason
}

func Invalid(field, reason string) error {
	return &ValidationError{Field: field, Reason: reason}
}
