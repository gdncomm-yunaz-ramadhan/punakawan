package roleconfig

import "github.com/ygrip/punakawan/pkg/protocol"

// defaultStyle is every role's recommended prompt style when none has been
// chosen. The four roles have no distinguishable default posture beyond this
// shared middle ground; only a project's explicit choice moves a role off it.
const defaultStyle = protocol.RolePreferenceStyleBalanced

// defaultRole returns role's recommended default preference: the shared
// default style and no free-text instructions.
func defaultRole(role Role) protocol.RolePreference {
	return protocol.RolePreference{Style: defaultStyle, Instructions: ""}
}

// Defaults returns the recommended configuration for all four roles at
// revision 0. This is what Load synthesizes for a project that has never
// had roles.yaml written, and what Reset restores a single role to.
func Defaults() protocol.RolePreferences {
	return protocol.RolePreferences{
		Version:  SupportedVersion,
		Revision: 0,
		Roles: protocol.RolePreferencesRoles{
			Semar:  defaultRole(Semar),
			Gareng: defaultRole(Gareng),
			Petruk: defaultRole(Petruk),
			Bagong: defaultRole(Bagong),
		},
	}
}
