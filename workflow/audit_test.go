package workflow_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tctest "github.com/yourorg/temporal-common/testing"
	"github.com/yourorg/temporal-common/workflow"
	goworkflow "go.temporal.io/sdk/workflow"
)

// ---------------------------------------------------------------------------
// Capturing AuditWriter for tests
// ---------------------------------------------------------------------------

type capturingWriter struct {
	mu      sync.Mutex
	entries []workflow.AuditEntry
}

func (c *capturingWriter) Write(_ goworkflow.Context, entry workflow.AuditEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, entry)
}

func (c *capturingWriter) Entries() []workflow.AuditEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]workflow.AuditEntry, len(c.entries))
	copy(cp, c.entries)
	return cp
}

// ---------------------------------------------------------------------------
// Test activities & workflows
// ---------------------------------------------------------------------------

func saveAuditActivity(_ context.Context, _ workflow.AuditEntry) error { return nil }

// auditStepWorkflow calls AuditStep and returns the captured entry count via result.
func auditStepWorkflow(ctx goworkflow.Context) (int, error) {
	writer := &capturingWriter{}
	ctx = workflow.WithAuditTrail(ctx, writer)

	workflow.AuditStep(ctx, "TransferFunds", "ok", map[string]string{"amount": "1000"})
	workflow.AuditStep(ctx, "NotifyBorrower", "ok", nil)

	return len(writer.Entries()), nil
}

func auditApprovalWorkflow(ctx goworkflow.Context) (string, error) {
	writer := &capturingWriter{}
	ctx = workflow.WithAuditTrail(ctx, writer)

	workflow.AuditApproval(ctx, "LoanApproval", true)

	entries := writer.Entries()
	if len(entries) == 0 {
		return "", nil
	}
	return entries[0].Outcome, nil
}

func auditRejectionWorkflow(ctx goworkflow.Context) (string, error) {
	writer := &capturingWriter{}
	ctx = workflow.WithAuditTrail(ctx, writer)

	workflow.AuditApproval(ctx, "LoanApproval", false)

	entries := writer.Entries()
	if len(entries) == 0 {
		return "", nil
	}
	return entries[0].Outcome, nil
}

func auditStageChangeWorkflow(ctx goworkflow.Context) (map[string]string, error) {
	writer := &capturingWriter{}
	ctx = workflow.WithAuditTrail(ctx, writer)

	workflow.AuditStageChange(ctx, "INIT", "PROCESSING")

	entries := writer.Entries()
	if len(entries) == 0 {
		return nil, nil
	}
	return entries[0].Details, nil
}

func auditCustomWorkflow(ctx goworkflow.Context) (workflow.AuditEventKind, error) {
	writer := &capturingWriter{}
	ctx = workflow.WithAuditTrail(ctx, writer)

	workflow.AuditCustom(ctx, "RateLimitHit", "retrying", map[string]string{"limit": "100"})

	entries := writer.Entries()
	if len(entries) == 0 {
		return "", nil
	}
	return entries[0].Kind, nil
}

func auditCompensationWorkflow(ctx goworkflow.Context) (string, error) {
	writer := &capturingWriter{}
	ctx = workflow.WithAuditTrail(ctx, writer)

	workflow.AuditCompensation(ctx, "RollbackTransfer", "ok", nil)

	entries := writer.Entries()
	if len(entries) == 0 {
		return "", nil
	}
	return string(entries[0].Kind), nil
}

func auditWorkflowIDWorkflow(ctx goworkflow.Context) (string, error) {
	writer := &capturingWriter{}
	ctx = workflow.WithAuditTrail(ctx, writer)

	workflow.AuditStep(ctx, "SomeStep", "ok", nil)

	entries := writer.Entries()
	if len(entries) == 0 {
		return "", nil
	}
	return entries[0].WorkflowID, nil
}

func auditTimestampWorkflow(ctx goworkflow.Context) (bool, error) {
	writer := &capturingWriter{}
	ctx = workflow.WithAuditTrail(ctx, writer)

	workflow.AuditStep(ctx, "SomeStep", "ok", nil)

	entries := writer.Entries()
	if len(entries) == 0 {
		return false, nil
	}
	return !entries[0].OccurredAt.IsZero(), nil
}

func auditNoOpWorkflow(ctx goworkflow.Context) (int, error) {
	// No WithAuditTrail — should default to NoOpAuditWriter (no panic).
	workflow.AuditStep(ctx, "Step", "ok", nil)
	return 0, nil
}

func auditActivityBackedWorkflow(ctx goworkflow.Context) error {
	writer := workflow.NewActivityAuditWriter(saveAuditActivity)
	ctx = workflow.WithAuditTrail(ctx, writer)

	ctx2 := goworkflow.WithActivityOptions(ctx, goworkflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
	})
	_ = ctx2 // activity options context won't be used for audit writer options

	workflow.AuditStep(ctx, "PersistStep", "ok", nil)
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAudit_AuditStep_RecordsMultipleEntries(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(auditStepWorkflow)

	var count int
	require.NoError(t, env.GetResult(&count))
	assert.Equal(t, 2, count)
}

func TestAudit_AuditApproval_Approved(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(auditApprovalWorkflow)

	var outcome string
	require.NoError(t, env.GetResult(&outcome))
	assert.Equal(t, "approved", outcome)
}

func TestAudit_AuditApproval_Rejected(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(auditRejectionWorkflow)

	var outcome string
	require.NoError(t, env.GetResult(&outcome))
	assert.Equal(t, "rejected", outcome)
}

func TestAudit_AuditStageChange_SetsDetails(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(auditStageChangeWorkflow)

	var details map[string]string
	require.NoError(t, env.GetResult(&details))
	assert.Equal(t, "INIT", details["from"])
	assert.Equal(t, "PROCESSING", details["to"])
}

func TestAudit_AuditCustom_SetsKind(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(auditCustomWorkflow)

	var kind workflow.AuditEventKind
	require.NoError(t, env.GetResult(&kind))
	assert.Equal(t, workflow.AuditKindCustom, kind)
}

func TestAudit_AuditCompensation_SetsKind(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(auditCompensationWorkflow)

	var kind string
	require.NoError(t, env.GetResult(&kind))
	assert.Equal(t, string(workflow.AuditKindCompensation), kind)
}

func TestAudit_Entry_ContainsWorkflowID(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(auditWorkflowIDWorkflow)

	var wfID string
	require.NoError(t, env.GetResult(&wfID))
	assert.NotEmpty(t, wfID)
}

func TestAudit_Entry_HasTimestamp(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(auditTimestampWorkflow)

	var nonZero bool
	require.NoError(t, env.GetResult(&nonZero))
	assert.True(t, nonZero)
}

func TestAudit_NoOpWriter_DoesNotPanic(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.ExecuteWorkflow(auditNoOpWorkflow)

	var count int
	require.NoError(t, env.GetResult(&count))
	assert.Equal(t, 0, count)
}

func TestAudit_ActivityWriter_SchedulesActivity(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(saveAuditActivity).Return(nil).Times(1)
	env.ExecuteWorkflow(auditActivityBackedWorkflow)

	require.NoError(t, env.GetError())
}

func TestAudit_NoOpWriter_IsNoOp(t *testing.T) {
	// NoOpAuditWriter.Write must not panic even with zero context.
	var noop workflow.NoOpAuditWriter
	// Pass nil context — NoOp should ignore it completely.
	noop.Write(nil, workflow.AuditEntry{Step: "test", Outcome: "ok"})
}
