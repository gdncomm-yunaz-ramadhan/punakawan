package roleconfig

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ygrip/punakawan/internal/learning"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// styleGuidance is the fixed prompt-guidance text for each style. This is the
// entire effect a style has on a served prompt: it is wording only. It never
// authorizes a tool or gates a workflow stage - the real enforcement is
// internal/workflow's and the delivery scheduler's own role-stage
// requirements, which do not read this package at all.
var styleGuidance = map[protocol.RolePreferenceStyle]string{
	protocol.RolePreferenceStyleStrict:   "Verify every required input, cite concrete evidence, and reject unsupported assumptions.",
	protocol.RolePreferenceStyleBalanced: "Use evidence proportionate to risk and prefer the simplest sufficient plan.",
	protocol.RolePreferenceStyleCreative: "Explore multiple viable approaches, then choose one using explicit trade-offs.",
}

// PromptGuidance renders role's concrete prompt block from pref: the role's
// fixed style guidance, followed by pref.Instructions (already bounded to
// maxInstructionsLen characters by Update) when set. This is the whole of
// what a prompt preference contributes to a served prompt - it never
// mentions permission or approval, because it does not grant or gate
// anything.
func PromptGuidance(role Role, pref protocol.RolePreference) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Role prompt preferences (%s):\n", role)
	fmt.Fprintf(&b, "- %s\n", styleGuidance[pref.Style])
	if pref.Instructions != "" {
		fmt.Fprintf(&b, "- %s\n", pref.Instructions)
	}
	return strings.TrimRight(b.String(), "\n")
}

// PromptBlock renders PromptGuidance plus a "Learned project facts" section
// (see LearnedFactsBlock) listing currently accepted learning proposals, for
// appending to a served role prompt. proposals is typically a project's
// learning.Store.List() output; it is filtered and gated internally, so
// callers may pass the store's raw, unfiltered list.
func PromptBlock(role Role, pref protocol.RolePreference, proposals []learning.Proposal) string {
	block := PromptGuidance(role, pref)
	if facts := LearnedFactsBlock(proposals); facts != "" {
		block += "\n\n" + facts
	}
	return block
}

// LearnedFactsBlock renders the subset of proposals that are currently
// accepted (Status == learning.StatusAccepted) into a compact section, or ""
// when there is nothing to show. A proposal folds to exactly one Status per
// id (learning.Store.List's fold-to-latest semantics), so pending, rejected,
// and rolled-back proposals - including one that was accepted and later
// rolled back - are excluded by this single check without any separate
// "rolled back" filter: a rollback appends a fresh row whose Status is
// StatusRolledBack, which folds over the prior accepted row. This is the
// literal gate an inferred convention (or any other proposal) must clear: it
// stays invisible to a role's context until it is accepted, and disappears
// again if later rolled back.
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

// Resolver resolves a project's persisted role prompt preferences. Load maps
// a project id to its persisted preferences file.
type Resolver struct {
	Load func(projectID string) (*protocol.RolePreferences, error)
}

// Get returns role's prompt preference for project.
func (r Resolver) Get(projectID string, role Role) (protocol.RolePreference, error) {
	cfg, err := r.Load(projectID)
	if err != nil {
		return protocol.RolePreference{}, err
	}
	rp, err := RoleOf(cfg, role)
	if err != nil {
		return protocol.RolePreference{}, err
	}
	return *rp, nil
}
