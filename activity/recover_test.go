package activity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktemporal "go.temporal.io/sdk/temporal"

	"github.com/yourorg/temporal-common/activity"
)

// ---------------------------------------------------------------------------
// Stub activity functions for recovery tests
// ---------------------------------------------------------------------------

func normalActivity(_ context.Context, input string) (string, error) {
	return "result-" + input, nil
}

func panicActivity(_ context.Context) error {
	panic("nil pointer dereference")
}

func panicWithValueActivity(_ context.Context) (string, error) {
	var s []string
	return s[0], nil // panics: index out of range
}

func errorActivity(_ context.Context) error {
	return errors.New("normal error")
}

func cancelledActivity(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func callWrapped(t *testing.T, fn interface{}, args ...interface{}) []interface{} {
	t.Helper()
	// We invoke the wrapped function directly using reflection to simulate
	// Temporal's activity dispatcher without needing a test worker.
	import_reflect := func() interface{} {
		// Build args as reflect.Values is done inside WithPanicRecovery itself.
		// Instead, call via type assertion for each specific test case.
		return fn
	}
	_ = import_reflect
	return nil
}

func TestWithPanicRecovery_NormalActivity_ReturnsResult(t *testing.T) {
	wrapped := activity.WithPanicRecovery(normalActivity).(func(context.Context, string) (string, error))

	result, err := wrapped(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "result-hello", result)
}

func TestWithPanicRecovery_NormalError_PropagatesUnchanged(t *testing.T) {
	wrapped := activity.WithPanicRecovery(errorActivity).(func(context.Context) error)

	err := wrapped(context.Background())
	require.Error(t, err)
	assert.EqualError(t, err, "normal error")
	// Must NOT be wrapped in a RetryableError.
	var appErr *sdktemporal.ApplicationError
	assert.False(t, errors.As(err, &appErr), "normal errors must pass through unchanged")
}

func TestWithPanicRecovery_PanicConvertedToRetryableError(t *testing.T) {
	wrapped := activity.WithPanicRecovery(panicActivity).(func(context.Context) error)

	err := wrapped(context.Background())
	require.Error(t, err)

	// Must be a retryable ApplicationError (Temporal will retry it).
	var appErr *sdktemporal.ApplicationError
	require.True(t, errors.As(err, &appErr), "panic must become ApplicationError")
	assert.False(t, appErr.NonRetryable(), "panic error must be retryable")
	assert.Contains(t, appErr.Message(), "activity panicked")
	assert.Contains(t, appErr.Message(), "nil pointer dereference")
}

func TestWithPanicRecovery_PanicMessageContainsStackTrace(t *testing.T) {
	wrapped := activity.WithPanicRecovery(panicActivity).(func(context.Context) error)

	err := wrapped(context.Background())
	require.Error(t, err)

	var appErr *sdktemporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	// Stack trace must be present in the message for debuggability.
	assert.Contains(t, appErr.Message(), "goroutine", "stack trace must be included")
}

func TestWithPanicRecovery_NilMapPanic_Recovered(t *testing.T) {
	wrapped := activity.WithPanicRecovery(panicWithValueActivity).(func(context.Context) (string, error))

	result, err := wrapped(context.Background())
	assert.Empty(t, result, "zero value returned on panic")
	require.Error(t, err)

	var appErr *sdktemporal.ApplicationError
	assert.True(t, errors.As(err, &appErr))
}

func TestWithPanicRecovery_CancelledContext_FastFail(t *testing.T) {
	wrapped := activity.WithPanicRecovery(normalActivity).(func(context.Context, string) (string, error))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := wrapped(ctx, "test")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWithPanicRecovery_NonFunctionPanics(t *testing.T) {
	assert.Panics(t, func() {
		activity.WithPanicRecovery("not a function")
	}, "passing non-function must panic at registration time")
}

func TestWithPanicRecovery_MultipleReturnValues_ZeroOnPanic(t *testing.T) {
	wrapped := activity.WithPanicRecovery(panicWithValueActivity).(func(context.Context) (string, error))

	result, err := wrapped(context.Background())

	// Non-error return values must be zero on panic.
	assert.Equal(t, "", result, "string zero value on panic")
	require.Error(t, err)
}
