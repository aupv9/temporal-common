// Package temporalcommon provides a production-ready foundation for Temporal
// workflow engine. Consuming projects import this single package and get
// retry presets, saga compensation, versioning, approval patterns, and
// observability — without ever touching Temporal internals.
//
// Quick start:
//
//	engine, err := temporalcommon.New(temporalcommon.Config{
//	    HostPort:  "localhost:7233",
//	    Namespace: "default",
//	    TaskQueue: "my-service",
//	})
//	engine.RegisterWorkflow(MyWorkflow)
//	engine.RegisterActivity(MyActivity)
//	engine.Start(ctx)
package temporalcommon

import (
	"github.com/yourorg/temporal-common/activity"
	"github.com/yourorg/temporal-common/client"
	wf "github.com/yourorg/temporal-common/workflow"
	"go.temporal.io/sdk/workflow"
)

// ---------------------------------------------------------------------------
// Idempotency store
// ---------------------------------------------------------------------------

// IdempotencyStore deduplicates activity executions across retries.
type IdempotencyStore = activity.IdempotencyStore

// NewInMemoryIdempotencyStore creates an in-memory store suitable for tests
// and single-process local development.
var NewInMemoryIdempotencyStore = activity.NewInMemoryIdempotencyStore

// NewRedisIdempotencyStore creates a Redis-backed store for production use.
var NewRedisIdempotencyStore = activity.NewRedisIdempotencyStore

// ExecuteIdempotent wraps an activity body with idempotency check-and-record.
// Skips fn and returns the cached result if the key was already executed.
var ExecuteIdempotent = activity.ExecuteIdempotent

// ExecuteIdempotentWithTTL is like ExecuteIdempotent with a custom cache TTL.
var ExecuteIdempotentWithTTL = activity.ExecuteIdempotentWithTTL

// ---------------------------------------------------------------------------
// Client / Engine
// ---------------------------------------------------------------------------

// Config is the sole configuration struct. Every field has a safe default.
type Config = client.Config

// TLSConfig holds mTLS settings for the Temporal client connection.
type TLSConfig = client.TLSConfig

// MetricsConfig controls Prometheus metrics.
type MetricsConfig = client.MetricsConfig

// WorkerConfig controls Temporal worker concurrency limits.
type WorkerConfig = client.WorkerConfig

// Engine is the central facade — Temporal client + worker in one struct.
type Engine = client.Engine

// New creates an Engine from the given Config.
var New = client.New

// ---------------------------------------------------------------------------
// Activity presets
// ---------------------------------------------------------------------------

// WithFinancialAPIOptions applies production-safe options for external bank /
// payment gateway calls: 5 min schedule, 5 attempts, exponential backoff.
func WithFinancialAPIOptions(ctx workflow.Context) workflow.Context {
	return activity.WithFinancialAPIOptions(ctx)
}

// WithNotificationOptions applies options for best-effort notification delivery
// (email, SMS, push): 2 min schedule, 3 attempts, fixed-rate retry.
func WithNotificationOptions(ctx workflow.Context) workflow.Context {
	return activity.WithNotificationOptions(ctx)
}

// WithInternalServiceOptions applies options for internal gRPC service calls:
// 1 min schedule, 10 attempts, fast exponential backoff.
func WithInternalServiceOptions(ctx workflow.Context) workflow.Context {
	return activity.WithInternalServiceOptions(ctx)
}

// WithLongRunningOptions applies options for batch jobs > 5 minutes:
// 24 hr schedule, mandatory heartbeat, 3 attempts.
func WithLongRunningOptions(ctx workflow.Context) workflow.Context {
	return activity.WithLongRunningOptions(ctx)
}

// ---------------------------------------------------------------------------
// Error taxonomy
// ---------------------------------------------------------------------------

// NewRetryableError creates a retryable error for transient infrastructure failures.
// Temporal will retry according to the activity's retry policy.
var NewRetryableError = activity.NewRetryableError

// NewBusinessError creates a non-retryable error for business rule violations.
// code examples: "InsufficientFund", "BorrowerBlacklisted", "LoanAlreadyDisbursed".
var NewBusinessError = activity.NewBusinessError

// NewCompensationError creates a non-retryable error for permanent saga
// compensation failures that require human intervention.
var NewCompensationError = activity.NewCompensationError

// IsBusinessError returns true if err is a business error with the given code.
var IsBusinessError = activity.IsBusinessError

// IsCompensationError returns true if err is a CompensationError.
var IsCompensationError = activity.IsCompensationError

// ---------------------------------------------------------------------------
// Idempotency
// ---------------------------------------------------------------------------

// IdempotencyKey returns "{WorkflowID}/{ActivityID}/{Attempt}" — unique per attempt.
var IdempotencyKey = activity.IdempotencyKey

// IdempotencyKeyNoRetry returns "{WorkflowID}/{ActivityID}" — stable across retries.
var IdempotencyKeyNoRetry = activity.IdempotencyKeyNoRetry

// ---------------------------------------------------------------------------
// Saga
// ---------------------------------------------------------------------------

// Saga manages distributed saga compensation with LIFO rollback and pivot support.
type Saga = wf.Saga

// NewSaga initialises a Saga. Call defer saga.Compensate() immediately after.
func NewSaga(ctx workflow.Context) *wf.Saga {
	return wf.NewSaga(ctx)
}

// ---------------------------------------------------------------------------
// Versioning
// ---------------------------------------------------------------------------

// ChangeSet provides a named, declarative API over workflow.GetVersion.
type ChangeSet = wf.ChangeSet

// NewChangeSet creates a ChangeSet. All Define calls must happen before any
// workflow branching to preserve replay determinism.
func NewChangeSet(ctx workflow.Context) *wf.ChangeSet {
	return wf.NewChangeSet(ctx)
}

// ---------------------------------------------------------------------------
// Human approval
// ---------------------------------------------------------------------------

// ApprovalRequest describes a human approval gate.
type ApprovalRequest = wf.ApprovalRequest

// ApprovalResult is the payload delivered via the approval signal.
type ApprovalResult = wf.ApprovalResult

// WaitForApproval blocks the workflow until an approval signal arrives or
// the timeout elapses.
func WaitForApproval(ctx workflow.Context, req wf.ApprovalRequest) (wf.ApprovalResult, error) {
	return wf.WaitForApproval(ctx, req)
}

// ApprovalSignalName is the Temporal signal name used by WaitForApproval.
const ApprovalSignalName = wf.ApprovalSignalName

// ---------------------------------------------------------------------------
// Scheduling
// ---------------------------------------------------------------------------

// ScheduleOptions configures a Temporal Schedule for recurring workflows.
type ScheduleOptions = wf.ScheduleOptions

// CreateSchedule registers a recurring workflow schedule with the Temporal server.
// Use engine.Client() to obtain the client argument.
//
// Example:
//
//	err := temporalcommon.CreateSchedule(ctx, engine.Client(), engine.TaskQueue(),
//	    DailyReportWorkflow, DailyReportInput{},
//	    temporalcommon.ScheduleOptions{
//	        ScheduleID:     "daily-report",
//	        CronExpression: "0 6 * * *",
//	    })
var CreateSchedule = wf.CreateSchedule

// DeleteSchedule removes a Temporal Schedule by ID.
var DeleteSchedule = wf.DeleteSchedule

// ---------------------------------------------------------------------------
// Parallel execution
// ---------------------------------------------------------------------------

// ActivityCall describes a single activity invocation for parallel execution.
type ActivityCall = wf.ActivityCall

// ParallelResult holds the outcome of a single activity in a parallel batch.
type ParallelResult = wf.ParallelResult

// ExecuteParallel runs all activities concurrently (fan-out) and waits for
// all to complete (fan-in). Returns combined errors if any activity fails.
func ExecuteParallel(ctx workflow.Context, calls []wf.ActivityCall) ([]wf.ParallelResult, error) {
	return wf.ExecuteParallel(ctx, calls)
}

// ExecuteParallelBestEffort is like ExecuteParallel but never returns an error.
// Failed activities are recorded in ParallelResult.Err — the caller decides
// which failures are acceptable.
func ExecuteParallelBestEffort(ctx workflow.Context, calls []wf.ActivityCall) []wf.ParallelResult {
	return wf.ExecuteParallelBestEffort(ctx, calls)
}

// ---------------------------------------------------------------------------
// Custom activity options builder
// ---------------------------------------------------------------------------

// NewOptionsBuilder creates a fluent builder for workflow.ActivityOptions.
// Use when the four built-in presets don't fit your use case.
var NewOptionsBuilder = activity.NewOptionsBuilder
