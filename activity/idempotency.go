package activity

import (
	"fmt"

	"go.temporal.io/sdk/activity"
)

// IdempotencyKey returns a unique key per workflow + activity + attempt.
//
// Format: "{WorkflowID}/{ActivityID}/{Attempt}"
//
// Use when the downstream system is NOT idempotent — each retry attempt
// should be treated as a distinct request. This prevents double-execution
// if the activity succeeds but the response is lost before Temporal records it.
func IdempotencyKey(info activity.Info) string {
	return fmt.Sprintf("%s/%s/%d",
		info.WorkflowExecution.ID,
		info.ActivityID,
		info.Attempt,
	)
}

// IdempotencyKeyNoRetry returns a stable key across all retry attempts
// for the same workflow + activity.
//
// Format: "{WorkflowID}/{ActivityID}"
//
// Use when the downstream system IS idempotent and deduplicates by key —
// retries should reuse the same key so the system returns the cached result.
// Example: payment gateways that accept an idempotency-key header.
func IdempotencyKeyNoRetry(info activity.Info) string {
	return fmt.Sprintf("%s/%s",
		info.WorkflowExecution.ID,
		info.ActivityID,
	)
}
