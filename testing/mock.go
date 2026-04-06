package testing

import (
	"reflect"

	"github.com/stretchr/testify/mock"
)

// MockCall is a fluent builder for activity mock expectations.
// It wraps the opaque *internal.MockCallWrapper returned by OnActivity
// via reflection, so callers never need to import internal packages.
type MockCall struct {
	call   any          // *internal.ActivityMockCallWrapper from go.temporal.io/sdk
	fnType reflect.Type // activity function type — used by ReturnError to infer zero return values
}

// MockActivity sets up a mock expectation for the given activity function.
// When no argument matchers are provided, mock.Anything is generated for every
// parameter of activityFn so the expectation matches regardless of actual call values.
// Pass explicit matchers to assert on specific argument values.
//
// Example:
//
//	env.MockActivity(DebitFeeActivity).Return(DebitResult{TxID: "tx-1"}, nil)
//	env.MockActivity(CoreBankingActivity).ReturnError(errors.New("timeout"))
func (te *TestEnvironment) MockActivity(activityFn interface{}, args ...interface{}) *MockCall {
	if len(args) == 0 {
		// Auto-generate mock.Anything for every parameter so the expectation
		// matches any call to this activity regardless of argument values.
		fnType := reflect.TypeOf(activityFn)
		args = make([]interface{}, fnType.NumIn())
		for i := range args {
			args[i] = mock.Anything
		}
	}
	call := te.env.OnActivity(activityFn, args...)
	return &MockCall{call: call, fnType: reflect.TypeOf(activityFn)}
}

// Return specifies the return values for the mocked activity.
// Values must match the activity function's return signature.
func (mc *MockCall) Return(returnArgs ...interface{}) *MockCall {
	mc.invoke("Return", returnArgs...)
	return mc
}

// ReturnError mocks the activity to fail with err.
// All non-error return values are set to their zero values automatically,
// so the caller does not need to know the activity's result types.
//
// Example — activity returns (SomeResult, error):
//
//	env.MockActivity(MyActivity).ReturnError(errors.New("service down"))
func (mc *MockCall) ReturnError(err error) *MockCall {
	numOut := mc.fnType.NumOut()
	returnArgs := make([]interface{}, numOut)
	// Set all non-error outputs to their zero values.
	for i := 0; i < numOut-1; i++ {
		returnArgs[i] = reflect.Zero(mc.fnType.Out(i)).Interface()
	}
	// Last return value is always error for Temporal activities.
	returnArgs[numOut-1] = err
	mc.invoke("Return", returnArgs...)
	return mc
}

// Once constrains the mock to be called exactly once.
func (mc *MockCall) Once() *MockCall {
	mc.invoke("Once")
	return mc
}

// Times constrains the mock to be called exactly n times.
func (mc *MockCall) Times(n int) *MockCall {
	mc.invoke("Times", n)
	return mc
}

// Maybe marks the mock as optional — the test will not fail if never called.
func (mc *MockCall) Maybe() *MockCall {
	mc.invoke("Maybe")
	return mc
}

// RunFn sets a function to be called when the mocked activity is invoked.
// The fn signature must match the activity function signature exactly.
// Use this to capture arguments passed to the activity in a test:
//
//	env.MockActivity(MyDLQSinkActivity).RunFn(func(_ context.Context, evt CompensationFailureEvent) error {
//	    captured = evt
//	    return nil
//	})
func (mc *MockCall) RunFn(fn interface{}) *MockCall {
	mc.invoke("Return", fn)
	return mc
}

// invoke calls a method by name on the underlying MockCallWrapper via reflection.
// Handles variadic methods (e.g. Return(...interface{})) and non-variadic methods
// (e.g. Once(), Times(n), Maybe()) correctly.
func (mc *MockCall) invoke(method string, args ...interface{}) {
	v := reflect.ValueOf(mc.call)
	m := v.MethodByName(method)
	if !m.IsValid() {
		return
	}

	mt := m.Type()
	in := make([]reflect.Value, len(args))

	if mt.IsVariadic() {
		// For variadic methods (Return), each arg is passed individually.
		// Nils must become zero-value interface{} so reflect can place them in
		// the ...interface{} variadic slots without a type mismatch.
		elemType := mt.In(mt.NumIn() - 1).Elem() // element type of the variadic slice
		for i, a := range args {
			if a == nil {
				in[i] = reflect.Zero(elemType)
			} else {
				in[i] = reflect.ValueOf(a)
			}
		}
	} else {
		// Non-variadic method (Once, Times, Maybe): one reflect.Value per param.
		for i, a := range args {
			if a == nil {
				in[i] = reflect.Zero(mt.In(i))
			} else {
				in[i] = reflect.ValueOf(a)
			}
		}
	}

	m.Call(in)
}
