package agentpolicy

import (
	"errors"
	"fmt"
)

// ErrRevisionConflict is returned by Update/Reset when the caller's
// baseRevision does not match the configuration's current revision -
// someone else saved a change since the caller last loaded it.
var ErrRevisionConflict = errors.New("agentpolicy: base revision does not match current revision")

// Patch is a partial update to a project's agent policy. A nil field means
// "leave unchanged"; a non-nil field replaces that whole sub-config, since
// (unlike internal/roleconfig's per-capability-key Capabilities patch) each
// of these is already a small, fixed-shape struct with no per-key ownership
// rules to enforce.
type Patch struct {
	Capabilities   *DeclaredCapabilities
	Orchestrator   *PurposePolicy
	Implementation *PurposePolicy
	Review         *PurposePolicy
}

// Update applies patch to cfg under optimistic locking: baseRevision must
// equal cfg.Revision or ErrRevisionConflict is returned and nothing is
// mutated. On success cfg.Revision is bumped by one; the caller persists
// with Save.
func Update(cfg *Config, patch Patch, baseRevision int) error {
	if baseRevision != cfg.Revision {
		return revisionConflict(baseRevision, cfg.Revision)
	}
	if patch.Capabilities != nil {
		cfg.Capabilities = *patch.Capabilities
	}
	if patch.Orchestrator != nil {
		cfg.Agents.Orchestrator = *patch.Orchestrator
	}
	if patch.Implementation != nil {
		cfg.Agents.Implementation = *patch.Implementation
	}
	if patch.Review != nil {
		cfg.Agents.Review = *patch.Review
	}
	cfg.Revision++
	return nil
}

// Reset restores cfg to its recommended defaults under the same optimistic
// locking as Update, bumping cfg.Revision on success.
func Reset(cfg *Config, baseRevision int) error {
	if baseRevision != cfg.Revision {
		return revisionConflict(baseRevision, cfg.Revision)
	}
	revision := cfg.Revision
	*cfg = Defaults()
	cfg.Revision = revision + 1
	return nil
}

func revisionConflict(base, current int) error {
	return fmt.Errorf("agentpolicy: base revision %d does not match current revision %d: %w", base, current, ErrRevisionConflict)
}
