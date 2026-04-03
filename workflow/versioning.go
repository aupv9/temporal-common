package workflow

import (
	"go.temporal.io/sdk/workflow"
)

// ChangeSet provides a named, declarative API over workflow.GetVersion.
//
// All Define calls MUST happen at the top of the workflow function, before
// any branching, to preserve replay determinism. GetVersion is recorded in
// the workflow history, so the call order must be identical on every replay.
//
// Usage:
//
//	changes := workflow.NewChangeSet(ctx)
//	changes.Define("add-fraud-check", 1)
//	changes.Define("new-notification-step", 1)
//
//	if changes.IsEnabled("add-fraud-check") {
//	    // new path
//	}
type ChangeSet struct {
	ctx     workflow.Context
	results map[string]workflow.Version
}

// NewChangeSet creates a ChangeSet bound to the given workflow context.
func NewChangeSet(ctx workflow.Context) *ChangeSet {
	return &ChangeSet{
		ctx:     ctx,
		results: make(map[string]workflow.Version),
	}
}

// Define registers a versioned change by calling workflow.GetVersion.
// name must be a unique, descriptive string per change (e.g. "add-kyc-step-2024-01").
// maxVersion is the highest version this code supports (typically 1 for new changes).
//
// Must be called before any branching that depends on the version.
func (c *ChangeSet) Define(name string, maxVersion int) {
	v := workflow.GetVersion(c.ctx, name, workflow.DefaultVersion, workflow.Version(maxVersion))
	c.results[name] = v
}

// IsEnabled returns true if the named change is at version >= 1 (i.e. the new code path).
// Returns false for DefaultVersion (old code path, for existing running instances).
func (c *ChangeSet) IsEnabled(name string) bool {
	v, ok := c.results[name]
	if !ok {
		return false
	}
	return v >= 1
}

// Version returns the raw workflow.Version for the named change.
// Use this for multi-step migrations where you need to distinguish between
// versions 1, 2, 3, etc.
// Returns workflow.DefaultVersion if the change was not defined.
func (c *ChangeSet) Version(name string) workflow.Version {
	v, ok := c.results[name]
	if !ok {
		return workflow.DefaultVersion
	}
	return v
}
