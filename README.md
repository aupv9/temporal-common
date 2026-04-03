# temporal-common

> Production-ready Go foundation library for [Temporal](https://temporal.io) workflow engine.
> New projects write only business logic — not boilerplate.

[![Go Version](https://img.shields.io/badge/go-1.22+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

**[🇻🇳 Tiếng Việt](README.vi.md)**

---

## Why temporal-common?

Every Temporal project needs the same boilerplate: retry policies, saga compensation, idempotency keys, versioning helpers, test utilities, and observability wiring. `temporal-common` solves this once, correctly, with production-safe defaults.

```go
// ❌ Without temporal-common — every team reinvents this
retryPolicy := &temporal.RetryPolicy{
    MaxAttempts:        5,
    InitialInterval:    2 * time.Second,
    BackoffCoefficient: 2.0,
    MaximumInterval:    30 * time.Second,
    NonRetryableErrorTypes: []string{"InsufficientFund", ...},
}
ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
    ScheduleToCloseTimeout: 5 * time.Minute,
    StartToCloseTimeout:    2 * time.Minute,
    HeartbeatTimeout:       30 * time.Second,
    RetryPolicy:            retryPolicy,
})

// ✅ With temporal-common — one line
ctx = temporalcommon.WithFinancialAPIOptions(ctx)
```

---

## Installation

```bash
go get github.com/yourorg/temporal-common
```

---

## Quick Start

```go
package main

import (
    "context"
    tc "github.com/yourorg/temporal-common"
)

func main() {
    engine, err := tc.New(tc.Config{
        HostPort:  "localhost:7233",
        Namespace: "default",
        TaskQueue: "my-service",
    })
    if err != nil {
        panic(err)
    }

    engine.RegisterWorkflow(MyWorkflow)
    engine.RegisterActivity(MyActivity)

    engine.Start(context.Background()) // blocks until SIGTERM/SIGINT
}
```

---

## Core Concepts

### 1. Activity Retry Presets

Four production-calibrated presets cover the most common call patterns:

| Preset | Use Case | Schedule | Attempts | Backoff |
|---|---|---|---|---|
| `WithFinancialAPIOptions` | Bank APIs, payment gateways | 5 min | 5 | 2s×2, max 30s |
| `WithNotificationOptions` | Email, SMS, push — best effort | 2 min | 3 | 5s fixed |
| `WithInternalServiceOptions` | Internal gRPC/HTTP services | 1 min | 10 | 500ms×2, max 10s |
| `WithLongRunningOptions` | Batch jobs > 5 minutes | 24 hr | 3 | 10s×2, max 5 min |

```go
func MyWorkflow(ctx workflow.Context, input Input) error {
    // Each preset sets ScheduleToClose, StartToClose, HeartbeatTimeout,
    // RetryPolicy, and NonRetryableErrorTypes — all at once.
    finCtx  := temporalcommon.WithFinancialAPIOptions(ctx)
    notifCtx := temporalcommon.WithNotificationOptions(ctx)
    grpcCtx  := temporalcommon.WithInternalServiceOptions(ctx)
    batchCtx := temporalcommon.WithLongRunningOptions(ctx)
    // ...
}
```

### 2. Saga Compensation

The `Saga` manager handles distributed saga rollback automatically:

```go
func PaymentWorkflow(ctx workflow.Context, input PaymentInput) error {
    saga := temporalcommon.NewSaga(ctx)
    defer saga.Compensate() // auto-rollback LIFO if not completed

    // Step 1: debit
    var debit DebitResult
    if err := workflow.ExecuteActivity(ctx, DebitActivity, input).Get(ctx, &debit); err != nil {
        return err
    }
    saga.AddCompensation(ReverseDebitActivity, debit.TxID) // register rollback immediately

    // Step 2: credit
    if err := workflow.ExecuteActivity(ctx, CreditActivity, input).Get(ctx, nil); err != nil {
        return err // ← Compensate fires: ReverseDebitActivity runs automatically
    }

    saga.Complete() // success — prevents Compensate from rolling back
    return nil
}
```

**Pivot point** — marks where full rollback is no longer possible (e.g., after money leaves the bank):

```go
saga.AddCompensation(ReleaseReservation, id) // before pivot — safe rollback
saga.SetPivot()                              // funds committed externally
// steps after pivot are still compensated, but failures emit CompensationError
```

### 3. Error Taxonomy

```go
// Retryable — Temporal will retry per retry policy
temporalcommon.NewRetryableError("core banking unavailable", err)

// Non-retryable — business rule violation, never retry
temporalcommon.NewBusinessError("InsufficientFund", "balance too low", err)
// Other codes: "BorrowerBlacklisted", "LoanAlreadyDisbursed", "DuplicateTransaction"

// Compensation failure — requires human intervention
temporalcommon.NewCompensationError("refund API rejected", sagaID, "RefundFee", err)
```

### 4. Idempotency Keys

```go
func TransferActivity(ctx context.Context, input TransferInput) error {
    info := activity.GetInfo(ctx)

    // Per-attempt key — different key each retry (use when downstream is NOT idempotent)
    key := temporalcommon.IdempotencyKey(info)
    // → "workflow-123/transfer-act/2"

    // Stable key — same across retries (use when downstream IS idempotent, e.g. payment GW)
    key = temporalcommon.IdempotencyKeyNoRetry(info)
    // → "workflow-123/transfer-act"
}
```

### 5. Workflow Versioning

Safe in-place changes to running workflows:

```go
func MyWorkflow(ctx workflow.Context, input Input) error {
    // Declare ALL changes at the top — before any branching
    changes := temporalcommon.NewChangeSet(ctx)
    changes.Define("add-fraud-check-2024-03", 1)
    changes.Define("new-notify-step-2024-06", 1)

    // New path for new executions; old path replays correctly for existing instances
    if changes.IsEnabled("add-fraud-check-2024-03") {
        // run fraud check
    }

    // ...
}
```

### 6. Human Approval Gate

```go
approval, err := temporalcommon.WaitForApproval(ctx, temporalcommon.ApprovalRequest{
    ApprovalID:  loanID,
    Description: "Approve large loan disbursement",
    Timeout:     48 * time.Hour,
})
if err != nil {
    return err
}
if !approval.Approved {
    return temporalcommon.NewBusinessError("LoanRejected", "not approved by manager", nil)
}
```

Send the signal from outside the workflow:

```go
client.SignalWorkflow(ctx, workflowID, runID, temporalcommon.ApprovalSignalName,
    temporalcommon.ApprovalResult{Approved: true, ApproverID: "manager@acme.com"})
```

---

## Testing

```go
func TestPaymentWorkflow_Compensation(t *testing.T) {
    env := testing.NewTestEnvironment(t)
    defer env.Cleanup()
    env.TrackCompensations()

    env.MockActivity(DebitActivity).Return(DebitResult{TxID: "tx-1"}, nil)
    env.MockActivity(CreditActivity).ReturnError(errors.New("credit service down"))
    env.MockActivity(ReverseDebitActivity).Return(nil)

    env.ExecuteWorkflow(PaymentWorkflow, PaymentInput{Amount: 100})

    env.AssertWorkflowFailed(t, "credit failed")
    env.AssertCompensationOrder(t, ReverseDebitActivity)
    env.AssertActivityNotCalled(t, NotifyActivity)
}
```

**Three required test types per feature:**

1. **Unit test** — pure logic, no Temporal runtime
2. **Workflow test** — `TestEnvironment`, cover happy / compensation / retry paths
3. **Failure scenario test** — activity failure, crash recovery, duplicate execution

---

## Observability

```go
engine, _ := temporalcommon.New(temporalcommon.Config{
    // Prometheus metrics on /metrics endpoint
    Metrics: temporalcommon.MetricsConfig{Enabled: true},

    // OpenTelemetry tracing to OTLP collector
    Tracing: observability.TracingConfig{
        OTLPEndpoint:   "otel-collector:4317",
        ServiceName:    "loan-service",
        ServiceVersion: "1.2.0",
        Insecure:       true, // false in production
    },
})
```

Structured logging via `go.uber.org/zap` is wired automatically.

---

## Repository Structure

```
temporal-common/
├── temporalcommon.go         # Root facade — single import for consuming projects
├── activity/
│   ├── errors.go             # Error taxonomy (Retryable, Business, Compensation)
│   ├── idempotency.go        # IdempotencyKey, IdempotencyKeyNoRetry
│   └── options.go            # Retry presets (Financial, Notification, Internal, LongRunning)
├── client/
│   ├── config.go             # Config struct with safe defaults
│   ├── engine.go             # Engine: Temporal client + worker facade
│   ├── health.go             # HealthCheck for readiness probes
│   └── shutdown.go           # Graceful shutdown with drain timeout
├── workflow/
│   ├── saga.go               # Saga manager: LIFO compensation, pivot point
│   ├── versioning.go         # ChangeSet API over workflow.GetVersion
│   ├── approval.go           # WaitForApproval: signal-based human gate
│   └── scheduler.go          # CreateSchedule, DeleteSchedule helpers
├── observability/
│   ├── logging.go            # Zap → Temporal log.Logger adapter
│   ├── metrics.go            # Prometheus MetricsHandler
│   └── tracing.go            # OTLP trace exporter setup
├── testing/
│   ├── environment.go        # TestEnvironment wrapping testsuite
│   ├── mock.go               # Fluent MockActivity builder
│   └── assertions.go         # AssertActivityCalled, AssertCompensationOrder
├── examples/
│   ├── loan-disbursement/    # Full reference: saga + versioning + approval
│   └── payment-saga/         # Minimal saga compensation demo
└── references/
    ├── saga-pattern.md
    ├── versioning.md
    ├── retry-presets.md
    ├── testing.md
    └── observability.md
```

---

## Design Principles

1. **Users never see Temporal internals** — no `temporal.RetryPolicy` in business code
2. **All defaults are production-safe** — zero values never silently disable timeouts or retries
3. **Compensation-first** — every forward step registers its rollback before moving on
4. **Determinism is enforced** — no `time.Now()`, no raw `rand`, always use `workflow.Now(ctx)`

---

## Common Mistakes

```go
// ❌ Never define ActivityOptions manually in business workflows
workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
    StartToCloseTimeout: 30 * time.Second, // use a preset instead
})

// ❌ Never use time.Now() inside a workflow
if time.Now().After(deadline) { ... } // use workflow.Now(ctx)

// ❌ Never let a compensation activity fail permanently
func RefundFeeActivity(ctx context.Context, txID string) error {
    return externalAPI.Refund(txID) // if this returns BusinessError, saga blocks forever
    // ✅ Compensation must never return non-retryable errors
}

// ❌ Never skip idempotency in activities
func TransferActivity(ctx context.Context, input TransferInput) error {
    return bankAPI.Transfer(input.Amount) // double transfer on retry
    // ✅ Always check idempotency key first
}
```

---

## License

MIT
