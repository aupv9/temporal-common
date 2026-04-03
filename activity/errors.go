package activity

import (
	"errors"
	"fmt"

	"go.temporal.io/sdk/temporal"
)

// NewRetryableError creates a retryable application error.
// Temporal will retry this error according to the activity's retry policy.
// Use for transient infrastructure failures (network timeout, temporary unavailability).
func NewRetryableError(msg string, cause error) error {
	return temporal.NewApplicationError(msg, "RetryableError", cause)
}

// NewBusinessError creates a non-retryable application error for business rule violations.
// Temporal will NOT retry this error — it represents a permanent failure due to business logic.
//
// code examples: "InsufficientFund", "BorrowerBlacklisted", "LoanAlreadyDisbursed",
// "DuplicateTransaction", "InvalidLoanAmount", "ExpiredOffer".
func NewBusinessError(code, msg string, cause error) error {
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("[%s] %s", code, msg),
		code,
		cause,
	)
}

// NewCompensationError creates a non-retryable error signaling that a saga
// compensation step has failed permanently and requires human intervention.
//
// This error is emitted by the Saga manager when a compensation activity
// exhausts its retry budget. It carries sagaID and step for incident triage.
func NewCompensationError(msg string, sagaID string, step string, cause error) error {
	details := fmt.Sprintf("sagaID=%s step=%s", sagaID, step)
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("compensation failed: %s [%s]", msg, details),
		"CompensationError",
		cause,
	)
}

// IsBusinessError returns true if the error is a non-retryable business error
// with the given error code. Useful for error handling in caller workflows.
func IsBusinessError(err error, code string) bool {
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) {
		return false
	}
	return appErr.Type() == code
}

// IsCompensationError returns true if the error is a CompensationError.
func IsCompensationError(err error) bool {
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) {
		return false
	}
	return appErr.Type() == "CompensationError"
}
