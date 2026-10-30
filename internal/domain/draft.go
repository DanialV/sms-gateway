package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	gsmPageLimit     = 160
	unicodePageLimit = 70
)

var phonePattern = regexp.MustCompile(`^\+?[0-9]{8,15}$`)

type Draft struct {
	CustomerID uint32
	Phone      string
	Body       string
	TTL        time.Duration
}

func (d Draft) Validate() error {
	if err := ValidateCustomer(d.CustomerID); err != nil {
		return err
	}
	if !phonePattern.MatchString(d.Phone) {
		return Invalid("phone", "must be 8 to 15 digits with an optional leading +")
	}
	if strings.TrimSpace(d.Body) == "" {
		return Invalid("text", "must not be empty")
	}
	if limit := PageLimit(d.Body); utf8.RuneCountInString(d.Body) > limit {
		return Invalid("text", fmt.Sprintf("must fit in one SMS page of %d characters", limit))
	}
	if d.TTL <= 0 {
		return Invalid("ttl_seconds", "must be positive")
	}
	return nil
}

func PageLimit(body string) int {
	for _, r := range body {
		if r > unicode.MaxASCII {
			return unicodePageLimit
		}
	}
	return gsmPageLimit
}

func ValidateCustomer(id uint32) error {
	if id == 0 {
		return Invalid("customer_id", "must be a positive number")
	}
	return nil
}
