package learning

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/project"
	"github.com/ygrip/punakawan/internal/workflowdef"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// The three adapters below make workflow definitions, project metadata, and
// knowledge records reviewable through the existing artifact-review acceptance
// path (agent-context plan §6.3). Each implements artifact.Store: the review
// UI reads Current/Version, and acceptance calls CreateVersion, which is the
// ONLY place canonical state is written — a proposal cannot alter canonical
// context before acceptance (plan Phase 4 exit criterion). Stale-base is
// detected by the accept handler comparing Current().RevisionHash to the
// proposal's recorded base; each adapter's RevisionHash therefore folds in the
// live revision so any intervening change is caught.

func marshalCanonical(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return b, nil
}

func clampVersion(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

func mintVersionID(headID string) (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("learning: mint version id: %w", err)
	}
	return fmt.Sprintf("%s+%s", headID, hex.EncodeToString(buf)), nil
}

// ---------------------------------------------------------------------------
// Workflow adapter
// ---------------------------------------------------------------------------

// WorkflowAdapter reviews a workflow definition. On acceptance it writes a new
// immutable definition revision but NEVER enables it: activation stays a
// separate explicit action (plan §8.5).
type WorkflowAdapter struct {
	Root      string
	locksOnce sync.Once
	locks     *artifact.KeyedMutex
}

func (a *WorkflowAdapter) LockArtifact(id string) func() {
	a.locksOnce.Do(func() { a.locks = artifact.NewKeyedMutex() })
	return a.locks.Lock(id)
}

func (a *WorkflowAdapter) reference(def workflowdef.Definition) (protocol.ArtifactReference, error) {
	content, err := marshalCanonical(def)
	if err != nil {
		return protocol.ArtifactReference{}, err
	}
	return protocol.ArtifactReference{
		Type:         protocol.ArtifactReferenceTypeWorkflow,
		Id:           def.ID,
		Version:      clampVersion(def.Revision),
		RevisionHash: artifact.Hash(content),
		Format:       protocol.ArtifactReferenceFormatJson,
	}, nil
}

func (a *WorkflowAdapter) Current(id string) (protocol.ArtifactReference, error) {
	store, err := workflowdef.Open(a.Root)
	if err != nil {
		return protocol.ArtifactReference{}, err
	}
	def, err := store.Get(id)
	if err != nil {
		return protocol.ArtifactReference{}, err
	}
	return a.reference(def)
}

func (a *WorkflowAdapter) Version(id string, version int) ([]byte, protocol.ArtifactReference, error) {
	ref, err := a.Current(id)
	if err != nil {
		return nil, protocol.ArtifactReference{}, err
	}
	if ref.Version != version {
		return nil, protocol.ArtifactReference{}, fmt.Errorf("learning: workflow %q version %d not available (current is %d)", id, version, ref.Version)
	}
	store, _ := workflowdef.Open(a.Root)
	def, err := store.Get(id)
	if err != nil {
		return nil, protocol.ArtifactReference{}, err
	}
	content, err := marshalCanonical(def)
	if err != nil {
		return nil, protocol.ArtifactReference{}, err
	}
	return content, ref, nil
}

func (a *WorkflowAdapter) CreateVersion(id, workspaceID string, content []byte, now time.Time) (protocol.ArtifactReference, error) {
	var proposed workflowdef.Definition
	if err := json.Unmarshal(content, &proposed); err != nil {
		return protocol.ArtifactReference{}, fmt.Errorf("learning: workflow candidate is not a valid definition: %w", err)
	}
	proposed.ID = id
	if proposed.Version == "" {
		proposed.Version = workflowdef.SchemaVersion
	}

	store, err := workflowdef.Open(a.Root)
	if err != nil {
		return protocol.ArtifactReference{}, err
	}
	// Preserve the current enabled state (or start disabled for a new
	// definition). Acceptance must never silently enable a workflow.
	enabled := false
	if cur, err := store.Get(id); err == nil {
		proposed.Revision = cur.Revision // satisfy optimistic-concurrency on Save
		enabled = cur.Enabled
	} else if !errors.Is(err, workflowdef.ErrNotFound) {
		return protocol.ArtifactReference{}, err
	}
	proposed.Enabled = enabled

	saved, err := store.Save(proposed)
	if err != nil {
		return protocol.ArtifactReference{}, err
	}
	return a.reference(saved)
}

// ---------------------------------------------------------------------------
// Project-metadata adapter
// ---------------------------------------------------------------------------

// MetadataAdapter reviews a single project-metadata entry, keyed by its
// metadata key. Current folds the project revision into the revision hash so
// any intervening metadata change on the project invalidates a stale base.
type MetadataAdapter struct {
	Root      string
	locksOnce sync.Once
	locks     *artifact.KeyedMutex
}

func (a *MetadataAdapter) LockArtifact(id string) func() {
	a.locksOnce.Do(func() { a.locks = artifact.NewKeyedMutex() })
	return a.locks.Lock(id)
}

// metadataDigestInput is what the revision hash is taken over: the entry (or
// its absence) AND the project revision, so any project mutation moves the
// hash and a stale-base acceptance is rejected.
type metadataDigestInput struct {
	Key             string `json:"key"`
	Present         bool   `json:"present"`
	Description     string `json:"description,omitempty"`
	Value           any    `json:"value,omitempty"`
	ProjectRevision int    `json:"project_revision"`
}

func (a *MetadataAdapter) reference(key string) (protocol.ArtifactReference, []byte, error) {
	proj, err := project.Load(a.Root)
	if err != nil {
		return protocol.ArtifactReference{}, nil, err
	}
	entry, present := proj.MetadataFor(key)
	digest := metadataDigestInput{Key: key, Present: present, ProjectRevision: proj.Revision}
	if present {
		digest.Description = entry.Description
		digest.Value = entry.Value
	}
	digestBytes, err := marshalCanonical(digest)
	if err != nil {
		return protocol.ArtifactReference{}, nil, err
	}
	// The diffable content is the entry itself (or an empty object).
	var content []byte
	if present {
		content, err = marshalCanonical(entry)
	} else {
		content, err = marshalCanonical(struct{}{})
	}
	if err != nil {
		return protocol.ArtifactReference{}, nil, err
	}
	ref := protocol.ArtifactReference{
		Type:         protocol.ArtifactReferenceTypeProjectMetadata,
		Id:           key,
		Version:      clampVersion(proj.Revision),
		RevisionHash: artifact.Hash(digestBytes),
		WorkspaceId:  proj.ID,
		Format:       protocol.ArtifactReferenceFormatJson,
	}
	return ref, content, nil
}

func (a *MetadataAdapter) Current(id string) (protocol.ArtifactReference, error) {
	ref, _, err := a.reference(id)
	return ref, err
}

func (a *MetadataAdapter) Version(id string, version int) ([]byte, protocol.ArtifactReference, error) {
	ref, content, err := a.reference(id)
	if err != nil {
		return nil, protocol.ArtifactReference{}, err
	}
	if ref.Version != version {
		return nil, protocol.ArtifactReference{}, fmt.Errorf("learning: metadata %q version %d not available (current is %d)", id, version, ref.Version)
	}
	return content, ref, nil
}

func (a *MetadataAdapter) CreateVersion(id, workspaceID string, content []byte, now time.Time) (protocol.ArtifactReference, error) {
	var entry project.MetadataEntry
	if err := json.Unmarshal(content, &entry); err != nil {
		return protocol.ArtifactReference{}, fmt.Errorf("learning: metadata candidate is not a valid entry: %w", err)
	}
	entry.Key = id

	proj, err := project.Load(a.Root)
	if err != nil {
		return protocol.ArtifactReference{}, err
	}
	base := proj.Revision
	action := "add"
	if _, present := proj.MetadataFor(id); present {
		action = "update"
		desc := entry.Description
		if err := proj.UpdateMetadata(id, &desc, entry.Value, base); err != nil {
			return protocol.ArtifactReference{}, err
		}
	} else {
		if err := proj.AddMetadata(entry, base); err != nil {
			return protocol.ArtifactReference{}, err
		}
	}
	if err := project.Save(a.Root, proj, project.SaveOptions{Now: now, Actor: "learning-acceptance", Action: action, Key: id}); err != nil {
		return protocol.ArtifactReference{}, err
	}
	return a.Current(id)
}

// ---------------------------------------------------------------------------
// Convention adapter
// ---------------------------------------------------------------------------

// conventionMetadataKey namespaces a convention id inside the project
// metadata keyspace, so an accepted convention ("no-ternary") can never
// collide with an unrelated genuine metadata entry that happens to share the
// same short name.
func conventionMetadataKey(id string) string { return "convention:" + id }

// ConventionAdapter reviews a single proposed project convention (e.g. a
// "no-ternary" example), keyed by a short convention id. It is deliberately
// the thinnest adapter in this file: rather than
// inventing a new canonical store, review type, and protocol enum value for
// conventions, it delegates every call to a MetadataAdapter over the
// namespaced key conventionMetadataKey(id) and simply presents the caller's
// own id back on the resulting reference. This is intentionally
// under-built for now: a convention accepted through this path materializes
// exactly like an accepted metadata entry (readable back via
// project.Project.MetadataFor), which is all AC4's vertical slice needs -
// it does not attempt to represent a convention as its own richer type.
type ConventionAdapter struct {
	Root      string
	locksOnce sync.Once
	locks     *artifact.KeyedMutex
}

func (a *ConventionAdapter) LockArtifact(id string) func() {
	a.locksOnce.Do(func() { a.locks = artifact.NewKeyedMutex() })
	return a.locks.Lock(id)
}

// presentConventionRef rewrites a MetadataAdapter reference for the
// namespaced key back onto the caller's own convention id, so a caller of
// ConventionAdapter never sees the "convention:" prefix leak through.
func presentConventionRef(id string, ref protocol.ArtifactReference) protocol.ArtifactReference {
	ref.Id = id
	return ref
}

func (a *ConventionAdapter) Current(id string) (protocol.ArtifactReference, error) {
	ref, err := (&MetadataAdapter{Root: a.Root}).Current(conventionMetadataKey(id))
	if err != nil {
		return protocol.ArtifactReference{}, err
	}
	return presentConventionRef(id, ref), nil
}

func (a *ConventionAdapter) Version(id string, version int) ([]byte, protocol.ArtifactReference, error) {
	content, ref, err := (&MetadataAdapter{Root: a.Root}).Version(conventionMetadataKey(id), version)
	if err != nil {
		return nil, protocol.ArtifactReference{}, err
	}
	return content, presentConventionRef(id, ref), nil
}

func (a *ConventionAdapter) CreateVersion(id, workspaceID string, content []byte, now time.Time) (protocol.ArtifactReference, error) {
	ref, err := (&MetadataAdapter{Root: a.Root}).CreateVersion(conventionMetadataKey(id), workspaceID, content, now)
	if err != nil {
		return protocol.ArtifactReference{}, err
	}
	return presentConventionRef(id, ref), nil
}

// Compile-time assertions that all four adapters satisfy artifact.Store.
var (
	_ artifact.Store = (*WorkflowAdapter)(nil)
	_ artifact.Store = (*MetadataAdapter)(nil)
	_ artifact.Store = (*ConventionAdapter)(nil)
)
