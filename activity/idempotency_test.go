package activity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	goactivity "go.temporal.io/sdk/activity"

	tc "github.com/yourorg/temporal-common/activity"
)

// makeInfo builds an activity.Info for testing without referencing internal types.
// Fields are set via dot access — Go allows this even for aliased internal structs.
func makeInfo(workflowID, activityID string, attempt int32) goactivity.Info {
	var info goactivity.Info
	info.WorkflowExecution.ID = workflowID
	info.ActivityID = activityID
	info.Attempt = attempt
	return info
}

func TestIdempotencyKey_Format(t *testing.T) {
	info := makeInfo("wf-123", "transfer-act", 2)
	assert.Equal(t, "wf-123/transfer-act/2", tc.IdempotencyKey(info))
}

func TestIdempotencyKey_FirstAttempt(t *testing.T) {
	info := makeInfo("wf-abc", "credit-act", 1)
	assert.Equal(t, "wf-abc/credit-act/1", tc.IdempotencyKey(info))
}

func TestIdempotencyKey_UniqueAcrossAttempts(t *testing.T) {
	info1 := makeInfo("wf-1", "act", 1)
	info2 := makeInfo("wf-1", "act", 2)
	assert.NotEqual(t, tc.IdempotencyKey(info1), tc.IdempotencyKey(info2),
		"each retry attempt must produce a different key")
}

func TestIdempotencyKey_UniqueAcrossWorkflows(t *testing.T) {
	info1 := makeInfo("wf-1", "act", 1)
	info2 := makeInfo("wf-2", "act", 1)
	assert.NotEqual(t, tc.IdempotencyKey(info1), tc.IdempotencyKey(info2))
}

func TestIdempotencyKey_UniqueAcrossActivities(t *testing.T) {
	info1 := makeInfo("wf-1", "act-A", 1)
	info2 := makeInfo("wf-1", "act-B", 1)
	assert.NotEqual(t, tc.IdempotencyKey(info1), tc.IdempotencyKey(info2))
}

func TestIdempotencyKeyNoRetry_Format(t *testing.T) {
	info := makeInfo("wf-123", "transfer-act", 3)
	assert.Equal(t, "wf-123/transfer-act", tc.IdempotencyKeyNoRetry(info))
}

func TestIdempotencyKeyNoRetry_StableAcrossAttempts(t *testing.T) {
	info1 := makeInfo("wf-1", "act", 1)
	info2 := makeInfo("wf-1", "act", 2)
	info3 := makeInfo("wf-1", "act", 99)
	key1 := tc.IdempotencyKeyNoRetry(info1)
	assert.Equal(t, key1, tc.IdempotencyKeyNoRetry(info2), "key must be stable across retries")
	assert.Equal(t, key1, tc.IdempotencyKeyNoRetry(info3), "key must be stable across retries")
}

func TestIdempotencyKeyNoRetry_UniqueAcrossWorkflows(t *testing.T) {
	info1 := makeInfo("wf-1", "act", 1)
	info2 := makeInfo("wf-2", "act", 1)
	assert.NotEqual(t, tc.IdempotencyKeyNoRetry(info1), tc.IdempotencyKeyNoRetry(info2))
}

func TestIdempotencyKey_DiffersFromNoRetry(t *testing.T) {
	// IdempotencyKey and IdempotencyKeyNoRetry must produce different strings.
	info := makeInfo("wf-1", "act", 1)
	assert.NotEqual(t, tc.IdempotencyKey(info), tc.IdempotencyKeyNoRetry(info))
}
