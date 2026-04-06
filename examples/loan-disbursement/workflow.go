package loandisbursement

import (
	"fmt"
	"time"

	temporalcommon "github.com/yourorg/temporal-common"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// Typed search attribute keys — defined once, reused across the workflow.
var (
	saLoanID     = temporal.NewSearchAttributeKeyString("LoanID")
	saBorrowerID = temporal.NewSearchAttributeKeyString("BorrowerID")
	saStage      = temporal.NewSearchAttributeKeyString("Stage")
)

const TaskQueue = "loan-disbursement"

// LoanDisbursementInput is the workflow's starting payload.
type LoanDisbursementInput struct {
	LoanID        string
	BorrowerID    string
	Amount        float64
	AccountNumber string
	RequiresApproval bool
}

// LoanDisbursementResult is returned on successful completion.
type LoanDisbursementResult struct {
	TransactionID string
	DisbursedAt   time.Time
}

// LoanDisbursementWorkflow is a full reference implementation demonstrating:
//   - Saga compensation (LIFO rollback)
//   - Activity preset options
//   - Workflow versioning
//   - Human approval gate
//   - Idempotency in every activity
func LoanDisbursementWorkflow(ctx workflow.Context, input LoanDisbursementInput) (LoanDisbursementResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("loan disbursement started", "loanID", input.LoanID)

	// --- Versioning: declare all changes before any branching ---
	changes := temporalcommon.NewChangeSet(ctx)
	changes.Define("add-kyc-score-check-2024-01", 1)

	// --- Observability: tag workflow for search ---
	_ = workflow.UpsertTypedSearchAttributes(ctx,
		saLoanID.ValueSet(input.LoanID),
		saBorrowerID.ValueSet(input.BorrowerID),
		saStage.ValueSet("INIT"),
	)

	// --- Saga setup ---
	saga := temporalcommon.NewSaga(ctx)
	defer saga.Compensate()

	// Step 1: KYC check (internal service)
	kycCtx := temporalcommon.WithInternalServiceOptions(ctx)
	var kycResult KYCResult
	if err := workflow.ExecuteActivity(kycCtx, KYCCheckActivity, KYCInput{
		BorrowerID: input.BorrowerID,
	}).Get(ctx, &kycResult); err != nil {
		return LoanDisbursementResult{}, fmt.Errorf("KYC check failed: %w", err)
	}

	if !kycResult.Approved {
		return LoanDisbursementResult{}, temporalcommon.NewBusinessError(
			"BorrowerBlacklisted", "KYC check rejected borrower", nil)
	}

	// Versioned path: stricter score threshold introduced in 2024-01
	if changes.IsEnabled("add-kyc-score-check-2024-01") {
		if kycResult.Score < 600 {
			return LoanDisbursementResult{}, temporalcommon.NewBusinessError(
				"InsufficientCreditScore", "credit score below minimum threshold", nil)
		}
	}

	// Step 2: Human approval gate (for large loans)
	if input.RequiresApproval || input.Amount > 100_000 {
		_ = workflow.UpsertTypedSearchAttributes(ctx, saStage.ValueSet("AWAITING_APPROVAL"))

		approval, err := temporalcommon.WaitForApproval(ctx, temporalcommon.ApprovalRequest{
			ApprovalID:  input.LoanID,
			Description: fmt.Sprintf("Approve loan %.2f for borrower %s", input.Amount, input.BorrowerID),
			Timeout:     48 * time.Hour,
		})
		if err != nil {
			return LoanDisbursementResult{}, err
		}
		if !approval.Approved {
			return LoanDisbursementResult{}, temporalcommon.NewBusinessError(
				"LoanRejected", "loan application was not approved", nil)
		}
	}

	_ = workflow.UpsertTypedSearchAttributes(ctx, saStage.ValueSet("DISBURSING"))

	// Step 3: Reserve funds (financial API)
	finCtx := temporalcommon.WithFinancialAPIOptions(ctx)
	var reservation ReserveFundsResult
	if err := workflow.ExecuteActivity(finCtx, ReserveFundsActivity, ReserveFundsInput{
		LoanID: input.LoanID,
		Amount: input.Amount,
	}).Get(ctx, &reservation); err != nil {
		return LoanDisbursementResult{}, fmt.Errorf("reserve funds failed: %w", err)
	}

	// Register compensation immediately after success.
	saga.AddCompensation(ReleaseReservationActivity, ReleaseReservationInput{
		ReservationID: reservation.ReservationID,
		Reason:        "disbursement failed — rolling back",
	})

	// PIVOT POINT: funds are committed externally — no full rollback past here.
	saga.SetPivot()

	// Step 4: Disburse funds (financial API)
	var disbursement DisbursementResult
	if err := workflow.ExecuteActivity(finCtx, DisbursementActivity, DisbursementInput{
		LoanID:        input.LoanID,
		ReservationID: reservation.ReservationID,
		Amount:        input.Amount,
		AccountNumber: input.AccountNumber,
	}).Get(ctx, &disbursement); err != nil {
		// Past the pivot — compensation runs but is logged as CompensationError.
		return LoanDisbursementResult{}, fmt.Errorf("disbursement failed: %w", err)
	}

	_ = workflow.UpsertTypedSearchAttributes(ctx, saStage.ValueSet("NOTIFYING"))

	// Step 5: Notify borrower (best-effort, does NOT block saga completion).
	notifyCtx := temporalcommon.WithNotificationOptions(ctx)
	_ = workflow.ExecuteActivity(notifyCtx, NotifyBorrowerActivity, NotifyInput{
		BorrowerID:    input.BorrowerID,
		LoanID:        input.LoanID,
		TransactionID: disbursement.TransactionID,
	}).Get(ctx, nil) // ignore notification errors

	// Mark saga complete — prevents defer Compensate from rolling back.
	saga.Complete()

	_ = workflow.UpsertTypedSearchAttributes(ctx, saStage.ValueSet("COMPLETED"))

	return LoanDisbursementResult{
		TransactionID: disbursement.TransactionID,
		DisbursedAt:   workflow.Now(ctx),
	}, nil
}
