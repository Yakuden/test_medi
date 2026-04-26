package observability

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type OrderMetrics struct {
	total    *prometheus.CounterVec
	duration *prometheus.HistogramVec
	pending  prometheus.Gauge
}

func NewOrderMetrics(reg prometheus.Registerer) *OrderMetrics {
	f := promauto.With(reg)
	return &OrderMetrics{
		total: f.NewCounterVec(prometheus.CounterOpts{
			Name: "orders_created_total",
			Help: "Orders created, by outcome and customer tier.",
		}, []string{"status", "customer_tier"}),

		duration: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "order_processing_duration_seconds",
			Help:    "Processing latency by pipeline step.",
			Buckets: prometheus.DefBuckets,
		}, []string{"step"}),

		pending: f.NewGauge(prometheus.GaugeOpts{
			Name: "orders_pending_count",
			Help: "Orders currently in pending state.",
		}),
	}
}

func (m *OrderMetrics) IncOrders(status, tier string) {
	m.total.WithLabelValues(status, tier).Inc()
}

func (m *OrderMetrics) ObserveStep(step string, d time.Duration) {
	m.duration.WithLabelValues(step).Observe(d.Seconds())
}

func (m *OrderMetrics) SetPending(n float64) {
	m.pending.Set(n)
}
