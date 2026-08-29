package sources

import (
	"context"
	"fmt"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/panel/runtime"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// AppResolver resolves the *app.App that backs a project id so a reader can
// serve any registered project, not just the one the panel started for. The
// primary app is returned directly (no pooling); every other project is
// Acquire'd from the bounded runtime pool (Phase 3) and its release func is
// returned so the caller can hand the runtime back after the read.
//
// It is the shared core of the Project* readers below: each wraps one call in
// with() and delegates to the ordinary single-app source over the resolved
// app, so the project-scoped read routes reuse the exact reader logic the
// primary-workspace routes do.
type AppResolver struct {
	PrimaryID string
	Primary   *app.App
	Runtime   *runtime.ProjectRuntimeManager
	// Resolve maps a project id to its workspace root (registry-backed, with a
	// primary fallback). Only consulted for non-primary projects.
	Resolve func(projectID string) (string, error)
}

// with resolves the app for projectID and invokes fn, always releasing a
// pooled runtime afterwards. The primary app is never released.
func (r *AppResolver) with(ctx context.Context, projectID string, fn func(a *app.App) error) error {
	if r.Primary != nil && projectID == r.PrimaryID {
		return fn(r.Primary)
	}
	if r.Runtime == nil || r.Resolve == nil {
		return fmt.Errorf("sources: project %q is not available: %w", projectID, contract.ErrWorkspaceUnavailable)
	}
	root, err := r.Resolve(projectID)
	if err != nil {
		return fmt.Errorf("sources: project %q: %w", projectID, contract.ErrWorkspaceUnavailable)
	}
	rt, release, err := r.Runtime.Acquire(ctx, projectID, root)
	if err != nil {
		// A project that cannot be loaded (e.g. the path is neither a git
		// repository nor carries a .punakawan/workspace.yaml, so
		// workspace.Discover fails) is unavailable, not an internal error -
		// map it to ErrWorkspaceUnavailable so the read routes return 404 and
		// the panel shows an "unavailable" state instead of a 500. Mirrors the
		// Resolve-failure branch above.
		return fmt.Errorf("sources: acquire project %q: %w: %v", projectID, contract.ErrWorkspaceUnavailable, err)
	}
	defer release()
	return fn(rt.App)
}

// ProjectSessionReader is a contract.SessionReader resolved per project id.
type ProjectSessionReader struct{ *AppResolver }

func (p ProjectSessionReader) List(ctx context.Context, workspaceID string, filter contract.SessionFilter) ([]protocol.PanelSessionSummary, error) {
	var out []protocol.PanelSessionSummary
	err := p.with(ctx, workspaceID, func(a *app.App) error {
		var e error
		out, e = (&SessionSource{App: a}).List(ctx, workspaceID, filter)
		return e
	})
	return out, err
}

func (p ProjectSessionReader) Get(ctx context.Context, workspaceID, sessionID string) (contract.SessionDetail, error) {
	var out contract.SessionDetail
	err := p.with(ctx, workspaceID, func(a *app.App) error {
		var e error
		out, e = (&SessionSource{App: a}).Get(ctx, workspaceID, sessionID)
		return e
	})
	return out, err
}

// ProjectEvidenceReader is a contract.EvidenceReader resolved per project id.
type ProjectEvidenceReader struct{ *AppResolver }

func (p ProjectEvidenceReader) List(ctx context.Context, workspaceID, sessionID string) ([]protocol.EvidenceRecord, error) {
	var out []protocol.EvidenceRecord
	err := p.with(ctx, workspaceID, func(a *app.App) error {
		var e error
		out, e = (&EvidenceSource{App: a}).List(ctx, workspaceID, sessionID)
		return e
	})
	return out, err
}

func (p ProjectEvidenceReader) Get(ctx context.Context, workspaceID, evidenceID string) (protocol.EvidenceRecord, error) {
	var out protocol.EvidenceRecord
	err := p.with(ctx, workspaceID, func(a *app.App) error {
		var e error
		out, e = (&EvidenceSource{App: a}).Get(ctx, workspaceID, evidenceID)
		return e
	})
	return out, err
}

func (p ProjectEvidenceReader) Preview(ctx context.Context, workspaceID, evidenceID string, offset, limit int64) (contract.EvidencePreview, error) {
	var out contract.EvidencePreview
	err := p.with(ctx, workspaceID, func(a *app.App) error {
		var e error
		out, e = (&EvidenceSource{App: a}).Preview(ctx, workspaceID, evidenceID, offset, limit)
		return e
	})
	return out, err
}

// ProjectKnowledgeReader is a contract.KnowledgeReader resolved per project id.
type ProjectKnowledgeReader struct{ *AppResolver }

func (p ProjectKnowledgeReader) List(ctx context.Context, workspaceID string, filter contract.KnowledgeFilter) ([]protocol.KnowledgeRecord, error) {
	var out []protocol.KnowledgeRecord
	err := p.with(ctx, workspaceID, func(a *app.App) error {
		var e error
		out, e = (&KnowledgeSource{App: a}).List(ctx, workspaceID, filter)
		return e
	})
	return out, err
}

func (p ProjectKnowledgeReader) Search(ctx context.Context, workspaceID string, req search.Request) ([]search.Result, error) {
	var out []search.Result
	err := p.with(ctx, workspaceID, func(a *app.App) error {
		var e error
		out, e = (&KnowledgeSource{App: a}).Search(ctx, workspaceID, req)
		return e
	})
	return out, err
}

func (p ProjectKnowledgeReader) Get(ctx context.Context, workspaceID, knowledgeID string) (protocol.KnowledgeRecord, error) {
	var out protocol.KnowledgeRecord
	err := p.with(ctx, workspaceID, func(a *app.App) error {
		var e error
		out, e = (&KnowledgeSource{App: a}).Get(ctx, workspaceID, knowledgeID)
		return e
	})
	return out, err
}

func (p ProjectKnowledgeReader) Relations(ctx context.Context, workspaceID, knowledgeID string) ([]protocol.KnowledgeRecord, error) {
	var out []protocol.KnowledgeRecord
	err := p.with(ctx, workspaceID, func(a *app.App) error {
		var e error
		out, e = (&KnowledgeSource{App: a}).Relations(ctx, workspaceID, knowledgeID)
		return e
	})
	return out, err
}

func (p ProjectKnowledgeReader) History(ctx context.Context, workspaceID, knowledgeID string) ([]knowledge.Event, error) {
	var out []knowledge.Event
	err := p.with(ctx, workspaceID, func(a *app.App) error {
		var e error
		out, e = (&KnowledgeSource{App: a}).History(ctx, workspaceID, knowledgeID)
		return e
	})
	return out, err
}
