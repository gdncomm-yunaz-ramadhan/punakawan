package workflowdef

import (
	"context"
	"errors"
	"fmt"
)

// ErrDisabled is returned by Invoke when the definition is not enabled.
var ErrDisabled = errors.New("workflowdef: definition is disabled")

// Invoker starts a run from a workflow definition. Invoke re-validates the
// definition (enabled + capabilities) and then delegates the actual run
// creation, returning the new run's id.
type Invoker interface {
	Invoke(ctx context.Context, def Definition, inputs map[string]any) (runID string, err error)
}

// RunCreator is the injected hook that turns a validated definition +
// resolved inputs into a concrete workflow run and returns its id. The
// integrator supplies an implementation that calls the real run-creation path
// (internal/workflow / the create_workflow_run tool). Keeping it as a function
// value means this package does not import internal/workflow or mcpserver, so
// it stays decoupled and unit-testable with a fake.
type RunCreator func(ctx context.Context, def Definition, inputs map[string]any) (string, error)

// defaultInvoker is the standard Invoker. It re-checks enabled state and
// capability membership on every invocation (a definition on disk may
// reference a capability that has since been removed from the registry, or may
// have been disabled after the caller listed it) before delegating to create.
type defaultInvoker struct {
	caps   CapabilitySet
	create RunCreator
}

// NewInvoker returns an Invoker that validates against caps and delegates run
// creation to create. create must be non-nil.
func NewInvoker(caps CapabilitySet, create RunCreator) Invoker {
	return &defaultInvoker{caps: caps, create: create}
}

// Invoke re-validates def against the capability set, rejects a disabled
// definition, then delegates run creation to the injected RunCreator.
func (i *defaultInvoker) Invoke(ctx context.Context, def Definition, inputs map[string]any) (string, error) {
	if i.create == nil {
		return "", errors.New("workflowdef: no RunCreator configured")
	}
	if !def.Enabled {
		return "", fmt.Errorf("%w: %q", ErrDisabled, def.ID)
	}
	if err := Validate(def, i.caps); err != nil {
		return "", err
	}
	runID, err := i.create(ctx, def, inputs)
	if err != nil {
		return "", fmt.Errorf("workflowdef: create run for %q: %w", def.ID, err)
	}
	return runID, nil
}
