package activity

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// WithFinancialAPIOptions applies activity options suitable for external financial
// API calls: bank APIs, payment gateways, core banking systems.
//
// Characteristics: moderate timeout, exponential backoff with 5 attempts,
// business errors are non-retryable.
func WithFinancialAPIOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		ScheduleToCloseTimeout: 5 * time.Minute,
		StartToCloseTimeout:    2 * time.Minute,
		HeartbeatTimeout:       30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    5,
			// Non-retryable: all business error codes declared in errors.go.
			// Infrastructure errors (no type annotation) are always retried.
			NonRetryableErrorTypes: businessErrorCodes,
		},
	})
}

// WithNotificationOptions applies activity options for best-effort notification
// delivery: email, SMS, push notifications.
//
// Characteristics: short timeout, fixed-rate retry (no backoff), business errors
// are non-retryable. Notification failures should never block the main saga.
func WithNotificationOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		ScheduleToCloseTimeout: 2 * time.Minute,
		StartToCloseTimeout:    1 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        5 * time.Second,
			BackoffCoefficient:     1.0, // fixed rate — notifications don't benefit from backoff
			MaximumInterval:        5 * time.Second,
			MaximumAttempts:        3,
			NonRetryableErrorTypes: businessErrorCodes,
		},
	})
}

// WithInternalServiceOptions applies activity options for internal gRPC service calls.
//
// Characteristics: tight timeout, aggressive retry with exponential backoff,
// suitable for services that should recover quickly.
func WithInternalServiceOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		ScheduleToCloseTimeout: 1 * time.Minute,
		StartToCloseTimeout:    30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        500 * time.Millisecond,
			BackoffCoefficient:     2.0,
			MaximumInterval:        10 * time.Second,
			MaximumAttempts:        10,
			NonRetryableErrorTypes: businessErrorCodes,
		},
	})
}

// WithLongRunningOptions applies activity options for long-running batch jobs
// or operations that take more than 5 minutes.
//
// Characteristics: 24-hour schedule window, mandatory heartbeat every 60s,
// conservative retry count (job failures are expensive to restart).
func WithLongRunningOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		ScheduleToCloseTimeout: 24 * time.Hour,
		StartToCloseTimeout:    23 * time.Hour,
		HeartbeatTimeout:       60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        10 * time.Second,
			BackoffCoefficient:     2.0,
			MaximumInterval:        5 * time.Minute,
			MaximumAttempts:        3,
			NonRetryableErrorTypes: businessErrorCodes,
		},
	})
}

// businessErrorCodes is the list of error type strings that are non-retryable
// across all presets. These correspond to the errType argument passed to
// temporal.NewNonRetryableApplicationError in errors.go.
//
// Adding a new business error code: update this list AND errors.go.
var businessErrorCodes = []string{
	// Financial domain
	"InsufficientFund",
	"BorrowerBlacklisted",
	"LoanAlreadyDisbursed",
	"DuplicateTransaction",
	"InvalidLoanAmount",
	"ExpiredOffer",
	// Infrastructure — permanent failures
	"CompensationError",
}
