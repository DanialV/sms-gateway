package domain

import (
	"strings"
	"testing"
)

func TestValidateCustomerName(t *testing.T) {
	cases := map[string]struct {
		name  string
		field string
	}{
		"valid":         {name: "Acme Shop"},
		"blank":         {name: "  ", field: "name"},
		"name too long": {name: strings.Repeat("a", 101), field: "name"},
		"persian fits":  {name: strings.Repeat("س", 100)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assertField(t, ValidateCustomerName(tc.name), tc.field)
		})
	}
}
