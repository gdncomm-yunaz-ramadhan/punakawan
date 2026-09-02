package agent

import (
	"fmt"
	"strings"
)

// SchemaChecker reports whether id names a known structured-output
// schema. internal/agent defines this interface itself, rather than
// depending on a concrete schema package, so its own tests can supply a
// fabricated checker; KnowledgeSchemaChecker (schema.go) is the real
// implementation.
type SchemaChecker interface {
	Has(id string) bool
}

// CapabilityChecker reports whether name is a registered capability
// (an MCP tool name, or an adapter operation id). internal/agent defines
// this interface itself - rather than importing internal/capability
// directly - to avoid an import-cycle risk (internal/capability is wired
// up from internal/mcpserver, which will in turn depend on
// internal/agent) and to keep this package's own tests simple. The real
// *capability.Registry already exposes Has(name string) bool and
// satisfies this interface structurally, with no adapter type needed.
type CapabilityChecker interface {
	Has(name string) bool
}

// Validate checks specs for internal consistency and, where a checker is
// supplied, against the real schema and capability surfaces. A nil
// schemaRegistry or capRegistry skips the corresponding check entirely -
// useful for unit tests that only care about duplicate-ID detection, or
// for a caller that doesn't yet have one of the two registries built.
//
// Validate is meant to run once at startup (see
// internal/mcpserver/server.go's assembleServer, after
// registerPublicTools has populated the real capability.Registry): a
// manifest that references a tool or output schema that doesn't exist is
// a build-time/startup error, not something to discover at request time.
func Validate(specs []RoleSpec, schemaRegistry SchemaChecker, capRegistry CapabilityChecker) error {
	var errs []string

	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		if seen[spec.ID] {
			errs = append(errs, fmt.Sprintf("duplicate role id %q", spec.ID))
			continue
		}
		seen[spec.ID] = true

		if schemaRegistry != nil && spec.OutputSchemaID != "" {
			if !schemaRegistry.Has(spec.OutputSchemaID) {
				errs = append(errs, fmt.Sprintf("role %q: unknown output_schema %q", spec.ID, spec.OutputSchemaID))
			}
		}

		if capRegistry != nil {
			for _, name := range spec.ToolPolicy.AllowedTools {
				if !capRegistry.Has(name) {
					errs = append(errs, fmt.Sprintf("role %q: unknown allowed tool %q", spec.ID, name))
				}
			}
			for _, name := range spec.ToolPolicy.DeniedTools {
				if !capRegistry.Has(name) {
					errs = append(errs, fmt.Sprintf("role %q: unknown denied tool %q", spec.ID, name))
				}
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("agent: validate: %s", strings.Join(errs, "; "))
	}
	return nil
}
