package activity_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"

	"github.com/yourorg/temporal-common/activity"
)

func TestNewRetryableError(t *testing.T) {
	cause := errors.New("connection timeout")
	err := activity.NewRetryableError("service unavailable", cause)

	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr), "expected ApplicationError")
	assert.False(t, appErr.NonRetryable(), "retryable error must not be non-retryable")
	assert.Equal(t, "RetryableError", appErr.Type())
	assert.Contains(t, err.Error(), "service unavailable")
}

func TestNewRetryableError_NilCause(t *testing.T) {
	err := activity.NewRetryableError("timeout", nil)
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	assert.False(t, appErr.NonRetryable())
}

func TestNewBusinessError(t *testing.T) {
	tests := []struct {
		code string
		msg  string
	}{
		{"InsufficientFund", "account balance too low"},
		{"BorrowerBlacklisted", "borrower on blacklist"},
		{"LoanAlreadyDisbursed", "loan was previously disbursed"},
		{"DuplicateTransaction", "duplicate transaction ID"},
	}

	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			err := activity.NewBusinessError(tc.code, tc.msg, nil)

			require.Error(t, err)

			var appErr *temporal.ApplicationError
			require.True(t, errors.As(err, &appErr))
			assert.True(t, appErr.NonRetryable(), "business error must be non-retryable")
			assert.Equal(t, tc.code, appErr.Type())
			assert.Contains(t, err.Error(), tc.code)
			assert.Contains(t, err.Error(), tc.msg)
		})
	}
}

func TestNewBusinessError_WithCause(t *testing.T) {
	cause := errors.New("upstream rejected")
	err := activity.NewBusinessError("InsufficientFund", "balance too low", cause)

	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	assert.True(t, appErr.NonRetryable())
}

func TestNewCompensationError(t *testing.T) {
	cause := errors.New("refund API rejected request")
	err := activity.NewCompensationError("refund failed", "saga-abc-123", "RefundFee", cause)

	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	assert.True(t, appErr.NonRetryable(), "compensation error must be non-retryable")
	assert.Equal(t, "CompensationError", appErr.Type())
	assert.Contains(t, err.Error(), "saga-abc-123")
	assert.Contains(t, err.Error(), "RefundFee")
}

func TestIsBusinessError(t *testing.T) {
	t.Run("matches correct code", func(t *testing.T) {
		err := activity.NewBusinessError("InsufficientFund", "not enough", nil)
		assert.True(t, activity.IsBusinessError(err, "InsufficientFund"))
	})

	t.Run("does not match wrong code", func(t *testing.T) {
		err := activity.NewBusinessError("InsufficientFund", "not enough", nil)
		assert.False(t, activity.IsBusinessError(err, "BorrowerBlacklisted"))
	})

	t.Run("returns false for plain error", func(t *testing.T) {
		assert.False(t, activity.IsBusinessError(errors.New("plain"), "InsufficientFund"))
	})

	t.Run("returns false for retryable error", func(t *testing.T) {
		// A retryable error must not match any business error code.
		err := activity.NewRetryableError("timeout", nil)
		assert.False(t, activity.IsBusinessError(err, "InsufficientFund"))
	})

	t.Run("returns false for nil", func(t *testing.T) {
		assert.False(t, activity.IsBusinessError(nil, "InsufficientFund"))
	})
}

func TestIsCompensationError(t *testing.T) {
	t.Run("true for CompensationError", func(t *testing.T) {
		err := activity.NewCompensationError("failed", "saga-1", "step", nil)
		assert.True(t, activity.IsCompensationError(err))
	})

	t.Run("false for BusinessError", func(t *testing.T) {
		err := activity.NewBusinessError("InsufficientFund", "msg", nil)
		assert.False(t, activity.IsCompensationError(err))
	})

	t.Run("false for RetryableError", func(t *testing.T) {
		err := activity.NewRetryableError("timeout", nil)
		assert.False(t, activity.IsCompensationError(err))
	})

	t.Run("false for plain error", func(t *testing.T) {
		assert.False(t, activity.IsCompensationError(errors.New("plain")))
	})

	t.Run("false for nil", func(t *testing.T) {
		assert.False(t, activity.IsCompensationError(nil))
	})
}
