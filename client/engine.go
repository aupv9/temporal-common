package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yourorg/temporal-common/observability"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.uber.org/zap"
)

// Engine is the central facade for a Temporal client + worker.
// Consuming projects create one Engine at startup, register their workflows
// and activities, then call Start.
type Engine struct {
	client          client.Client
	worker          worker.Worker
	config          Config
	tracingShutdown func(context.Context) error
	logger          *zap.Logger
	metricsRegistry *prometheus.Registry // non-nil when Metrics.Enabled = true
	slaEntries      []slaEntry            // registered SLA configs, populated by RegisterWorkflowWithSLA
}

// New creates an Engine from the given Config, wiring in observability
// (Prometheus metrics + OTLP tracing) based on the config flags.
//
// New validates that TaskQueue is non-empty and returns an error if the
// Temporal client cannot be established.
func New(cfg Config) (*Engine, error) {
	cfg.applyDefaults()

	if cfg.TaskQueue == "" {
		return nil, fmt.Errorf("temporal-common: Config.TaskQueue is required")
	}

	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("create zap logger: %w", err)
	}

	// Build client options
	clientOpts := client.Options{
		HostPort:  cfg.HostPort,
		Namespace: cfg.Namespace,
		Logger:    observability.NewZapLogger(logger),
	}

	if cfg.Identity != "" {
		clientOpts.Identity = cfg.Identity
	}

	// TLS
	if cfg.TLS != nil {
		tlsCfg, err := buildTLSConfig(cfg.TLS)
		if err != nil {
			return nil, fmt.Errorf("build TLS config: %w", err)
		}
		clientOpts.ConnectionOptions = client.ConnectionOptions{
			TLS: tlsCfg,
		}
	}

	// Prometheus metrics
	var metricsRegistry *prometheus.Registry
	if cfg.Metrics.Enabled {
		metricsRegistry = prometheus.NewRegistry()
		clientOpts.MetricsHandler = observability.NewPrometheusMetricsHandler(metricsRegistry)
	}

	// OpenTelemetry tracing — set up global provider; wire interceptors from Config.
	tracingShutdown := func(_ context.Context) error { return nil }
	if cfg.Tracing.OTLPEndpoint != "" {
		_, shutdown, err := observability.SetupTracing(cfg.Tracing)
		if err != nil {
			return nil, fmt.Errorf("setup tracing: %w", err)
		}
		tracingShutdown = shutdown
	}

	// User-supplied interceptors (e.g. opentelemetry.NewTracingInterceptor).
	if len(cfg.Interceptors) > 0 {
		clientOpts.Interceptors = cfg.Interceptors
	}

	// Create Temporal client
	c, err := client.Dial(clientOpts)
	if err != nil {
		return nil, fmt.Errorf("dial Temporal server at %s: %w", cfg.HostPort, err)
	}

	// Create worker
	workerOpts := worker.Options{
		MaxConcurrentActivityTaskPollers:   cfg.Worker.MaxConcurrentActivityTaskPollers,
		MaxConcurrentWorkflowTaskPollers:   cfg.Worker.MaxConcurrentWorkflowTaskPollers,
		MaxConcurrentActivityExecutionSize: cfg.Worker.MaxConcurrentActivityExecutionSize,
	}
	w := worker.New(c, cfg.TaskQueue, workerOpts)

	return &Engine{
		client:          c,
		worker:          w,
		config:          cfg,
		tracingShutdown: tracingShutdown,
		logger:          logger,
		metricsRegistry: metricsRegistry,
	}, nil
}

// RegisterWorkflow registers a workflow function with the Temporal worker.
// Must be called before Start.
func (e *Engine) RegisterWorkflow(w interface{}) {
	e.worker.RegisterWorkflow(w)
}

// RegisterActivity registers an activity function or struct with the Temporal worker.
// Must be called before Start.
func (e *Engine) RegisterActivity(a interface{}) {
	e.worker.RegisterActivity(a)
}

// Client returns the underlying Temporal client, for advanced use cases such
// as starting workflows or sending signals from outside a workflow.
func (e *Engine) Client() client.Client {
	return e.client
}

// TaskQueue returns the task queue this Engine's worker is bound to.
func (e *Engine) TaskQueue() string {
	return e.config.TaskQueue
}

// StartOrAttach starts a workflow using the Engine's client and task queue,
// or returns a handle to the already-running execution if one exists for workflowID.
// See client.StartOrAttach for full semantics.
func (e *Engine) StartOrAttach(
	ctx context.Context,
	workflowID string,
	workflowFn interface{},
	args ...interface{},
) (WorkflowHandle, error) {
	return StartOrAttach(ctx, e.client, workflowID, e.config.TaskQueue, workflowFn, args...)
}

// ServeMetrics starts an HTTP server exposing Prometheus metrics at /metrics
// on the given address (e.g. ":9090"). The server runs until ctx is cancelled.
//
// Requires Config.Metrics.Enabled = true. Returns an error immediately if
// metrics are disabled or the listener cannot be bound.
//
// Typical usage:
//
//	go engine.ServeMetrics(ctx, ":9090")
func (e *Engine) ServeMetrics(ctx context.Context, addr string) error {
	if e.metricsRegistry == nil {
		return fmt.Errorf("ServeMetrics: metrics are not enabled — set Config.Metrics.Enabled = true")
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(e.metricsRegistry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: addr, Handler: mux}

	errCh := make(chan error, 1)
	go func() {
		e.logger.Info("metrics server starting", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("metrics server: %w", err)
	case <-ctx.Done():
		_ = srv.Shutdown(context.Background())
		return nil
	}
}

func buildTLSConfig(cfg *TLSConfig) (*tls.Config, error) {
	tlsCfg := &tls.Config{
		ServerName: cfg.ServerName,
		MinVersion: tls.VersionTLS12,
	}

	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	if cfg.CAFile != "" {
		caData, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("parse CA certificate from %s", cfg.CAFile)
		}
		tlsCfg.RootCAs = pool
	}

	return tlsCfg, nil
}
