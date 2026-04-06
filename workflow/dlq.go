package workflow

import (
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/yourorg/temporal-common/activity"
)

// CompensationFailureEvent is the structured payload delivered to a DLQ handler
// when a saga compensation step fails permanently.
// All fields are safe to serialise as a Temporal activity argument.
type CompensationFailureEvent struct {
	// SagaID is the workflow execution ID that owns this saga.
	SagaID string

	// RunID is the workflow run ID at the time of failure.
	RunID string

	// StepName is the fully-qualified name of the compensation activity function.
	StepName string

	// StepIndex is the position of the step in the compensation stack (0 = first registered).
	StepIndex int

	// ErrorMessage is the terminal error message from the failed compensation activity.
	// Stored as string (not error) so it serialises cleanly as a workflow activity argument.
	ErrorMessage string

	// OccurredAt is workflow.Now(ctx) at the moment of failure — deterministic.
	OccurredAt time.Time
}

// CompensationDLQHandler is notified when a saga compensation step fails permanently.
//
// Implementations are called synchronously inside Saga.Compensate(), within the
// workflow coroutine. They MAY call workflow.ExecuteActivity to durably persist
// the event. They MUST NOT perform blocking I/O directly (use activities for that).
type CompensationDLQHandler interface {
	OnCompensationFailed(ctx workflow.Context, event CompensationFailureEvent)
}

// ActivityDLQHandler is a CompensationDLQHandler that routes failures to a
// Temporal activity for durable persistence (database, PagerDuty, Jira, etc.).
//
// The sink activity is scheduled fire-and-forget — the handler does not block
// on the result so that remaining compensation steps continue executing.
//
// Usage:
//
//	saga := temporalcommon.NewSaga(ctx,
//	    temporalcommon.WithDLQHandler(
//	        temporalcommon.NewActivityDLQHandler(PersistFailureActivity),
//	    ),
//	)
//
//	// Sink activity — implement once per service:
//	func PersistFailureActivity(ctx context.Context, evt temporalcommon.CompensationFailureEvent) error {
//	    return db.InsertIncident(ctx, evt.SagaID, evt.StepName, evt.ErrorMessage)
//	}
type ActivityDLQHandler struct {
	activityFn interface{}
	optionsFn  func(workflow.Context) workflow.Context
}

// NewActivityDLQHandler creates a handler that calls activityFn with the
// CompensationFailureEvent when a compensation step fails.
// activityFn must accept (context.Context, CompensationFailureEvent) and return error.
// Uses WithInternalServiceOptions retry policy by default.
func NewActivityDLQHandler(activityFn interface{}) *ActivityDLQHandler {
	return &ActivityDLQHandler{activityFn: activityFn}
}

// NewActivityDLQHandlerWithOptions is like NewActivityDLQHandler but lets the
// caller supply a custom activity options function (e.g. WithFinancialAPIOptions).
func NewActivityDLQHandlerWithOptions(
	activityFn interface{},
	optionsFn func(workflow.Context) workflow.Context,
) *ActivityDLQHandler {
	return &ActivityDLQHandler{activityFn: activityFn, optionsFn: optionsFn}
}

// OnCompensationFailed executes the DLQ sink activity and awaits its result.
// If the sink itself fails, the error is logged but does NOT propagate —
// the Saga.Compensate loop continues to the next step regardless.
func (h *ActivityDLQHandler) OnCompensationFailed(ctx workflow.Context, event CompensationFailureEvent) {
	applyOpts := h.optionsFn
	if applyOpts == nil {
		applyOpts = activity.WithInternalServiceOptions
	}
	sinkCtx := applyOpts(ctx)
	if err := workflow.ExecuteActivity(sinkCtx, h.activityFn, event).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Error("saga: DLQ sink activity failed",
			"sagaID", event.SagaID,
			"step", event.StepName,
			"error", err,
		)
		// Non-fatal: compensation loop continues.
	}
}

// SagaOption is a functional option for NewSaga.
type SagaOption func(*Saga)

// WithDLQHandler attaches a CompensationDLQHandler to the saga.
// When a compensation step fails permanently, the handler is called before
// continuing to the next step.
func WithDLQHandler(h CompensationDLQHandler) SagaOption {
	return func(s *Saga) {
		s.dlqHandler = h
	}
}
