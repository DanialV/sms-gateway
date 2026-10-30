package httpapi

import (
	"fmt"
	"runtime/debug"
	"time"

	"github.com/gofiber/fiber/v3"
)

type RequestObserver interface {
	ObserveRequest(method, route string, status int, elapsed time.Duration)
}

func (h *handler) observe(c fiber.Ctx) error {
	start := time.Now()
	if err := c.Next(); err != nil {
		if handleErr := h.handleError(c, err); handleErr != nil {
			return handleErr
		}
	}
	h.requests.ObserveRequest(c.Method(), c.Route().Path, c.Response().StatusCode(), time.Since(start))
	return nil
}

func recoverPanics(c fiber.Ctx) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return c.Next()
}
