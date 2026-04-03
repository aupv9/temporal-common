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
// Stub activities for parallel tests
// ---------------------------------------------------------------------------

func enrichAActivity(_ context.Context) (string, error)     { return "enrich-a", nil }
func enrichBActivity(_ context.Context) (string, error)     { return "enrich-b", nil }
func enrichCActivity(_ context.Context) (string, error)     { return "enrich-c", nil }
func enrichFailActivity(_ context.Context) (string, error)  { return "", nil }

// ---------------------------------------------------------------------------
// Test workflows
// ---------------------------------------------------------------------------

type parallelResult struct {
	A string
	B string
	C string
}

func parallelHappyWorkflow(ctx goworkflow.Context) (parallelResult, error) {
	ctx = goworkflow.WithActivityOptions(ctx, goworkflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
	})

	var result parallelResult
	_, err := workflow.ExecuteParallel(ctx, []workflow.ActivityCall{
		{Fn: enrichAActivity, ResultPtr: &result.A},
		{Fn: enrichBActivity, ResultPtr: &result.B},
		{Fn: enrichCActivity, ResultPtr: &result.C},
	})
	return result, err
}

func parallelOneFailWorkflow(ctx goworkflow.Context) error {
	ctx = goworkflow.WithActivityOptions(ctx, goworkflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         nil, // no retry for tests
	})

	_, err := workflow.ExecuteParallel(ctx, []workflow.ActivityCall{
		{Fn: enrichAActivity},
		{Fn: enrichFailActivity},
		{Fn: enrichBActivity},
	})
	return err
}

// bestEffortSummary is a serializable summary of BestEffort results.
type bestEffortSummary struct {
	Total    int
	Failures int
}

func parallelBestEffortWorkflow(ctx goworkflow.Context) (bestEffortSummary, error) {
	ctx = goworkflow.WithActivityOptions(ctx, goworkflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
	})

	results := workflow.ExecuteParallelBestEffort(ctx, []workflow.ActivityCall{
		{Fn: enrichAActivity},
		{Fn: enrichFailActivity},
	})

	summary := bestEffortSummary{Total: len(results)}
	for _, r := range results {
		if r.Err != nil {
			summary.Failures++
		}
	}
	return summary, nil
}

func parallelEmptyWorkflow(ctx goworkflow.Context) error {
	_, err := workflow.ExecuteParallel(ctx, []workflow.ActivityCall{})
	return err
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestExecuteParallel_AllSucceed(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(enrichAActivity).Return("enrich-a", nil)
	env.MockActivity(enrichBActivity).Return("enrich-b", nil)
	env.MockActivity(enrichCActivity).Return("enrich-c", nil)

	env.ExecuteWorkflow(parallelHappyWorkflow)

	var result parallelResult
	require.NoError(t, env.GetResult(&result))
	assert.Equal(t, "enrich-a", result.A)
	assert.Equal(t, "enrich-b", result.B)
	assert.Equal(t, "enrich-c", result.C)
}

func TestExecuteParallel_AllActivitiesExecuted(t *testing.T) {
	// All three activities must be called even if they're running in parallel.
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(enrichAActivity).Return("enrich-a", nil)
	env.MockActivity(enrichBActivity).Return("enrich-b", nil)
	env.MockActivity(enrichCActivity).Return("enrich-c", nil)

	env.ExecuteWorkflow(parallelHappyWorkflow)

	require.NoError(t, env.GetError())
	env.AssertActivityCalled(t, enrichAActivity)
	env.AssertActivityCalled(t, enrichBActivity)
	env.AssertActivityCalled(t, enrichCActivity)
}

func TestExecuteParallel_OneFails_ReturnsError(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(enrichAActivity).Return("enrich-a", nil)
	env.MockActivity(enrichFailActivity).ReturnError(errors.New("enrichment service down"))
	env.MockActivity(enrichBActivity).Return("enrich-b", nil)

	env.ExecuteWorkflow(parallelOneFailWorkflow)

	env.AssertWorkflowFailed(t, "enrichment service down")
}

func TestExecuteParallel_OneFails_OthersStillExecute(t *testing.T) {
	// Even when one activity fails, the others must complete (no early cancellation).
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(enrichAActivity).Return("enrich-a", nil)
	env.MockActivity(enrichFailActivity).ReturnError(errors.New("fail"))
	env.MockActivity(enrichBActivity).Return("enrich-b", nil)

	env.ExecuteWorkflow(parallelOneFailWorkflow)

	env.AssertWorkflowFailed(t, "")
	env.AssertActivityCalled(t, enrichAActivity)
	env.AssertActivityCalled(t, enrichBActivity)
	env.AssertActivityCalled(t, enrichFailActivity)
}

func TestExecuteParallel_Empty_NoError(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(parallelEmptyWorkflow)
	env.AssertWorkflowCompleted(t)
}

func TestExecuteParallelBestEffort_FailureDoesNotPropagateError(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(enrichAActivity).Return("enrich-a", nil)
	env.MockActivity(enrichFailActivity).ReturnError(errors.New("non-critical fail"))

	env.ExecuteWorkflow(parallelBestEffortWorkflow)

	// BestEffort: workflow must complete even if one activity failed.
	var summary bestEffortSummary
	require.NoError(t, env.GetResult(&summary))
	assert.Equal(t, 2, summary.Total)
}

func TestExecuteParallelBestEffort_ErrorsAccessibleInResults(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(enrichAActivity).Return("enrich-a", nil)
	env.MockActivity(enrichFailActivity).ReturnError(errors.New("non-critical fail"))

	env.ExecuteWorkflow(parallelBestEffortWorkflow)

	var summary bestEffortSummary
	require.NoError(t, env.GetResult(&summary))
	assert.Equal(t, 1, summary.Failures, "one failure must be recorded in summary")
}
