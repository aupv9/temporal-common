package workflow_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tctest "github.com/yourorg/temporal-common/testing"
	"github.com/yourorg/temporal-common/workflow"
	goworkflow "go.temporal.io/sdk/workflow"
)

// ---------------------------------------------------------------------------
// Test workflow that calls WaitForApproval
// ---------------------------------------------------------------------------

type approvalWorkflowResult struct {
	Approved   bool
	ApproverID string
	Comment    string
	TimedOut   bool
}

func approvalWorkflow(ctx goworkflow.Context, req workflow.ApprovalRequest) (approvalWorkflowResult, error) {
	result, err := workflow.WaitForApproval(ctx, req)
	if err != nil {
		return approvalWorkflowResult{}, err
	}
	return approvalWorkflowResult{
		Approved:   result.Approved,
		ApproverID: result.ApproverID,
		Comment:    result.Comment,
		TimedOut:   result.TimedOut,
	}, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestWaitForApproval_Approved(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	req := workflow.ApprovalRequest{
		ApprovalID:  "loan-123",
		Description: "Approve large loan",
		Timeout:     48 * time.Hour,
	}

	// Send approval signal after 1 hour of workflow time.
	env.RegisterDelayedCallback(func() {
		env.Env().SignalWorkflow(workflow.ApprovalSignalName, workflow.ApprovalResult{
			Approved:   true,
			ApproverID: "manager@acme.com",
			Comment:    "looks good",
		})
	}, time.Hour)

	env.ExecuteWorkflow(approvalWorkflow, req)

	var result approvalWorkflowResult
	require.NoError(t, env.GetResult(&result))
	assert.True(t, result.Approved)
	assert.Equal(t, "manager@acme.com", result.ApproverID)
	assert.Equal(t, "looks good", result.Comment)
	assert.False(t, result.TimedOut)
}

func TestWaitForApproval_Rejected(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	req := workflow.ApprovalRequest{
		ApprovalID: "loan-456",
		Timeout:    48 * time.Hour,
	}

	env.RegisterDelayedCallback(func() {
		env.Env().SignalWorkflow(workflow.ApprovalSignalName, workflow.ApprovalResult{
			Approved:   false,
			ApproverID: "manager@acme.com",
			Comment:    "risk too high",
		})
	}, 2*time.Hour)

	env.ExecuteWorkflow(approvalWorkflow, req)

	var result approvalWorkflowResult
	require.NoError(t, env.GetResult(&result))
	assert.False(t, result.Approved)
	assert.Equal(t, "risk too high", result.Comment)
	assert.False(t, result.TimedOut)
}

func TestWaitForApproval_Timeout(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	// Timeout set to 1 hour; no signal sent → timeout fires.
	req := workflow.ApprovalRequest{
		ApprovalID: "loan-789",
		Timeout:    time.Hour,
	}

	// No RegisterDelayedCallback — timeout must fire naturally.
	env.ExecuteWorkflow(approvalWorkflow, req)

	var result approvalWorkflowResult
	require.NoError(t, env.GetResult(&result), "timeout is not an error — workflow completes normally")
	assert.False(t, result.Approved)
	assert.True(t, result.TimedOut)
}

func TestWaitForApproval_MissingApprovalID_ReturnsError(t *testing.T) {
	badReqWorkflow := func(ctx goworkflow.Context) error {
		_, err := workflow.WaitForApproval(ctx, workflow.ApprovalRequest{
			ApprovalID: "", // missing
			Timeout:    time.Hour,
		})
		return err
	}

	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(badReqWorkflow)
	env.AssertWorkflowFailed(t, "ApprovalID is required")
}

func TestWaitForApproval_ZeroTimeout_ReturnsError(t *testing.T) {
	badTimeoutWorkflow := func(ctx goworkflow.Context) error {
		_, err := workflow.WaitForApproval(ctx, workflow.ApprovalRequest{
			ApprovalID: "loan-000",
			Timeout:    0, // invalid
		})
		return err
	}

	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(badTimeoutWorkflow)
	env.AssertWorkflowFailed(t, "Timeout must be positive")
}

func TestWaitForApproval_SignalBeforeTimeout(t *testing.T) {
	// Signal arrives well before the timeout — timer must be cancelled implicitly.
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	req := workflow.ApprovalRequest{
		ApprovalID: "loan-early",
		Timeout:    24 * time.Hour,
	}

	// Signal at 1 minute — far before 24h timeout.
	env.RegisterDelayedCallback(func() {
		env.Env().SignalWorkflow(workflow.ApprovalSignalName, workflow.ApprovalResult{
			Approved:   true,
			ApproverID: "quick-approver",
		})
	}, time.Minute)

	env.ExecuteWorkflow(approvalWorkflow, req)

	var result approvalWorkflowResult
	require.NoError(t, env.GetResult(&result))
	assert.True(t, result.Approved)
	assert.False(t, result.TimedOut)
}
