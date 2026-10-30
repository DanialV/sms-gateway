package env

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Reader struct {
	errs []error
}

func (r *Reader) String(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func (r *Reader) Int(key string, fallback int) int {
	return lookup(r, key, fallback, strconv.Atoi)
}

func (r *Reader) Int64(key string, fallback int64) int64 {
	return lookup(r, key, fallback, func(s string) (int64, error) {
		return strconv.ParseInt(s, 10, 64)
	})
}

func (r *Reader) Float(key string, fallback float64) float64 {
	return lookup(r, key, fallback, func(s string) (float64, error) {
		return strconv.ParseFloat(s, 64)
	})
}

func (r *Reader) Duration(key string, fallback time.Duration) time.Duration {
	return lookup(r, key, fallback, time.ParseDuration)
}

func (r *Reader) Err() error {
	return errors.Join(r.errs...)
}

func lookup[T any](r *Reader, key string, fallback T, parse func(string) (T, error)) T {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	value, err := parse(raw)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s: %w", key, err))
		return fallback
	}
	return value
}
