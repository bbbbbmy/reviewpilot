package reviewengine

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetrics_NewWithNilRegistryReturnsNil(t *testing.T) {
	m := NewMetrics(nil)
	if m != nil {
		t.Fatalf("expected nil for nil registry, got %v", m)
	}
}

func TestMetrics_ObserveStatus_AndTokens_AndStep(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveStatus(StatusDone)
	m.ObserveStatus(StatusDone)
	m.ObserveStatus(StatusSkipped)
	m.ObserveTokens(100, 50)
	m.ObserveStep(StepSplit, 0.001)

	if got := testutil.ToFloat64(m.reviewTotal.WithLabelValues("done")); got != 2 {
		t.Fatalf("review_total{done}=%v", got)
	}
	if got := testutil.ToFloat64(m.reviewTotal.WithLabelValues("skipped")); got != 1 {
		t.Fatalf("review_total{skipped}=%v", got)
	}
	if got := testutil.ToFloat64(m.tokensTotal.WithLabelValues("in")); got != 100 {
		t.Fatalf("tokens_total{in}=%v", got)
	}
	if got := testutil.ToFloat64(m.tokensTotal.WithLabelValues("out")); got != 50 {
		t.Fatalf("tokens_total{out}=%v", got)
	}

	// Histogram: observe one sample and gather.
	m.ObserveStep(StepLLM, 0.42)
	got, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	var found bool
	for _, fam := range got {
		if strings.Contains(fam.GetName(), "reviewpilot_review_step_duration_seconds") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("step histogram not registered")
	}
}

func TestMetrics_NilSafe(t *testing.T) {
	var m *Metrics
	// Should not panic.
	m.ObserveStatus(StatusDone)
	m.ObserveTokens(1, 2)
	m.ObserveStep(StepLLM, 0.1)
}
