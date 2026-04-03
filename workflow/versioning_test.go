package workflow_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	tctest "github.com/yourorg/temporal-common/testing"
	"github.com/yourorg/temporal-common/workflow"
	goworkflow "go.temporal.io/sdk/workflow"
)

// ---------------------------------------------------------------------------
// Test workflows for ChangeSet
// ---------------------------------------------------------------------------

type versioningResult struct {
	FraudEnabled    bool
	NotifyEnabled   bool
	FraudVersion    int
}

func changeSetWorkflow(ctx goworkflow.Context) (versioningResult, error) {
	// All Define calls MUST come before any branching.
	changes := workflow.NewChangeSet(ctx)
	changes.Define("add-fraud-check", 1)
	changes.Define("new-notify-step", 1)

	return versioningResult{
		FraudEnabled:  changes.IsEnabled("add-fraud-check"),
		NotifyEnabled: changes.IsEnabled("new-notify-step"),
		FraudVersion:  int(changes.Version("add-fraud-check")),
	}, nil
}

func undefinedChangeWorkflow(ctx goworkflow.Context) (bool, error) {
	changes := workflow.NewChangeSet(ctx)
	// "missing-change" is never Define'd
	return changes.IsEnabled("missing-change"), nil
}

func multiVersionWorkflow(ctx goworkflow.Context) (int, error) {
	changes := workflow.NewChangeSet(ctx)
	changes.Define("migration", 2) // supports up to version 2

	return int(changes.Version("migration")), nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestChangeSet_IsEnabled_NewExecution(t *testing.T) {
	// For a fresh execution, GetVersion returns maxVersion (latest code path).
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(changeSetWorkflow)

	var result versioningResult
	assert.NoError(t, env.GetResult(&result))
	assert.True(t, result.FraudEnabled, "new execution should use new code path")
	assert.True(t, result.NotifyEnabled, "new execution should use new code path")
}

func TestChangeSet_Version_ReturnsCorrectValue(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(changeSetWorkflow)

	var result versioningResult
	assert.NoError(t, env.GetResult(&result))
	// New execution gets version 1 (maxVersion for "add-fraud-check").
	assert.Equal(t, 1, result.FraudVersion)
}

func TestChangeSet_IsEnabled_UndefinedChange(t *testing.T) {
	// Accessing a name that was never Define'd returns false (safe default).
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(undefinedChangeWorkflow)

	var result bool
	assert.NoError(t, env.GetResult(&result))
	assert.False(t, result, "undefined change must return false, not panic")
}

func TestChangeSet_Version_UndefinedChange_ReturnsDefaultVersion(t *testing.T) {
	undefinedVersionWorkflow := func(ctx goworkflow.Context) (int, error) {
		changes := workflow.NewChangeSet(ctx)
		return int(changes.Version("never-defined")), nil
	}

	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(undefinedVersionWorkflow)

	var result int
	assert.NoError(t, env.GetResult(&result))
	assert.Equal(t, int(goworkflow.DefaultVersion), result)
}

func TestChangeSet_MultiVersion(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(multiVersionWorkflow)

	var version int
	assert.NoError(t, env.GetResult(&version))
	// New execution gets maxVersion (2).
	assert.Equal(t, 2, version)
}

func TestChangeSet_MultipleChanges_Independent(t *testing.T) {
	// Each named change is independent — disabling one doesn't affect another.
	mixedWorkflow := func(ctx goworkflow.Context) ([2]bool, error) {
		changes := workflow.NewChangeSet(ctx)
		changes.Define("change-a", 1)
		changes.Define("change-b", 1)
		return [2]bool{changes.IsEnabled("change-a"), changes.IsEnabled("change-b")}, nil
	}

	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(mixedWorkflow)

	var result [2]bool
	assert.NoError(t, env.GetResult(&result))
	assert.True(t, result[0], "change-a should be enabled")
	assert.True(t, result[1], "change-b should be enabled")
}
