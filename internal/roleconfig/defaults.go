package roleconfig

import "github.com/ygrip/punakawan/pkg/protocol"

// ownedCapabilities is the fixed set of capability toggles each role may carry,
// per plan §8-11. A capability not listed for a role is not owned by it and is
// rejected by validation - this is what stops the API accepting, say, Bagong's
// modify_files. Order is preserved (slice, not set) because it drives the order
// of toggles rendered in the Panel and listed in the prompt block.
var ownedCapabilities = map[Role][]string{
	Semar:  {"workflows", "clarification", "coordinate_roles", "change_dossier"},
	Gareng: {"contradictions", "cross_repository_impact", "security_checks", "blocking_risks", "change_dossier"},
	Petruk: {"plans", "tasks", "modify_files", "cross_repository_changes", "create_pull_request", "change_dossier"},
	Bagong: {"plan_verification", "rerun_checks", "cross_repository_verification", "challenge_dossier", "block_completion", "review_pull_request"},
}

// OwnedCapabilities returns the capability keys role may carry, in Panel/prompt
// order. The returned slice is a copy, safe for the caller to mutate.
func OwnedCapabilities(role Role) []string {
	src := ownedCapabilities[role]
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// ownsCapability reports whether role owns capability key.
func ownsCapability(role Role, key string) bool {
	for _, k := range ownedCapabilities[role] {
		if k == key {
			return true
		}
	}
	return false
}

// recommendedStyleMode is the plan's §7 default posture per role.
var recommendedStyleMode = map[Role]struct {
	style protocol.RoleConfigStyle
	mode  protocol.RoleConfigMode
}{
	Semar:  {protocol.RoleConfigStyleBalanced, protocol.RoleConfigModeExecute},
	Gareng: {protocol.RoleConfigStyleStrict, protocol.RoleConfigModePropose},
	Petruk: {protocol.RoleConfigStyleCreative, protocol.RoleConfigModeExecute},
	Bagong: {protocol.RoleConfigStyleStrict, protocol.RoleConfigModePropose},
}

// defaultRole builds a role's recommended default: enabled, its recommended
// style/mode, and every owned capability turned on (§8-11 all default true).
func defaultRole(role Role) protocol.RoleConfig {
	sm := recommendedStyleMode[role]
	caps := make(map[string]bool, len(ownedCapabilities[role]))
	for _, k := range ownedCapabilities[role] {
		caps[k] = true
	}
	return protocol.RoleConfig{
		Enabled:      true,
		Style:        sm.style,
		Mode:         sm.mode,
		Capabilities: caps,
	}
}

// Defaults returns the recommended configuration for all four roles at
// revision 0 (plan §7, §12). This is what Load synthesizes for a project that
// has never had roles.yaml written, and what Reset restores a single role to.
func Defaults() protocol.RoleConfiguration {
	return protocol.RoleConfiguration{
		Version:  SupportedVersion,
		Revision: 0,
		Roles: protocol.RoleConfigurationRoles{
			Semar:  defaultRole(Semar),
			Gareng: defaultRole(Gareng),
			Petruk: defaultRole(Petruk),
			Bagong: defaultRole(Bagong),
		},
	}
}
