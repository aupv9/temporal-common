# Testing Guide

## Three Required Test Types

Every feature must have all three:

### 1. Unit Test — pure logic, no Temporal

```go
func TestIdempotencyKey(t *testing.T) {
    info := activity.Info{
        WorkflowExecution: workflow.Execution{ID: "wf-1"},
        ActivityID: "act-1",
        Attempt:    2,
    }
    assert.Equal(t, "wf-1/act-1/2", activity.IdempotencyKey(info))
    assert.Equal(t, "wf-1/act-1", activity.IdempotencyKeyNoRetry(info))
}
```

### 2. Workflow Test — TestEnvironment, cover all paths

```go
func TestLoanDisbursement_HappyPath(t *testing.T) {
    env := testing.NewTestEnvironment(t)
    defer env.Cleanup()

    env.MockActivity(KYCCheckActivity).Return(KYCResult{Approved: true, Score: 750}, nil)
    env.MockActivity(ReserveFundsActivity).Return(ReserveFundsResult{ReservationID: "rsv-1"}, nil)
    env.MockActivity(DisbursementActivity).Return(DisbursementResult{TransactionID: "tx-1"}, nil)
    env.MockActivity(NotifyBorrowerActivity).Return(nil).Maybe()

    env.ExecuteWorkflow(LoanDisbursementWorkflow, LoanDisbursementInput{
        LoanID: "loan-1", BorrowerID: "b-1", Amount: 1000,
    })

    env.AssertWorkflowCompleted(t)
    env.AssertActivityCalled(t, DisbursementActivity)
}
```

### 3. Failure Scenario Test — compensation, crash, duplicate

```go
func TestLoanDisbursement_DisbursementFails_CompensatesReservation(t *testing.T) {
    env := testing.NewTestEnvironment(t)
    defer env.Cleanup()
    env.TrackCompensations()

    env.MockActivity(KYCCheckActivity).Return(KYCResult{Approved: true, Score: 750}, nil)
    env.MockActivity(ReserveFundsActivity).Return(ReserveFundsResult{ReservationID: "rsv-1"}, nil)
    env.MockActivity(DisbursementActivity).ReturnError(errors.New("bank timeout"))
    env.MockActivity(ReleaseReservationActivity).Return(nil)

    env.ExecuteWorkflow(LoanDisbursementWorkflow, LoanDisbursementInput{
        LoanID: "loan-1", BorrowerID: "b-1", Amount: 1000,
    })

    env.AssertWorkflowFailed(t, "disbursement failed")
    env.AssertCompensationOrder(t, ReleaseReservationActivity)
    env.AssertActivityNotCalled(t, NotifyBorrowerActivity)
}
```

## Testing the Approval Signal

Use `RegisterDelayedCallback` to inject the signal mid-workflow:

```go
env.RegisterDelayedCallback(func() {
    env.Env().SignalWorkflow(temporalcommon.ApprovalSignalName, temporalcommon.ApprovalResult{
        Approved:   true,
        ApproverID: "manager@example.com",
    })
}, 1*time.Hour)
```

## How AssertCompensationOrder Works

`TrackCompensations()` installs an `OnActivityStarted` listener that records every activity name in execution order into `te.compensationOrder`. `AssertCompensationOrder` then verifies the slice matches the expected order.

Because Temporal test execution is synchronous (no goroutines), the order is deterministic and matches the LIFO order of `saga.AddCompensation` calls.

## Failure Scenario Checklist

- [ ] Activity returns retryable error → Temporal retries, workflow eventually succeeds
- [ ] Activity returns business error → workflow fails immediately, no retry
- [ ] Disbursement fails before pivot → compensation runs in LIFO order
- [ ] Disbursement fails after pivot → compensation attempted, `CompensationError` emitted
- [ ] Duplicate execution (same workflowID) → idempotency key prevents double-processing
- [ ] Approval timeout → workflow returns `ApprovalResult{TimedOut: true}`
