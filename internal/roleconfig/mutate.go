package roleconfig

import (
	"errors"
	"fmt"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Sentinel errors, all errors.Is-matchable so the HTTP layer can map them to
// stable status codes and machine-readable codes (mirrors internal/project).
var (
	ErrRevisionConflict  = errors.New("roleconfig: base revision does not match current revision")
	ErrUnknownRole       = errors.New("roleconfig: unknown role")
	ErrInvalidStyle      = errors.New("roleconfig: invalid style")
	ErrInvalidMode       = errors.New("roleconfig: invalid mode")
	ErrUnownedCapability = errors.New("roleconfig: capability not owned by role")
)

// Patch is a partial update to one role's configuration. A nil pointer field
// means "leave unchanged"; a non-nil Capabilities merges (each named key is set
// to its given value, keys absent from the map are left as-is), so the Panel
// can toggle one capability without resending the whole set.
type Patch struct {
	Enabled      *bool
	Style        *protocol.RoleConfigStyle
	Mode         *protocol.RoleConfigMode
	Capabilities map[string]bool
}

// ValidStyle reports whether s is one of strict|balanced|creative.
func ValidStyle(s protocol.RoleConfigStyle) bool {
	switch s {
	case protocol.RoleConfigStyleStrict, protocol.RoleConfigStyleBalanced, protocol.RoleConfigStyleCreative:
		return true
	}
	return false
}

// ValidMode reports whether m is one of assist|propose|execute.
func ValidMode(m protocol.RoleConfigMode) bool {
	switch m {
	case protocol.RoleConfigModeAssist, protocol.RoleConfigModePropose, protocol.RoleConfigModeExecute:
		return true
	}
	return false
}

// Update applies patch to role within cfg under optimistic locking: baseRevision
// must equal cfg.Revision or ErrRevisionConflict is returned and nothing is
// mutated. Style/mode are validated against their enums; every capability key
// in the patch must be owned by the role (ErrUnownedCapability otherwise) - a
// role can never be granted another role's capability. On success cfg.Revision
// is bumped by one; the caller persists with Save.
func Update(cfg *protocol.RoleConfiguration, role Role, patch Patch, baseRevision int) error {
	rc, err := RoleOf(cfg, role)
	if err != nil {
		return err
	}
	if baseRevision != cfg.Revision {
		return revisionConflict(baseRevision, cfg.Revision)
	}
	if patch.Style != nil && !ValidStyle(*patch.Style) {
		return fmt.Errorf("roleconfig: style %q: %w", *patch.Style, ErrInvalidStyle)
	}
	if patch.Mode != nil && !ValidMode(*patch.Mode) {
		return fmt.Errorf("roleconfig: mode %q: %w", *patch.Mode, ErrInvalidMode)
	}
	for key := range patch.Capabilities {
		if !ownsCapability(role, key) {
			return fmt.Errorf("roleconfig: role %q does not own capability %q: %w", role, key, ErrUnownedCapability)
		}
	}

	// All checks passed; apply. Capabilities merges onto a defensive copy so a
	// partial failure above could never have left a half-applied map.
	if patch.Enabled != nil {
		rc.Enabled = *patch.Enabled
	}
	if patch.Style != nil {
		rc.Style = *patch.Style
	}
	if patch.Mode != nil {
		rc.Mode = *patch.Mode
	}
	if len(patch.Capabilities) > 0 {
		if rc.Capabilities == nil {
			rc.Capabilities = map[string]bool{}
		}
		for key, val := range patch.Capabilities {
			rc.Capabilities[key] = val
		}
	}
	cfg.Revision++
	return nil
}

// Reset restores role to its recommended defaults under the same optimistic
// locking as Update, bumping cfg.Revision on success.
func Reset(cfg *protocol.RoleConfiguration, role Role, baseRevision int) error {
	rc, err := RoleOf(cfg, role)
	if err != nil {
		return err
	}
	if baseRevision != cfg.Revision {
		return revisionConflict(baseRevision, cfg.Revision)
	}
	*rc = defaultRole(role)
	cfg.Revision++
	return nil
}

func revisionConflict(base, current int) error {
	return fmt.Errorf("roleconfig: base revision %d does not match current revision %d: %w", base, current, ErrRevisionConflict)
}
