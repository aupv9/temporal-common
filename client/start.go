package client

import (
	"context"
	"errors"
	"fmt"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

// WorkflowHandle is a lightweight reference to a running or completed workflow execution.
type WorkflowHandle struct {
	// WorkflowID is the business-level workflow identifier.
	WorkflowID string

	// RunID is the specific execution run ID.
	RunID string

	// AlreadyRunning is true when an existing execution was returned
	// rather than a new one being started.
	AlreadyRunning bool
}

// StartOrAttach starts a workflow with the given workflowID, or returns a handle
// to the already-running execution if one exists for that ID.
//
// Semantics:
//   - First call: starts a new execution, returns AlreadyRunning=false.
//   - Subsequent calls with the same workflowID while workflow is running:
//     returns handle to existing execution with AlreadyRunning=true. NOT re-started.
//   - If the previous execution completed (success or failure), a new execution starts.
//
// This prevents double-execution from API retries or at-least-once delivery.
// Use workflowID = "<entity-type>-<entity-id>" for natural deduplication,
// e.g. "loan-disbursement-loan-123".
func StartOrAttach(
	ctx context.Context,
	c client.Client,
	workflowID string,
	taskQueue string,
	workflowFn interface{},
	args ...interface{},
) (WorkflowHandle, error) {
	return StartOrAttachWithOptions(ctx, c, workflowID, client.StartWorkflowOptions{
		TaskQueue: taskQueue,
	}, workflowFn, args...)
}

// StartOrAttachWithOptions is like StartOrAttach but accepts full StartWorkflowOptions.
// WorkflowID in opts is overridden by the workflowID argument.
// WorkflowIDReusePolicy is always set to AllowDuplicate to preserve idempotency semantics.
func StartOrAttachWithOptions(
	ctx context.Context,
	c client.Client,
	workflowID string,
	opts client.StartWorkflowOptions,
	workflowFn interface{},
	args ...interface{},
) (WorkflowHandle, error) {
	if workflowID == "" {
		return WorkflowHandle{}, fmt.Errorf("StartOrAttach: workflowID is required")
	}

	// Always set WorkflowID from the explicit argument.
	opts.ID = workflowID
	// AllowDuplicate: start new if previous completed; reuse if currently running.
	opts.WorkflowIDReusePolicy = enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE

	we, err := c.ExecuteWorkflow(ctx, opts, workflowFn, args...)
	if err == nil {
		return WorkflowHandle{
			WorkflowID:     we.GetID(),
			RunID:          we.GetRunID(),
			AlreadyRunning: false,
		}, nil
	}

	// Temporal returns WorkflowExecutionAlreadyStarted when the workflow is running.
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	if !errors.As(err, &alreadyStarted) {
		return WorkflowHandle{}, fmt.Errorf("start workflow %q: %w", workflowID, err)
	}

	// Fetch the current RunID of the existing execution.
	resp, descErr := c.DescribeWorkflowExecution(ctx, workflowID, "")
	if descErr != nil {
		return WorkflowHandle{}, fmt.Errorf(
			"workflow %q already running but describe failed: %w", workflowID, descErr)
	}

	return WorkflowHandle{
		WorkflowID:     workflowID,
		RunID:          resp.WorkflowExecutionInfo.Execution.RunId,
		AlreadyRunning: true,
	}, nil
}
