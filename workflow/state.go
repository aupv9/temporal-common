package workflow

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// QueryCurrentState is the Temporal query type for fetching WorkflowState.
// Use this constant instead of a magic string at call sites.
const QueryCurrentState = "temporal-common.current-state"

// WorkflowStateSnapshot is the serialisable snapshot returned by the
// QueryCurrentState query handler.
type WorkflowStateSnapshot struct {
	// Stage is the current business stage (e.g. "INIT", "DISBURSING", "COMPLETED").
	Stage string `json:"stage"`

	// StartedAt is the workflow.Now(ctx) time when NewWorkflowState was called.
	StartedAt time.Time `json:"startedAt"`

	// UpdatedAt is the workflow.Now(ctx) time of the last SetStage call.
	UpdatedAt time.Time `json:"updatedAt"`

	// Meta holds arbitrary key-value annotations set via SetMeta.
	Meta map[string]string `json:"meta,omitempty"`
}

// WorkflowState provides a standard query interface for any workflow.
// It registers a QueryCurrentState handler so operations teams can inspect
// workflow progress without digging into Temporal's raw event history.
//
// Usage — at the top of every workflow:
//
//	state := temporalcommon.NewWorkflowState(ctx)
//	state.SetStage("INIT")
//
//	// … after KYC check:
//	state.SetStage("KYC_DONE")
//	state.SetMeta("kycScore", "750")
//
//	// Query from outside the workflow:
//	resp, _ := temporalClient.QueryWorkflow(ctx, wfID, "", temporalcommon.QueryCurrentState)
//	var snap temporalcommon.WorkflowStateSnapshot
//	resp.Get(&snap) // → {Stage:"KYC_DONE", Meta:{"kycScore":"750"}, ...}
type WorkflowState struct {
	ctx      workflow.Context
	snapshot WorkflowStateSnapshot
}

// NewWorkflowState registers the QueryCurrentState handler and returns a
// WorkflowState tracker. Call this at the very top of every workflow function,
// before any activity execution.
func NewWorkflowState(ctx workflow.Context) *WorkflowState {
	now := workflow.Now(ctx)
	ws := &WorkflowState{
		ctx: ctx,
		snapshot: WorkflowStateSnapshot{
			Stage:     "INIT",
			StartedAt: now,
			UpdatedAt: now,
			Meta:      make(map[string]string),
		},
	}

	// Register the query handler once. If registration fails (e.g. duplicate
	// registration in tests), log and continue — state is still tracked in-memory.
	if err := workflow.SetQueryHandler(ctx, QueryCurrentState, func() (WorkflowStateSnapshot, error) {
		return ws.snapshot, nil
	}); err != nil {
		workflow.GetLogger(ctx).Error("WorkflowState: failed to register query handler",
			"error", err,
			"query", QueryCurrentState,
		)
	}

	return ws
}

// SetStage updates the current business stage and records the update time.
// stage should be an uppercase constant meaningful to your domain,
// e.g. "AWAITING_APPROVAL", "DISBURSING", "COMPLETED", "FAILED".
func (ws *WorkflowState) SetStage(stage string) {
	ws.snapshot.Stage = stage
	ws.snapshot.UpdatedAt = workflow.Now(ws.ctx)
}

// SetMeta stores an arbitrary key-value annotation on the state snapshot.
// Use this for structured metadata that doesn't warrant a dedicated field,
// e.g. approver identity, external reference IDs, retry counts.
func (ws *WorkflowState) SetMeta(key, value string) {
	ws.snapshot.Meta[key] = value
	ws.snapshot.UpdatedAt = workflow.Now(ws.ctx)
}

// Stage returns the current stage string.
func (ws *WorkflowState) Stage() string {
	return ws.snapshot.Stage
}

// Snapshot returns a copy of the current state snapshot.
func (ws *WorkflowState) Snapshot() WorkflowStateSnapshot {
	snap := ws.snapshot
	// Deep-copy Meta map so callers can't mutate internal state.
	snap.Meta = make(map[string]string, len(ws.snapshot.Meta))
	for k, v := range ws.snapshot.Meta {
		snap.Meta[k] = v
	}
	return snap
}
