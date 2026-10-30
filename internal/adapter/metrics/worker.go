package metrics

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"sms-gateway/internal/app/worker"
	"sms-gateway/internal/domain"
)

var (
	outcomeLabels = []string{"sent", "failed", "expired", "retry", "stale", "error"}
	resultLabels  = []string{"accepted", "rejected", "timeout", "error"}
)

type queue struct {
	worker.Queue
	batch    *prometheus.HistogramVec
	outcomes *prometheus.CounterVec
	latency  *prometheus.HistogramVec
}

func InstrumentQueue(next worker.Queue, reg prometheus.Registerer) worker.Queue {
	f := promauto.With(reg)
	q := &queue{
		Queue: next,
		batch: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "claim_batch_size", Help: "Messages claimed per worker turn.",
			Buckets: []float64{0, 1, 2, 5, 10, 20, 50},
		}, []string{"lane"}),
		outcomes: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "outcomes_total", Help: "Delivery attempts by result.",
		}, []string{"lane", "outcome"}),
		latency: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "delivery_latency_seconds", Help: "Time from acceptance until the operator accepted the message.",
			Buckets: []float64{.05, .1, .25, .5, 1, 2, 5, 10, 30, 60, 300},
		}, []string{"lane"}),
	}
	for _, lane := range domain.Lanes {
		q.batch.WithLabelValues(string(lane))
		q.latency.WithLabelValues(string(lane))
		for _, outcome := range outcomeLabels {
			q.outcomes.WithLabelValues(string(lane), outcome)
		}
	}
	return q
}

func (q *queue) Claim(ctx context.Context, req worker.ClaimRequest) ([]domain.Message, error) {
	batch, err := q.Queue.Claim(ctx, req)
	if err == nil {
		q.batch.WithLabelValues(string(req.Lane)).Observe(float64(len(batch)))
	}
	return batch, err
}

func (q *queue) Complete(ctx context.Context, batch []worker.Settlement) ([]uint64, error) {
	stale, err := q.Queue.Complete(ctx, batch)
	reclaimed := make(map[uint64]bool, len(stale))
	for _, id := range stale {
		reclaimed[id] = true
	}
	for _, item := range batch {
		q.record(item, reclaimed[item.Message.ID], err)
	}
	return stale, err
}

func (q *queue) record(item worker.Settlement, reclaimed bool, err error) {
	lane, status := string(item.Message.Lane), item.Outcome.Status
	q.outcomes.WithLabelValues(lane, outcomeLabel(status, reclaimed, err)).Inc()
	if err == nil && !reclaimed && status == domain.StatusSent {
		q.latency.WithLabelValues(lane).Observe(item.Outcome.At.Sub(item.Message.CreatedAt).Seconds())
	}
}

func outcomeLabel(status domain.Status, reclaimed bool, err error) string {
	switch {
	case err != nil:
		return "error"
	case reclaimed:
		return "stale"
	case status == domain.StatusPending:
		return "retry"
	}
	return strings.ToLower(string(status))
}

type operator struct {
	next     worker.Operator
	duration *prometheus.HistogramVec
}

func InstrumentOperator(next worker.Operator, reg prometheus.Registerer) worker.Operator {
	o := &operator{next: next, duration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Name: "operator_request_duration_seconds", Help: "Time the operator took to answer.",
		Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"lane", "result"})}
	for _, lane := range domain.Lanes {
		for _, result := range resultLabels {
			o.duration.WithLabelValues(string(lane), result)
		}
	}
	return o
}

func (o *operator) Send(ctx context.Context, msg domain.Message) (string, error) {
	start := time.Now()
	ref, err := o.next.Send(ctx, msg)
	o.duration.WithLabelValues(string(msg.Lane), sendResult(err)).Observe(time.Since(start).Seconds())
	return ref, err
}

func sendResult(err error) string {
	switch {
	case err == nil:
		return "accepted"
	case errors.Is(err, domain.ErrRejected):
		return "rejected"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	return "error"
}

type limiter struct {
	next worker.Limiter
	wait *prometheus.HistogramVec
}

func InstrumentLimiter(next worker.Limiter, reg prometheus.Registerer) worker.Limiter {
	l := &limiter{next: next, wait: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Name: "limiter_wait_seconds", Help: "Time spent waiting for operator capacity.",
		Buckets: []float64{.001, .01, .05, .1, .25, .5, 1, 2},
	}, []string{"lane"})}
	for _, lane := range domain.Lanes {
		l.wait.WithLabelValues(string(lane))
	}
	return l
}

func (l *limiter) Wait(ctx context.Context, lane domain.Lane) error {
	start := time.Now()
	err := l.next.Wait(ctx, lane)
	l.wait.WithLabelValues(string(lane)).Observe(time.Since(start).Seconds())
	return err
}
