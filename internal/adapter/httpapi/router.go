package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"sms-gateway/internal/app/receiver"
	"sms-gateway/internal/domain"
)

const (
	maxBodyBytes    = 16 << 10
	readTimeout     = 5 * time.Second
	shutdownTimeout = 10 * time.Second
)

type Service interface {
	CreateCustomer(ctx context.Context, name string) (domain.Customer, error)
	Customer(ctx context.Context, id uint32) (domain.Customer, error)
	Send(ctx context.Context, cmd receiver.SendCommand) (domain.Message, error)
	TopUp(ctx context.Context, customerID uint32, amount int64) (domain.Wallet, error)
	Balance(ctx context.Context, customerID uint32) (domain.Wallet, error)
	Message(ctx context.Context, customerID uint32, id uint64) (domain.Message, error)
	MessageByClientID(ctx context.Context, customerID uint32, clientID uuid.UUID) (domain.Message, error)
	Messages(ctx context.Context, query receiver.MessageQuery) ([]domain.Message, uint64, error)
}

type Server struct {
	app *fiber.App
}

type handler struct {
	svc      Service
	ids      *IDCodec
	requests RequestObserver
	log      *slog.Logger
}

func NewServer(svc Service, ids *IDCodec, requests RequestObserver, log *slog.Logger) *Server {
	h := &handler{svc: svc, ids: ids, requests: requests, log: log}
	app := fiber.New(fiber.Config{
		Immutable:    true,
		BodyLimit:    maxBodyBytes,
		ReadTimeout:  readTimeout,
		ErrorHandler: h.handleError,
	})
	app.Use(h.observe, recoverPanics)

	app.Post("/v1/customers", h.createCustomer)
	app.Get("/v1/customers/:customerID", h.getCustomer)
	customers := app.Group("/v1/customers/:customerID")
	customers.Post("/messages", h.send)
	customers.Get("/messages", h.listMessages)
	customers.Get("/messages/:messageID", h.getMessage)
	customers.Get("/messages/by-client-id/:clientID", h.getMessageByClientID)
	customers.Post("/topups", h.topUp)
	customers.Get("/balance", h.balance)
	app.Get("/healthz", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })

	return &Server{app: app}
}

func (s *Server) Run(ctx context.Context, addr string) error {
	return s.app.Listen(addr, fiber.ListenConfig{
		GracefulContext:       ctx,
		ShutdownTimeout:       shutdownTimeout,
		DisableStartupMessage: true,
	})
}

func (h *handler) handleError(c fiber.Ctx, err error) error {
	status, body := h.problem(c, err)
	return c.Status(status).JSON(body)
}

func (h *handler) problem(c fiber.Ctx, err error) (int, errorResponse) {
	var invalid *domain.ValidationError
	var fiberErr *fiber.Error
	switch {
	case errors.As(err, &invalid):
		return fiber.StatusBadRequest, errorResponse{Error: invalid.Reason, Field: invalid.Field}
	case errors.Is(err, domain.ErrInsufficientBalance):
		return fiber.StatusPaymentRequired, errorResponse{Error: err.Error()}
	case errors.Is(err, domain.ErrNotFound):
		return fiber.StatusNotFound, errorResponse{Error: err.Error()}
	case errors.As(err, &fiberErr):
		return fiberErr.Code, errorResponse{Error: fiberErr.Message}
	}
	h.log.ErrorContext(c.Context(), "request failed", "method", c.Method(), "path", c.Path(), "err", err)
	return fiber.StatusInternalServerError, errorResponse{Error: "internal error"}
}
