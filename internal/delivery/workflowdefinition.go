// This file decouples this package's optional consultation of a
// workflow definition's per-role restrictions from
// internal/workflowdef's own concrete type, following the same pattern
// this package already uses elsewhere for other externally-owned
// dependencies: define the narrow interface this package actually
// needs, and let the integrator supply the real implementation. This
// package only ever needs two things from a workflow definition -
// "does this id exist and is it enabled" and "which role stages does it
// mark required" - never the full Definition struct, and
// internal/workflowdef persists to an entirely different location
// (one YAML file per id) than this package's own event log, so a Store
// here has no reason to own that store's lifecycle directly.
package delivery

import "context"

// WorkflowDefinitionResolver looks up an externally-owned workflow
// definition on this package's behalf: once at
// CreateOrchestrationWithOptions time (must exist and be enabled),
// and repeatedly at role-stage gate time (which of a lane's four stages
// it marks required). The integrator supplies the real implementation,
// backed by internal/workflowdef.Store. A Store with none configured
// behaves exactly as it did before workflow_definition_id existed for
// attachment: an attach attempt is rejected outright rather than
// silently accepted. Every gate check still falls back to this
// package's own default - Semar and Bagong required, Gareng and Petruk
// optional - regardless of whether a resolver is configured.
type WorkflowDefinitionResolver interface {
	// ValidateEnabled returns nil if workflowDefinitionID names an
	// existing, enabled workflow definition, or a descriptive error
	// otherwise.
	ValidateEnabled(ctx context.Context, workflowDefinitionID string) error

	// RequiredRoleStages reports, for each of semar/gareng/petruk/bagong
	// present in workflowDefinitionID's Roles map, whether that role is
	// required. A role name absent from the returned map falls back to
	// this package's own default (required for Semar/Bagong, optional
	// for Gareng/Petruk) - only an explicit entry overrides that
	// default for the named role. That default is enforced by this
	// package's own gate logic, not by implementations of this method.
	RequiredRoleStages(ctx context.Context, workflowDefinitionID string) (map[string]bool, error)
}

// StoreOption configures optional Store dependencies at construction.
// Adding this as a variadic NewStore parameter, rather than changing
// NewStore's required parameters, keeps every existing NewStore(db)
// call site compiling and behaving unchanged.
type StoreOption func(*Store)

// WithWorkflowDefinitionResolver configures Store to validate
// workflow_definition_id attachments and consult per-role restrictions
// through r. Without this option, attaching a workflow_definition_id is
// rejected, and every role-stage gate keeps applying this package's
// default (Semar and Bagong required, Gareng and Petruk optional)
// regardless of anything recorded on an orchestration.
func WithWorkflowDefinitionResolver(r WorkflowDefinitionResolver) StoreOption {
	return func(s *Store) { s.workflowDefinitions = r }
}

// resolveRequiredStages looks up which of a lane's four role stages
// workflowDefinitionID marks required. An empty id (no definition
// attached to this lane's orchestration) or no configured resolver both
// mean there is nothing to override, so the caller's own gate logic
// applies its default - Semar and Bagong required, Gareng and Petruk
// optional.
func (s *Store) resolveRequiredStages(ctx context.Context, workflowDefinitionID string) (map[string]bool, error) {
	if workflowDefinitionID == "" || s.workflowDefinitions == nil {
		return nil, nil
	}
	return s.workflowDefinitions.RequiredRoleStages(ctx, workflowDefinitionID)
}
