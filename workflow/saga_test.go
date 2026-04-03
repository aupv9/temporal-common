package workflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tctest "github.com/yourorg/temporal-common/testing"
	"github.com/yourorg/temporal-common/workflow"
	goworkflow "go.temporal.io/sdk/workflow"
)

// ---------------------------------------------------------------------------
// Stub activities used only in saga tests.
// Real implementations live in consuming services.
// ---------------------------------------------------------------------------

func stepAActivity(_ context.Context) (string, error)          { return "result-a", nil }
func stepBActivity(_ context.Context) (string, error)          { return "result-b", nil }
func stepCActivity(_ context.Context) (string, error)          { return "result-c", nil }
func rollbackAActivity(_ context.Context, _ string) error      { return nil }
func rollbackBActivity(_ context.Context, _ string) error      { return nil }
func rollbackCActivity(_ context.Context, _ string) error      { return nil }

// activityOptions provides minimal timeouts so the test env does not complain.
var activityOptions = goworkflow.ActivityOptions{StartToCloseTimeout: 5 * time.Second}

// ---------------------------------------------------------------------------
// Test workflows
// ---------------------------------------------------------------------------

// sagaHappyWorkflow: all steps succeed → saga.Complete() → no compensation.
func sagaHappyWorkflow(ctx goworkflow.Context) error {
	s := workflow.NewSaga(ctx)
	defer s.Compensate()

	ctx = goworkflow.WithActivityOptions(ctx, activityOptions)

	var a string
	if err := goworkflow.ExecuteActivity(ctx, stepAActivity).Get(ctx, &a); err != nil {
		return err
	}
	s.AddCompensation(rollbackAActivity, a)

	if err := goworkflow.ExecuteActivity(ctx, stepBActivity).Get(ctx, nil); err != nil {
		return err
	}
	s.AddCompensation(rollbackBActivity, "result-b")

	s.Complete()
	return nil
}

// sagaFailAfterAWorkflow: stepB fails → compensation runs rollbackA (only A was registered).
func sagaFailAfterAWorkflow(ctx goworkflow.Context) error {
	s := workflow.NewSaga(ctx)
	defer s.Compensate()

	ctx = goworkflow.WithActivityOptions(ctx, activityOptions)

	var a string
	if err := goworkflow.ExecuteActivity(ctx, stepAActivity).Get(ctx, &a); err != nil {
		return err
	}
	s.AddCompensation(rollbackAActivity, a)

	// stepB is mocked to fail in tests.
	if err := goworkflow.ExecuteActivity(ctx, stepBActivity).Get(ctx, nil); err != nil {
		return err // triggers Compensate: rollbackA runs
	}
	s.Complete()
	return nil
}

// sagaTwoStepCompensateWorkflow: A and B succeed, C fails → LIFO: rollbackB then rollbackA.
func sagaTwoStepCompensateWorkflow(ctx goworkflow.Context) error {
	s := workflow.NewSaga(ctx)
	defer s.Compensate()

	ctx = goworkflow.WithActivityOptions(ctx, activityOptions)

	var a string
	if err := goworkflow.ExecuteActivity(ctx, stepAActivity).Get(ctx, &a); err != nil {
		return err
	}
	s.AddCompensation(rollbackAActivity, a)

	var b string
	if err := goworkflow.ExecuteActivity(ctx, stepBActivity).Get(ctx, &b); err != nil {
		return err
	}
	s.AddCompensation(rollbackBActivity, b)

	// stepC fails → LIFO compensation: rollbackB, rollbackA
	if err := goworkflow.ExecuteActivity(ctx, stepCActivity).Get(ctx, nil); err != nil {
		return err
	}
	s.Complete()
	return nil
}

// sagaPivotWorkflow: pivot set after stepB. If stepC fails, rollbackB and rollbackA still run.
func sagaPivotWorkflow(ctx goworkflow.Context) error {
	s := workflow.NewSaga(ctx)
	defer s.Compensate()

	ctx = goworkflow.WithActivityOptions(ctx, activityOptions)

	var a string
	if err := goworkflow.ExecuteActivity(ctx, stepAActivity).Get(ctx, &a); err != nil {
		return err
	}
	s.AddCompensation(rollbackAActivity, a)

	var b string
	if err := goworkflow.ExecuteActivity(ctx, stepBActivity).Get(ctx, &b); err != nil {
		return err
	}
	s.AddCompensation(rollbackBActivity, b)

	s.SetPivot() // funds committed — point of no return

	// stepC fails post-pivot
	if err := goworkflow.ExecuteActivity(ctx, stepCActivity).Get(ctx, nil); err != nil {
		return err
	}
	s.AddCompensation(rollbackCActivity, "result-c")

	s.Complete()
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSaga_HappyPath_NoCompensation(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()
	env.TrackCompensations()

	env.MockActivity(stepAActivity).Return("result-a", nil)
	env.MockActivity(stepBActivity).Return("result-b", nil)

	env.ExecuteWorkflow(sagaHappyWorkflow)

	env.AssertWorkflowCompleted(t)
	env.AssertActivityNotCalled(t, rollbackAActivity)
	env.AssertActivityNotCalled(t, rollbackBActivity)
}

func TestSaga_FailAfterOneStep_CompensatesOnce(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()
	env.TrackCompensations()

	env.MockActivity(stepAActivity).Return("result-a", nil)
	env.MockActivity(stepBActivity).ReturnError(errors.New("step B failed"))
	env.MockActivity(rollbackAActivity).Return(nil)

	env.ExecuteWorkflow(sagaFailAfterAWorkflow)

	env.AssertWorkflowFailed(t, "step B failed")
	env.AssertActivityCalled(t, rollbackAActivity)
	env.AssertActivityNotCalled(t, rollbackBActivity)
}

func TestSaga_LIFO_CompensationOrder(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()
	env.TrackCompensations()

	env.MockActivity(stepAActivity).Return("result-a", nil)
	env.MockActivity(stepBActivity).Return("result-b", nil)
	env.MockActivity(stepCActivity).ReturnError(errors.New("step C failed"))
	env.MockActivity(rollbackBActivity).Return(nil)
	env.MockActivity(rollbackAActivity).Return(nil)

	env.ExecuteWorkflow(sagaTwoStepCompensateWorkflow)

	env.AssertWorkflowFailed(t, "step C failed")

	// LIFO: B registered second → compensated first; A compensated second.
	env.AssertActivityCalled(t, rollbackAActivity)
	env.AssertActivityCalled(t, rollbackBActivity)
	assert.Equal(t, 2, env.ActivityCallCount(rollbackAActivity)+env.ActivityCallCount(rollbackBActivity))
}

func TestSaga_PivotPoint_CompensatesAroundPivot(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()
	env.TrackCompensations()

	env.MockActivity(stepAActivity).Return("result-a", nil)
	env.MockActivity(stepBActivity).Return("result-b", nil)
	env.MockActivity(stepCActivity).ReturnError(errors.New("step C failed post-pivot"))
	env.MockActivity(rollbackBActivity).Return(nil)
	env.MockActivity(rollbackAActivity).Return(nil)

	env.ExecuteWorkflow(sagaPivotWorkflow)

	// Workflow should fail — compensation runs for steps before pivot (B and A).
	env.AssertWorkflowFailed(t, "")
	env.AssertActivityCalled(t, rollbackAActivity)
	env.AssertActivityCalled(t, rollbackBActivity)
	// rollbackC was never registered (stepC failed before AddCompensation)
	env.AssertActivityNotCalled(t, rollbackCActivity)
}

func TestSaga_NoSteps_CompensateIsNoop(t *testing.T) {
	emptyWorkflow := func(ctx goworkflow.Context) error {
		s := workflow.NewSaga(ctx)
		defer s.Compensate()
		// no steps registered — Compensate must be a silent no-op
		return errors.New("early exit")
	}

	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(emptyWorkflow)
	env.AssertWorkflowFailed(t, "early exit")
}

func TestSaga_Complete_PreventsCompensation(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()
	env.TrackCompensations()

	env.MockActivity(stepAActivity).Return("result-a", nil)
	env.MockActivity(stepBActivity).Return("result-b", nil)

	env.ExecuteWorkflow(sagaHappyWorkflow)

	require.NoError(t, env.GetError())
	// Complete() was called → compensations must NOT run
	env.AssertActivityNotCalled(t, rollbackAActivity)
	env.AssertActivityNotCalled(t, rollbackBActivity)
}
