package workflow

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// AuditEventKind classifies audit log entries.
type AuditEventKind string

const (
	// AuditKindStep records a completed workflow step.
	AuditKindStep AuditEventKind = "STEP"

	// AuditKindApproval records a human approval decision.
	AuditKindApproval AuditEventKind = "APPROVAL"

	// AuditKindCompensation records a saga compensation action.
	AuditKindCompensation AuditEventKind = "COMPENSATION"

	// AuditKindStageChange records a workflow stage transition.
	AuditKindStageChange AuditEventKind = "STAGE_CHANGE"

	// AuditKindCustom is a catch-all for application-defined events.
	AuditKindCustom AuditEventKind = "CUSTOM"
)

// AuditEntry is a single immutable audit record produced inside a workflow.
type AuditEntry struct {
	// Kind classifies the entry.
	Kind AuditEventKind `json:"kind"`

	// WorkflowID is the Temporal workflow ID.
	WorkflowID string `json:"workflowID"`

	// RunID is the Temporal run ID.
	RunID string `json:"runID"`

	// OccurredAt is the deterministic workflow time of the event.
	OccurredAt time.Time `json:"occurredAt"`

	// Step is a human-readable label, e.g. "TransferFunds" or "NotifyBorrower".
	Step string `json:"step"`

	// Outcome is "ok", "failed", "approved", "rejected", or any custom string.
	Outcome string `json:"outcome"`

	// Details holds arbitrary key/value annotations.
	Details map[string]string `json:"details,omitempty"`
}

// AuditWriter is implemented by anything that can persist audit entries.
// In production, use ActivityAuditWriter. In tests, use NoOpAuditWriter.
type AuditWriter interface {
	// Write persists an audit entry. Called inside a workflow execution —
	// implementors must not block; they must schedule a Temporal activity.
	Write(ctx workflow.Context, entry AuditEntry)
}

// auditKey is the context key for the current AuditWriter.
type auditKey struct{}

// WithAuditTrail attaches an AuditWriter to the workflow context.
// Call this once at the top of every workflow, just after NewSaga/NewWorkflowState.
//
//	writer := temporalcommon.ActivityAuditWriter(SaveAuditActivity)
//	ctx = temporalcommon.WithAuditTrail(ctx, writer)
func WithAuditTrail(ctx workflow.Context, writer AuditWriter) workflow.Context {
	return workflow.WithValue(ctx, auditKey{}, writer)
}

func auditWriterFromCtx(ctx workflow.Context) AuditWriter {
	if w, ok := ctx.Value(auditKey{}).(AuditWriter); ok {
		return w
	}
	return NoOpAuditWriter{}
}

func currentInfo(ctx workflow.Context) (workflowID, runID string) {
	info := workflow.GetInfo(ctx)
	return info.WorkflowExecution.ID, info.WorkflowExecution.RunID
}

func buildEntry(ctx workflow.Context, kind AuditEventKind, step, outcome string, details map[string]string) AuditEntry {
	wfID, runID := currentInfo(ctx)
	return AuditEntry{
		Kind:       kind,
		WorkflowID: wfID,
		RunID:      runID,
		OccurredAt: workflow.Now(ctx),
		Step:       step,
		Outcome:    outcome,
		Details:    details,
	}
}

// AuditStep records a completed workflow step.
//
//	temporalcommon.AuditStep(ctx, "TransferFunds", "ok", nil)
func AuditStep(ctx workflow.Context, step, outcome string, details map[string]string) {
	auditWriterFromCtx(ctx).Write(ctx, buildEntry(ctx, AuditKindStep, step, outcome, details))
}

// AuditApproval records a human approval decision.
//
//	temporalcommon.AuditApproval(ctx, "LoanApproval", result.Approved)
func AuditApproval(ctx workflow.Context, step string, approved bool) {
	outcome := "approved"
	if !approved {
		outcome = "rejected"
	}
	auditWriterFromCtx(ctx).Write(ctx, buildEntry(ctx, AuditKindApproval, step, outcome, nil))
}

// AuditCompensation records a saga compensation action.
//
//	temporalcommon.AuditCompensation(ctx, "RollbackTransfer", "ok", nil)
func AuditCompensation(ctx workflow.Context, step, outcome string, details map[string]string) {
	auditWriterFromCtx(ctx).Write(ctx, buildEntry(ctx, AuditKindCompensation, step, outcome, details))
}

// AuditStageChange records a workflow stage transition.
//
//	temporalcommon.AuditStageChange(ctx, "PROCESSING", "COMPLETED")
func AuditStageChange(ctx workflow.Context, from, to string) {
	details := map[string]string{"from": from, "to": to}
	auditWriterFromCtx(ctx).Write(ctx, buildEntry(ctx, AuditKindStageChange, "stage-change", to, details))
}

// AuditCustom records an arbitrary application event.
//
//	temporalcommon.AuditCustom(ctx, "RateLimitHit", "retrying", map[string]string{"limit": "100"})
func AuditCustom(ctx workflow.Context, step, outcome string, details map[string]string) {
	auditWriterFromCtx(ctx).Write(ctx, buildEntry(ctx, AuditKindCustom, step, outcome, details))
}

// ---------------------------------------------------------------------------
// ActivityAuditWriter — production implementation
// ---------------------------------------------------------------------------

// activityAuditWriter routes every AuditEntry to a Temporal activity for
// durable persistence (database, data lake, PagerDuty, etc.).
type activityAuditWriter struct {
	activityFn interface{}
	optionsFn  func(workflow.Context) workflow.Context
}

// NewActivityAuditWriter creates an AuditWriter that durably persists entries
// by scheduling activityFn as a Temporal activity.
//
// activityFn must accept (context.Context, AuditEntry) and return error.
//
//	writer := temporalcommon.NewActivityAuditWriter(SaveAuditActivity)
//	ctx = temporalcommon.WithAuditTrail(ctx, writer)
func NewActivityAuditWriter(activityFn interface{}) AuditWriter {
	return &activityAuditWriter{
		activityFn: activityFn,
		optionsFn:  defaultAuditActivityOptions,
	}
}

// defaultAuditActivityOptions applies conservative activity options for audit writes:
// 1 min start-to-close, 10 attempts, fast exponential backoff.
func defaultAuditActivityOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:        10,
			InitialInterval:        time.Second,
			BackoffCoefficient:     2.0,
			MaximumInterval:        30 * time.Second,
		},
	})
}

// NewActivityAuditWriterWithOptions is like NewActivityAuditWriter with a
// custom activity options function.
func NewActivityAuditWriterWithOptions(activityFn interface{}, optionsFn func(workflow.Context) workflow.Context) AuditWriter {
	return &activityAuditWriter{activityFn: activityFn, optionsFn: optionsFn}
}

// Write schedules the audit activity and awaits its result.
// Failures are silently swallowed — audit must not block the business flow.
func (w *activityAuditWriter) Write(ctx workflow.Context, entry AuditEntry) {
	actCtx := w.optionsFn(ctx)
	_ = workflow.ExecuteActivity(actCtx, w.activityFn, entry).Get(ctx, nil)
}

// ---------------------------------------------------------------------------
// NoOpAuditWriter — test/development helper
// ---------------------------------------------------------------------------

// NoOpAuditWriter discards all audit entries. Use in unit tests and
// local development when you don't want a real persistence activity.
type NoOpAuditWriter struct{}

// Write is a no-op.
func (NoOpAuditWriter) Write(_ workflow.Context, _ AuditEntry) {}
