package loandisbursement

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"

	temporalcommon "github.com/yourorg/temporal-common"
	tcactivity "github.com/yourorg/temporal-common/activity"
)

// --- Input / Output types ---

type KYCInput struct {
	BorrowerID string
}

type KYCResult struct {
	Approved bool
	Score    int
}

type ReserveFundsInput struct {
	LoanID string
	Amount float64
}

type ReserveFundsResult struct {
	ReservationID string
}

type DisbursementInput struct {
	LoanID        string
	ReservationID string
	Amount        float64
	AccountNumber string
}

type DisbursementResult struct {
	TransactionID string
}

type NotifyInput struct {
	BorrowerID    string
	LoanID        string
	TransactionID string
}

type ReleaseReservationInput struct {
	ReservationID string
	Reason        string
}

// --- Activity implementations ---

// KYCCheckActivity validates borrower identity and creditworthiness.
func KYCCheckActivity(ctx context.Context, input KYCInput) (KYCResult, error) {
	info := activity.GetInfo(ctx)
	iKey := tcactivity.IdempotencyKey(info)
	_ = iKey // pass to downstream KYC service

	// Simulate KYC check.
	if input.BorrowerID == "" {
		return KYCResult{}, temporalcommon.NewBusinessError("BorrowerBlacklisted",
			fmt.Sprintf("borrower %q is on the blacklist", input.BorrowerID), nil)
	}

	return KYCResult{Approved: true, Score: 750}, nil
}

// ReserveFundsActivity reserves funds in the core banking system.
func ReserveFundsActivity(ctx context.Context, input ReserveFundsInput) (ReserveFundsResult, error) {
	info := activity.GetInfo(ctx)
	iKey := tcactivity.IdempotencyKeyNoRetry(info) // stable key across retries for bank API
	_ = iKey

	activity.RecordHeartbeat(ctx, "reserving funds")

	if input.Amount <= 0 {
		return ReserveFundsResult{}, temporalcommon.NewBusinessError("InvalidLoanAmount",
			fmt.Sprintf("loan amount %.2f is invalid", input.Amount), nil)
	}

	// Simulate reservation.
	return ReserveFundsResult{ReservationID: "rsv-" + input.LoanID}, nil
}

// DisbursementActivity transfers funds to the borrower's account.
// This is a pivot point — once funds leave the bank, the saga cannot fully undo it.
func DisbursementActivity(ctx context.Context, input DisbursementInput) (DisbursementResult, error) {
	info := activity.GetInfo(ctx)
	iKey := tcactivity.IdempotencyKeyNoRetry(info)
	_ = iKey

	activity.RecordHeartbeat(ctx, "disbursing funds")

	// Simulate disbursement.
	txID := fmt.Sprintf("tx-%s-%s", input.LoanID, input.ReservationID)
	return DisbursementResult{TransactionID: txID}, nil
}

// NotifyBorrowerActivity sends a disbursement confirmation to the borrower.
// Best-effort — failure should not block the saga.
func NotifyBorrowerActivity(ctx context.Context, input NotifyInput) error {
	// Simulate notification.
	activity.GetLogger(ctx).Info("notifying borrower",
		"borrowerID", input.BorrowerID,
		"loanID", input.LoanID,
		"transactionID", input.TransactionID,
	)
	return nil
}

// ReleaseReservationActivity is the compensation for ReserveFundsActivity.
// Must be idempotent and retry-able indefinitely.
func ReleaseReservationActivity(ctx context.Context, input ReleaseReservationInput) error {
	info := activity.GetInfo(ctx)
	iKey := tcactivity.IdempotencyKeyNoRetry(info)
	_ = iKey

	activity.RecordHeartbeat(ctx, "releasing reservation")

	// Simulate release — always succeeds (idempotent).
	activity.GetLogger(ctx).Info("reservation released",
		"reservationID", input.ReservationID,
		"reason", input.Reason,
	)
	return nil
}
