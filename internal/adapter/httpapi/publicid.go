package httpapi

import (
	"github.com/sqids/sqids-go"

	"sms-gateway/internal/domain"
)

type IDCodec struct {
	sqids *sqids.Sqids
}

func NewIDCodec(alphabet string, minLength uint8) (*IDCodec, error) {
	s, err := sqids.New(sqids.Options{Alphabet: alphabet, MinLength: minLength})
	if err != nil {
		return nil, err
	}
	return &IDCodec{sqids: s}, nil
}

func (c *IDCodec) Encode(id uint64) string {
	encoded, _ := c.sqids.Encode([]uint64{id})
	return encoded
}

func (c *IDCodec) Decode(field, public string) (uint64, error) {
	ids := c.sqids.Decode(public)
	if len(ids) != 1 || c.Encode(ids[0]) != public {
		return 0, domain.Invalid(field, "is not a valid id")
	}
	return ids[0], nil
}
