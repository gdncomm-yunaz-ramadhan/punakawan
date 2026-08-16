package profile

import (
	"fmt"
	"sort"

	"github.com/ygrip/punakawan/internal/learning"
	"github.com/ygrip/punakawan/internal/project"
)

// Conflict records one case where the repo-owned layer's explicit value for
// a key overrides an accepted learning proposal that targeted the same key.
// It carries enough for a caller (e.g. Panel) to explain the override
// without re-deriving it from the two layers itself.
type Conflict struct {
	Key            string `json:"key"`
	RepoOwnedValue any    `json:"repo_owned_value"`
	LearnedValue   any    `json:"learned_value,omitempty"`
	ProposalId     string `json:"proposal_id"`
	Reason         string `json:"reason"`
}

// Merge is the two hardcoded layers AC6 asks for: Repo is the repo-owned,
// read-only profile loaded by Load above; Overlay is the layer learned
// facts already get written into today (project.Project.Metadata,
// materialized by internal/learning's MetadataAdapter when a
// project_metadata proposal is accepted); Proposals is the proposal history
// a caller already has in hand (typically learning.Store.List()). Merge
// fetches neither layer itself and recognizes no precedence beyond these
// two fixed layers - it is deliberately not a general precedence engine,
// just a key-presence-wins comparison between exactly two layers.
type Merge struct {
	Repo      *RepoProfile
	Overlay   *project.Project
	Proposals []learning.Proposal
}

// Resolve returns the effective value for key across both layers: the
// repo-owned value always wins when present, so an explicit repo-owned
// entry stays in force even if a learning proposal for the same key was
// later accepted into the overlay. Resolve falls back to the overlay's
// project metadata value when the repo-owned layer has no entry for key,
// and reports ok=false when neither layer has one.
func (m Merge) Resolve(key string) (value any, source string, ok bool) {
	if v, found := m.Repo.Value(key); found {
		return v, "repo-owned", true
	}
	if m.Overlay != nil {
		if e, found := m.Overlay.MetadataFor(key); found {
			return e.Value, "global-overlay", true
		}
	}
	return nil, "", false
}

// Conflicts reports every accepted project_metadata learning proposal whose
// TargetId names a key the repo-owned profile also defines explicitly.
// Resolve silently prefers the repo-owned value in that case; Conflicts
// makes that override visible and queryable (AC6's "the conflict is
// visible") instead of leaving it only implicit in Resolve's return value.
// Rejected, pending, and rolled-back proposals are not conflicts: only an
// accepted proposal actually reached the overlay. Results are sorted by key
// for a deterministic order.
func (m Merge) Conflicts() []Conflict {
	if m.Repo == nil {
		return nil
	}
	var out []Conflict
	for _, p := range m.Proposals {
		if p.Status != learning.StatusAccepted || p.ArtifactType != learning.TypeMetadata {
			continue
		}
		repoVal, ok := m.Repo.Value(p.TargetId)
		if !ok {
			continue
		}
		var learnedVal any
		if m.Overlay != nil {
			if e, found := m.Overlay.MetadataFor(p.TargetId); found {
				learnedVal = e.Value
			}
		}
		out = append(out, Conflict{
			Key:            p.TargetId,
			RepoOwnedValue: repoVal,
			LearnedValue:   learnedVal,
			ProposalId:     p.Id,
			Reason: fmt.Sprintf(
				"repo-owned value for %q overrides accepted learned proposal %q",
				p.TargetId, p.Id),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
