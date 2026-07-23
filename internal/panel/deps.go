// Package panel wires the Punakawan Panel's HTTP server to a loaded
// *app.App, per punakawan-panel-implementation-plan.md. Version is the
// panel's own release version; the project has no separate build-time
// version stamping yet, so /api/v1/system reports this same value for
// both "panel version" and "punakawan version" until one exists.
package panel

import (
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/panel/sources"
)

// Version is the Punakawan Panel's own release version.
const Version = "0.1.0"

// Readers bundles the six read-only reader interfaces
// (internal/panel/contract) that every HTTP handler reaches Punakawan's
// data through.
type Readers struct {
	Workspace contract.WorkspaceReader
	Session   contract.SessionReader
	Task      contract.TaskReader
	Knowledge contract.KnowledgeReader
	Evidence  contract.EvidenceReader
	Approval  contract.ApprovalReader
}

// NewReaders builds Readers backed by internal/panel/sources'
// implementations over a.
func NewReaders(a *app.App) Readers {
	return Readers{
		Workspace: &sources.WorkspaceSource{App: a},
		Session:   &sources.SessionSource{App: a},
		Task:      &sources.TaskSource{App: a},
		Knowledge: &sources.KnowledgeSource{App: a},
		Evidence:  &sources.EvidenceSource{App: a},
		Approval:  &sources.ApprovalSource{App: a},
	}
}
