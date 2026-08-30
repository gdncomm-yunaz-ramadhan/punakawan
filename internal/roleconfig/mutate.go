package roleconfig

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// maxInstructionsLen is the free-text instructions bound (protocol
// roleconfig.schema.json's instructions.maxLength).
const maxInstructionsLen = 2000

// Sentinel errors, all errors.Is-matchable so the HTTP layer can map them to
// stable status codes and machine-readable codes (mirrors internal/project).
var (
	ErrRevisionConflict    = errors.New("roleconfig: base revision does not match current revision")
	ErrUnknownRole         = errors.New("roleconfig: unknown role")
	ErrInvalidStyle        = errors.New("roleconfig: invalid style")
	ErrInstructionsTooLong = errors.New("roleconfig: instructions exceed the 2000-character bound")
)

// Patch is a partial update to one role's prompt preference. A nil pointer
// field means "leave unchanged".
type Patch struct {
	Style        *protocol.RolePreferenceStyle
	Instructions *string
}

// ValidStyle reports whether s is one of strict|balanced|creative.
func ValidStyle(s protocol.RolePreferenceStyle) bool {
	switch s {
	case protocol.RolePreferenceStyleStrict, protocol.RolePreferenceStyleBalanced, protocol.RolePreferenceStyleCreative:
		return true
	}
	return false
}

// Update applies patch to role within cfg under optimistic locking:
// baseRevision must equal cfg.Revision or ErrRevisionConflict is returned and
// nothing is mutated. Style is validated against its enum; instructions is
// bounded to maxInstructionsLen runes. On success cfg.Revision is bumped by
// one; the caller persists with Save.
func Update(cfg *protocol.RolePreferences, role Role, patch Patch, baseRevision int) error {
	rp, err := RoleOf(cfg, role)
	if err != nil {
		return err
	}
	if baseRevision != cfg.Revision {
		return revisionConflict(baseRevision, cfg.Revision)
	}
	if patch.Style != nil && !ValidStyle(*patch.Style) {
		return fmt.Errorf("roleconfig: style %q: %w", *patch.Style, ErrInvalidStyle)
	}
	if patch.Instructions != nil && utf8.RuneCountInString(*patch.Instructions) > maxInstructionsLen {
		return fmt.Errorf("roleconfig: instructions length %d exceeds %d: %w",
			utf8.RuneCountInString(*patch.Instructions), maxInstructionsLen, ErrInstructionsTooLong)
	}

	// All checks passed; apply.
	if patch.Style != nil {
		rp.Style = *patch.Style
	}
	if patch.Instructions != nil {
		rp.Instructions = *patch.Instructions
	}
	cfg.Revision++
	return nil
}

// Reset restores role to its recommended default under the same optimistic
// locking as Update, bumping cfg.Revision on success.
func Reset(cfg *protocol.RolePreferences, role Role, baseRevision int) error {
	rp, err := RoleOf(cfg, role)
	if err != nil {
		return err
	}
	if baseRevision != cfg.Revision {
		return revisionConflict(baseRevision, cfg.Revision)
	}
	*rp = defaultRole(role)
	cfg.Revision++
	return nil
}

func revisionConflict(base, current int) error {
	return fmt.Errorf("roleconfig: base revision %d does not match current revision %d: %w", base, current, ErrRevisionConflict)
}
