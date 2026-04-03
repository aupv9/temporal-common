package workflow

import (
	"fmt"

	"go.temporal.io/sdk/workflow"
)

// ActivityCall describes a single activity invocation for parallel execution.
// Fn must be a registered activity function; Args are its arguments (excluding ctx).
// ResultPtr, if non-nil, receives the deserialised activity result.
type ActivityCall struct {
	// Fn is the activity function to execute. Must be registered with the worker.
	Fn interface{}

	// Args are the arguments passed to Fn (excluding the context.Context first arg).
	Args []interface{}

	// ResultPtr is an optional pointer into which the activity result is decoded.
	// Pass nil for activities that return only error.
	ResultPtr interface{}
}

// ParallelResult holds the outcome of a single activity in a parallel batch.
type ParallelResult struct {
	// Index is the position of this activity in the original ActivityCall slice.
	Index int

	// Err is non-nil if this activity failed.
	Err error
}

// ExecuteParallel runs all activities concurrently using a Temporal Selector
// (fan-out) and waits until all complete (fan-in). Activities run in parallel
// and are deterministic — each is scheduled as an independent Temporal task.
//
// Results are collected in the order activities complete, not input order.
// Use ParallelResult.Index to correlate results with the input slice.
//
// If any activity fails, ExecuteParallel still waits for the remaining ones
// and returns all errors combined. Callers can inspect individual errors via
// the returned []ParallelResult.
//
// Example:
//
//	results, err := workflow.ExecuteParallel(ctx, []workflow.ActivityCall{
//	    {Fn: CheckKYCActivity,      Args: []interface{}{kycInput},  ResultPtr: &kycResult},
//	    {Fn: CheckCreditActivity,   Args: []interface{}{creditInput}, ResultPtr: &creditResult},
//	    {Fn: CheckBlacklistActivity, Args: []interface{}{blInput}},
//	})
//	if err != nil {
//	    return err // at least one activity failed
//	}
func ExecuteParallel(ctx workflow.Context, calls []ActivityCall) ([]ParallelResult, error) {
	if len(calls) == 0 {
		return nil, nil
	}

	type indexedFuture struct {
		idx    int
		future workflow.Future
	}

	// Fan-out: schedule all activities concurrently.
	futures := make([]indexedFuture, len(calls))
	for i, call := range calls {
		futures[i] = indexedFuture{
			idx:    i,
			future: workflow.ExecuteActivity(ctx, call.Fn, call.Args...),
		}
	}

	// Fan-in: collect results in completion order using a selector.
	sel := workflow.NewSelector(ctx)
	results := make([]ParallelResult, 0, len(calls))

	for _, f := range futures {
		f := f // capture loop variable
		call := calls[f.idx]
		sel.AddFuture(f.future, func(fut workflow.Future) {
			var err error
			if call.ResultPtr != nil {
				err = fut.Get(ctx, call.ResultPtr)
			} else {
				err = fut.Get(ctx, nil)
			}
			results = append(results, ParallelResult{Index: f.idx, Err: err})
		})
	}

	// Block until all futures resolve.
	for i := 0; i < len(calls); i++ {
		sel.Select(ctx)
	}

	// Aggregate errors.
	var errs []error
	for _, r := range results {
		if r.Err != nil {
			errs = append(errs, fmt.Errorf("activity[%d]: %w", r.Index, r.Err))
		}
	}
	if len(errs) > 0 {
		return results, combineErrors(errs)
	}
	return results, nil
}

// ExecuteParallelBestEffort is like ExecuteParallel but never returns an error.
// Failed activities are recorded in ParallelResult.Err — the caller decides
// which failures are acceptable. Useful for non-critical parallel enrichments
// (notifications, analytics events) that should never block the main saga.
func ExecuteParallelBestEffort(ctx workflow.Context, calls []ActivityCall) []ParallelResult {
	results, _ := ExecuteParallel(ctx, calls)
	return results
}

// combineErrors joins multiple errors into a single descriptive error.
func combineErrors(errs []error) error {
	if len(errs) == 1 {
		return errs[0]
	}
	msg := fmt.Sprintf("%d activities failed:", len(errs))
	for _, e := range errs {
		msg += "\n  - " + e.Error()
	}
	return fmt.Errorf("%s", msg)
}
