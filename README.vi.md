# temporal-common

> Thư viện Go cung cấp **production-ready foundation** cho [Temporal](https://temporal.io) workflow engine.
> Project mới chỉ cần viết business logic — không phải boilerplate.

[![Go Version](https://img.shields.io/badge/go-1.22+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

**[🇺🇸 English](README.md)**

---

## Tại sao cần temporal-common?

Mọi project dùng Temporal đều cần viết đi viết lại cùng một boilerplate: retry policy, saga compensation, idempotency key, versioning helper, test utility, và observability. `temporal-common` giải quyết một lần, đúng cách, với default production-safe.

```go
// ❌ Không có temporal-common — mỗi team tự viết lại từ đầu
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

// ✅ Với temporal-common — một dòng
ctx = temporalcommon.WithFinancialAPIOptions(ctx)
```

---

## Cài đặt

```bash
go get github.com/yourorg/temporal-common
```

---

## Bắt đầu nhanh

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

    engine.Start(context.Background()) // block đến khi nhận SIGTERM/SIGINT
}
```

---

## Các khái niệm cốt lõi

### 1. Activity Retry Presets

Bốn preset được cấu hình sẵn cho các pattern gọi phổ biến nhất:

| Preset | Dùng cho | Schedule | Số lần retry | Backoff |
|---|---|---|---|---|
| `WithFinancialAPIOptions` | Bank API, payment gateway | 5 phút | 5 | 2s×2, tối đa 30s |
| `WithNotificationOptions` | Email, SMS, push — best effort | 2 phút | 3 | 5s cố định |
| `WithInternalServiceOptions` | gRPC/HTTP nội bộ | 1 phút | 10 | 500ms×2, tối đa 10s |
| `WithLongRunningOptions` | Batch job > 5 phút | 24 giờ | 3 | 10s×2, tối đa 5 phút |

```go
func MyWorkflow(ctx workflow.Context, input Input) error {
    // Mỗi preset tự động set ScheduleToClose, StartToClose, HeartbeatTimeout,
    // RetryPolicy, và NonRetryableErrorTypes — tất cả trong một lần gọi.
    finCtx   := temporalcommon.WithFinancialAPIOptions(ctx)
    notifCtx := temporalcommon.WithNotificationOptions(ctx)
    grpcCtx  := temporalcommon.WithInternalServiceOptions(ctx)
    batchCtx := temporalcommon.WithLongRunningOptions(ctx)
    // ...
}
```

### 2. Saga Compensation

`Saga` manager xử lý distributed saga rollback tự động theo thứ tự LIFO:

```go
func PaymentWorkflow(ctx workflow.Context, input PaymentInput) error {
    saga := temporalcommon.NewSaga(ctx)
    defer saga.Compensate() // tự động rollback LIFO nếu chưa Complete

    // Bước 1: trừ tiền
    var debit DebitResult
    if err := workflow.ExecuteActivity(ctx, DebitActivity, input).Get(ctx, &debit); err != nil {
        return err
    }
    saga.AddCompensation(ReverseDebitActivity, debit.TxID) // đăng ký rollback ngay sau khi thành công

    // Bước 2: cộng tiền
    if err := workflow.ExecuteActivity(ctx, CreditActivity, input).Get(ctx, nil); err != nil {
        return err // ← Compensate tự động chạy: ReverseDebitActivity được gọi
    }

    saga.Complete() // thành công — ngăn Compensate rollback
    return nil
}
```

**Pivot point** — đánh dấu điểm không thể rollback hoàn toàn (ví dụ: sau khi tiền đã rời ngân hàng):

```go
saga.AddCompensation(ReleaseReservation, id) // trước pivot — rollback được
saga.SetPivot()                              // tiền đã chuyển đi bên ngoài
// các bước sau pivot vẫn được compensate, nhưng nếu thất bại thì phát ra CompensationError
```

### 3. Error Taxonomy

```go
// Retryable — Temporal sẽ retry theo retry policy
temporalcommon.NewRetryableError("core banking không khả dụng", err)

// Non-retryable — vi phạm business rule, không bao giờ retry
temporalcommon.NewBusinessError("InsufficientFund", "số dư không đủ", err)
// Các code khác: "BorrowerBlacklisted", "LoanAlreadyDisbursed", "DuplicateTransaction"

// Lỗi compensation — cần con người xử lý
temporalcommon.NewCompensationError("refund API từ chối", sagaID, "RefundFee", err)
```

### 4. Idempotency Key

```go
func TransferActivity(ctx context.Context, input TransferInput) error {
    info := activity.GetInfo(ctx)

    // Key theo từng lần retry — key khác nhau mỗi lần retry
    // (dùng khi downstream KHÔNG idempotent)
    key := temporalcommon.IdempotencyKey(info)
    // → "workflow-123/transfer-act/2"

    // Key ổn định — giống nhau qua các lần retry
    // (dùng khi downstream CÓ idempotent, ví dụ: payment gateway)
    key = temporalcommon.IdempotencyKeyNoRetry(info)
    // → "workflow-123/transfer-act"
}
```

### 5. Workflow Versioning

Thay đổi workflow đang có running instances một cách an toàn:

```go
func MyWorkflow(ctx workflow.Context, input Input) error {
    // Khai báo TẤT CẢ changes ở đầu workflow — trước mọi branching
    changes := temporalcommon.NewChangeSet(ctx)
    changes.Define("add-fraud-check-2024-03", 1)
    changes.Define("new-notify-step-2024-06", 1)

    // Đường mới cho execution mới; instance cũ replay đúng theo path cũ
    if changes.IsEnabled("add-fraud-check-2024-03") {
        // chạy fraud check
    }
}
```

### 6. Human Approval Gate

Tạm dừng workflow chờ người duyệt:

```go
approval, err := temporalcommon.WaitForApproval(ctx, temporalcommon.ApprovalRequest{
    ApprovalID:  loanID,
    Description: "Duyệt giải ngân khoản vay lớn",
    Timeout:     48 * time.Hour,
})
if err != nil {
    return err
}
if !approval.Approved {
    return temporalcommon.NewBusinessError("LoanRejected", "khoản vay không được duyệt", nil)
}
```

Gửi signal từ bên ngoài workflow:

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
    env.AssertCompensationOrder(t, ReverseDebitActivity) // đảm bảo rollback đúng thứ tự
    env.AssertActivityNotCalled(t, NotifyActivity)
}
```

**Ba loại test bắt buộc cho mỗi feature:**

| Loại | Mục đích | File |
|---|---|---|
| Unit test | Logic thuần, không cần Temporal | `xxx_test.go` |
| Workflow test | TestEnvironment, cover happy/compensation/retry | `xxx_workflow_test.go` |
| Failure scenario | Activity fail, crash recovery, duplicate execution | `xxx_failure_test.go` |

### Testing approval signal

```go
env.RegisterDelayedCallback(func() {
    env.Env().SignalWorkflow(temporalcommon.ApprovalSignalName, temporalcommon.ApprovalResult{
        Approved:   true,
        ApproverID: "manager@acme.com",
    })
}, 1*time.Hour)
```

---

## Observability

```go
engine, _ := temporalcommon.New(temporalcommon.Config{
    // Prometheus metrics — expose tại /metrics
    Metrics: temporalcommon.MetricsConfig{Enabled: true},

    // OpenTelemetry tracing — gửi đến OTLP collector
    Tracing: observability.TracingConfig{
        OTLPEndpoint:   "otel-collector:4317",
        ServiceName:    "loan-service",
        ServiceVersion: "1.2.0",
        Insecure:       true, // false trong production
    },
})
```

Structured logging qua `go.uber.org/zap` được wire tự động.

### Metrics được collect

| Metric | Loại | Mô tả |
|---|---|---|
| `temporal_workflow_started_total` | Counter | Số workflow được khởi động |
| `temporal_workflow_completed_total` | Counter | Số workflow hoàn thành thành công |
| `temporal_workflow_failed_total` | Counter | Số workflow thất bại |
| `temporal_activity_started_total` | Counter | Số activity task bắt đầu |
| `temporal_activity_completed_total` | Counter | Số activity hoàn thành |
| `temporal_activity_failed_total` | Counter | Số activity thất bại |
| `temporal_activity_execution_latency_seconds` | Histogram | Thời gian thực thi activity |

---

## Cấu trúc repo

```
temporal-common/
├── temporalcommon.go         # Root facade — single import cho consuming project
├── activity/
│   ├── errors.go             # Error taxonomy (Retryable, Business, Compensation)
│   ├── idempotency.go        # IdempotencyKey, IdempotencyKeyNoRetry
│   └── options.go            # Retry preset (Financial, Notification, Internal, LongRunning)
├── client/
│   ├── config.go             # Config struct với default an toàn
│   ├── engine.go             # Engine: facade cho Temporal client + worker
│   ├── health.go             # HealthCheck cho readiness probe
│   └── shutdown.go           # Graceful shutdown với drain timeout
├── workflow/
│   ├── saga.go               # Saga manager: LIFO compensation, pivot point
│   ├── versioning.go         # ChangeSet API bọc workflow.GetVersion
│   ├── approval.go           # WaitForApproval: human gate qua signal
│   └── scheduler.go          # CreateSchedule, DeleteSchedule
├── observability/
│   ├── logging.go            # Adapter Zap → Temporal log.Logger
│   ├── metrics.go            # Prometheus MetricsHandler
│   └── tracing.go            # OTLP trace exporter setup
├── testing/
│   ├── environment.go        # TestEnvironment bọc testsuite
│   ├── mock.go               # Fluent MockActivity builder
│   └── assertions.go         # AssertActivityCalled, AssertCompensationOrder
├── examples/
│   ├── loan-disbursement/    # Reference đầy đủ: saga + versioning + approval
│   └── payment-saga/         # Demo saga compensation tối giản
└── references/
    ├── saga-pattern.md       # Chi tiết Saga manager
    ├── versioning.md         # ChangeSet API, naming convention, cleanup
    ├── retry-presets.md      # Bảng giá trị timeout/retry của từng preset
    ├── testing.md            # Hướng dẫn viết test, test signal approval
    └── observability.md      # Metrics, tracing, search attributes
```

---

## Activity Checklist

Mỗi activity **BẮT BUỘC** phải có:

```go
func XxxActivity(ctx context.Context, input XxxInput) (XxxResult, error) {
    // 1. Lấy idempotency key từ activity info
    info := activity.GetInfo(ctx)
    iKey := temporalcommon.IdempotencyKey(info)

    // 2. Heartbeat nếu operation > 10s
    activity.RecordHeartbeat(ctx, "đang xử lý...")

    // 3. Check idempotency store trước khi thực thi
    // (tránh double-execution khi retry)

    // 4. Classify error đúng loại khi trả về
    // → business error: NewBusinessError
    // → infra error: return err thường (Temporal tự retry)
}
```

## Workflow Checklist

Mỗi workflow **BẮT BUỘC** phải có:

```go
func XxxWorkflow(ctx workflow.Context, input XxxInput) error {
    // 1. Khai báo versioning trước mọi thứ
    changes := temporalcommon.NewChangeSet(ctx)
    changes.Define("...", 1)

    // 2. Khởi tạo saga
    saga := temporalcommon.NewSaga(ctx)
    defer saga.Compensate()

    // 3. Set search attributes
    workflow.UpsertSearchAttributes(ctx, map[string]interface{}{"Stage": "INIT"})

    // 4. Dùng preset options, không tự define ActivityOptions
    ctx = temporalcommon.WithInternalServiceOptions(ctx)

    // 5. AddCompensation ngay sau mỗi step thành công
    saga.AddCompensation(RollbackXxx, result.ID)

    // 6. SetPivot đúng vị trí
    saga.SetPivot()

    // 7. Complete khi thành công
    saga.Complete()
    return nil
}
```

---

## Các lỗi thường gặp

```go
// ❌ Không tự define ActivityOptions trong business workflow
workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
    StartToCloseTimeout: 30 * time.Second, // dùng preset thay thế
})

// ❌ Không dùng time.Now() trong workflow (non-deterministic)
if time.Now().After(deadline) { ... } // dùng workflow.Now(ctx)

// ❌ Compensation activity không được phép fail vĩnh viễn
func RefundFeeActivity(ctx context.Context, txID string) error {
    return externalAPI.Refund(txID)
    // Nếu trả về BusinessError → saga bị kẹt mãi mãi
    // ✅ Compensation KHÔNG BAO GIỜ trả về non-retryable error
}

// ❌ Bỏ qua idempotency trong activity
func TransferActivity(ctx context.Context, input TransferInput) error {
    return bankAPI.Transfer(input.Amount)
    // Double transfer khi retry
    // ✅ Luôn check idempotency key trước khi thực thi
}

// ❌ Gọi Define sau khi đã có branching
if someCondition {
    changes.Define("change-a", 1) // không deterministic khi replay
}
// ✅ Define TẤT CẢ ở đầu workflow, trước mọi if/activity/sleep
```

---

## License

MIT
