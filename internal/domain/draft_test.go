package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDraftValidate(t *testing.T) {
	valid := Draft{CustomerID: 1, Phone: "+989121234567", Body: "hello", TTL: time.Minute}
	cases := map[string]struct {
		mutate func(*Draft)
		field  string
	}{
		"valid":                {mutate: func(*Draft) {}},
		"missing customer":     {mutate: func(d *Draft) { d.CustomerID = 0 }, field: "customer_id"},
		"bad phone":            {mutate: func(d *Draft) { d.Phone = "09-12" }, field: "phone"},
		"empty text":           {mutate: func(d *Draft) { d.Body = "  " }, field: "text"},
		"latin fits 160":       {mutate: func(d *Draft) { d.Body = strings.Repeat("a", 160) }},
		"latin over 160":       {mutate: func(d *Draft) { d.Body = strings.Repeat("a", 161) }, field: "text"},
		"persian fits 70":      {mutate: func(d *Draft) { d.Body = strings.Repeat("س", 70) }},
		"persian over 70":      {mutate: func(d *Draft) { d.Body = strings.Repeat("س", 71) }, field: "text"},
		"non-positive ttl":     {mutate: func(d *Draft) { d.TTL = -time.Second }, field: "ttl_seconds"},
		"local iranian format": {mutate: func(d *Draft) { d.Phone = "09121234567" }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			draft := valid
			tc.mutate(&draft)
			assertField(t, draft.Validate(), tc.field)
		})
	}
}

func assertField(t *testing.T, err error, field string) {
	t.Helper()
	if field == "" {
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		return
	}
	var invalid *ValidationError
	if !errors.As(err, &invalid) || invalid.Field != field {
		t.Fatalf("expected validation error on %q, got %v", field, err)
	}
}
