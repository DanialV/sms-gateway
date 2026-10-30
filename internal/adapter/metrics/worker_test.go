package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"

	"sms-gateway/internal/app/worker"
	"sms-gateway/internal/domain"
)

type fakeQueue struct {
	worker.Queue
	stale []uint64
	err   error
}

func (f fakeQueue) Complete(context.Context, []worker.Settlement) ([]uint64, error) {
	return f.stale, f.err
}

func TestQueueRecordsOutcomes(t *testing.T) {
	created := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	msg := domain.Message{ID: 7, Lane: domain.LaneExpress, CreatedAt: created}
	cases := []struct {
		status domain.Status
		queue  fakeQueue
		label  string
	}{
		{status: domain.StatusSent, label: "sent"},
		{status: domain.StatusFailed, label: "failed"},
		{status: domain.StatusExpired, label: "expired"},
		{status: domain.StatusPending, label: "retry"},
		{status: domain.StatusSent, queue: fakeQueue{stale: []uint64{7}}, label: "stale"},
		{status: domain.StatusSent, queue: fakeQueue{err: errors.New("mysql is down")}, label: "error"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			q := InstrumentQueue(tc.queue, prometheus.NewRegistry()).(*queue)
			outcome := worker.Outcome{Status: tc.status, At: created.Add(300 * time.Millisecond)}
			_, _ = q.Complete(context.Background(), []worker.Settlement{{Message: msg, Outcome: outcome}})

			if got := testutil.ToFloat64(q.outcomes.WithLabelValues("express", tc.label)); got != 1 {
				t.Fatalf("outcome %q counted %v times, want 1", tc.label, got)
			}
			var wantLatency uint64
			if tc.label == "sent" {
				wantLatency = 1
			}
			if got := observations(t, q.latency, "express"); got != wantLatency {
				t.Fatalf("latency observations = %d, want %d", got, wantLatency)
			}
		})
	}
}

func TestQueuePresetsSeriesBeforeTraffic(t *testing.T) {
	q := InstrumentQueue(fakeQueue{}, prometheus.NewRegistry()).(*queue)
	want := len(domain.Lanes) * len(outcomeLabels)
	if got := testutil.CollectAndCount(q.outcomes); got != want {
		t.Fatalf("outcome series = %d, want %d", got, want)
	}
}

func observations(t *testing.T, vec *prometheus.HistogramVec, lane string) uint64 {
	t.Helper()
	var m dto.Metric
	if err := vec.WithLabelValues(lane).(prometheus.Metric).Write(&m); err != nil {
		t.Fatalf("read histogram: %v", err)
	}
	return m.GetHistogram().GetSampleCount()
}
