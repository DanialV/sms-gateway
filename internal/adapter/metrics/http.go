package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type HTTP struct {
	duration *prometheus.HistogramVec
}

func NewHTTP(reg prometheus.Registerer) *HTTP {
	return &HTTP{duration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "http_request_duration_seconds",
		Help:      "Time to serve an API request.",
		Buckets:   []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
	}, []string{"method", "route", "status"})}
}

func (h *HTTP) ObserveRequest(method, route string, status int, elapsed time.Duration) {
	h.duration.WithLabelValues(method, route, strconv.Itoa(status)).Observe(elapsed.Seconds())
}
