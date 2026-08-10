package roleconfig

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ygrip/punakawan/internal/learning"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// EffectiveRoleConfig is a role's configuration after a workflow's restrictions
// have been intersected with the project configuration (§15, §47). A workflow
// may only *reduce* a role's authority, never increase it beyond the project
// configuration, so every field here is <= the project setting.
type EffectiveRoleConfig struct {
	Enabled      bool
	Style        protocol.RoleConfigStyle
	Mode         protocol.RoleConfigMode
	Capabilities map[string]bool
}

// Restriction is a workflow's per-role restriction (§15). Mode, if set, is a
// ceiling the effective mode is clamped down to; it can never raise the mode.
// Capabilities entries that are false switch that capability off; entries that
// are true are ignored (a workflow cannot grant a capability the project
// disabled). A nil Restriction means "no workflow restriction".
type Restriction struct {
	Mode         *protocol.RoleConfigMode
	Capabilities map[string]bool
}

// modeRank orders the three modes so they can be compared as a ceiling:
// assist(0) < propose(1) < execute(2). An unrecognized mode ranks as assist,
// the most restrictive, so a corrupt value fails closed.
func modeRank(m protocol.RoleConfigMode) int {
	switch m {
	case protocol.RoleConfigModeExecute:
		return 2
	case protocol.RoleConfigModePropose:
		return 1
	default:
		return 0
	}
}

// minMode returns the more restrictive (lower-ranked) of a and b.
func minMode(a, b protocol.RoleConfigMode) protocol.RoleConfigMode {
	if modeRank(b) < modeRank(a) {
		return b
	}
	return a
}

// Effective intersects a role's project configuration rc with an optional
// workflow restriction, per §15's "a workflow must not increase permissions
// beyond the project role configuration". Mode is clamped to the lower of the
// project mode and any workflow ceiling; a capability is on only if the project
// has it on AND the workflow did not switch it off.
func Effective(rc protocol.RoleConfig, restriction *Restriction) EffectiveRoleConfig {
	eff := EffectiveRoleConfig{
		Enabled:      rc.Enabled,
		Style:        rc.Style,
		Mode:         rc.Mode,
		Capabilities: map[string]bool{},
	}
	for k, v := range rc.Capabilities {
		eff.Capabilities[k] = v
	}
	if restriction != nil {
		if restriction.Mode != nil {
			eff.Mode = minMode(eff.Mode, *restriction.Mode)
		}
		for k, v := range restriction.Capabilities {
			if !v {
				eff.Capabilities[k] = false // workflow may only reduce
			}
		}
	}
	return eff
}

// Resolver implements the §47 RoleConfigResolver over a Load function that maps
// a project id to its persisted configuration, plus an optional workflow
// restriction lookup. Keeping the lookups as funcs keeps this package free of
// any dependency on the panel registry or the workflow store.
type Resolver struct {
	// Load returns the persisted role configuration for a project id.
	Load func(projectID string) (*protocol.RoleConfiguration, error)
	// Restrictions returns the per-role restriction a workflow imposes, or nil.
	// It may be nil itself, in which case no workflow ever restricts.
	Restrictions func(projectID, workflowID string, role Role) (*Restriction, error)
}

// Get returns the project-level configuration for role.
func (r Resolver) Get(projectID string, role Role) (protocol.RoleConfig, error) {
	cfg, err := r.Load(projectID)
	if err != nil {
		return protocol.RoleConfig{}, err
	}
	rc, err := RoleOf(cfg, role)
	if err != nil {
		return protocol.RoleConfig{}, err
	}
	return *rc, nil
}

// Effective returns role's configuration for project after applying the given
// workflow's restrictions. workflowID may be empty for "no workflow".
func (r Resolver) Effective(projectID, workflowID string, role Role) (EffectiveRoleConfig, error) {
	rc, err := r.Get(projectID, role)
	if err != nil {
		return EffectiveRoleConfig{}, err
	}
	var restriction *Restriction
	if workflowID != "" && r.Restrictions != nil {
		restriction, err = r.Restrictions(projectID, workflowID, role)
		if err != nil {
			return EffectiveRoleConfig{}, err
		}
	}
	return Effective(rc, restriction), nil
}

// ErrNotAuthorized is returned by Authorize when an action is not permitted.
// It wraps a concrete reason so callers can surface it, and is errors.Is-able.
type ErrNotAuthorized struct{ Reason string }

func (e ErrNotAuthorized) Error() string { return "roleconfig: not authorized: " + e.Reason }

// Authorize is the server-side gate (§49): it decides whether a role may take
// an action that requires capability (may be empty when the action is not
// capability-gated) at the needed mode level (assist=read, propose=create
// proposals, execute=perform actions). It fails closed: a disabled role, a
// disabled capability, or an effective mode below what the action needs all
// deny. This is the authoritative check - prompts are not security controls.
func Authorize(eff EffectiveRoleConfig, capability string, needed protocol.RoleConfigMode) error {
	if !eff.Enabled {
		return ErrNotAuthorized{Reason: "role is disabled"}
	}
	if modeRank(eff.Mode) < modeRank(needed) {
		return ErrNotAuthorized{Reason: fmt.Sprintf("role mode %q is below required %q", eff.Mode, needed)}
	}
	if capability != "" && !eff.Capabilities[capability] {
		return ErrNotAuthorized{Reason: fmt.Sprintf("capability %q is disabled", capability)}
	}
	return nil
}

// PromptBlock renders the compact role-configuration block injected into a
// role's prompt (§48). It lists style, mode, enabled and disabled capabilities
// (sorted for determinism), and a one-line mode reminder, followed by a
// "Learned project facts" section rendering proposals - detected facts, user
// corrections, or reviewer-approved conventions - that are currently accepted
// (punokawan-14yn.9 AC3/AC4). proposals is typically a project's
// learning.Store.List() output; it is filtered and gated internally (see
// LearnedFactsBlock), so callers may pass the store's raw, unfiltered list.
// This is guidance for the model; Authorize is the enforcement.
func PromptBlock(role Role, eff EffectiveRoleConfig, proposals []learning.Proposal) string {
	var enabled, disabled []string
	for k, v := range eff.Capabilities {
		if v {
			enabled = append(enabled, k)
		} else {
			disabled = append(disabled, k)
		}
	}
	sort.Strings(enabled)
	sort.Strings(disabled)

	var b strings.Builder
	fmt.Fprintf(&b, "Role configuration (%s):\n", role)
	fmt.Fprintf(&b, "- Style: %s\n", eff.Style)
	fmt.Fprintf(&b, "- Mode: %s\n", eff.Mode)
	b.WriteString("- Enabled:\n")
	if len(enabled) == 0 {
		b.WriteString("  - (none)\n")
	}
	for _, k := range enabled {
		fmt.Fprintf(&b, "  - %s\n", k)
	}
	if len(disabled) > 0 {
		b.WriteString("- Disabled:\n")
		for _, k := range disabled {
			fmt.Fprintf(&b, "  - %s\n", k)
		}
	}
	b.WriteString("- " + modeReminder(eff.Mode))

	if facts := LearnedFactsBlock(proposals); facts != "" {
		b.WriteString("\n\n")
		b.WriteString(facts)
	}
	return b.String()
}

// LearnedFactsBlock renders the subset of proposals that are currently
// accepted (Status == learning.StatusAccepted) into a compact section
// matching PromptBlock's heading/list style, or "" when there is nothing to
// show. A proposal folds to exactly one Status per id (learning.Store.List's
// fold-to-latest semantics), so pending, rejected, and rolled-back proposals -
// including one that was accepted and later rolled back - are excluded by
// this single check without any separate "rolled back" filter: a rollback
// appends a fresh row whose Status is StatusRolledBack, which folds over the
// prior accepted row. This is the literal AC4 gate: an inferred convention
// (or any other proposal) stays invisible to a role's context until it is
// accepted, and disappears again if later rolled back.
func LearnedFactsBlock(proposals []learning.Proposal) string {
	accepted := make([]learning.Proposal, 0, len(proposals))
	for _, p := range proposals {
		if p.Status == learning.StatusAccepted {
			accepted = append(accepted, p)
		}
	}
	if len(accepted) == 0 {
		return ""
	}
	// Newest-updated first, tie-broken by id for determinism.
	sort.Slice(accepted, func(i, j int) bool {
		if !accepted[i].UpdatedAt.Equal(accepted[j].UpdatedAt) {
			return accepted[i].UpdatedAt.After(accepted[j].UpdatedAt)
		}
		return accepted[i].Id < accepted[j].Id
	})

	var b strings.Builder
	b.WriteString("Learned project facts:\n")
	for _, p := range accepted {
		detail := p.Rationale
		if detail == "" {
			detail = p.TargetId
		}
		fmt.Fprintf(&b, "  - [%s] %s\n", p.ArtifactType, detail)
	}
	return strings.TrimRight(b.String(), "\n")
}

func modeReminder(m protocol.RoleConfigMode) string {
	switch m {
	case protocol.RoleConfigModeExecute:
		return "You may execute enabled actions, under project policy and human approval."
	case protocol.RoleConfigModePropose:
		return "You may propose durable changes but may not execute them."
	default:
		return "You may read and analyze only; you may not make durable changes."
	}
}
