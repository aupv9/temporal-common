# Workflow Versioning

## Rule

When modifying a workflow that has **running instances**, always use `GetVersion` via `ChangeSet`. Never modify the flow directly — old replays will diverge and panic.

## API

```go
changes := temporalcommon.NewChangeSet(ctx)
changes.Define("add-fraud-check-2024-03", 1)   // must be at top of workflow
changes.Define("new-notify-step-2024-06", 1)   // before any branching

if changes.IsEnabled("add-fraud-check-2024-03") {
    // new path for new executions
}
// old path for existing running instances (DefaultVersion)
```

## changeID Naming Convention

Format: `{description}-{YYYY-MM}`

Examples:
- `add-kyc-score-check-2024-01`
- `replace-sms-with-push-2024-06`
- `remove-legacy-fee-step-2025-01`

Rules:
- Must be globally unique within a workflow type
- Never reuse a changeID, even after cleanup
- Must be descriptive enough to understand without context

## Call Order Is Deterministic

All `Define` calls must happen in the **same order** on every replay. Put them all at the top of the workflow, before any `if`, `ExecuteActivity`, or `workflow.Sleep`.

```go
// ✅ Correct
func MyWorkflow(ctx workflow.Context, input Input) error {
    changes := temporalcommon.NewChangeSet(ctx)
    changes.Define("change-a", 1)
    changes.Define("change-b", 1)
    // ... rest of workflow

// ❌ Wrong — conditional Define breaks determinism
    if someCondition {
        changes.Define("change-a", 1)  // may not be called on replay
    }
```

## Multi-Version Migration

For changes that need intermediate versions:

```go
changes.Define("refactor-payment-step", 2)

switch changes.Version("refactor-payment-step") {
case workflow.DefaultVersion:
    // original code path
case 1:
    // intermediate version
case 2:
    // current version
}
```

## Cleanup Checklist

Once all old instances with `DefaultVersion` have drained:

1. Confirm via Temporal UI / `tctl` that no running instances use the old path
2. Remove the `changes.Define(...)` call
3. Remove the `if changes.IsEnabled(...)` / `switch` block — keep only the new path
4. Do NOT reuse the changeID in future changes
5. Update this file with the cleanup date

## When to Create a New Workflow Type Instead

Prefer a new workflow type (e.g. `LoanDisbursementWorkflowV2`) when:
- More than 3 active version branches exist in the workflow
- The new flow is fundamentally different (different saga shape)
- The migration would require more than 5 `GetVersion` calls
