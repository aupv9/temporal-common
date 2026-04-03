package activity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/temporal-common/activity"
)

// ---------------------------------------------------------------------------
// InMemoryIdempotencyStore tests
// ---------------------------------------------------------------------------

func TestInMemoryStore_CheckMiss(t *testing.T) {
	store := activity.NewInMemoryIdempotencyStore()
	_, found, err := store.Check(context.Background(), "missing-key")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestInMemoryStore_RecordAndCheck(t *testing.T) {
	store := activity.NewInMemoryIdempotencyStore()
	ctx := context.Background()

	err := store.Record(ctx, "key-1", []byte(`"result"`), time.Hour)
	require.NoError(t, err)

	data, found, err := store.Check(ctx, "key-1")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []byte(`"result"`), data)
}

func TestInMemoryStore_ExpiredEntry(t *testing.T) {
	store := activity.NewInMemoryIdempotencyStore()
	ctx := context.Background()

	err := store.Record(ctx, "exp-key", []byte(`"v"`), time.Millisecond)
	require.NoError(t, err)

	time.Sleep(5 * time.Millisecond) // let it expire

	_, found, err := store.Check(ctx, "exp-key")
	require.NoError(t, err)
	assert.False(t, found, "expired entry must be treated as a miss")
}

func TestInMemoryStore_RecordIsIdempotent(t *testing.T) {
	store := activity.NewInMemoryIdempotencyStore()
	ctx := context.Background()

	require.NoError(t, store.Record(ctx, "k", []byte(`"v1"`), time.Hour))
	require.NoError(t, store.Record(ctx, "k", []byte(`"v2"`), time.Hour)) // overwrite is fine

	data, found, _ := store.Check(ctx, "k")
	assert.True(t, found)
	assert.Equal(t, []byte(`"v2"`), data)
}

func TestInMemoryStore_Flush(t *testing.T) {
	store := activity.NewInMemoryIdempotencyStore()
	ctx := context.Background()

	_ = store.Record(ctx, "a", []byte(`1`), time.Hour)
	_ = store.Record(ctx, "b", []byte(`2`), time.Hour)
	assert.Equal(t, 2, store.Len())

	store.Flush()
	assert.Equal(t, 0, store.Len())

	_, found, _ := store.Check(ctx, "a")
	assert.False(t, found)
}

// ---------------------------------------------------------------------------
// ExecuteIdempotent tests
// ---------------------------------------------------------------------------

func TestExecuteIdempotent_FirstExecution(t *testing.T) {
	store := activity.NewInMemoryIdempotencyStore()
	ctx := context.Background()

	callCount := 0
	var result string

	err := activity.ExecuteIdempotent(ctx, store, "key-first", &result, func() error {
		callCount++
		result = "computed"
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "fn must be called once on first execution")
	assert.Equal(t, "computed", result)
}

func TestExecuteIdempotent_SecondExecution_SkipsFn(t *testing.T) {
	store := activity.NewInMemoryIdempotencyStore()
	ctx := context.Background()
	key := "key-second"

	callCount := 0
	var result string

	// First execution — populates store.
	err := activity.ExecuteIdempotent(ctx, store, key, &result, func() error {
		callCount++
		result = "first-result"
		return nil
	})
	require.NoError(t, err)

	// Second execution (simulated retry) — must skip fn.
	result = "" // reset to detect if fn was skipped
	err = activity.ExecuteIdempotent(ctx, store, key, &result, func() error {
		callCount++
		result = "second-result" // must NOT be set
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "fn must NOT be called on duplicate execution")
	assert.Equal(t, "first-result", result, "cached result must be returned")
}

func TestExecuteIdempotent_FnError_DoesNotRecord(t *testing.T) {
	store := activity.NewInMemoryIdempotencyStore()
	ctx := context.Background()
	key := "key-err"

	var result string
	err := activity.ExecuteIdempotent(ctx, store, key, &result, func() error {
		return errors.New("transient failure")
	})

	assert.Error(t, err)

	// The key must NOT be recorded — next attempt should re-execute.
	_, found, _ := store.Check(ctx, key)
	assert.False(t, found, "failed executions must not be recorded")
}

func TestExecuteIdempotent_NilResult(t *testing.T) {
	// Activities that return only error (no result struct).
	store := activity.NewInMemoryIdempotencyStore()
	ctx := context.Background()

	callCount := 0
	err := activity.ExecuteIdempotent(ctx, store, "nil-key", nil, func() error {
		callCount++
		return nil
	})
	require.NoError(t, err)

	// Retry — must skip.
	err = activity.ExecuteIdempotent(ctx, store, "nil-key", nil, func() error {
		callCount++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "nil-result activity must also be deduplicated")
}

func TestExecuteIdempotent_StoreReadFailure_ExecutesFn(t *testing.T) {
	// If the store is unavailable, fall through and execute the fn.
	// This is "fail open" — prefer executing over blocking all activities.
	ctx := context.Background()
	var result string
	callCount := 0

	err := activity.ExecuteIdempotent(ctx, &brokenStore{}, "k", &result, func() error {
		callCount++
		result = "ok"
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "broken store must not block activity execution")
}

// brokenStore always returns an error on Check.
type brokenStore struct{}

func (b *brokenStore) Check(_ context.Context, _ string) ([]byte, bool, error) {
	return nil, false, errors.New("redis: connection refused")
}
func (b *brokenStore) Record(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return errors.New("redis: connection refused")
}

func TestExecuteIdempotentWithTTL_CustomTTL(t *testing.T) {
	store := activity.NewInMemoryIdempotencyStore()
	ctx := context.Background()

	var result string
	err := activity.ExecuteIdempotentWithTTL(ctx, store, "ttl-key", &result, 10*time.Millisecond, func() error {
		result = "v"
		return nil
	})
	require.NoError(t, err)

	// Before TTL — should hit cache.
	callCount := 0
	err = activity.ExecuteIdempotentWithTTL(ctx, store, "ttl-key", &result, 10*time.Millisecond, func() error {
		callCount++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 0, callCount)

	// After TTL — cache miss, fn should run.
	time.Sleep(20 * time.Millisecond)
	err = activity.ExecuteIdempotentWithTTL(ctx, store, "ttl-key", &result, time.Hour, func() error {
		callCount++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "expired TTL must trigger re-execution")
}
