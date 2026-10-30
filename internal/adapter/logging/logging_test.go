package logging

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type collector struct {
	mu     sync.Mutex
	bodies []string
}

func (c *collector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.bodies = append(c.bodies, string(body))
	c.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (c *collector) received() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.bodies, "\n")
}

func TestOnlyErrorsReachSentry(t *testing.T) {
	sink := &collector{}
	server := httptest.NewServer(sink)
	defer server.Close()
	dsn := strings.Replace(server.URL, "http://", "http://public@", 1) + "/1"

	log, flush, err := New(Config{Service: "worker", SentryDSN: dsn, Environment: "test"})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	log.With("lane", "express").Warn("expected business outcome")
	log.With("lane", "express").Error("recording outcome failed", "err", errors.New("mysql is down"))
	flush()

	got := sink.received()
	for _, want := range []string{"recording outcome failed", "mysql is down", "express", "worker"} {
		if !strings.Contains(got, want) {
			t.Fatalf("sentry payload is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "expected business outcome") {
		t.Fatal("warnings must not be sent to sentry")
	}
}
