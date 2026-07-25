package panel

import (
	"context"
	"errors"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/contradiction"
	"github.com/ygrip/punakawan/internal/dossier"
	"github.com/ygrip/punakawan/internal/handoff"
	"github.com/ygrip/punakawan/internal/handoffprobe"
	"github.com/ygrip/punakawan/internal/impact"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// This file extends ProjectSource (see deps.go) with the four project-scoped
// subsystems that share its id->root resolution and per-project .punakawan
// tree: the Contradiction Ledger, the Impact Graph, Change Dossiers, and
// Handoff Capsules. Each subsystem's store (internal/contradiction,
// internal/impact, internal/dossier, internal/handoff) is stateless and keyed
// by a workspace root, so every method resolves the root through resolveRoot
// and then calls the store func directly - exactly like the metadata and
// role-config surfaces already on ProjectSource.

// --- ContradictionReader ---------------------------------------------------

// ListContradictions returns every record in the project's ledger.
func (s *ProjectSource) ListContradictions(ctx context.Context, projectID string) ([]protocol.Contradiction, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	return contradiction.List(root)
}

// GetContradiction returns one record, or contradiction.ErrNotFound.
func (s *ProjectSource) GetContradiction(ctx context.Context, projectID, id string) (*protocol.Contradiction, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	return contradiction.Get(root, id)
}

// CreateContradiction persists c and returns the stored record (with the
// server-stamped version and timestamps). The caller (handler) assigns c.Id
// when empty, so Put's non-empty-id contract is always satisfied here.
func (s *ProjectSource) CreateContradiction(ctx context.Context, projectID string, c protocol.Contradiction) (*protocol.Contradiction, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	if err := contradiction.Put(root, c, contradiction.PutOptions{}); err != nil {
		return nil, err
	}
	return contradiction.Get(root, c.Id)
}

// ProposeContradictionResolution records a proposed resolution and advances the
// record to resolution_proposed, returning the updated record.
func (s *ProjectSource) ProposeContradictionResolution(ctx context.Context, projectID, id, proposedStatement, rationale string, requiresHumanConfirmation bool) (*protocol.Contradiction, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	if err := contradiction.ProposeResolution(root, id, proposedStatement, rationale, requiresHumanConfirmation); err != nil {
		return nil, err
	}
	return contradiction.Get(root, id)
}

// ResolveContradiction records the confirmed statement and advances to resolved.
func (s *ProjectSource) ResolveContradiction(ctx context.Context, projectID, id, statement, by string) (*protocol.Contradiction, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	if err := contradiction.Resolve(root, id, statement, by); err != nil {
		return nil, err
	}
	return contradiction.Get(root, id)
}

// AcceptContradictionDivergence records an accepted divergence.
func (s *ProjectSource) AcceptContradictionDivergence(ctx context.Context, projectID, id, by string) (*protocol.Contradiction, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	if err := contradiction.AcceptDivergence(root, id, by); err != nil {
		return nil, err
	}
	return contradiction.Get(root, id)
}

// --- ImpactReader ----------------------------------------------------------

// ImpactNodes returns every node in the project's impact graph.
func (s *ProjectSource) ImpactNodes(ctx context.Context, projectID string) ([]protocol.ImpactNode, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	return impact.Nodes(root)
}

// ImpactNode returns one node and whether it exists.
func (s *ProjectSource) ImpactNode(ctx context.Context, projectID, nodeID string) (protocol.ImpactNode, bool, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return protocol.ImpactNode{}, false, err
	}
	return impact.GetNode(root, nodeID)
}

// QueryImpact answers "if subjectID changes, what is affected?".
func (s *ProjectSource) QueryImpact(ctx context.Context, projectID, subjectID string, depth int, include []string) (impact.ImpactResult, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return impact.ImpactResult{}, err
	}
	return impact.Query(root, subjectID, depth, include)
}

// RefreshImpact re-runs the impact builders to reconcile the graph.
func (s *ProjectSource) RefreshImpact(ctx context.Context, projectID string) error {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return err
	}
	return impact.Refresh(root)
}

// --- DossierReader ---------------------------------------------------------

// ListDossiers returns the current dossier per id (a summary: no claims or
// evidence). A missing dossier directory is a normal empty state.
func (s *ProjectSource) ListDossiers(ctx context.Context, projectID string) ([]protocol.ChangeDossier, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	ids, err := dossier.List(root)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.ChangeDossier, 0, len(ids))
	for _, id := range ids {
		loaded, err := dossier.Get(root, id)
		if err != nil {
			return nil, err
		}
		out = append(out, loaded.Dossier)
	}
	return out, nil
}

// CreateDossier initializes a brand-new dossier.
func (s *ProjectSource) CreateDossier(ctx context.Context, projectID string, d protocol.ChangeDossier) (protocol.ChangeDossier, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return protocol.ChangeDossier{}, err
	}
	return dossier.Create(root, d)
}

// GetDossier returns the full dossier plus its claims and evidence.
func (s *ProjectSource) GetDossier(ctx context.Context, projectID, id string) (dossier.Loaded, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return dossier.Loaded{}, err
	}
	return dossier.Get(root, id)
}

// AddDossierClaim appends a claim to the dossier's append-only claims log.
func (s *ProjectSource) AddDossierClaim(ctx context.Context, projectID, id string, claim protocol.DossierClaim) (protocol.DossierClaim, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return protocol.DossierClaim{}, err
	}
	return dossier.AddClaim(root, id, claim)
}

// VerifyDossierClaim records byRole's verification of a claim (ErrSelfVerification
// when byRole produced it).
func (s *ProjectSource) VerifyDossierClaim(ctx context.Context, projectID, id, claimID, byRole, note string) (protocol.DossierClaim, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return protocol.DossierClaim{}, err
	}
	return dossier.VerifyClaim(root, id, claimID, byRole, note)
}

// DisputeDossierClaim records byRole's dispute of a claim.
func (s *ProjectSource) DisputeDossierClaim(ctx context.Context, projectID, id, claimID, byRole, note string) (protocol.DossierClaim, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return protocol.DossierClaim{}, err
	}
	return dossier.DisputeClaim(root, id, claimID, byRole, note)
}

// AddDossierEvidence writes an evidence record for the dossier.
func (s *ProjectSource) AddDossierEvidence(ctx context.Context, projectID, id string, ev protocol.DossierEvidence) (protocol.DossierEvidence, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return protocol.DossierEvidence{}, err
	}
	return dossier.AddEvidence(root, id, ev)
}

// FinalizeDossier advances the dossier to completed, or returns a
// *dossier.BlockingError listing every blocker.
func (s *ProjectSource) FinalizeDossier(ctx context.Context, projectID, id string) error {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return err
	}
	return dossier.Finalize(root, id)
}

// ExportDossierMarkdown renders the human-readable dossier export.
func (s *ProjectSource) ExportDossierMarkdown(ctx context.Context, projectID, id string) (string, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return "", err
	}
	return dossier.ExportMarkdown(root, id)
}

// ExportDossierJSON renders the deterministic JSON dossier export.
func (s *ProjectSource) ExportDossierJSON(ctx context.Context, projectID, id string) ([]byte, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	return dossier.ExportJSON(root, id)
}

// --- HandoffReader ---------------------------------------------------------

// ListHandoffs returns every capsule in the project.
func (s *ProjectSource) ListHandoffs(ctx context.Context, projectID string) ([]protocol.HandoffCapsule, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	ids, err := handoff.List(root)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.HandoffCapsule, 0, len(ids))
	for _, id := range ids {
		h, err := handoff.Get(root, id)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

// GetHandoff returns one capsule (a synthesized empty one for an unknown id,
// matching handoff.Get's "absent capsule is a normal state" contract).
func (s *ProjectSource) GetHandoff(ctx context.Context, projectID, id string) (protocol.HandoffCapsule, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return protocol.HandoffCapsule{}, err
	}
	return handoff.Get(root, id)
}

// CreateHandoff persists a new capsule.
func (s *ProjectSource) CreateHandoff(ctx context.Context, projectID string, h protocol.HandoffCapsule) (protocol.HandoffCapsule, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return protocol.HandoffCapsule{}, err
	}
	return handoff.Create(root, h)
}

// ValidateHandoff classifies whether the capsule can be resumed, wiring the
// validation deps from the project's own stores (see buildValidationDeps).
func (s *ProjectSource) ValidateHandoff(ctx context.Context, projectID, id string) (handoff.ValidationResult, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return handoff.ValidationResult{}, err
	}
	return handoff.Validate(root, id, s.buildValidationDeps(root))
}

// ResumeHandoff returns the smallest necessary resume context, refusing a
// superseded capsule with contract.ErrHandoffSuperseded so the handler answers
// 409 rather than handing back a stale context.
func (s *ProjectSource) ResumeHandoff(ctx context.Context, projectID, id string) (map[string]any, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	res, err := handoff.Validate(root, id, s.buildValidationDeps(root))
	if err != nil {
		return nil, err
	}
	if res.Status == handoff.StatusSuperseded {
		return nil, contract.ErrHandoffSuperseded
	}
	return handoff.ResumeContext(root, id)
}

// SupersedeHandoff marks the capsule superseded and returns the updated capsule.
func (s *ProjectSource) SupersedeHandoff(ctx context.Context, projectID, id string) (protocol.HandoffCapsule, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return protocol.HandoffCapsule{}, err
	}
	if err := handoff.Supersede(root, id); err != nil {
		return protocol.HandoffCapsule{}, err
	}
	return handoff.Get(root, id)
}

// buildValidationDeps wires handoff.ValidationDeps from the project's stateless
// stores. Only the deps that are cheap and honest to check from this workspace
// are wired; the rest are left nil, which handoff.Validate treats as "cannot
// check -> passing" (never a fabricated failing result), per §42:
//
//   - PlanVersionExists    -> the project's plan store (artifact.PlanStore).
//   - RoleConfigRevision   -> roleconfig.Load(root).Revision.
//   - ContradictionsChanged-> the contradiction store: a listed open
//     contradiction that has since left an open status (detected/triaged/
//     needs_clarification) counts as materially changed.
//   - DossierSuperseded     -> the dossier store's current status.
//
// The remaining deps are left nil with a TODO rather than faked:
//   - TaskIsCurrent: bd has no cheap "is this still the current task" check
//     reachable from a workspace root here. TODO(handoff): wire once the task
//     snapshot service is threaded through ProjectSource.
//   - EvidenceExists: evidence artifacts are session-scoped and not resolvable
//     from a project root alone. TODO(handoff): wire to the evidence ledger.
//   - RepositoryStateMatches: comparing recorded vs. live git state needs a git
//     probe not available here. TODO(handoff): wire to a git status source.
func (s *ProjectSource) buildValidationDeps(root string) handoff.ValidationDeps {
	plans := &artifact.PlanStore{WorkspaceRoot: root}
	return handoff.ValidationDeps{
		PlanVersionExists: func(planID string, version int) (bool, error) {
			_, _, err := plans.Version(planID, version)
			if err == nil {
				return true, nil
			}
			if errors.Is(err, artifact.ErrVersionNotFound) || errors.Is(err, artifact.ErrPlanNotFound) {
				return false, nil
			}
			return false, err
		},
		RoleConfigRevision: func() (int, error) {
			cfg, err := roleconfig.Load(root)
			if err != nil {
				return 0, err
			}
			return cfg.Revision, nil
		},
		ContradictionsChanged: func(ids []string) ([]string, error) {
			changed := make([]string, 0, len(ids))
			for _, id := range ids {
				c, err := contradiction.Get(root, id)
				if err != nil {
					if errors.Is(err, contradiction.ErrNotFound) {
						// A contradiction that vanished from the ledger has
						// materially changed relative to the capsule.
						changed = append(changed, id)
						continue
					}
					return nil, err
				}
				if !isOpenContradiction(c.Status) {
					changed = append(changed, id)
				}
			}
			return changed, nil
		},
		DossierSuperseded: func(id string) (bool, error) {
			loaded, err := dossier.Get(root, id)
			if err != nil {
				return false, err
			}
			return loaded.Dossier.Status == protocol.ChangeDossierStatusSuperseded, nil
		},
		// Git tree-state, evidence-ledger, and task-currency probes come from
		// internal/handoffprobe (see that package for each probe's contract).
		RepositoryStateMatches: handoffprobe.RepositoryStateMatches(root),
		EvidenceExists:         handoffprobe.EvidenceExists(root),
		TaskIsCurrent:          handoffprobe.TaskIsCurrent(root),
	}
}

// isOpenContradiction reports whether a status is one of the three "still open"
// states of the §18 resolution chain. A capsule's listed open contradiction
// that has left this set (resolved, accepted_divergence, superseded,
// resolution_proposed) has materially changed since the handoff.
func isOpenContradiction(status protocol.ContradictionStatus) bool {
	switch status {
	case protocol.ContradictionStatusDetected,
		protocol.ContradictionStatusTriaged,
		protocol.ContradictionStatusNeedsClarification:
		return true
	default:
		return false
	}
}
