package dossier

import (
	"errors"
	"fmt"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// ErrRevisionMismatch is returned by a mutating store operation whose
// PutOptions carried an ExpectedRevision that no longer matches the dossier's
// current revision. It lets callers implement optimistic concurrency: read a
// dossier (learning its revision), then write back only if nothing else has
// advanced it in between. Nothing is mutated when it fires.
var ErrRevisionMismatch = errors.New("dossier: revision mismatch")

// PutOptions carries optional controls for a mutating store operation. The zero
// value is always valid and imposes no constraints, so a caller that does not
// need optimistic concurrency simply passes PutOptions{}.
type PutOptions struct {
	// ExpectedRevision, when non-nil, guards the mutation with optimistic
	// concurrency: the write proceeds only if the dossier's current revision
	// (currentRevision - the number of version snapshots taken so far) equals
	// *ExpectedRevision, otherwise it fails with ErrRevisionMismatch and mutates
	// nothing. A dossier that has never been Put has revision 0.
	ExpectedRevision *int
}

// ExcludedRepository names a repository deliberately left out of a change's
// impact and the reason it was excluded. It mirrors the schema's
// impact.excluded_repositories element without importing internal/impact, so
// callers construct impact input with a plain, named struct.
type ExcludedRepository struct {
	Repository string
	Reason     string
}

// ImpactSection is the local, dependency-free mirror of the schema's impact
// block (repositories / excluded_repositories / missing_coverage). SetImpact
// accepts it so the caller need not build protocol types (and this package need
// not import internal/impact, avoiding an import cycle).
type ImpactSection struct {
	Repositories         []string
	ExcludedRepositories []ExcludedRepository
	MissingCoverage      []string
}

// currentRevision reports how many times the dossier has been superseded: the
// count of version snapshots taken so far. It is 0 before the first Put (Create
// takes no snapshot) and increments by one on every subsequent Put. It is the
// token PutOptions.ExpectedRevision is compared against.
func currentRevision(root, id string) int {
	return nextVersion(root, id) - 1
}

// checkRevision enforces PutOptions.ExpectedRevision when set, returning
// ErrRevisionMismatch (and mutating nothing) if the stored revision has moved.
func checkRevision(root, id string, opts PutOptions) error {
	if opts.ExpectedRevision == nil {
		return nil
	}
	if got := currentRevision(root, id); got != *opts.ExpectedRevision {
		return fmt.Errorf("dossier: expected revision %d, have %d: %w",
			*opts.ExpectedRevision, got, ErrRevisionMismatch)
	}
	return nil
}

// SetContradictions replaces the dossier's contradictions section with the
// given resolved/unresolved sets and versions the record (DOSSIER-008). The
// resolved/unresolved split is what Finalize reads: an unresolved contradiction
// is a blocking finding, so writing one here makes Finalize refuse to complete
// the dossier until it is moved to resolved. There is no stored "blocking" flag
// to recompute - blockingFindings derives it on demand from this section - so
// the write is the only state change needed. Slices are copied so a later
// mutation of the caller's arguments cannot alter the persisted record.
func SetContradictions(root, dossierID string, resolved, unresolved []string, opts PutOptions) (protocol.ChangeDossier, error) {
	d, err := readCurrent(root, dossierID)
	if err != nil {
		return protocol.ChangeDossier{}, err
	}
	if err := checkRevision(root, dossierID, opts); err != nil {
		return protocol.ChangeDossier{}, err
	}
	d.Contradictions = &protocol.ChangeDossierContradictions{
		Resolved:   append([]string(nil), resolved...),
		Unresolved: append([]string(nil), unresolved...),
	}
	return Put(root, d)
}

// SetImpact replaces the dossier's impact section from the dependency-free
// ImpactSection and versions the record (DOSSIER-009). Repositories and
// missing-coverage lists are copied; each excluded repository is mapped to the
// schema's excluded_repositories element (repository + reason). The impact
// section feeds the §38 "Repositories handled" indicator and the Impact section
// of both exports.
func SetImpact(root, dossierID string, section ImpactSection, opts PutOptions) (protocol.ChangeDossier, error) {
	d, err := readCurrent(root, dossierID)
	if err != nil {
		return protocol.ChangeDossier{}, err
	}
	if err := checkRevision(root, dossierID, opts); err != nil {
		return protocol.ChangeDossier{}, err
	}
	excluded := make([]protocol.ChangeDossierImpactExcludedRepositoriesElem, 0, len(section.ExcludedRepositories))
	for _, ex := range section.ExcludedRepositories {
		excluded = append(excluded, protocol.ChangeDossierImpactExcludedRepositoriesElem{
			Repository: ex.Repository,
			Reason:     ex.Reason,
		})
	}
	d.Impact = &protocol.ChangeDossierImpact{
		Repositories:         append([]string(nil), section.Repositories...),
		ExcludedRepositories: excluded,
		MissingCoverage:      append([]string(nil), section.MissingCoverage...),
	}
	return Put(root, d)
}
