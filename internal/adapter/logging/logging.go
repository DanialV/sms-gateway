package logging

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/getsentry/sentry-go"
)

const flushTimeout = 2 * time.Second

type Config struct {
	Service     string
	SentryDSN   string
	Environment string
}

func New(cfg Config) (*slog.Logger, func(), error) {
	var handler slog.Handler = slog.NewJSONHandler(os.Stdout, nil)
	flush := func() {}
	if cfg.SentryDSN != "" {
		err := sentry.Init(sentry.ClientOptions{Dsn: cfg.SentryDSN, Environment: cfg.Environment, ServerName: cfg.Service})
		if err != nil {
			return nil, nil, err
		}
		handler = sentryHandler{Handler: handler}
		flush = func() { sentry.Flush(flushTimeout) }
	}
	return slog.New(handler).With("service", cfg.Service), flush, nil
}

type sentryHandler struct {
	slog.Handler
	attrs []slog.Attr
}

func (h sentryHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level >= slog.LevelError {
		capture(record, h.attrs)
	}
	return h.Handler.Handle(ctx, record)
}

func (h sentryHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return sentryHandler{Handler: h.Handler.WithAttrs(attrs), attrs: append(slices.Clip(h.attrs), attrs...)}
}

func (h sentryHandler) WithGroup(name string) slog.Handler {
	return sentryHandler{Handler: h.Handler.WithGroup(name), attrs: h.attrs}
}

func capture(record slog.Record, attrs []slog.Attr) {
	fields := sentry.Context{}
	add := func(attr slog.Attr) bool {
		fields[attr.Key] = attr.Value.String()
		return true
	}
	for _, attr := range attrs {
		add(attr)
	}
	record.Attrs(add)

	event := sentry.NewEvent()
	event.Level = sentry.LevelError
	event.Logger = "slog"
	event.Message = record.Message
	event.Contexts["log"] = fields
	sentry.CaptureEvent(event)
}
