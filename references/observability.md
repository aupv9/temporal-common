# Observability

## Metrics (Prometheus)

Enable via `Config.Metrics.Enabled = true`. Metrics are registered on a private `prometheus.Registry` instance.

| Metric | Type | Description |
|---|---|---|
| `temporal_workflow_started_total` | Counter | Workflows started |
| `temporal_workflow_completed_total` | Counter | Workflows completed successfully |
| `temporal_workflow_failed_total` | Counter | Workflows that terminated with error |
| `temporal_activity_started_total` | Counter | Activity task starts |
| `temporal_activity_completed_total` | Counter | Activity completions |
| `temporal_activity_failed_total` | Counter | Activity failures (before retry exhaustion) |
| `temporal_activity_execution_latency_seconds` | Histogram | Activity execution duration |

## Tracing (OpenTelemetry)

Enable via `Config.Tracing.OTLPEndpoint = "localhost:4317"`. Sets up a global `TracerProvider` that exports spans to an OTLP collector.

```go
engine, _ := temporalcommon.New(temporalcommon.Config{
    Tracing: observability.TracingConfig{
        OTLPEndpoint:   "otel-collector:4317",
        ServiceName:    "loan-service",
        ServiceVersion: "1.2.0",
        Insecure:       false,
    },
})
```

To add Temporal-specific tracing spans (workflow/activity auto-instrumentation), pass the Temporal SDK's tracing interceptor via `Config.Interceptors`:

```go
import "go.temporal.io/sdk/contrib/opentelemetry"

tracingInterceptor, _ := opentelemetry.NewTracingInterceptor(opentelemetry.TracerOptions{})
engine, _ := temporalcommon.New(temporalcommon.Config{
    Interceptors: []interceptor.ClientInterceptor{tracingInterceptor},
})
```

## Logging (Zap)

The engine automatically creates a `zap.NewProduction()` logger and adapts it to Temporal's `log.Logger` interface. Temporal SDK log calls are routed through zap with structured fields.

Key log events emitted by temporal-common:
- Worker started / stopped — INFO
- Saga compensation started — INFO per step
- Compensation step failed — ERROR with `sagaID`, `step`, `error`
- Approval timed out — WARN with `approvalID`, `timeout`

## Search Attributes

Workflows should set search attributes for operational visibility:

```go
workflow.UpsertSearchAttributes(ctx, map[string]interface{}{
    "LoanID":     input.LoanID,
    "BorrowerID": input.BorrowerID,
    "Stage":      "DISBURSING",
})
```

Recommended attribute names (register in Temporal namespace before use):
- `LoanID` (Keyword)
- `BorrowerID` (Keyword)
- `Stage` (Keyword): INIT → AWAITING_APPROVAL → DISBURSING → NOTIFYING → COMPLETED
- `Amount` (Double)
