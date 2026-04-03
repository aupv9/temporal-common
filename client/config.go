package client

import (
	"time"

	"github.com/yourorg/temporal-common/observability"
	"go.temporal.io/sdk/interceptor"
)

// Config is the sole configuration struct a consuming project needs to set.
// Every field has a safe default so projects can start with the minimum:
//
//	Config{HostPort: "localhost:7233", Namespace: "default", TaskQueue: "my-queue"}
type Config struct {
	// HostPort is the Temporal server address. Default: "localhost:7233".
	HostPort string

	// Namespace is the Temporal namespace. Default: "default".
	Namespace string

	// TaskQueue is the task queue this worker listens on. Required.
	TaskQueue string

	// Identity is reported to Temporal for debugging. Defaults to hostname+pid.
	Identity string

	// TLS holds optional mTLS configuration for the Temporal client connection.
	TLS *TLSConfig

	// Metrics controls Prometheus metrics exposure. Zero value = disabled.
	Metrics MetricsConfig

	// Tracing controls OpenTelemetry trace export. Zero value = disabled.
	Tracing observability.TracingConfig

	// Worker controls concurrency limits for the Temporal worker.
	Worker WorkerConfig

	// ShutdownTimeout is how long to wait for in-flight activities to complete
	// before forcing shutdown. Default: 30s.
	ShutdownTimeout time.Duration

	// Interceptors are additional client interceptors, e.g. a tracing interceptor
	// built from go.temporal.io/sdk/contrib/opentelemetry.
	Interceptors []interceptor.ClientInterceptor
}

// TLSConfig holds TLS/mTLS settings for the Temporal client.
type TLSConfig struct {
	CertFile   string
	KeyFile    string
	CAFile     string
	ServerName string
}

// MetricsConfig controls Prometheus metrics registration.
type MetricsConfig struct {
	// Enabled turns on Prometheus metric collection.
	Enabled bool
}

// WorkerConfig controls Temporal worker concurrency.
type WorkerConfig struct {
	// MaxConcurrentActivityTaskPollers is the number of goroutines polling for
	// activity tasks. Default: 5.
	MaxConcurrentActivityTaskPollers int

	// MaxConcurrentWorkflowTaskPollers is the number of goroutines polling for
	// workflow tasks. Default: 5.
	MaxConcurrentWorkflowTaskPollers int

	// MaxConcurrentActivityExecutionSize caps concurrent activity executions.
	// Default: 100.
	MaxConcurrentActivityExecutionSize int
}

func (c *Config) applyDefaults() {
	if c.HostPort == "" {
		c.HostPort = "localhost:7233"
	}
	if c.Namespace == "" {
		c.Namespace = "default"
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 30 * time.Second
	}
	if c.Worker.MaxConcurrentActivityTaskPollers == 0 {
		c.Worker.MaxConcurrentActivityTaskPollers = 5
	}
	if c.Worker.MaxConcurrentWorkflowTaskPollers == 0 {
		c.Worker.MaxConcurrentWorkflowTaskPollers = 5
	}
	if c.Worker.MaxConcurrentActivityExecutionSize == 0 {
		c.Worker.MaxConcurrentActivityExecutionSize = 100
	}
}
