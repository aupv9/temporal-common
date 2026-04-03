# Saga Pattern

## Overview

The `Saga` manager implements the distributed saga pattern with **LIFO compensation** and **pivot point** support. It is safe to use inside Temporal workflows (single-threaded coroutine model — no mutex needed).

## Core API

```go
saga := temporalcommon.NewSaga(ctx)
defer saga.Compensate()               // rolls back all steps if not completed

saga.AddCompensation(Rollback, args)  // register after each successful step
saga.SetPivot()                       // mark point of no return
saga.Complete()                       // prevent rollback on success path
```

## LIFO Rollback Order

Compensations execute in reverse registration order:

```
Forward:      A → B → C (pivot) → D → E
Compensation: E → D → C → B → A
```

## Pivot Point

`SetPivot()` marks the boundary after which full rollback is impossible (e.g., after real money has moved). Steps after the pivot are still compensated LIFO, but a permanent failure there emits `CompensationError` (human intervention required) rather than retrying forever.

```go
saga.AddCompensation(ReleaseReservation, id)  // before pivot — safe to undo
saga.SetPivot()                               // funds committed externally
saga.AddCompensation(ReverseTransfer, id)     // after pivot — attempted, but may fail
```

## Compensation Activity Contract

All compensation activities **must**:
1. Be **idempotent** — safe to call multiple times with the same input
2. Never return `NewBusinessError` — business errors are non-retryable and will block the saga forever
3. Use **heartbeats** for operations > 10s

The saga runs compensation activities with `MaximumAttempts: 0` (unlimited retries) and a 1-hour `StartToCloseTimeout`.

## Failure Handling

If a compensation activity fails permanently (workflow terminated / context cancelled), the saga:
1. Logs a `CompensationError` with `sagaID` and `step` fields
2. Continues compensating remaining steps (does not short-circuit)
3. Returns the `CompensationError` as the workflow's terminal error

This triggers human intervention via alerting on `CompensationError` type.

## Usage in Tests

```go
env := testing.NewTestEnvironment(t)
env.TrackCompensations()

env.MockActivity(ReserveFundsActivity).Return(ReserveFundsResult{ID: "rsv-1"}, nil)
env.MockActivity(DisbursementActivity).ReturnError(errors.New("bank down"))
env.MockActivity(ReleaseReservationActivity).Return(nil)

env.ExecuteWorkflow(LoanDisbursementWorkflow, input)

env.AssertWorkflowFailed(t, "disbursement failed")
env.AssertCompensationOrder(t, ReleaseReservationActivity)
```
