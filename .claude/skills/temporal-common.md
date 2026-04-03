---
name: temporal-common
description: >
  Hướng dẫn phát triển thư viện temporal-common — Go library đóng gói Temporal workflow engine
  thành reusable foundation cho các dự án fintech/lending. Dùng skill này khi làm việc với bất kỳ
  file nào trong repo temporal-common, thêm pattern mới (Saga, versioning, approval, retry preset),
  viết activity/workflow, implement observability, viết test, hoặc review code liên quan đến
  Temporal SDK. Trigger ngay khi user đề cập: temporal, workflow engine, saga compensation,
  workflow versioning, activity retry, durable execution, loan workflow, hoặc bất kỳ task nào
  trong project temporal-common.
---

# temporal-common — Developer Skill

## Project Purpose

Library Go cung cấp **production-ready foundation** cho Temporal workflow engine.
Mục tiêu: project mới chỉ viết business logic, không phải boilerplate.

```
Project dùng library chỉ cần:
  1. engine.New(config)
  2. engine.RegisterWorkflow(MyWorkflow)
  3. engine.RegisterActivity(MyActivity)
  4. engine.Start(ctx)
```

---

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
└── references/              # Chi tiết từng pattern (xem bên dưới)
```

**Reference files — đọc khi cần:**
- `references/saga-pattern.md` — Saga manager internals, compensation stack, pivot point
- `references/versioning.md` — ChangeSet API, migration strategy, cleanup guide
- `references/retry-presets.md` — Tất cả preset definitions, NonRetryableErrors taxonomy
- `references/testing.md` — TestEnvironment API, mock patterns, assertion helpers
- `references/observability.md` — Metrics list, tracing spans, log fields chuẩn

---

## Core Design Principles

### 1. Người dùng không bao giờ thấy Temporal internals
```go
// ❌ Người dùng KHÔNG nên phải viết thế này
retryPolicy := &temporal.RetryPolicy{
    MaxAttempts:        5,
    InitialInterval:    2 * time.Second,
    BackoffCoefficient: 2.0,
    ...
}
ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
    RetryPolicy: retryPolicy,
    ...
})

// ✅ Người dùng chỉ cần
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
- Mọi source of non-determinism phải dùng workflow.SideEffect
- Không dùng time.Now() — dùng workflow.Now(ctx)
- Không dùng rand trực tiếp — wrap qua SideEffect

---

## Code Conventions

### Package naming
```
temporalcommon.New()              # client factory
temporalcommon.NewSaga()          # saga manager
temporalcommon.NewChangeSet()     # versioning helper
temporalcommon.WithXxxOptions()   # activity option presets
temporalcommon.NewTestEnvironment() # testing
```

### Error taxonomy — luôn dùng types này
```go
// Trong package activity/errors.go

// Retryable — infrastructure/transient failures
temporalcommon.NewRetryableError("message", cause)

// Non-retryable — business logic violations
temporalcommon.NewBusinessError("ErrorCode", "message", cause)
// ErrorCode examples: "InsufficientFund", "BorrowerBlacklisted",
//                     "LoanAlreadyDisbursed", "DuplicateTransaction"

// Compensation failure — cần human intervention
temporalcommon.NewCompensationError("message", sagaID, step, cause)
```

### Activity implementation checklist
Mỗi activity PHẢI có:
```go
func XxxActivity(ctx context.Context, input XxxInput) (XxxResult, error) {
    // 1. Extract idempotency key từ activity info
    info := activity.GetInfo(ctx)
    iKey := temporalcommon.IdempotencyKey(info)

    // 2. Heartbeat nếu operation > 10s
    activity.RecordHeartbeat(ctx, "starting operation")

    // 3. Check idempotency store trước khi execute
    // (dùng temporalcommon.IdempotencyStore nếu injected)

    // 4. Classify errors đúng type khi return
    // → business error: NewBusinessError
    // → infra error: return err (Temporal sẽ retry)
}
```

### Workflow implementation checklist
Mỗi workflow PHẢI có:
```go
func XxxWorkflow(ctx workflow.Context, input XxxInput) error {
    // 1. Khởi tạo saga nếu có compensation
    saga := temporalcommon.NewSaga(ctx)
    defer saga.Compensate()

    // 2. Set search attributes cho observability
    workflow.UpsertSearchAttributes(ctx, map[string]interface{}{
        "EntityID": input.ID,
        "Stage":    "INIT",
    })

    // 3. Dùng preset options, không tự define ActivityOptions
    ctx = temporalcommon.WithInternalServiceOptions(ctx)

    // 4. AddCompensation ngay sau mỗi step thành công
    saga.AddCompensation(RollbackXxx, result.ID)

    // 5. SetPivot() đúng vị trí — sau đây không compensate
    saga.SetPivot()

    // 6. Complete() khi thành công
    saga.Complete()
}
```

---

## Adding New Features — Workflow

### Thêm Retry Preset mới
1. Đọc `references/retry-presets.md` để hiểu taxonomy hiện tại
2. Thêm vào `activity/options.go`
3. Thêm NonRetryableErrors tương ứng vào `activity/errors.go`
4. Viết test trong `activity/options_test.go`
5. Cập nhật `references/retry-presets.md`

### Thêm Workflow Pattern mới
1. Tạo file mới trong `workflow/`
2. Export qua `workflow.go` (main package file)
3. Thêm example vào `examples/`
4. Viết integration test dùng `testing.TestEnvironment`
5. Cập nhật reference file tương ứng

### Thêm Metric mới
1. Đọc `references/observability.md` — naming conventions
2. Định nghĩa trong `observability/metrics.go`
3. Emit tại đúng điểm trong code
4. Cập nhật `references/observability.md`

---

## Testing Requirements

**Mọi feature PHẢI có 3 loại test:**

```
1. Unit test — logic thuần, không cần Temporal
   File: xxx_test.go (cùng package)

2. Workflow test — dùng TestEnvironment
   File: xxx_workflow_test.go
   Phải cover: happy path, compensation path, retry path

3. Failure scenario test — quan trọng nhất
   - Activity fail → compensation chạy đúng thứ tự
   - Orchestrator crash mid-saga → recovery đúng
   - Compensation fail → DLQ + human required flag
   - Duplicate execution → idempotency đảm bảo
```

**Test helpers — luôn dùng, không mock thủ công:**
```go
env := temporalcommon.NewTestEnvironment(t)
defer env.Cleanup()

env.MockActivity(DebitFeeActivity).Return(result, nil)
env.MockActivity(CoreBankingActivity).Return(nil, errors.New("timeout"))

env.ExecuteWorkflow(LoanDisbursementWorkflow, input)

env.AssertActivityCalled(t, RefundFeeActivity)
env.AssertActivityNotCalled(t, NotifyActivity)
env.AssertCompensationOrder(t, ReleaseReservation, RefundFee)
```

---

## Versioning Rules — QUAN TRỌNG

Khi thay đổi bất kỳ workflow nào đã có running instances:

```go
// LUÔN dùng GetVersion — KHÔNG bao giờ modify flow trực tiếp
v := workflow.GetVersion(ctx, "change-id-descriptive", workflow.DefaultVersion, 1)
if v == 1 {
    // new path
}
// DefaultVersion path chạy code cũ
```

Xem `references/versioning.md` để biết:
- Naming convention cho changeID
- Strategy khi có nhiều changes chồng nhau
- Cleanup checklist sau khi old workflows drain
- Khi nào nên tạo workflow mới hoàn toàn thay vì versioning

---

## Common Mistakes — Không làm

```go
// ❌ Không tự define ActivityOptions từ đầu trong business workflow
workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
    StartToCloseTimeout: 30 * time.Second, // dùng preset thay thế
})

// ❌ Không dùng time.Now() trong workflow
if time.Now().After(deadline) { // non-deterministic

// ❌ Không return error sau pivot mà không có xử lý
saga.SetPivot()
err = workflow.ExecuteActivity(...).Get(ctx, nil)
return err // ✅ OK — nhưng phải comment rõ "no compensation past pivot"

// ❌ Không viết compensation step có thể fail permanently
func RefundFeeActivity(ctx context.Context, txID string) error {
    return externalAPI.Refund(txID) // nếu fail → saga stuck forever
    // ✅ Phải: retry với backoff, hoặc queue sang DLQ + alert
}

// ❌ Không bỏ qua idempotency trong activity
func TransferActivity(ctx context.Context, input TransferInput) error {
    return bankAPI.Transfer(input.Amount) // double transfer nếu retry
    // ✅ Phải: check idempotency key trước
}
```

---

## Quick Reference — Hay dùng nhất

```go
import temporalcommon "github.com/yourorg/temporal-common"

// Bootstrap
engine, _ := temporalcommon.New(temporalcommon.Config{...})

// Retry presets
temporalcommon.WithFinancialAPIOptions(ctx)   // external bank, payment GW
temporalcommon.WithNotificationOptions(ctx)   // email, SMS, push — best effort
temporalcommon.WithInternalServiceOptions(ctx) // gRPC internal services
temporalcommon.WithLongRunningOptions(ctx)    // jobs > 5 minutes

// Saga
saga := temporalcommon.NewSaga(ctx)
defer saga.Compensate()
saga.AddCompensation(RollbackActivity, payload)
saga.SetPivot()
saga.Complete()

// Versioning
changes := temporalcommon.NewChangeSet(ctx)
changes.Define("feature-name", 1)
if changes.IsEnabled("feature-name") { ... }

// Human approval
approval, err := temporalcommon.WaitForApproval(ctx, temporalcommon.ApprovalRequest{
    ApprovalID: entityID,
    Timeout:    48 * time.Hour,
})

// Idempotency key
info := activity.GetInfo(ctx)
key := temporalcommon.IdempotencyKey(info)          // per workflow+activity
key := temporalcommon.IdempotencyKeyNoRetry(info)   // same across retries
```
