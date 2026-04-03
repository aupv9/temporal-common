package observability

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.temporal.io/sdk/client"
)

// prometheusMetricsHandler implements client.MetricsHandler backed by Prometheus.
type prometheusMetricsHandler struct {
	counters   map[string]prometheus.Counter
	gauges     map[string]prometheus.Gauge
	histograms map[string]prometheus.Observer
	registry   *prometheus.Registry
	tags       map[string]string
}

// NewPrometheusMetricsHandler returns a client.MetricsHandler that records
// Temporal SDK metrics to Prometheus. Pass the returned handler to
// client.Options.MetricsHandler when constructing the Temporal client.
//
// If registry is nil, prometheus.DefaultRegisterer is used.
func NewPrometheusMetricsHandler(registry *prometheus.Registry) client.MetricsHandler {
	if registry == nil {
		registry = prometheus.NewRegistry()
	}
	h := &prometheusMetricsHandler{
		counters:   make(map[string]prometheus.Counter),
		gauges:     make(map[string]prometheus.Gauge),
		histograms: make(map[string]prometheus.Observer),
		registry:   registry,
		tags:       make(map[string]string),
	}
	h.registerMetrics()
	return h
}

func (h *prometheusMetricsHandler) registerMetrics() {
	names := []string{
		"temporal_workflow_started_total",
		"temporal_workflow_completed_total",
		"temporal_workflow_failed_total",
		"temporal_activity_started_total",
		"temporal_activity_completed_total",
		"temporal_activity_failed_total",
	}
	for _, name := range names {
		c := prometheus.NewCounter(prometheus.CounterOpts{Name: name})
		// Best-effort registration — ignore "already registered" errors.
		_ = h.registry.Register(c)
		h.counters[name] = c
	}

	latency := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "temporal_activity_execution_latency_seconds",
		Buckets: prometheus.DefBuckets,
	})
	_ = h.registry.Register(latency)
	h.histograms["temporal_activity_execution_latency_seconds"] = latency
}

// WithTags returns a handler scoped to the given tags (labels).
// Tags are informational only in this implementation; Prometheus labels
// require pre-declaration. For full label support, use the official
// go.temporal.io/sdk/contrib/tally or prom adapter.
func (h *prometheusMetricsHandler) WithTags(tags map[string]string) client.MetricsHandler {
	merged := make(map[string]string, len(h.tags)+len(tags))
	for k, v := range h.tags {
		merged[k] = v
	}
	for k, v := range tags {
		merged[k] = v
	}
	return &prometheusMetricsHandler{
		counters:   h.counters,
		gauges:     h.gauges,
		histograms: h.histograms,
		registry:   h.registry,
		tags:       merged,
	}
}

func (h *prometheusMetricsHandler) Counter(name string) client.MetricsCounter {
	if c, ok := h.counters[name]; ok {
		return &promCounter{c}
	}
	// Dynamically create unregistered counter for unknown metric names.
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: name})
	return &promCounter{c}
}

func (h *prometheusMetricsHandler) Gauge(name string) client.MetricsGauge {
	if g, ok := h.gauges[name]; ok {
		return &promGauge{g}
	}
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: name})
	return &promGauge{g}
}

func (h *prometheusMetricsHandler) Timer(name string) client.MetricsTimer {
	if o, ok := h.histograms[name]; ok {
		return &promTimer{o}
	}
	hist := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    name,
		Buckets: prometheus.DefBuckets,
	})
	return &promTimer{hist}
}

func (h *prometheusMetricsHandler) Stop(_ context.Context) {}

// promCounter wraps prometheus.Counter to implement client.MetricsCounter.
type promCounter struct{ c prometheus.Counter }

func (p *promCounter) Inc(delta int64) { p.c.Add(float64(delta)) }

// promGauge wraps prometheus.Gauge to implement client.MetricsGauge.
type promGauge struct{ g prometheus.Gauge }

func (p *promGauge) Update(v float64) { p.g.Set(v) }

// promTimer wraps prometheus.Observer to implement client.MetricsTimer.
type promTimer struct{ o prometheus.Observer }

func (p *promTimer) Record(d time.Duration) { p.o.Observe(d.Seconds()) }
