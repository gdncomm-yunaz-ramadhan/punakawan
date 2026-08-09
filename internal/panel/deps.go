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
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/panel/runtime"
	"github.com/ygrip/punakawan/internal/panel/settings"
	"github.com/ygrip/punakawan/internal/panel/sources"
	"github.com/ygrip/punakawan/internal/panel/tasksnapshot"
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
	Workspace     contract.WorkspaceReader
	Session       contract.SessionReader
	Task          contract.TaskReader
	Knowledge     contract.KnowledgeReader
	Evidence      contract.EvidenceReader
	Approval      contract.ApprovalReader
	GlobalSearch  contract.GlobalSearchReader
	Project       contract.ProjectReader
	Roles         contract.RolesReader
	Contradiction contract.ContradictionReader
	Impact        contract.ImpactReader
	Dossier       contract.DossierReader

	// Runtime is the bounded *app.App pool (Phase 3). The server owns its
	// lifecycle: it runs a periodic CloseIdle sweep and Closes all non-primary
	// runtimes on shutdown. Exposed here because NewReaders constructs it.
	Runtime *runtime.ProjectRuntimeManager
}

// NewReaders builds Readers backed by internal/panel/sources'
// implementations over a. reg is the global workspace registry
// (WorkspaceReader and GlobalSearchReader use it to reach every
// registered workspace, not just a's own).
func NewReaders(a *app.App, reg *registry.Store) Readers {
	// One shared task snapshot service: the board, table, dependency graph,
	// and count widgets reuse a single `bd list` + `bd ready` refresh instead
	// of each shelling out to bd independently (Phase 5, §8/§12).
	taskSnap := tasksnapshot.NewService(tasksnapshot.BeadsRunner(a.Supervisor, a.Workspace.Root))

	// Bounded pool of loaded *app.App runtimes, seeded with the long-lived
	// primary. Non-primary workspaces are Acquire'd and reused across requests
	// instead of app.Load/Close per call; the primary is never evicted or
	// closed by the manager (Phase 3, §10.3). The cap and idle-shutdown window
	// come from the user-tunable panel settings (each live runtime backs one
	// dolt sql-server, so this bounds dolt resource use); a live SetMaxActive
	// from the System panel adjusts the running pool.
	st := settings.Load(a.Workspace.Root)
	runtimeMgr := runtime.NewManager(a.Workspace.ID, a,
		runtime.WithMaxActive(st.MaxActiveRuntimes),
		runtime.WithIdleTimeout(time.Duration(st.RuntimeIdleTimeoutSeconds)*time.Second),
	)

	// Front the deep per-workspace inspector with a stale-while-revalidate
	// snapshot cache so /workspaces, /overview, and the Tier-2 reconciler
	// serve cached counts instead of opening Dolt / running bd + git on every
	// request (Phase 1, §10.2). ttl=0 keeps snapshot.DefaultTTL.
	wsSource := &sources.WorkspaceSource{App: a, Registry: reg, Runtime: runtimeMgr}
	workspaceReader := sources.NewCachedWorkspaceReader(wsSource, reg, a.Workspace.ID, 0)
	// The project source composes the (cached) workspace reader for its counts
	// and the registry for id->root resolution; it serves both the metadata
	// (ProjectReader) and role-config (RolesReader) surfaces, which share the
	// same id->root resolution and per-project .punakawan tree.
	projectSource := &ProjectSource{
		Workspace:   workspaceReader,
		Registry:    reg,
		PrimaryID:   a.Workspace.ID,
		PrimaryRoot: a.Workspace.Root,
	}
	return Readers{
		Workspace:    workspaceReader,
		Session:      &sources.SessionSource{App: a},
		Task:         &sources.TaskSource{App: a, Snapshot: taskSnap},
		Knowledge:    &sources.KnowledgeSource{App: a},
		Evidence:     &sources.EvidenceSource{App: a},
		Approval:     &sources.ApprovalSource{App: a},
		GlobalSearch: &sources.GlobalSearchSource{App: a, Registry: reg, Runtime: runtimeMgr},
		// The project source composes the (cached) workspace reader for its
		// counts and the registry for id->root resolution, so it never
		// re-runs the deep bd/dolt/git inspection the workspace reader
		// already performs (plan §8's "one snapshot, reused everywhere").
		Project: projectSource,
		Roles:   projectSource,
		// The same shared ProjectSource also serves the three project-scoped
		// subsystems (Contradiction Ledger, Impact Graph, Change Dossiers);
		// they all reuse its id->root resolution and per-project .punakawan
		// tree.
		Contradiction: projectSource,
		Impact:        projectSource,
		Dossier:       projectSource,
		Runtime:       runtimeMgr,
	}
}

// ProjectSource implements contract.ProjectReader over the project files at
// each workspace root, per the project performance plan §3/§4. Reads reuse
// the injected WorkspaceReader for the per-workspace counts (repositories,
// knowledge, tasks, sessions) instead of duplicating that inspection;
// project identity and metadata come from internal/project.Load. Metadata
// mutations load the project fresh, apply an optimistically-locked change,
// and persist a new immutable revision via internal/project.Save.
//
// It lives in package panel (not internal/panel/sources or
// internal/panel/api) purely to keep the import graph acyclic: contract
// imports internal/project, and internal/panel/api imports panel, so the one
// package that can depend on contract, internal/project, registry, and app
// at once without a cycle is panel itself.
type ProjectSource struct {
	Workspace contract.WorkspaceReader
	Registry  *registry.Store
	// PrimaryID / PrimaryRoot describe the single workspace this panel
	// instance's *app.App was loaded for. They are the fallback when the
	// registry is empty or nil (Phase 1 parity): the primary workspace is
	// always resolvable even before it is registered.
	PrimaryID   string
	PrimaryRoot string
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
		KnowledgeCount:     ws.KnowledgeCount,
		OpenTaskCount:      ws.OpenTaskCount,
		BlockedTaskCount:   ws.BlockedTaskCount,
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

// Summary describes one project by id.
func (s *ProjectSource) Summary(ctx context.Context, projectID string) (contract.ProjectSummary, error) {
	detail, err := s.Workspace.Get(ctx, projectID)
	if err != nil {
		return contract.ProjectSummary{}, err
	}
	return s.summaryFromWorkspace(detail.WorkspaceSummary), nil
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

// GetRoles loads the project's four-role configuration and returns it alongside
// the static owned-capability catalog for all four roles (so the Panel renders
// which toggles each role carries in one round-trip). A project that has never
// had roles.yaml written yields the recommended defaults at revision 0, not an
// error, matching roleconfig.Load's "absent file is a normal state" contract.
func (s *ProjectSource) GetRoles(ctx context.Context, projectID string) (*protocol.RoleConfiguration, []contract.RoleCapabilityInfo, error) {
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := roleconfig.Load(root)
	if err != nil {
		return nil, nil, err
	}
	owned := make([]contract.RoleCapabilityInfo, 0, len(roleconfig.AllRoles))
	for _, role := range roleconfig.AllRoles {
		owned = append(owned, contract.RoleCapabilityInfo{
			Role:         string(role),
			Capabilities: roleconfig.OwnedCapabilities(role),
		})
	}
	return cfg, owned, nil
}

// UpdateRole applies patch to one role under optimistic locking, then persists a
// new immutable revision. An unknown role name yields roleconfig.ErrUnknownRole
// so the handler answers 404 rather than mutating the wrong field.
func (s *ProjectSource) UpdateRole(ctx context.Context, projectID, role string, patch roleconfig.Patch, baseRevision int) (*protocol.RoleConfiguration, error) {
	return s.mutateRoles(projectID, role, "update", func(cfg *protocol.RoleConfiguration) error {
		return roleconfig.Update(cfg, roleconfig.Role(role), patch, baseRevision)
	})
}

// ResetRole restores one role to its recommended defaults under the same
// optimistic locking as UpdateRole, then persists a new revision.
func (s *ProjectSource) ResetRole(ctx context.Context, projectID, role string, baseRevision int) (*protocol.RoleConfiguration, error) {
	return s.mutateRoles(projectID, role, "reset", func(cfg *protocol.RoleConfiguration) error {
		return roleconfig.Reset(cfg, roleconfig.Role(role), baseRevision)
	})
}

// mutateRoles is the shared load -> apply -> save path for the two role-config
// mutations, mirroring mutate for metadata. The role string is validated up
// front so an unknown role never reaches Load/Save (roleconfig.ErrUnknownRole is
// surfaced unchanged for the handler to map). apply returns a roleconfig error
// (revision conflict, invalid style/mode, unowned capability) surfaced verbatim.
func (s *ProjectSource) mutateRoles(projectID, role, action string, apply func(*protocol.RoleConfiguration) error) (*protocol.RoleConfiguration, error) {
	if !roleconfig.IsRole(role) {
		return nil, fmt.Errorf("panel: role %q: %w", role, roleconfig.ErrUnknownRole)
	}
	root, err := s.resolveRoot(projectID)
	if err != nil {
		return nil, err
	}
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
