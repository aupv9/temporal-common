package workflow_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tctest "github.com/yourorg/temporal-common/testing"
	"github.com/yourorg/temporal-common/workflow"
	goworkflow "go.temporal.io/sdk/workflow"
)

// ---------------------------------------------------------------------------
// Stub activities for DLQ tests
// ---------------------------------------------------------------------------

func forwardStepActivity(_ context.Context) error    { return nil }
func compensationOKActivity(_ context.Context) error { return nil }
func compensationFailActivity(_ context.Context) error {
	return errors.New("refund API permanently down")
}
func dlqSinkActivity(_ context.Context, _ workflow.CompensationFailureEvent) error { return nil }

// ---------------------------------------------------------------------------
// Test workflows
// ---------------------------------------------------------------------------

// sagaDLQWorkflow: one forward step, compensation fails → DLQ handler fires.
func sagaDLQWorkflow(ctx goworkflow.Context) error {
	ctx = goworkflow.WithActivityOptions(ctx, goworkflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
	})

	dlqHandler := workflow.NewActivityDLQHandler(dlqSinkActivity)
	saga := workflow.NewSaga(ctx, workflow.WithDLQHandler(dlqHandler))
	defer saga.Compensate()

	if err := goworkflow.ExecuteActivity(ctx, forwardStepActivity).Get(ctx, nil); err != nil {
		return err
	}
	saga.AddCompensation(compensationFailActivity)

	return errors.New("forward step 2 failed — trigger compensation")
}

// sagaNoDLQWorkflow: no DLQ handler — backward-compat regression test.
func sagaNoDLQWorkflow(ctx goworkflow.Context) error {
	ctx = goworkflow.WithActivityOptions(ctx, goworkflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
	})

	saga := workflow.NewSaga(ctx) // no options
	defer saga.Compensate()

	if err := goworkflow.ExecuteActivity(ctx, forwardStepActivity).Get(ctx, nil); err != nil {
		return err
	}
	saga.AddCompensation(compensationFailActivity)

	return errors.New("trigger compensation")
}

// sagaMultiFailWorkflow: two compensation steps both fail → DLQ fired twice.
func sagaMultiFailWorkflow(ctx goworkflow.Context) error {
	ctx = goworkflow.WithActivityOptions(ctx, goworkflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
	})

	dlqHandler := workflow.NewActivityDLQHandler(dlqSinkActivity)
	saga := workflow.NewSaga(ctx, workflow.WithDLQHandler(dlqHandler))
	defer saga.Compensate()

	if err := goworkflow.ExecuteActivity(ctx, forwardStepActivity).Get(ctx, nil); err != nil {
		return err
	}
	saga.AddCompensation(compensationFailActivity)
	saga.AddCompensation(compensationFailActivity)

	return errors.New("trigger compensation")
}

// sagaHappyDLQWorkflow: completes successfully — DLQ handler must NOT be called.
func sagaHappyDLQWorkflow(ctx goworkflow.Context) error {
	ctx = goworkflow.WithActivityOptions(ctx, goworkflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
	})

	dlqHandler := workflow.NewActivityDLQHandler(dlqSinkActivity)
	saga := workflow.NewSaga(ctx, workflow.WithDLQHandler(dlqHandler))
	defer saga.Compensate()

	if err := goworkflow.ExecuteActivity(ctx, forwardStepActivity).Get(ctx, nil); err != nil {
		return err
	}
	saga.AddCompensation(compensationOKActivity)
	saga.Complete()
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSaga_DLQHandler_CalledOnCompensationFailure(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(forwardStepActivity).Return(nil)
	env.MockActivity(compensationFailActivity).ReturnError(errors.New("refund API down"))
	env.MockActivity(dlqSinkActivity).Return(nil)

	env.ExecuteWorkflow(sagaDLQWorkflow)

	// Workflow must fail (forward step 2 failed).
	env.AssertWorkflowFailed(t, "")
	// DLQ sink must have been called.
	env.AssertActivityCalled(t, dlqSinkActivity)
}

func TestSaga_DLQHandler_NotCalledOnSuccess(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(forwardStepActivity).Return(nil)
	// compensationOKActivity is registered but saga.Complete() prevents it from running.
	env.MockActivity(compensationOKActivity).Return(nil).Maybe()

	env.ExecuteWorkflow(sagaHappyDLQWorkflow)

	env.AssertWorkflowCompleted(t)
	env.AssertActivityNotCalled(t, dlqSinkActivity)
}

func TestSaga_DLQHandler_CalledForEachFailedStep(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(forwardStepActivity).Return(nil)
	// Both compensation steps fail — DLQ sink should be called twice.
	env.MockActivity(compensationFailActivity).ReturnError(errors.New("refund down")).Times(2)
	env.MockActivity(dlqSinkActivity).Return(nil).Times(2)

	env.ExecuteWorkflow(sagaMultiFailWorkflow)

	env.AssertWorkflowFailed(t, "")
	env.AssertActivityCalled(t, dlqSinkActivity)
}

func TestSaga_NoDLQHandler_BackwardCompat(t *testing.T) {
	// Without a DLQ handler, compensation failure is only logged — no panic.
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(forwardStepActivity).Return(nil)
	env.MockActivity(compensationFailActivity).ReturnError(errors.New("fail"))

	env.ExecuteWorkflow(sagaNoDLQWorkflow)

	// Workflow must still fail (forward step failed), but no DLQ sink involved.
	env.AssertWorkflowFailed(t, "")
	env.AssertActivityNotCalled(t, dlqSinkActivity)
}

func TestSaga_DLQHandler_EventFieldsCorrect(t *testing.T) {
	var capturedEvent workflow.CompensationFailureEvent
	var called atomic.Bool

	// Custom in-process DLQ handler for field inspection.
	type captureDLQ struct{}
	// We cannot easily use an in-process handler in Temporal test env because
	// DLQ handler calls workflow.ExecuteActivity. Instead, verify via mock args.
	// This test uses ActivityDLQHandler and inspects what was passed to the sink.

	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(forwardStepActivity).Return(nil)
	env.MockActivity(compensationFailActivity).ReturnError(errors.New("bank API down"))
	env.MockActivity(dlqSinkActivity).RunFn(func(_ context.Context, evt workflow.CompensationFailureEvent) error {
		capturedEvent = evt
		called.Store(true)
		return nil
	})

	env.ExecuteWorkflow(sagaDLQWorkflow)

	require.True(t, called.Load(), "DLQ sink must have been called")
	assert.NotEmpty(t, capturedEvent.SagaID, "SagaID must be set")
	assert.NotEmpty(t, capturedEvent.StepName, "StepName must be set")
	assert.Contains(t, capturedEvent.ErrorMessage, "bank API down", "ErrorMessage must contain original error")
	assert.False(t, capturedEvent.OccurredAt.IsZero(), "OccurredAt must be set")
}

func TestSaga_DLQHandler_RemainingStepsExecuteAfterDLQFailure(t *testing.T) {
	// Even if DLQ sink itself fails (sink activity error), remaining
	// compensation steps must still execute.
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(forwardStepActivity).Return(nil)
	env.MockActivity(compensationFailActivity).ReturnError(errors.New("fail")).Times(2)
	// DLQ sink also fails — must not block the second compensation step.
	env.MockActivity(dlqSinkActivity).ReturnError(errors.New("dlq sink down")).Times(2)

	env.ExecuteWorkflow(sagaMultiFailWorkflow)

	env.AssertWorkflowFailed(t, "")
	// Both compensation steps and both DLQ sink calls must have been attempted.
	env.AssertActivityCalled(t, compensationFailActivity)
	env.AssertActivityCalled(t, dlqSinkActivity)
}
