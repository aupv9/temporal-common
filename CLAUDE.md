# temporal-common — CLAUDE.md

Go library cung cấp **production-ready foundation** cho Temporal workflow engine.
Mục tiêu: project mới chỉ viết business logic, không phải boilerplate.

## Repo Structure

```
temporal-common/
├── client/           # Factory, config, health check, graceful shutdown
├── activity/         # Retry presets, error taxonomy, idempotency helpers
├── workflow/         # Saga manager, versioning, approval pattern, scheduler
├── observability/    # Prometheus metrics, OpenTelemetry tracing, logging
├── testing/          # Test environment, mock builder, assertions
├── examples/
│   ├── loan-disbursement/   # Full reference implementation
│   └── payment-saga/
└── references/              # Chi tiết từng pattern
    ├── saga-pattern.md
    ├── versioning.md
    ├── retry-presets.md
    ├── testing.md
    └── observability.md
```

## Core Design Principles

### 1. Người dùng không bao giờ thấy Temporal internals
```go
// ❌ KHÔNG viết thế này
retryPolicy := &temporal.RetryPolicy{MaxAttempts: 5, ...}
ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{RetryPolicy: retryPolicy})

// ✅ Chỉ cần
ctx = temporalcommon.WithFinancialAPIOptions(ctx)
```

### 2. Mọi default đều production-safe
- Timeout phải luôn có — không để zero value
- Retry policy phải explicit — không dùng Temporal default ngầm
- Error classification phải tường minh — retryable vs non-retryable

### 3. Compensation-first design
- Saga manager tự động rollback theo LIFO khi có error
- Pivot point phải được đánh dấu tường minh
- Compensation step phải idempotent và retry-able vô hạn

### 4. Determinism is enforced, not optional
- Mọi source of non-determinism phải dùng `workflow.SideEffect`
- Không dùng `time.Now()` — dùng `workflow.Now(ctx)`
- Không dùng `rand` trực tiếp — wrap qua SideEffect

## Error Taxonomy

```go
// Retryable — infrastructure/transient failures
temporalcommon.NewRetryableError("message", cause)

// Non-retryable — business logic violations
temporalcommon.NewBusinessError("ErrorCode", "message", cause)
// ErrorCode: "InsufficientFund", "BorrowerBlacklisted", "LoanAlreadyDisbursed", "DuplicateTransaction"

// Compensation failure — cần human intervention
temporalcommon.NewCompensationError("message", sagaID, step, cause)
```

## Activity Checklist

Mỗi activity PHẢI có:
1. Extract idempotency key: `temporalcommon.IdempotencyKey(activity.GetInfo(ctx))`
2. Heartbeat nếu operation > 10s: `activity.RecordHeartbeat(ctx, "...")`
3. Check idempotency store trước khi execute
4. Classify errors đúng type khi return

## Workflow Checklist

Mỗi workflow PHẢI có:
1. Khởi tạo saga: `saga := temporalcommon.NewSaga(ctx); defer saga.Compensate()`
2. Set search attributes cho observability
3. Dùng preset options, không tự define ActivityOptions
4. `saga.AddCompensation(...)` ngay sau mỗi step thành công
5. `saga.SetPivot()` đúng vị trí
6. `saga.Complete()` khi thành công

## Testing Requirements

Mọi feature PHẢI có 3 loại test:
- **Unit test**: logic thuần, không cần Temporal
- **Workflow test**: dùng TestEnvironment, cover happy/compensation/retry path
- **Failure scenario test**: activity fail, orchestrator crash, compensation fail, duplicate execution

## Versioning Rules

Khi thay đổi workflow đã có running instances — LUÔN dùng `GetVersion`:
```go
v := workflow.GetVersion(ctx, "descriptive-change-id", workflow.DefaultVersion, 1)
if v == 1 { /* new path */ }
// DefaultVersion: old path
```

Xem `references/versioning.md` để biết naming convention và cleanup checklist.

## Common Mistakes — Không làm

```go
// ❌ Tự define ActivityOptions trong business workflow
workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 30 * time.Second})

// ❌ Dùng time.Now() trong workflow (non-deterministic)
if time.Now().After(deadline) { ... }

// ❌ Compensation step có thể fail permanently (saga stuck forever)
func RefundFeeActivity(ctx context.Context, txID string) error {
    return externalAPI.Refund(txID) // phải có retry + DLQ fallback
}

// ❌ Bỏ qua idempotency trong activity (double execution khi retry)
func TransferActivity(ctx context.Context, input TransferInput) error {
    return bankAPI.Transfer(input.Amount) // phải check idempotency key trước
}
```

## Quick Reference

```go
import temporalcommon "github.com/yourorg/temporal-common"

// Bootstrap
engine, _ := temporalcommon.New(temporalcommon.Config{...})

// Retry presets
temporalcommon.WithFinancialAPIOptions(ctx)    // external bank, payment GW
temporalcommon.WithNotificationOptions(ctx)    // email, SMS, push — best effort
temporalcommon.WithInternalServiceOptions(ctx) // gRPC internal services
temporalcommon.WithLongRunningOptions(ctx)     // jobs > 5 minutes

// Saga
saga := temporalcommon.NewSaga(ctx)
defer saga.Compensate()
saga.AddCompensation(RollbackActivity, payload)
saga.SetPivot()
saga.Complete()

// Versioning
v := workflow.GetVersion(ctx, "change-id", workflow.DefaultVersion, 1)

// Human approval
approval, err := temporalcommon.WaitForApproval(ctx, temporalcommon.ApprovalRequest{
    ApprovalID: entityID,
    Timeout:    48 * time.Hour,
})

// Idempotency
key := temporalcommon.IdempotencyKey(activity.GetInfo(ctx))
key := temporalcommon.IdempotencyKeyNoRetry(activity.GetInfo(ctx))
```
