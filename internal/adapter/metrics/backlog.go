package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"sms-gateway/internal/domain"
)

const collectTimeout = 2 * time.Second

type BacklogSource interface {
	Backlog(ctx context.Context, lane domain.Lane) (count int64, oldest time.Time, err error)
}

type ReadySource interface {
	Size(ctx context.Context, lane domain.Lane) (int64, error)
}

type backlog struct {
	messages  BacklogSource
	ready     ReadySource
	log       *slog.Logger
	waiting   *prometheus.Desc
	oldest    *prometheus.Desc
	customers *prometheus.Desc
}

func RegisterBacklog(reg prometheus.Registerer, messages BacklogSource, ready ReadySource, log *slog.Logger) {
	reg.MustRegister(&backlog{
		messages:  messages,
		ready:     ready,
		log:       log,
		waiting:   prometheus.NewDesc(namespace+"_backlog_messages", "Messages not yet sent, failed or expired.", []string{"lane"}, nil),
		oldest:    prometheus.NewDesc(namespace+"_backlog_oldest_seconds", "Age of the oldest message ready to send.", []string{"lane"}, nil),
		customers: prometheus.NewDesc(namespace+"_ready_customers", "Customers waiting in the lane's round robin.", []string{"lane"}, nil),
	})
}

func (b *backlog) Describe(ch chan<- *prometheus.Desc) {
	ch <- b.waiting
	ch <- b.oldest
	ch <- b.customers
}

func (b *backlog) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()
	for _, lane := range domain.Lanes {
		b.collectMessages(ctx, ch, lane)
		b.collectCustomers(ctx, ch, lane)
	}
}

func (b *backlog) collectMessages(ctx context.Context, ch chan<- prometheus.Metric, lane domain.Lane) {
	count, oldest, err := b.messages.Backlog(ctx, lane)
	if err != nil {
		b.log.WarnContext(ctx, "backlog metrics unavailable", "lane", lane, "err", err)
		return
	}
	ch <- prometheus.MustNewConstMetric(b.waiting, prometheus.GaugeValue, float64(count), string(lane))
	ch <- prometheus.MustNewConstMetric(b.oldest, prometheus.GaugeValue, age(oldest), string(lane))
}

func (b *backlog) collectCustomers(ctx context.Context, ch chan<- prometheus.Metric, lane domain.Lane) {
	size, err := b.ready.Size(ctx, lane)
	if err != nil {
		b.log.WarnContext(ctx, "ready customer metrics unavailable", "lane", lane, "err", err)
		return
	}
	ch <- prometheus.MustNewConstMetric(b.customers, prometheus.GaugeValue, float64(size), string(lane))
}

func age(oldest time.Time) float64 {
	if oldest.IsZero() {
		return 0
	}
	return max(time.Since(oldest).Seconds(), 0)
}
