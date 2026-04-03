package activity_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/temporal-common/activity"
)

func TestNewOptionsBuilder_DefaultValues(t *testing.T) {
	opts := activity.NewOptionsBuilder().Build()

	assert.Equal(t, 5*time.Minute, opts.ScheduleToCloseTimeout, "default ScheduleToClose")
	assert.Equal(t, 2*time.Minute, opts.StartToCloseTimeout, "default StartToClose")
	require.NotNil(t, opts.RetryPolicy)
	assert.Equal(t, int32(5), opts.RetryPolicy.MaximumAttempts, "default MaxAttempts")
	assert.Equal(t, 2*time.Second, opts.RetryPolicy.InitialInterval)
	assert.Equal(t, float64(2.0), opts.RetryPolicy.BackoffCoefficient)
	assert.Equal(t, 30*time.Second, opts.RetryPolicy.MaximumInterval)
	assert.NotEmpty(t, opts.RetryPolicy.NonRetryableErrorTypes, "default non-retryable codes must be set")
}

func TestOptionsBuilder_WithScheduleToClose(t *testing.T) {
	opts := activity.NewOptionsBuilder().
		WithScheduleToClose(10 * time.Minute).
		Build()
	assert.Equal(t, 10*time.Minute, opts.ScheduleToCloseTimeout)
}

func TestOptionsBuilder_WithStartToClose(t *testing.T) {
	opts := activity.NewOptionsBuilder().
		WithStartToClose(45 * time.Second).
		Build()
	assert.Equal(t, 45*time.Second, opts.StartToCloseTimeout)
}

func TestOptionsBuilder_WithHeartbeat(t *testing.T) {
	opts := activity.NewOptionsBuilder().
		WithHeartbeat(30 * time.Second).
		Build()
	assert.Equal(t, 30*time.Second, opts.HeartbeatTimeout)
}

func TestOptionsBuilder_NoHeartbeat_OmitsField(t *testing.T) {
	opts := activity.NewOptionsBuilder().Build()
	// Default builder has no heartbeat — field must be zero.
	assert.Zero(t, opts.HeartbeatTimeout, "heartbeat must be zero when not set")
}

func TestOptionsBuilder_WithMaxAttempts(t *testing.T) {
	opts := activity.NewOptionsBuilder().
		WithMaxAttempts(10).
		Build()
	assert.Equal(t, int32(10), opts.RetryPolicy.MaximumAttempts)
}

func TestOptionsBuilder_NoRetry_UnlimitedAttempts(t *testing.T) {
	opts := activity.NewOptionsBuilder().NoRetry().Build()
	assert.Equal(t, int32(0), opts.RetryPolicy.MaximumAttempts, "0 = unlimited")
	assert.Empty(t, opts.RetryPolicy.NonRetryableErrorTypes)
}

func TestOptionsBuilder_WithNonRetryableErrors_Replaces(t *testing.T) {
	opts := activity.NewOptionsBuilder().
		WithNonRetryableErrors("CustomCode1", "CustomCode2").
		Build()
	assert.Equal(t, []string{"CustomCode1", "CustomCode2"}, opts.RetryPolicy.NonRetryableErrorTypes)
}

func TestOptionsBuilder_AddNonRetryableErrors_Appends(t *testing.T) {
	opts := activity.NewOptionsBuilder().
		AddNonRetryableErrors("MyCustomError").
		Build()

	codes := opts.RetryPolicy.NonRetryableErrorTypes
	assert.Contains(t, codes, "MyCustomError", "custom code must be added")
	assert.Contains(t, codes, "InsufficientFund", "built-in codes must be preserved")
}

func TestOptionsBuilder_WithBackoffCoefficient(t *testing.T) {
	opts := activity.NewOptionsBuilder().
		WithBackoffCoefficient(1.5).
		Build()
	assert.InDelta(t, 1.5, opts.RetryPolicy.BackoffCoefficient, 0.001)
}

func TestOptionsBuilder_WithInitialAndMaxInterval(t *testing.T) {
	opts := activity.NewOptionsBuilder().
		WithInitialInterval(500 * time.Millisecond).
		WithMaxInterval(1 * time.Minute).
		Build()
	assert.Equal(t, 500*time.Millisecond, opts.RetryPolicy.InitialInterval)
	assert.Equal(t, time.Minute, opts.RetryPolicy.MaximumInterval)
}

func TestOptionsBuilder_Fluent_Chaining(t *testing.T) {
	// Verify all methods can be chained and the result is consistent.
	opts := activity.NewOptionsBuilder().
		WithScheduleToClose(15 * time.Minute).
		WithStartToClose(5 * time.Minute).
		WithHeartbeat(60 * time.Second).
		WithMaxAttempts(7).
		WithInitialInterval(time.Second).
		WithMaxInterval(2 * time.Minute).
		WithBackoffCoefficient(3.0).
		WithNonRetryableErrors("ErrA", "ErrB").
		Build()

	assert.Equal(t, 15*time.Minute, opts.ScheduleToCloseTimeout)
	assert.Equal(t, 5*time.Minute, opts.StartToCloseTimeout)
	assert.Equal(t, 60*time.Second, opts.HeartbeatTimeout)
	assert.Equal(t, int32(7), opts.RetryPolicy.MaximumAttempts)
	assert.Equal(t, time.Second, opts.RetryPolicy.InitialInterval)
	assert.Equal(t, 2*time.Minute, opts.RetryPolicy.MaximumInterval)
	assert.InDelta(t, 3.0, opts.RetryPolicy.BackoffCoefficient, 0.001)
	assert.Equal(t, []string{"ErrA", "ErrB"}, opts.RetryPolicy.NonRetryableErrorTypes)
}

func TestOptionsBuilder_IndependentInstances(t *testing.T) {
	// Modifying one builder must not affect another.
	b1 := activity.NewOptionsBuilder().WithMaxAttempts(3)
	b2 := activity.NewOptionsBuilder().WithMaxAttempts(8)

	opts1 := b1.Build()
	opts2 := b2.Build()

	assert.Equal(t, int32(3), opts1.RetryPolicy.MaximumAttempts)
	assert.Equal(t, int32(8), opts2.RetryPolicy.MaximumAttempts)
}
