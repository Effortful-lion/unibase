package adapter

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewPrometheus(t *testing.T) {
	cfg := PrometheusConfig{
		Namespace: "myapp",
		Subsystem: "http",
	}

	p := NewPrometheus(cfg)
	if p == nil {
		t.Fatal("expected non-nil Prometheus")
	}
	if p.registry == nil {
		t.Error("expected non-nil registry")
	}
}

func TestPrometheus_Registry(t *testing.T) {
	p := NewPrometheus(PrometheusConfig{})
	reg := p.Registry()
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestPrometheus_NewCounter(t *testing.T) {
	p := NewPrometheus(PrometheusConfig{Namespace: "test"})

	c := p.NewCounter(prometheus.CounterOpts{
		Name: "test_counter",
		Help: "a test counter",
	})
	if c == nil {
		t.Fatal("expected non-nil counter")
	}

	c.Inc()
	c.Add(5)

	// Verify via Gather - should return metric families for registered counter
	families, err := p.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	if len(families) == 0 {
		t.Error("expected non-empty metric families after registering counter")
	}
}

func TestPrometheus_NewGauge(t *testing.T) {
	p := NewPrometheus(PrometheusConfig{})

	g := p.NewGauge(prometheus.GaugeOpts{
		Name: "test_gauge",
		Help: "a test gauge",
	})
	if g == nil {
		t.Fatal("expected non-nil gauge")
	}

	g.Set(42)
	g.Inc()
	g.Dec()
}

func TestPrometheus_NewHistogram(t *testing.T) {
	p := NewPrometheus(PrometheusConfig{})

	h := p.NewHistogram(prometheus.HistogramOpts{
		Name:    "test_histogram",
		Help:    "a test histogram",
		Buckets: prometheus.DefBuckets,
	})
	if h == nil {
		t.Fatal("expected non-nil histogram")
	}

	h.Observe(0.5)
	h.Observe(1.0)
	h.Observe(2.0)
}

func TestPrometheus_NewSummary(t *testing.T) {
	p := NewPrometheus(PrometheusConfig{})

	s := p.NewSummary(prometheus.SummaryOpts{
		Name:       "test_summary",
		Help:       "a test summary",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01},
	})
	if s == nil {
		t.Fatal("expected non-nil summary")
	}

	s.Observe(1.0)
}

func TestPrometheus_RegisterDuplicate(t *testing.T) {
	p := NewPrometheus(PrometheusConfig{})

	c1 := p.NewCounter(prometheus.CounterOpts{
		Name: "dup_counter",
		Help: "duplicate test",
	})

	// Registering the same collector twice should fail
	err := p.Register(c1)
	if err == nil {
		t.Error("expected error when registering duplicate collector")
	}
	if !strings.Contains(err.Error(), "duplicate") && !strings.Contains(err.Error(), "already registered") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPrometheus_Unregister(t *testing.T) {
	p := NewPrometheus(PrometheusConfig{})

	c := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "unreg_counter",
		Help: "unregister test",
	})

	if err := p.Register(c); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	ok := p.Unregister(c)
	if !ok {
		t.Error("expected Unregister to return true")
	}

	// Unregistering again should return false
	ok = p.Unregister(c)
	if ok {
		t.Error("expected Unregister to return false for unknown collector")
	}
}
