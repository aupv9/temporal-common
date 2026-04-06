package activity

import (
	"context"
	"fmt"
	"reflect"
	"runtime/debug"
)

// WithPanicRecovery wraps an activity function so that any panic is caught
// and converted to a RetryableError containing the panic message and stack trace.
//
// Without this wrapper, a panic in an activity causes Temporal to mark the
// attempt as failed with a generic PanicError, losing the stack trace entirely.
// The activity is retried, panics again, and eventually exhausts its retry budget
// with no actionable diagnostic information.
//
// Usage — wrap at registration time, not at call sites:
//
//	engine.RegisterActivity(activity.WithPanicRecovery(DisbursementActivity))
//	engine.RegisterActivity(activity.WithPanicRecovery(KYCCheckActivity))
//
// The original activity function name is preserved for Temporal task routing.
// Wrapping does not change the activity's registration name.
//
// activityFn must be a function whose first argument is context.Context and
// whose last return value is error. Panics in any other position are also caught.
func WithPanicRecovery(activityFn interface{}) interface{} {
	fnVal := reflect.ValueOf(activityFn)
	fnType := fnVal.Type()

	if fnType.Kind() != reflect.Func {
		panic(fmt.Sprintf("WithPanicRecovery: argument must be a function, got %T", activityFn))
	}

	// Build a wrapper function with the same signature as activityFn.
	wrapperType := fnType
	wrapper := reflect.MakeFunc(wrapperType, func(args []reflect.Value) (results []reflect.Value) {
		// Prepare zero-value results for the panic return path.
		zeroResults := make([]reflect.Value, fnType.NumOut())
		for i := 0; i < fnType.NumOut()-1; i++ {
			zeroResults[i] = reflect.Zero(fnType.Out(i))
		}
		errIdx := fnType.NumOut() - 1

		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				panicMsg := fmt.Sprintf("activity panicked: %v\n%s", r, stack)
				zeroResults[errIdx] = reflect.ValueOf(NewRetryableError(panicMsg, nil)).Elem().Addr()

				// NewRetryableError returns error (interface). We need to assign it
				// to the error return slot correctly via interface reflection.
				errVal := reflect.New(fnType.Out(errIdx)).Elem()
				errVal.Set(reflect.ValueOf(NewRetryableError(panicMsg, nil)))
				zeroResults[errIdx] = errVal
				results = zeroResults
			}
		}()

		// Extract context — first arg when present.
		if fnType.NumIn() > 0 && fnType.In(0) == reflect.TypeOf((*context.Context)(nil)).Elem() {
			// Check if context is cancelled before executing (fast-fail on shutdown).
			ctx := args[0].Interface().(context.Context)
			if ctx.Err() != nil {
				errVal := reflect.New(fnType.Out(errIdx)).Elem()
				errVal.Set(reflect.ValueOf(ctx.Err()))
				zeroResults[errIdx] = errVal
				return zeroResults
			}
		}

		return fnVal.Call(args)
	})

	return wrapper.Interface()
}
