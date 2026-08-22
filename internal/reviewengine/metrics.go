package reviewengine

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds the engine's Prometheus collectors. All collectors
// are registered against the supplied Registry; nil means "do not
// record metrics" (test-only).
//
// Default registry choice: callers should create their own
// `prometheus.NewRegistry()` and pass it in. This avoids global
// state leaks between tests.
type Metrics struct {
	reviewTotal  *prometheus.CounterVec
	tokensTotal  *prometheus.CounterVec
	stepDuration *prometheus.HistogramVec
}

// NewMetrics builds collectors and registers them with reg.
// Returns nil if reg is nil.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	if reg == nil {
		return nil
	}
	m := &Metrics{
		reviewTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "reviewpilot_review_total",
				Help: "Total number of review runs by status (done|failed|skipped).",
			},
			[]string{"status"},
		),
		tokensTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "reviewpilot_review_tokens_total",
				Help: "Total tokens consumed, by stage (in|out).",
			},
			[]string{"stage"},
		),
		stepDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "reviewpilot_review_step_duration_seconds",
				Help:    "Per-step pipeline duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"step"},
		),
	}
	reg.MustRegister(m.reviewTotal, m.tokensTotal, m.stepDuration)
	return m
}

// ObserveStatus increments the review_total counter for the given status.
func (m *Metrics) ObserveStatus(status Status) {
	if m == nil || m.reviewTotal == nil {
		return
	}
	m.reviewTotal.WithLabelValues(string(status)).Inc()
}

// ObserveTokens adds input/output tokens to the tokens_total counter.
func (m *Metrics) ObserveTokens(in, out int) {
	if m == nil || m.tokensTotal == nil {
		return
	}
	if in > 0 {
		m.tokensTotal.WithLabelValues("in").Add(float64(in))
	}
	if out > 0 {
		m.tokensTotal.WithLabelValues("out").Add(float64(out))
	}
}

// ObserveStep records the duration of one pipeline step.
func (m *Metrics) ObserveStep(step StepName, seconds float64) {
	if m == nil || m.stepDuration == nil {
		return
	}
	m.stepDuration.WithLabelValues(string(step)).Observe(seconds)
}
