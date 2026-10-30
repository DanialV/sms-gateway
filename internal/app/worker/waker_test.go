package worker

import (
	"context"
	"slices"
	"testing"
	"time"

	"sms-gateway/internal/domain"
)

func TestWakeChecksEveryLane(t *testing.T) {
	scheduler := newFakeScheduler()
	if err := NewWaker(scheduler, time.Second, discard).Wake(context.Background()); err != nil {
		t.Fatalf("wake: %v", err)
	}
	if !slices.Equal(scheduler.woken, domain.Lanes) {
		t.Fatalf("woke %v, want every lane %v", scheduler.woken, domain.Lanes)
	}
}
