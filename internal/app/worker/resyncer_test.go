package worker

import (
	"context"
	"slices"
	"testing"
	"time"

	"sms-gateway/internal/domain"
)

type fakePending map[domain.Lane][]uint32

func (f fakePending) PendingCustomers(_ context.Context, lane domain.Lane) ([]uint32, error) {
	return f[lane], nil
}

func TestResyncAddsCustomersOnlyToTheirLane(t *testing.T) {
	scheduler := newFakeScheduler()
	pending := fakePending{domain.LaneExpress: {7}, domain.LaneNormal: {8, 9}}
	if err := NewResyncer(pending, scheduler, time.Minute, discard).Resync(context.Background()); err != nil {
		t.Fatalf("resync: %v", err)
	}
	if !slices.Equal(scheduler.added[domain.LaneExpress], []uint32{7}) || !slices.Equal(scheduler.added[domain.LaneNormal], []uint32{8, 9}) {
		t.Fatalf("added %v, want each customer only in its own lane", scheduler.added)
	}
}
