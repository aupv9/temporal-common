package testing

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
)

// AssertActivityCalled asserts that the given activity was executed at least once.
func (te *TestEnvironment) AssertActivityCalled(t *testing.T, activityFn interface{}) {
	t.Helper()
	name := activityName(activityFn)
	for _, executed := range te.executedActivities {
		if executed == name {
			return
		}
	}
	t.Errorf("expected activity %q to be called, but it was not\n%s", name, te)
}

// AssertActivityNotCalled asserts that the given activity was never executed.
func (te *TestEnvironment) AssertActivityNotCalled(t *testing.T, activityFn interface{}) {
	t.Helper()
	name := activityName(activityFn)
	for _, executed := range te.executedActivities {
		if executed == name {
			t.Errorf("expected activity %q NOT to be called, but it was\n%s", name, te)
			return
		}
	}
}

// AssertCompensationOrder asserts that compensation activities ran in the given order.
// Pass activities in the expected execution order (LIFO of AddCompensation order).
//
// Example — saga.AddCompensation(A, B, C) → compensations execute C, B, A:
//
//	env.AssertCompensationOrder(t, CActivity, BActivity, AActivity)
func (te *TestEnvironment) AssertCompensationOrder(t *testing.T, activityFns ...interface{}) {
	t.Helper()
	if len(activityFns) == 0 {
		return
	}

	expected := make([]string, len(activityFns))
	for i, fn := range activityFns {
		expected[i] = activityName(fn)
	}

	comp := te.compensationOrder
	if len(comp) < len(expected) {
		t.Errorf("compensation order: expected %d steps %v, got %d steps %v",
			len(expected), expected, len(comp), comp)
		return
	}

	for i, want := range expected {
		if comp[i] != want {
			t.Errorf("compensation order[%d]: expected %q, got %q\nfull order: %v",
				i, want, comp[i], comp)
			return
		}
	}
}

// TrackCompensations installs an activity-started listener that records
// all executed activities for compensation ordering assertions.
// Replaces the default listener — call before ExecuteWorkflow.
func (te *TestEnvironment) TrackCompensations() {
	te.env.SetOnActivityStartedListener(func(info *sdkactivity.Info, _ context.Context, _ converter.EncodedValues) {
		name := info.ActivityType.Name
		te.executedActivities = append(te.executedActivities, name)
		te.compensationOrder = append(te.compensationOrder, name)
	})
}

// AssertWorkflowCompleted asserts the workflow finished without error.
func (te *TestEnvironment) AssertWorkflowCompleted(t *testing.T) {
	t.Helper()
	if err := te.GetError(); err != nil {
		t.Errorf("expected workflow to complete successfully, got error: %v", err)
	}
}

// AssertWorkflowFailed asserts the workflow finished with an error whose message
// contains msgContains (pass "" to assert any failure).
func (te *TestEnvironment) AssertWorkflowFailed(t *testing.T, msgContains string) {
	t.Helper()
	err := te.GetError()
	if err == nil {
		t.Error("expected workflow to fail, but it completed successfully")
		return
	}
	if msgContains != "" && !strings.Contains(err.Error(), msgContains) {
		t.Errorf("expected workflow error to contain %q, got: %v", msgContains, err)
	}
}

// ActivityCallCount returns how many times the given activity was called.
func (te *TestEnvironment) ActivityCallCount(activityFn interface{}) int {
	name := activityName(activityFn)
	count := 0
	for _, executed := range te.executedActivities {
		if executed == name {
			count++
		}
	}
	return count
}

// String returns a diagnostic summary of executed activities.
func (te *TestEnvironment) String() string {
	return fmt.Sprintf("executed activities: %v\ncompensation order: %v",
		te.executedActivities, te.compensationOrder)
}

// activityName extracts the short function name from an activity function pointer.
func activityName(fn interface{}) string {
	full := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	if idx := strings.LastIndex(full, "."); idx >= 0 {
		return full[idx+1:]
	}
	return full
}
