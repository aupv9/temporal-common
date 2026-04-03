package paymentsaga

import (
	"fmt"

	temporalcommon "github.com/yourorg/temporal-common"
	"go.temporal.io/sdk/workflow"
)

const TaskQueue = "payment-saga"

type PaymentInput struct {
	PaymentID string
	FromAcct  string
	ToAcct    string
	Amount    float64
}

type DebitResult struct{ TxID string }
type CreditResult struct{ TxID string }

// DebitActivity debits the sender's account.
func DebitActivity(ctx workflow.Context, input PaymentInput) (DebitResult, error) {
	return DebitResult{TxID: "debit-" + input.PaymentID}, nil
}

// CreditActivity credits the receiver's account.
func CreditActivity(ctx workflow.Context, input PaymentInput) (CreditResult, error) {
	// Simulate a failure to demonstrate compensation.
	return CreditResult{}, temporalcommon.NewRetryableError("credit service unavailable", nil)
}

// ReverseDebitActivity is the compensation for DebitActivity.
func ReverseDebitActivity(ctx workflow.Context, txID string) error {
	workflow.GetLogger(ctx).Info("reversing debit", "txID", txID)
	return nil
}

// PaymentSagaWorkflow demonstrates saga rollback on failure.
// When CreditActivity fails, ReverseDebitActivity runs automatically.
func PaymentSagaWorkflow(ctx workflow.Context, input PaymentInput) error {
	saga := temporalcommon.NewSaga(ctx)
	defer saga.Compensate()

	finCtx := temporalcommon.WithFinancialAPIOptions(ctx)

	// Step 1: Debit sender.
	var debit DebitResult
	if err := workflow.ExecuteActivity(finCtx, DebitActivity, input).Get(ctx, &debit); err != nil {
		return fmt.Errorf("debit failed: %w", err)
	}
	saga.AddCompensation(ReverseDebitActivity, debit.TxID)

	// Step 2: Credit receiver — this will fail and trigger compensation.
	if err := workflow.ExecuteActivity(finCtx, CreditActivity, input).Get(ctx, nil); err != nil {
		return fmt.Errorf("credit failed: %w", err)
		// defer saga.Compensate() fires here → ReverseDebitActivity runs automatically.
	}

	saga.Complete()
	return nil
}
