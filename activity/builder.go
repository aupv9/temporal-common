package activity

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// OptionsBuilder builds workflow.ActivityOptions with a fluent API.
// Use when the four built-in presets don't fit your use case.
// All unset fields default to the same production-safe values used by the presets.
//
// Usage:
//
//	ctx = activity.NewOptionsBuilder().
//	    WithScheduleToClose(10 * time.Minute).
//	    WithMaxAttempts(7).
//	    WithHeartbeat(45 * time.Second).
//	    WithNonRetryableErrors("InsufficientFund", "BorrowerBlacklisted").
//	    Apply(ctx)
type OptionsBuilder struct {
	scheduleToClose time.Duration
	startToClose    time.Duration
	heartbeat       time.Duration
	maxAttempts     int32
	initialInterval time.Duration
	maxInterval     time.Duration
	backoff         float64
	nonRetryable    []string
}

// NewOptionsBuilder creates an OptionsBuilder pre-filled with conservative defaults.
// Override only what you need.
func NewOptionsBuilder() *OptionsBuilder {
	return &OptionsBuilder{
		scheduleToClose: 5 * time.Minute,
		startToClose:    2 * time.Minute,
		maxAttempts:     5,
		initialInterval: 2 * time.Second,
		maxInterval:     30 * time.Second,
		backoff:         2.0,
		nonRetryable:    businessErrorCodes,
	}
}

// WithScheduleToClose sets the maximum time from schedule to completion
// (including all retries). This is the hard cap for the entire activity.
func (b *OptionsBuilder) WithScheduleToClose(d time.Duration) *OptionsBuilder {
	b.scheduleToClose = d
	return b
}

// WithStartToClose sets the maximum time for a single attempt.
// Must be less than ScheduleToClose.
func (b *OptionsBuilder) WithStartToClose(d time.Duration) *OptionsBuilder {
	b.startToClose = d
	return b
}

// WithHeartbeat sets the heartbeat timeout. If the activity does not call
// activity.RecordHeartbeat within this window, Temporal marks it as timed out.
// Set for any operation expected to run longer than 10 seconds.
func (b *OptionsBuilder) WithHeartbeat(d time.Duration) *OptionsBuilder {
	b.heartbeat = d
	return b
}

// WithMaxAttempts sets the maximum number of attempts (including the first).
// Pass 0 for unlimited retries (use with caution in compensation activities only).
func (b *OptionsBuilder) WithMaxAttempts(n int32) *OptionsBuilder {
	b.maxAttempts = n
	return b
}

// WithInitialInterval sets the initial retry backoff interval.
func (b *OptionsBuilder) WithInitialInterval(d time.Duration) *OptionsBuilder {
	b.initialInterval = d
	return b
}

// WithMaxInterval caps the exponential backoff interval.
func (b *OptionsBuilder) WithMaxInterval(d time.Duration) *OptionsBuilder {
	b.maxInterval = d
	return b
}

// WithBackoffCoefficient sets the exponential backoff multiplier.
// 1.0 = fixed interval. 2.0 = doubles each retry (default).
func (b *OptionsBuilder) WithBackoffCoefficient(c float64) *OptionsBuilder {
	b.backoff = c
	return b
}

// WithNonRetryableErrors replaces the default non-retryable error type list.
// Pass the errType strings used in NewBusinessError (the `code` argument).
// The default list covers all built-in business error codes.
func (b *OptionsBuilder) WithNonRetryableErrors(codes ...string) *OptionsBuilder {
	b.nonRetryable = codes
	return b
}

// AddNonRetryableErrors appends error codes to the existing non-retryable list
// without replacing the built-in defaults.
func (b *OptionsBuilder) AddNonRetryableErrors(codes ...string) *OptionsBuilder {
	b.nonRetryable = append(b.nonRetryable, codes...)
	return b
}

// NoRetry configures unlimited retries (MaxAttempts = 0, no non-retryable codes).
// Only for compensation activities — never use in forward steps.
func (b *OptionsBuilder) NoRetry() *OptionsBuilder {
	b.maxAttempts = 0
	b.nonRetryable = nil
	return b
}

// Apply sets the built options on ctx and returns the new context.
// Pass the returned context to workflow.ExecuteActivity.
func (b *OptionsBuilder) Apply(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, b.Build())
}

// Build returns the constructed workflow.ActivityOptions.
// Use this if you need to inspect or further modify the options before applying.
func (b *OptionsBuilder) Build() workflow.ActivityOptions {
	opts := workflow.ActivityOptions{
		ScheduleToCloseTimeout: b.scheduleToClose,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        b.initialInterval,
			BackoffCoefficient:     b.backoff,
			MaximumInterval:        b.maxInterval,
			MaximumAttempts:        b.maxAttempts,
			NonRetryableErrorTypes: b.nonRetryable,
		},
	}
	if b.startToClose > 0 {
		opts.StartToCloseTimeout = b.startToClose
	}
	if b.heartbeat > 0 {
		opts.HeartbeatTimeout = b.heartbeat
	}
	return opts
}
