package httpapi

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"sms-gateway/internal/app/receiver"
	"sms-gateway/internal/domain"
)

func pathCustomer(c fiber.Ctx) (uint32, error) {
	id, err := strconv.ParseUint(c.Params("customerID"), 10, 32)
	if err != nil {
		return 0, domain.Invalid("customer_id", "must be a whole number from 1 to 4294967295")
	}
	return uint32(id), nil
}

func uuidParam(field, raw string) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, domain.Invalid(field, "is required")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, domain.Invalid(field, "must be a UUID")
	}
	return id, nil
}

func decode(c fiber.Ctx, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return domain.Invalid("body", "must be valid JSON: "+err.Error())
	}
	return nil
}

func messageQuery(c fiber.Ctx, customerID uint32, ids *IDCodec) (receiver.MessageQuery, error) {
	query := receiver.MessageQuery{CustomerID: customerID}
	var err error
	if raw := c.Query("cursor"); raw != "" {
		if query.Cursor, err = ids.Decode("cursor", raw); err != nil {
			return query, err
		}
	}
	if query.Limit, err = intParam("limit", c.Query("limit")); err != nil {
		return query, err
	}
	if query.From, err = timeParam("from", c.Query("from")); err != nil {
		return query, err
	}
	if query.To, err = timeParam("to", c.Query("to")); err != nil {
		return query, err
	}
	if raw := c.Query("status"); raw != "" {
		query.Status, err = domain.ParseStatus(raw)
	}
	return query, err
}

func intParam(key, raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, domain.Invalid(key, "must be a number")
	}
	return n, nil
}

func timeParam(key, raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, domain.Invalid(key, "must be an RFC 3339 timestamp")
	}
	return t.UTC(), nil
}
