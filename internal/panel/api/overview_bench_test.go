package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/project"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// These benchmarks measure the warm handler-assembly cost - the CPU/allocation
// work of aggregating and JSON-encoding responses - using FAKE readers that
// return in-memory fixtures. They deliberately exclude the real data layer
// (Dolt, bd, git): a warm-read microbenchmark should isolate the handler, and
// cold latency is dominated by those external processes, which the caches
// introduced in Phases 1/5 remove from the warm path entirely.
//
// The §18 warm-read targets these guard against are:
//   - Global overview  < 100 ms
//   - Project list      < 50 ms
//
// The fixtures scale at N = 1, 5, 20 workspaces (Phase 0's benchmark-fixture
// requirement). A large-knowledge-corpus / large-Beads-graph fixture belongs
// with the source-level caches those phases add and is out of Phase 0's
// handler scope (the readers here are already-materialized snapshots), so it
// is noted rather than faked at the handler boundary.

// benchWorkspaceReader returns n synthetic workspace summaries.
type benchWorkspaceReader struct {
	summaries []contract.WorkspaceSummary
}

func (b benchWorkspaceReader) List(ctx context.Context) ([]contract.WorkspaceSummary, error) {
	return b.summaries, nil
}
func (b benchWorkspaceReader) Get(ctx context.Context, id string) (contract.WorkspaceDetail, error) {
	for _, s := range b.summaries {
		if s.ID == id {
			return contract.WorkspaceDetail{WorkspaceSummary: s}, nil
		}
	}
	return contract.WorkspaceDetail{}, errors.New("not found")
}

func benchWorkspaces(n int) []contract.WorkspaceSummary {
	out := make([]contract.WorkspaceSummary, 0, n)
	now := time.Now().UTC()
	for i := 0; i < n; i++ {
		out = append(out, contract.WorkspaceSummary{
			ID:               fmt.Sprintf("ws-%02d", i),
			DisplayName:      fmt.Sprintf("Workspace %02d", i),
			Availability:     protocol.PanelSourceHealthAvailabilityAvailable,
			KnowledgeCount:   i * 3,
			BlockedTaskCount: i % 2,
			OpenTaskCount:    i,
			LastActivityAt:   now,
		})
	}
	return out
}

func benchSessions(n int) []protocol.PanelSessionSummary {
	out := make([]protocol.PanelSessionSummary, 0, n)
	now := time.Now().UTC()
	for i := 0; i < n; i++ {
		out = append(out, protocol.PanelSessionSummary{
			Id:          fmt.Sprintf("run-%02d", i),
			WorkspaceId: "ws-00",
			Status:      "executing",
			UpdatedAt:   now,
		})
	}
	return out
}

func benchOverviewReaders(n int) panel.Readers {
	return panel.Readers{
		Workspace: benchWorkspaceReader{summaries: benchWorkspaces(n)},
		Session:   fakeSessionReader{sessions: benchSessions(n)},
		Approval:  fakeApprovalReader{pending: nil},
	}
}

func benchmarkOverview(b *testing.B, n int) {
	handler := OverviewHandler(benchOverviewReaders(n), "ws-00")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d, want 200", rec.Code)
		}
	}
}

func BenchmarkOverviewHandler1(b *testing.B)  { benchmarkOverview(b, 1) }
func BenchmarkOverviewHandler5(b *testing.B)  { benchmarkOverview(b, 5) }
func BenchmarkOverviewHandler20(b *testing.B) { benchmarkOverview(b, 20) }

// benchProjectReader is a minimal contract.ProjectReader over in-memory
// summaries; only List is exercised by ProjectsHandler, the rest satisfy the
// interface.
type benchProjectReader struct {
	summaries []contract.ProjectSummary
}

func (b benchProjectReader) List(ctx context.Context) ([]contract.ProjectSummary, error) {
	return b.summaries, nil
}
func (b benchProjectReader) Summary(ctx context.Context, id string) (contract.ProjectSummary, error) {
	return contract.ProjectSummary{}, errors.New("not implemented")
}
func (b benchProjectReader) Get(ctx context.Context, id string) (*project.Project, error) {
	return nil, errors.New("not implemented")
}
func (b benchProjectReader) AddMetadata(ctx context.Context, id string, e project.MetadataEntry, base int) (*project.Project, error) {
	return nil, errors.New("not implemented")
}
func (b benchProjectReader) UpdateMetadata(ctx context.Context, id, key string, d *string, v any, base int) (*project.Project, error) {
	return nil, errors.New("not implemented")
}
func (b benchProjectReader) DeleteMetadata(ctx context.Context, id, key string, base int) (*project.Project, error) {
	return nil, errors.New("not implemented")
}

func benchProjects(n int) []contract.ProjectSummary {
	out := make([]contract.ProjectSummary, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, contract.ProjectSummary{
			ID:              fmt.Sprintf("proj-%02d", i),
			Name:            fmt.Sprintf("Project %02d", i),
			Availability:    string(protocol.PanelSourceHealthAvailabilityAvailable),
			RepositoryCount: 1,
			KnowledgeCount:  i * 3,
			OpenTaskCount:   i,
		})
	}
	return out
}

func benchmarkProjectsList(b *testing.B, n int) {
	handler := ProjectsHandler(benchProjectReader{summaries: benchProjects(n)})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d, want 200", rec.Code)
		}
	}
}

func BenchmarkProjectsList1(b *testing.B)  { benchmarkProjectsList(b, 1) }
func BenchmarkProjectsList5(b *testing.B)  { benchmarkProjectsList(b, 5) }
func BenchmarkProjectsList20(b *testing.B) { benchmarkProjectsList(b, 20) }
