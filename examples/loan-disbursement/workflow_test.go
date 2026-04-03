package loandisbursement_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	temporalcommon "github.com/yourorg/temporal-common"
	tctest "github.com/yourorg/temporal-common/testing"
	loandisbursement "github.com/yourorg/temporal-common/examples/loan-disbursement"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func defaultInput() loandisbursement.LoanDisbursementInput {
	return loandisbursement.LoanDisbursementInput{
		LoanID:        "loan-001",
		BorrowerID:    "borrower-42",
		Amount:        50_000,
		AccountNumber: "ACC-12345678",
	}
}

func mockHappyPath(env *tctest.TestEnvironment) {
	env.MockActivity(loandisbursement.KYCCheckActivity).
		Return(loandisbursement.KYCResult{Approved: true, Score: 750}, nil)
	env.MockActivity(loandisbursement.ReserveFundsActivity).
		Return(loandisbursement.ReserveFundsResult{ReservationID: "rsv-001"}, nil)
	env.MockActivity(loandisbursement.DisbursementActivity).
		Return(loandisbursement.DisbursementResult{TransactionID: "tx-001"}, nil)
	env.MockActivity(loandisbursement.NotifyBorrowerActivity).Return(nil)
}

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

func TestLoanDisbursementWorkflow_HappyPath(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	mockHappyPath(env)
	env.ExecuteWorkflow(loandisbursement.LoanDisbursementWorkflow, defaultInput())

	var result loandisbursement.LoanDisbursementResult
	require.NoError(t, env.GetResult(&result))
	assert.Equal(t, "tx-001", result.TransactionID)
	assert.False(t, result.DisbursedAt.IsZero())
}

func TestLoanDisbursementWorkflow_HappyPath_NoCompensation(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()
	env.TrackCompensations()

	mockHappyPath(env)
	env.ExecuteWorkflow(loandisbursement.LoanDisbursementWorkflow, defaultInput())

	require.NoError(t, env.GetError())
	env.AssertActivityNotCalled(t, loandisbursement.ReleaseReservationActivity)
}

// ---------------------------------------------------------------------------
// KYC failure (non-retryable business error — no compensation registered yet)
// ---------------------------------------------------------------------------

func TestLoanDisbursementWorkflow_KYCRejected_BusinessError(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()
	env.TrackCompensations()

	env.MockActivity(loandisbursement.KYCCheckActivity).
		Return(loandisbursement.KYCResult{Approved: false, Score: 0}, nil)

	env.ExecuteWorkflow(loandisbursement.LoanDisbursementWorkflow, defaultInput())

	env.AssertWorkflowFailed(t, "BorrowerBlacklisted")
	// KYC fails before any saga step is registered — no compensation needed.
	env.AssertActivityNotCalled(t, loandisbursement.ReleaseReservationActivity)
}

func TestLoanDisbursementWorkflow_KYCCheckFails_InfraError(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(loandisbursement.KYCCheckActivity).
		ReturnError(errors.New("KYC service unavailable"))

	env.ExecuteWorkflow(loandisbursement.LoanDisbursementWorkflow, defaultInput())

	env.AssertWorkflowFailed(t, "KYC check failed")
	env.AssertActivityNotCalled(t, loandisbursement.ReleaseReservationActivity)
}

// ---------------------------------------------------------------------------
// Credit score check (versioned path — score < 600)
// ---------------------------------------------------------------------------

func TestLoanDisbursementWorkflow_InsufficientCreditScore(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()
	env.TrackCompensations()

	// KYC approved but score too low for new versioned path.
	env.MockActivity(loandisbursement.KYCCheckActivity).
		Return(loandisbursement.KYCResult{Approved: true, Score: 550}, nil)

	env.ExecuteWorkflow(loandisbursement.LoanDisbursementWorkflow, defaultInput())

	env.AssertWorkflowFailed(t, "InsufficientCreditScore")
	env.AssertActivityNotCalled(t, loandisbursement.ReserveFundsActivity)
}

// ---------------------------------------------------------------------------
// Disbursement failure — compensation must run
// ---------------------------------------------------------------------------

func TestLoanDisbursementWorkflow_DisbursementFails_TriggersCompensation(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()
	env.TrackCompensations()

	env.MockActivity(loandisbursement.KYCCheckActivity).
		Return(loandisbursement.KYCResult{Approved: true, Score: 750}, nil)
	env.MockActivity(loandisbursement.ReserveFundsActivity).
		Return(loandisbursement.ReserveFundsResult{ReservationID: "rsv-002"}, nil)
	env.MockActivity(loandisbursement.DisbursementActivity).
		ReturnError(errors.New("core banking system timeout"))
	// Compensation activity must be called.
	env.MockActivity(loandisbursement.ReleaseReservationActivity).Return(nil)

	env.ExecuteWorkflow(loandisbursement.LoanDisbursementWorkflow, defaultInput())

	env.AssertWorkflowFailed(t, "disbursement failed")
	env.AssertActivityCalled(t, loandisbursement.ReleaseReservationActivity)
	// Notification must NOT be sent if disbursement failed.
	env.AssertActivityNotCalled(t, loandisbursement.NotifyBorrowerActivity)
}

func TestLoanDisbursementWorkflow_ReserveFundsFails_NoCompensation(t *testing.T) {
	// ReserveFunds fails before saga.SetPivot() — release is NOT registered yet.
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()
	env.TrackCompensations()

	env.MockActivity(loandisbursement.KYCCheckActivity).
		Return(loandisbursement.KYCResult{Approved: true, Score: 750}, nil)
	env.MockActivity(loandisbursement.ReserveFundsActivity).
		ReturnError(temporalcommon.NewRetryableError("core banking unavailable", nil))

	env.ExecuteWorkflow(loandisbursement.LoanDisbursementWorkflow, defaultInput())

	env.AssertWorkflowFailed(t, "reserve funds failed")
	// Reserve failed before AddCompensation was called — no release expected.
	env.AssertActivityNotCalled(t, loandisbursement.ReleaseReservationActivity)
}

// ---------------------------------------------------------------------------
// Notification failure (best-effort — workflow must still complete)
// ---------------------------------------------------------------------------

func TestLoanDisbursementWorkflow_NotificationFails_WorkflowSucceeds(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	env.MockActivity(loandisbursement.KYCCheckActivity).
		Return(loandisbursement.KYCResult{Approved: true, Score: 750}, nil)
	env.MockActivity(loandisbursement.ReserveFundsActivity).
		Return(loandisbursement.ReserveFundsResult{ReservationID: "rsv-003"}, nil)
	env.MockActivity(loandisbursement.DisbursementActivity).
		Return(loandisbursement.DisbursementResult{TransactionID: "tx-003"}, nil)
	env.MockActivity(loandisbursement.NotifyBorrowerActivity).
		ReturnError(errors.New("SMS gateway down"))

	env.ExecuteWorkflow(loandisbursement.LoanDisbursementWorkflow, defaultInput())

	// Notification error is ignored in the workflow — must complete successfully.
	var result loandisbursement.LoanDisbursementResult
	require.NoError(t, env.GetResult(&result), "notification failure must not fail the workflow")
	assert.Equal(t, "tx-003", result.TransactionID)
}

// ---------------------------------------------------------------------------
// Human approval gate
// ---------------------------------------------------------------------------

func TestLoanDisbursementWorkflow_LargeLoan_ApprovalGranted(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	input := defaultInput()
	input.Amount = 200_000 // > 100_000 threshold → requires approval

	env.MockActivity(loandisbursement.KYCCheckActivity).
		Return(loandisbursement.KYCResult{Approved: true, Score: 750}, nil)
	env.MockActivity(loandisbursement.ReserveFundsActivity).
		Return(loandisbursement.ReserveFundsResult{ReservationID: "rsv-big"}, nil)
	env.MockActivity(loandisbursement.DisbursementActivity).
		Return(loandisbursement.DisbursementResult{TransactionID: "tx-big"}, nil)
	env.MockActivity(loandisbursement.NotifyBorrowerActivity).Return(nil)

	// Simulate manager approving 2 hours into workflow time.
	env.RegisterDelayedCallback(func() {
		env.Env().SignalWorkflow(temporalcommon.ApprovalSignalName,
			temporalcommon.ApprovalResult{
				Approved:   true,
				ApproverID: "risk-manager@acme.com",
				Comment:    "approved after manual review",
			})
	}, 2*time.Hour)

	env.ExecuteWorkflow(loandisbursement.LoanDisbursementWorkflow, input)

	var result loandisbursement.LoanDisbursementResult
	require.NoError(t, env.GetResult(&result))
	assert.Equal(t, "tx-big", result.TransactionID)
}

func TestLoanDisbursementWorkflow_LargeLoan_ApprovalDenied(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()
	env.TrackCompensations()

	input := defaultInput()
	input.Amount = 200_000

	env.MockActivity(loandisbursement.KYCCheckActivity).
		Return(loandisbursement.KYCResult{Approved: true, Score: 750}, nil)

	// Manager rejects the loan.
	env.RegisterDelayedCallback(func() {
		env.Env().SignalWorkflow(temporalcommon.ApprovalSignalName,
			temporalcommon.ApprovalResult{
				Approved:   false,
				ApproverID: "risk-manager@acme.com",
				Comment:    "too much risk",
			})
	}, time.Hour)

	env.ExecuteWorkflow(loandisbursement.LoanDisbursementWorkflow, input)

	env.AssertWorkflowFailed(t, "LoanRejected")
	// Approval was denied before funds reserved — no compensation needed.
	env.AssertActivityNotCalled(t, loandisbursement.ReserveFundsActivity)
	env.AssertActivityNotCalled(t, loandisbursement.ReleaseReservationActivity)
}

func TestLoanDisbursementWorkflow_LargeLoan_ApprovalTimeout(t *testing.T) {
	env := tctest.NewTestEnvironment(t)
	defer env.Cleanup()

	input := defaultInput()
	input.Amount = 200_000

	env.MockActivity(loandisbursement.KYCCheckActivity).
		Return(loandisbursement.KYCResult{Approved: true, Score: 750}, nil)

	// No signal sent — 48h approval window expires.
	env.ExecuteWorkflow(loandisbursement.LoanDisbursementWorkflow, input)

	env.AssertWorkflowFailed(t, "LoanRejected")
}
