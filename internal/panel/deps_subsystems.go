package panel

import (
	"context"

	"github.com/ygrip/punakawan/internal/contradiction"
	"github.com/ygrip/punakawan/internal/dossier"
	"github.com/ygrip/punakawan/internal/impact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// This file extends ProjectSource (see deps.go) with the three project-scoped
// subsystems that share its id->root resolution and per-project .punakawan
// tree: the Contradiction Ledger, the Impact Graph, and Change Dossiers. Each
// subsystem's store (internal/contradiction, internal/impact,
// internal/dossier) is stateless and keyed by a workspace root, so every
// method resolves the root through resolveRoot and then calls the store func
// directly - exactly like the metadata and role-config surfaces already on
// ProjectSource.

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

