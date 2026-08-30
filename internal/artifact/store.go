package artifact

import (
	"errors"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// ErrVersionNotFound is returned by a Store's Version when the requested
// version number does not exist anywhere in id's lineage.
var ErrVersionNotFound = errors.New("artifact: version not found")

// Store is §4's per-artifact-type contract every panel review/proposal
// handler actually needs: "version reader" (Current/Version) and
// "acceptance handler" (CreateVersion) - the minimal surface that used to
// be hardcoded as a concrete *PlanStore parameter throughout
// internal/panel/api. Diff generation (DiffLines/UnifiedDiff) and hashing
// (Hash) are already type-agnostic free functions in this package and do
// not belong on the interface; a proposal renderer and validator are
// likewise handled generically by internal/validation plus each type's own
// content shape, not by a method here.
//
// internal/recipe's RecipeStore adapter (kept in that package, not here,
// so internal/artifact never needs to import internal/recipe) satisfies
// this interface, letting internal/panel/api's handlers accept whichever
// store matches the artifact type in the request path instead of being
// compiled against one concrete type.
type Store interface {
	// Current returns id's current (latest) version reference.
	Current(id string) (protocol.ArtifactReference, error)
	// Version returns one specific version's canonical content and
	// reference.
	Version(id string, version int) ([]byte, protocol.ArtifactReference, error)
	// CreateVersion writes content as the next immutable version of id
	// and repoints its current-version pointer at it.
	CreateVersion(id, workspaceID string, content []byte, now time.Time) (protocol.ArtifactReference, error)
	// LockArtifact serializes a caller's own multi-step read-check-write
	// sequence against id (e.g. AcceptProposalHandler's conflict-check-
	// then-CreateVersion) - see KeyedMutex's doc comment for why this
	// can't just be each method's own internal locking.
	LockArtifact(id string) func()
}
