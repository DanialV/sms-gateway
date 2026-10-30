package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const maxCustomerName = 100

type Customer struct {
	ID        uint32
	Name      string
	CreatedAt time.Time
}

func ValidateCustomerName(name string) error {
	if strings.TrimSpace(name) == "" {
		return Invalid("name", "must not be empty")
	}
	if utf8.RuneCountInString(name) > maxCustomerName {
		return Invalid("name", fmt.Sprintf("must not exceed %d characters", maxCustomerName))
	}
	return nil
}
