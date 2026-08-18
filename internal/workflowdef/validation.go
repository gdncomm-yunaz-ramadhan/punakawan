package workflowdef

import (
	"errors"
	"fmt"
	"strings"
)

// Validation errors. Callers use errors.Is to branch (e.g. the HTTP handler
// maps ErrUnknownCapability/ErrCommandNotAllowed to a 400 with a machine
// code). The wrapped message names the offending value.
var (
	// ErrMissingField is returned when a required field (id/name/version) is
	// absent.
	ErrMissingField = errors.New("workflowdef: missing required field")
	// ErrBadVersion is returned when Version is not SchemaVersion.
	ErrBadVersion = errors.New("workflowdef: unsupported schema version")
	// ErrDuplicateStepID is returned when two steps share an id.
	ErrDuplicateStepID = errors.New("workflowdef: duplicate step id")
	// ErrUnknownStepRef is returned when a Step.InputFrom names a step id that
	// is not a prior step.
	ErrUnknownStepRef = errors.New("workflowdef: input_from references unknown step")
	// ErrUnknownCapability is returned when a step or allowed_capabilities
	// entry names a capability not in the CapabilitySet.
	ErrUnknownCapability = errors.New("workflowdef: unknown capability")
	// ErrCommandNotAllowed is returned when a capability value looks like an
	// arbitrary command string (contains whitespace or shell metacharacters)
	// rather than a bare capability identifier.
	ErrCommandNotAllowed = errors.New("workflowdef: arbitrary command not allowed as capability")
	// ErrRolesAndSteps is returned when a definition declares both roles and
	// steps. Invocation dispatches on the presence of roles alone: a non-empty
	// roles map sends the definition to the delivery engine, whose fixed
	// lane/lease/role-stage sequence has no place to run a step graph, so the
	// steps would be dropped without a trace. Rejecting the combination up
	// front is the only way the author learns that one half of what they wrote
	// would never run.
	ErrRolesAndSteps = errors.New("workflowdef: definition declares both roles and steps")
)

// Validate checks a definition against the plan §6.2 constraints and the set
// of registered capabilities. It returns the first violation found, wrapped
// so the caller can both errors.Is the sentinel and read the offending value.
//
// Rules enforced:
//   - id, name and version are present;
//   - version == SchemaVersion;
//   - step ids are unique;
//   - every capability value is a bare identifier, not a command string;
//   - every Step.Capability and every AllowedCapabilities entry is registered;
//   - every Step.InputFrom entry refers to an earlier step's id;
//   - roles and steps are not both declared.
func Validate(def Definition, caps CapabilitySet) error {
	if strings.TrimSpace(def.ID) == "" {
		return fmt.Errorf("%w: id", ErrMissingField)
	}
	if strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("%w: name", ErrMissingField)
	}
	if strings.TrimSpace(def.Version) == "" {
		return fmt.Errorf("%w: version", ErrMissingField)
	}
	if def.Version != SchemaVersion {
		return fmt.Errorf("%w: %q (want %q)", ErrBadVersion, def.Version, SchemaVersion)
	}
	if len(def.Roles) > 0 && len(def.Steps) > 0 {
		return fmt.Errorf("%w: %d role(s) and %d step(s); only the roles are honoured — the definition runs as a delivery orchestration and none of its steps execute. Drop one or the other", ErrRolesAndSteps, len(def.Roles), len(def.Steps))
	}

	// allowed_capabilities entries must be registered bare identifiers.
	for _, cap := range def.AllowedCapabilities {
		if err := checkCapability(cap, caps); err != nil {
			return err
		}
	}

	seen := make(map[string]struct{}, len(def.Steps))
	for _, step := range def.Steps {
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("%w: step id", ErrMissingField)
		}
		if _, dup := seen[step.ID]; dup {
			return fmt.Errorf("%w: %q", ErrDuplicateStepID, step.ID)
		}
		if err := checkCapability(step.Capability, caps); err != nil {
			return err
		}
		// input_from may only reference steps already declared above, which
		// also rules out self-references and forward references.
		for _, ref := range step.InputFrom {
			if _, ok := seen[ref]; !ok {
				return fmt.Errorf("%w: step %q input_from %q", ErrUnknownStepRef, step.ID, ref)
			}
		}
		seen[step.ID] = struct{}{}
	}
	return nil
}

// checkCapability rejects command-like strings and unregistered identifiers.
// A capability is a bare identifier; anything containing whitespace or a shell
// metacharacter (; | & > < $ ` ) is treated as an attempt to smuggle a command
// and rejected with ErrCommandNotAllowed before the registry is consulted.
func checkCapability(name string, caps CapabilitySet) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: capability", ErrMissingField)
	}
	if strings.ContainsAny(name, " \t\n;|&><$`") {
		return fmt.Errorf("%w: %q", ErrCommandNotAllowed, name)
	}
	if !caps.Has(name) {
		return fmt.Errorf("%w: %q", ErrUnknownCapability, name)
	}
	return nil
}
