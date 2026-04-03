package workflow

import (
	"fmt"
	"reflect"
	"runtime"
	"time"

	"github.com/yourorg/temporal-common/activity"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// compensation holds a single rollback step.
type compensation struct {
	activityFn interface{}
	args       []interface{}
	stepName   string
}

// Saga manages distributed saga compensation.
//
// Usage pattern — in every workflow:
//
//	saga := workflow.NewSaga(ctx)
//	defer saga.Compensate() // automatically rolls back if not completed
//
//	result, err := executeStep(ctx)
//	if err != nil { return err }
//	saga.AddCompensation(RollbackStep, result.ID)
//
//	saga.SetPivot() // marks point of no return
//
//	saga.Complete() // prevents Compensate from rolling back on success
type Saga struct {
	ctx           workflow.Context
	id            string
	compensations []compensation
	pivotIdx      int  // -1 means no pivot set
	completed     bool // set by Complete(); guards defer Compensate()
	logger        log.Logger
}

// NewSaga initialises a Saga bound to the given workflow context.
// Call defer saga.Compensate() immediately after.
func NewSaga(ctx workflow.Context) *Saga {
	info := workflow.GetInfo(ctx)
	return &Saga{
		ctx:      ctx,
		id:       info.WorkflowExecution.ID,
		pivotIdx: -1,
		logger:   workflow.GetLogger(ctx),
	}
}

// AddCompensation registers a rollback step.
// Call this immediately after each forward step succeeds.
// activityFn must be a registered activity function.
func (s *Saga) AddCompensation(activityFn interface{}, args ...interface{}) {
	s.compensations = append(s.compensations, compensation{
		activityFn: activityFn,
		args:       args,
		stepName:   getFunctionName(activityFn),
	})
}

// SetPivot marks the current position as the point of no return.
// Steps added before this point are rolled back normally.
// Steps after this point are still rolled back (LIFO), but a failure
// emits a CompensationError and requires human intervention.
func (s *Saga) SetPivot() {
	s.pivotIdx = len(s.compensations)
}

// Complete marks the saga as successfully finished.
// After Complete, defer saga.Compensate() becomes a no-op.
// Must be the last call in the workflow happy path.
func (s *Saga) Complete() {
	s.completed = true
}

// Compensate executes all registered compensations in LIFO order.
// It is a no-op if Complete() was called (success path).
//
// Compensation activities run with unlimited retries. If a compensation
// activity fails permanently (context cancelled / workflow terminated),
// a CompensationError is logged but remaining steps still execute to
// maximise cleanup.
func (s *Saga) Compensate() {
	if s.completed {
		return
	}
	if len(s.compensations) == 0 {
		return
	}

	compensationCtx := workflow.WithActivityOptions(s.ctx, compensationActivityOptions)

	s.logger.Info("saga: starting compensation",
		"sagaID", s.id,
		"steps", len(s.compensations),
	)

	// LIFO: iterate from last to first.
	for i := len(s.compensations) - 1; i >= 0; i-- {
		c := s.compensations[i]
		s.logger.Info("saga: compensating step",
			"sagaID", s.id,
			"step", c.stepName,
			"index", i,
		)

		future := workflow.ExecuteActivity(compensationCtx, c.activityFn, c.args...)
		if err := future.Get(compensationCtx, nil); err != nil {
			compErr := activity.NewCompensationError(
				fmt.Sprintf("step %q failed permanently", c.stepName),
				s.id,
				c.stepName,
				err,
			)
			s.logger.Error("saga: compensation step failed — human intervention required",
				"sagaID", s.id,
				"step", c.stepName,
				"error", compErr,
			)
			// Do NOT return — continue compensating remaining steps.
		}
	}

	s.logger.Info("saga: compensation complete", "sagaID", s.id)
}

// compensationActivityOptions gives unlimited retries to compensation activities.
// Compensation must eventually succeed — it can never be non-retryable.
// The only way Get returns an error is workflow termination (context cancelled).
var compensationActivityOptions = workflow.ActivityOptions{
	StartToCloseTimeout: 1 * time.Hour,
	HeartbeatTimeout:    60 * time.Second,
	RetryPolicy: &temporal.RetryPolicy{
		InitialInterval:        5 * time.Second,
		BackoffCoefficient:     2.0,
		MaximumInterval:        5 * time.Minute,
		MaximumAttempts:        0,    // unlimited
		NonRetryableErrorTypes: nil,  // nothing is non-retryable during compensation
	},
}

// getFunctionName extracts a human-readable name from an activity function pointer.
// Used for logging and CompensationError step labels only — not workflow state.
func getFunctionName(fn interface{}) string {
	name := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	if name == "" {
		return "unknown"
	}
	return name
}
