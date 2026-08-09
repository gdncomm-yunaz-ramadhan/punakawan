package mcpserver

import (
	"fmt"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// authorizeRoleSubmit is the ROLE-010 server-side gate (§49) run at the start
// of a role-submission handler. It resolves role's effective configuration
// for the primary project (no workflow restriction) and enforces that the role
// is enabled, at least at propose mode, and - when capability is non-empty -
// that the capability is on. capability is "" for actions gated by mode alone.
//
// It is a no-op when the resolver is nil (no roles wiring, e.g. tests with no
// roles.yaml) so existing behavior is preserved; under the §7 defaults
// (gareng/bagong propose, petruk execute, all capabilities on) every check
// here passes. A resolver read failure is also skipped rather than blocking:
// Authorize is a reduce-only gate layered on top of the existing approval
// controls, not the sole security boundary.
func authorizeRoleSubmit(a *app.App, role roleconfig.Role, capability string) error {
	if a.RoleConfig == nil {
		return nil
	}
	eff, err := a.RoleConfig.Effective("", "", role)
	if err != nil {
		return nil
	}
	if err := roleconfig.Authorize(eff, capability, protocol.RoleConfigModePropose); err != nil {
		if capability != "" {
			return fmt.Errorf("mcpserver: role %q may not perform this action (capability %q, mode %q): %w", role, capability, protocol.RoleConfigModePropose, err)
		}
		return fmt.Errorf("mcpserver: role %q may not perform this action (mode %q): %w", role, protocol.RoleConfigModePropose, err)
	}
	return nil
}

// recordID builds the pkw:<kind>/<workspace>/<localID> id (§6.2) for a role
// submission. Callers only supply the short local id; the server fills in
// the workspace segment itself so a client cannot submit a record under
// the wrong workspace by mistake.
func recordID(a *app.App, kind, localID string) string {
	return fmt.Sprintf("pkw:%s/%s/%s", kind, a.Workspace.ID, localID)
}

// SubmitOutput is the common confirmation shape every submit_* tool returns.
type SubmitOutput struct {
	Id   string                       `json:"id"`
	Type protocol.KnowledgeRecordType `json:"type"`
}
