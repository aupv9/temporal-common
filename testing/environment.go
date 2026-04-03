package testing

import (
	"context"
	"testing"
	"time"

	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
)

// TestEnvironment wraps testsuite.TestWorkflowEnvironment with helpers for
// saga compensation ordering assertions and fluent mock setup.
//
// Usage:
//
//	func TestMyWorkflow(t *testing.T) {
//	    env := testing.NewTestEnvironment(t)
//	    defer env.Cleanup()
//
//	    env.MockActivity(MyActivity).Return(MyResult{}, nil)
//	    env.ExecuteWorkflow(MyWorkflow, MyInput{})
//
//	    env.AssertActivityCalled(t, MyActivity)
//	}
type TestEnvironment struct {
	suite testsuite.WorkflowTestSuite
	env   *testsuite.TestWorkflowEnvironment
	t     *testing.T

	// executedActivities tracks all activity names in execution order.
	executedActivities []string

	// compensationOrder tracks compensation activity names in execution order.
	compensationOrder []string
}

// NewTestEnvironment creates a TestEnvironment and initialises the underlying
// Temporal test workflow environment. An activity-started listener is installed
// automatically to support AssertActivityCalled and AssertCompensationOrder.
func NewTestEnvironment(t *testing.T) *TestEnvironment {
	te := &TestEnvironment{t: t}
	te.env = te.suite.NewTestWorkflowEnvironment()

	// Auto-install tracking listener.
	te.env.SetOnActivityStartedListener(func(info *sdkactivity.Info, _ context.Context, _ converter.EncodedValues) {
		te.executedActivities = append(te.executedActivities, info.ActivityType.Name)
	})

	return te
}

// Env returns the raw *testsuite.TestWorkflowEnvironment for advanced usage.
func (te *TestEnvironment) Env() *testsuite.TestWorkflowEnvironment {
	return te.env
}

// ExecuteWorkflow runs the given workflow function synchronously (test mode)
// with the provided arguments. Call GetResult or GetError after this.
func (te *TestEnvironment) ExecuteWorkflow(workflowFn interface{}, args ...interface{}) {
	te.env.ExecuteWorkflow(workflowFn, args...)
}

// GetResult deserialises the workflow result into valuePtr.
// Returns an error if the workflow failed.
func (te *TestEnvironment) GetResult(valuePtr interface{}) error {
	return te.env.GetWorkflowResult(valuePtr)
}

// GetError returns the workflow's terminal error, or nil on success.
func (te *TestEnvironment) GetError() error {
	return te.env.GetWorkflowError()
}

// RegisterDelayedCallback schedules a callback at a workflow-time offset.
// Use this to send signals or fire events mid-workflow during tests.
//
// Example — send approval signal 1 hour into workflow time:
//
//	env.RegisterDelayedCallback(func() {
//	    env.Env().SignalWorkflow(workflow.ApprovalSignalName, ApprovalResult{Approved: true})
//	}, time.Hour)
func (te *TestEnvironment) RegisterDelayedCallback(callback func(), d time.Duration) {
	te.env.RegisterDelayedCallback(callback, d)
}

// Cleanup finalises the test environment. Call with defer immediately after NewTestEnvironment.
func (te *TestEnvironment) Cleanup() {
	te.env.AssertExpectations(te.t)
}
