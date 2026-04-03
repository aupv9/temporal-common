package workflow

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"
)

const approvalSignalName = "approval-signal"

// ApprovalRequest describes a human approval gate inside a workflow.
type ApprovalRequest struct {
	// ApprovalID is the unique identifier for this approval (e.g. loan ID, order ID).
	ApprovalID string

	// Description is a human-readable label shown in approval UIs.
	Description string

	// Timeout is how long to wait before treating no response as a rejection.
	// Use 48*time.Hour for standard business approval SLAs.
	Timeout time.Duration

	// Metadata is arbitrary key-value data forwarded to the approval system.
	Metadata map[string]string
}

// ApprovalResult is the payload delivered via the approval signal.
type ApprovalResult struct {
	// Approved is true when the approver accepted, false for rejection or timeout.
	Approved bool

	// ApproverID identifies who acted on the request (user ID, service account).
	ApproverID string

	// Comment is an optional free-text note from the approver.
	Comment string

	// ApprovedAt is the workflow time of the approval decision (deterministic).
	ApprovedAt time.Time

	// TimedOut is true when the approval window expired with no response.
	TimedOut bool
}

// WaitForApproval blocks the workflow until an approval signal arrives or the
// timeout elapses. It uses workflow.GetSignalChannel and workflow.NewTimer to
// remain deterministic.
//
// To send an approval signal from outside the workflow:
//
//	client.SignalWorkflow(ctx, workflowID, runID, "approval-signal", ApprovalResult{
//	    Approved:   true,
//	    ApproverID: "manager@example.com",
//	})
//
// Returns an error only on unexpected internal failures — timeout is not an error,
// it returns ApprovalResult{Approved: false, TimedOut: true}.
func WaitForApproval(ctx workflow.Context, req ApprovalRequest) (ApprovalResult, error) {
	if req.ApprovalID == "" {
		return ApprovalResult{}, fmt.Errorf("WaitForApproval: ApprovalID is required")
	}
	if req.Timeout <= 0 {
		return ApprovalResult{}, fmt.Errorf("WaitForApproval: Timeout must be positive")
	}

	logger := workflow.GetLogger(ctx)
	logger.Info("waiting for approval",
		"approvalID", req.ApprovalID,
		"timeout", req.Timeout.String(),
	)

	signalCh := workflow.GetSignalChannel(ctx, approvalSignalName)
	timer := workflow.NewTimer(ctx, req.Timeout)

	var result ApprovalResult
	var timedOut bool

	sel := workflow.NewSelector(ctx)

	sel.AddReceive(signalCh, func(ch workflow.ReceiveChannel, more bool) {
		ch.Receive(ctx, &result)
		// Record deterministic workflow time of decision.
		result.ApprovedAt = workflow.Now(ctx)
	})

	sel.AddFuture(timer, func(f workflow.Future) {
		// Timer fired — approval window expired.
		timedOut = true
	})

	sel.Select(ctx)

	if timedOut {
		logger.Warn("approval timed out",
			"approvalID", req.ApprovalID,
			"timeout", req.Timeout.String(),
		)
		return ApprovalResult{TimedOut: true, Approved: false}, nil
	}

	logger.Info("approval received",
		"approvalID", req.ApprovalID,
		"approved", result.Approved,
		"approverID", result.ApproverID,
	)
	return result, nil
}

// ApprovalSignalName is the Temporal signal name used by WaitForApproval.
// Use this constant when sending signals from outside the workflow to avoid
// magic strings.
const ApprovalSignalName = approvalSignalName
