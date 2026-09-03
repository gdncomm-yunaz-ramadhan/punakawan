// Package panel wires the Punakawan Panel's HTTP server to a loaded
// *app.App, per punakawan-panel-implementation-plan.md. Version is the
// panel's own release version; the project has no separate build-time
// version stamping yet, so /api/v1/system reports this same value for
// both "panel version" and "punakawan version" until one exists.
package panel

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/panel/runtime"
	"github.com/ygrip/punakawan/internal/panel/sources"
	"github.com/ygrip/punakawan/internal/project"
	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Version is the Punakawan Panel's own release version.
const Version = "0.1.0"

// Readers bundles the read-only reader interfaces
// (internal/panel/contract) that every HTTP handler reaches Punakawan's
// data through.
type Readers struct {
	Workspace contract.WorkspaceReader
	Project   contract.ProjectReader
	Roles     contract.RolesReader
	// Delivery is nil when this panel instance could not reach the daemon
	// at startup (e.g. punakawand not installed). Delivery handlers degrade
	// to 503 instead of panicking. Server.Start populates it once connected.
	Delivery contract.DeliveryReader

	// Runtime is the bounded *app.App pool (Phase 3): it self-schedules its
	// own idle-expiry timer (reset on every Acquire/Release) and Closes all
	// non-primary runtimes on shutdown. Exposed here because NewReaders
	// constructs it.
	Runtime *runtime.ProjectRuntimeManager
}

// NewReaders builds Readers backed by internal/panel/sources'
// implementations over a. reg is the global workspace registry
// (WorkspaceReader uses it to reach every registered workspace, not just
// a's own).
func NewReaders(a *app.App, reg *registry.Store) Readers {
	// Bounded pool of loaded *app.App runtimes, seeded with the long-lived
	// primary. Non-primary workspaces are Acquire'd and reused across requests
	// instead of app.Load/Close per call; the primary is never evicted or
	// closed by the manager (Phase 3, §10.3). The cap and idle-shutdown window
	// are the manager's own internal defaults - there is no user-tunable
	// settings surface for them.
	// A panel started outside any project has no primary: the global
	// workspace is this machine's data directory, not a project somebody
	// would expect to see listed, pinned or resolvable by id.
	primaryID, primaryRoot := a.Workspace.ID, a.Workspace.Root
	if a.Workspace.Global {
		primaryID, primaryRoot = "", ""
	}
	runtimeMgr := runtime.NewManager(primaryID, a)

	// Front the deep per-workspace inspector with a stale-while-revalidate
	// snapshot cache so /workspaces, /overview, and the Tier-2 reconciler
	// serve cached counts instead of opening Dolt / running bd + git on every
	// request (Phase 1, §10.2). ttl=0 keeps snapshot.DefaultTTL.
	wsSource := &sources.WorkspaceSource{App: a, Registry: reg, Runtime: runtimeMgr}
	workspaceReader := sources.NewCachedWorkspaceReader(wsSource, reg, primaryID, 0)
	// The project source composes the (cached) workspace reader for its counts
	// and the registry for id->root resolution; it serves both the metadata
	// (ProjectReader) and role-config (RolesReader) surfaces, which share the
	// same id->root resolution and per-project .punakawan tree.
	projectSource := &ProjectSource{
		Workspace:   workspaceReader,
		Registry:    reg,
		PrimaryID:   primaryID,
		PrimaryRoot: primaryRoot,
		Runtime:     runtimeMgr,
	}
	return Readers{
		Workspace: workspaceReader,
		// The project source composes the cached workspace reader's
		// counts-only Summary and the registry for id->root resolution, so it
		// never re-runs the deep bd/dolt/git inspection whose result the
		// snapshot already holds (plan §8's "one snapshot, reused everywhere").
		Project: projectSource,
		Roles:   projectSource,
		Runtime: runtimeMgr,
	}
}

// ProjectSource implements contract.ProjectReader over the project files at
// each workspace root, per the project performance plan §3/§4. Reads reuse
// the injected ProjectWorkspaceReader's snapshot-backed per-workspace counts
// (repositories, knowledge, sessions) instead of duplicating that
// inspection;
// project identity and metadata come from internal/project.Load. Metadata
// mutations load the project fresh, apply an optimistically-locked change,
// and persist a new immutable revision via internal/project.Save.
//
// It lives in package panel (not internal/panel/sources or
// internal/panel/api) purely to keep the import graph acyclic: contract
// imports internal/project, and internal/panel/api imports panel, so the one
// package that can depend on contract, internal/project, registry, and app
// at once without a cycle is panel itself.
// ProjectWorkspaceReader is the workspace-side surface ProjectSource reads
// its per-project counts through.
//
// It is deliberately narrower than contract.WorkspaceReader, and in particular
// it has no Get: the project routes need the registered list and one project's
// counts, never the live per-source Health detail Get computes. Reading counts
// through Get is what made GET /api/v1/projects/{id} open the project's Dolt
// store and run `git status` per repository on every single request -
// measured at ~8s cold and ~2.6s warm for a project with several
// repositories, for numbers the snapshot cache was already maintaining.
// Summary serves those same counts from that snapshot.
type ProjectWorkspaceReader interface {
	List(ctx context.Context) ([]contract.WorkspaceSummary, error)
	Summary(ctx context.Context, projectID string) (contract.WorkspaceSummary, error)
}

type ProjectSource struct {
	Workspace ProjectWorkspaceReader
	Registry  *registry.Store
	// PrimaryID / PrimaryRoot describe the single workspace this panel
	// instance's *app.App was loaded for. They are the fallback when the
	// registry is empty or nil (Phase 1 parity): the primary workspace is
	// always resolvable even before it is registered.
	PrimaryID   string
	PrimaryRoot string
	// Runtime is consulted only on deregistration, to close any pooled
	// *app.App still held for a project that is no longer registered. Nil is
	// valid (nothing pooled to evict).
	Runtime *runtime.ProjectRuntimeManager

	// roleLocks serializes each project's roleconfig load->apply->save
	// sequence (keyed by resolved root) so two concurrent role mutations for
	// the same project can never interleave and silently drop one write -
	// roleconfig.Save has no on-disk compare-and-swap of its own.
	roleLocks sync.Map
}

// roleLock returns the mutex serializing roleconfig mutations for root,
// creating one on first use.
func (s *ProjectSource) roleLock(root string) *sync.Mutex {
	v, _ := s.roleLocks.LoadOrStore(root, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// Deregister removes the project from the panel's workspace registry.
//
// This deletes one row from the panel registry and nothing else. The
// workspace directory, its .punakawan tree, knowledge database, tasks,
// evidence, and repositories all stay exactly as they are on disk, and
// registering the same path again brings the project back (its pinned flag
// and original registration time are not restored). The registry holds no
// revision counter, so unlike the metadata mutations there is no
// base_revision to check - the row either exists or it does not.
func (s *ProjectSource) Deregister(ctx context.Context, projectID string) error {
	if projectID == s.PrimaryID {
		return fmt.Errorf("panel: project %q: %w", projectID, contract.ErrPrimaryProject)
	}
	// No registry at all means this panel instance has nowhere to deregister
	// from, which is a wiring fault rather than a bad project id.
	if s.Registry == nil {
		return fmt.Errorf("panel: deregister project %q: no workspace registry is configured", projectID)
	}
	if err := s.Registry.Remove(projectID); err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return fmt.Errorf("panel: project %q: %w", projectID, contract.ErrWorkspaceUnavailable)
		}
		return fmt.Errorf("panel: deregister project %q: %w", projectID, err)
	}
	// The pooled runtime outlives the registry row, so drop it too - otherwise
	// a re-registered project would be served by a runtime loaded against the
	// old row until the idle sweep got to it.
	//
	// A close failure is not reported back: the registry row is already gone,
	// so the deregistration did succeed, and failing the call would tell the
	// caller the opposite. The pool's idle sweep retries the close later.
	if s.Runtime != nil {
		_ = s.Runtime.Invalidate(projectID)
	}
	return nil
}

// resolveRoot maps a project id to the workspace root that contains its
// .punakawan/project.yaml. An unknown id yields contract.ErrWorkspaceUnavailable
// so handlers answer 404 rather than 500.
func (s *ProjectSource) resolveRoot(id string) (string, error) {
	if s.Registry != nil {
		entry, err := s.Registry.Get(id)
		if err == nil {
			return entry.Path, nil
		}
		if !errors.Is(err, registry.ErrNotFound) {
			return "", fmt.Errorf("panel: resolve project %q: %w", id, err)
		}
	}
	if id == s.PrimaryID && s.PrimaryRoot != "" {
		return s.PrimaryRoot, nil
	}
	return "", fmt.Errorf("panel: project %q: %w", id, contract.ErrWorkspaceUnavailable)
}

// summaryFromWorkspace folds a workspace summary's counts together with the
// project.yaml identity and metadata count into a ProjectSummary. A missing
// or unreadable project.yaml is not fatal: the workspace display name and a
// zero metadata count stand in, matching Load's "absent file is a normal
// state" contract.
func (s *ProjectSource) summaryFromWorkspace(ws contract.WorkspaceSummary) contract.ProjectSummary {
	name := ws.DisplayName
	description := ""
	metadataCount := 0
	if p, err := project.Load(ws.Path); err == nil {
		if p.Name != "" {
			name = p.Name
		}
		description = p.Description
		metadataCount = len(p.Metadata)
	}
	return contract.ProjectSummary{
		ID:                 ws.ID,
		Name:               name,
		Description:        description,
		Path:               ws.Path,
		Pinned:             ws.Pinned,
		Primary:            ws.Primary,
		Availability:       string(ws.Availability),
		RepositoryCount:    ws.RepositoryCount,
		ActiveSessionCount: ws.ActiveSessionCount,
		MetadataCount:      metadataCount,
	}
}

// List returns one ProjectSummary per workspace the WorkspaceReader knows
// about.
func (s *ProjectSource) List(ctx context.Context) ([]contract.ProjectSummary, error) {
	summaries, err := s.Workspace.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]contract.ProjectSummary, 0, len(summaries))
	for _, ws := range summaries {
		out = append(out, s.summaryFromWorkspace(ws))
	}
	return out, nil
}

// Summary describes one project by id, from the snapshot-backed counts rather
// than a live deep inspection - see ProjectWorkspaceReader.
func (s *ProjectSource) Summary(ctx context.Context, projectID string) (contract.ProjectSummary, error) {
	ws, err := s.Workspace.Summary(ctx, projectID)
	if err != nil {
		return contract.ProjectSummary{}, err
	}
	return s.summaryFromWorkspace(ws), nil
}

// Get loads the project's identity, metadata, and current revision.
func (s *ProjectSource) Get(ctx context.Context, projectID string) (*project.Project, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	return project.Load(root)
}

// AddMetadata validates and appends one entry under optimistic locking, then
// persists a new immutable revision.
func (s *ProjectSource) AddMetadata(ctx context.Context, projectID string, entry project.MetadataEntry, baseRevision int) (*project.Project, error) {
	return s.mutate(projectID, "add", entry.Key, func(p *project.Project) error {
		return p.AddMetadata(entry, baseRevision)
	})
}

// UpdateMetadata mutates one entry (nil description/value leave that field
// unchanged) under optimistic locking, then persists a new revision.
func (s *ProjectSource) UpdateMetadata(ctx context.Context, projectID, key string, newDescription *string, newValue any, baseRevision int) (*project.Project, error) {
	return s.mutate(projectID, "update", key, func(p *project.Project) error {
		return p.UpdateMetadata(key, newDescription, newValue, baseRevision)
	})
}

// DeleteMetadata removes one entry under optimistic locking, then persists a
// new revision.
func (s *ProjectSource) DeleteMetadata(ctx context.Context, projectID, key string, baseRevision int) (*project.Project, error) {
	return s.mutate(projectID, "delete", key, func(p *project.Project) error {
		return p.DeleteMetadata(key, baseRevision)
	})
}

// mutate is the shared load -> apply -> save path for the three metadata
// mutations. apply returns a project error (revision conflict, validation,
// unknown key) that is surfaced unchanged so the handler can map it.
func (s *ProjectSource) mutate(projectID, action, key string, apply func(*project.Project) error) (*project.Project, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	p, err := project.Load(root)
	if err != nil {
		return nil, err
	}
	if err := apply(p); err != nil {
		return nil, err
	}
	if err := project.Save(root, p, project.SaveOptions{Action: action, Key: key}); err != nil {
		return nil, err
	}
	return p, nil
}

// GetRoles loads the project's four-role prompt preferences. A project that
// has never had roles.yaml written yields the recommended defaults at
// revision 0, not an error, matching roleconfig.Load's "absent file is a
// normal state" contract.
func (s *ProjectSource) GetRoles(ctx context.Context, projectID string) (*protocol.RolePreferences, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
	return roleconfig.Load(root)
}

// UpdateRole applies patch to one role under optimistic locking, then persists a
// new immutable revision. An unknown role name yields roleconfig.ErrUnknownRole
// so the handler answers 404 rather than mutating the wrong field.
func (s *ProjectSource) UpdateRole(ctx context.Context, projectID, role string, patch roleconfig.Patch, baseRevision int) (*protocol.RolePreferences, error) {
	return s.mutateRoles(projectID, role, "update", func(cfg *protocol.RolePreferences) error {
		return roleconfig.Update(cfg, roleconfig.Role(role), patch, baseRevision)
	})
}

// ResetRole restores one role to its recommended defaults under the same
// optimistic locking as UpdateRole, then persists a new revision.
func (s *ProjectSource) ResetRole(ctx context.Context, projectID, role string, baseRevision int) (*protocol.RolePreferences, error) {
	return s.mutateRoles(projectID, role, "reset", func(cfg *protocol.RolePreferences) error {
		return roleconfig.Reset(cfg, roleconfig.Role(role), baseRevision)
	})
}

// mutateRoles is the shared load -> apply -> save path for the two role-config
// mutations, mirroring mutate for metadata. The role string is validated up
// front so an unknown role never reaches Load/Save (roleconfig.ErrUnknownRole is
// surfaced unchanged for the handler to map). The whole sequence runs under
// this project's roleLock so two concurrent mutations can never both read the
// same on-disk revision and have one silently overwrite the other. apply
// returns a roleconfig error (revision conflict, invalid style, instructions
// too long) surfaced verbatim.
func (s *ProjectSource) mutateRoles(projectID, role, action string, apply func(*protocol.RolePreferences) error) (*protocol.RolePreferences, error) {
	if !roleconfig.IsRole(role) {
		return nil, fmt.Errorf("panel: role %q: %w", role, roleconfig.ErrUnknownRole)
	}
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}

	lock := s.roleLock(root)
	lock.Lock()
	defer lock.Unlock()

	cfg, err := roleconfig.Load(root)
	if err != nil {
		return nil, err
	}
	if err := apply(cfg); err != nil {
		return nil, err
	}
	if err := roleconfig.Save(root, cfg, roleconfig.SaveOptions{Action: action, Role: role}); err != nil {
		return nil, err
	}
	return cfg, nil
}
