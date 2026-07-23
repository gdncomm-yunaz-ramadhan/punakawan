package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ygrip/punakawan/internal/beads"
	"github.com/ygrip/punakawan/internal/panel/contract"
)

type fakeTaskReader struct {
	summaries []contract.TaskSummary
	detail    beads.Issue
	graph     contract.TaskGraph
}

func (f fakeTaskReader) List(ctx context.Context, workspaceID string, filter contract.TaskFilter) ([]contract.TaskSummary, error) {
	var out []contract.TaskSummary
	for _, s := range f.summaries {
		if filter.Status != "" && s.Status != filter.Status {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (f fakeTaskReader) Get(ctx context.Context, workspaceID, taskID string) (beads.Issue, error) {
	if taskID != f.detail.ID {
		return beads.Issue{}, errors.New("not found")
	}
	return f.detail, nil
}

func (f fakeTaskReader) Dependencies(ctx context.Context, workspaceID string) (contract.TaskGraph, error) {
	return f.graph, nil
}

func TestTasksHandlerListsAndFiltersByStatus(t *testing.T) {
	reader := fakeTaskReader{summaries: []contract.TaskSummary{
		{ReadyIssue: beads.ReadyIssue{ID: "t-1", Status: "open"}, BoardStatus: "ready"},
		{ReadyIssue: beads.ReadyIssue{ID: "t-2", Status: "closed"}, BoardStatus: "completed"},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/tasks?status=open", nil)
	rec := httptest.NewRecorder()
	TasksHandler(reader)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []contract.TaskSummary `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != "t-1" {
		t.Fatalf("items = %+v, want only t-1", body.Items)
	}
}

func TestTasksHandlerBlockedFilterUsesBoardStatusNotStoredStatus(t *testing.T) {
	// t-2 is stored as status=open (bd does not flip stored status just
	// because a dependency is unmet) but TaskSource already computed its
	// real BoardStatus as blocked; the handler's blocked=true filter must
	// key off BoardStatus, not Status, to catch it.
	reader := fakeTaskReader{summaries: []contract.TaskSummary{
		{ReadyIssue: beads.ReadyIssue{ID: "t-1", Status: "open"}, BoardStatus: "ready"},
		{ReadyIssue: beads.ReadyIssue{ID: "t-2", Status: "open"}, BoardStatus: "blocked"},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/tasks?blocked=true", nil)
	rec := httptest.NewRecorder()
	TasksHandler(reader)(rec, req)

	var body struct {
		Items []contract.TaskSummary `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != "t-2" {
		t.Fatalf("items = %+v, want only t-2", body.Items)
	}
}

func TestTaskHandlerUnknownTaskReturns404(t *testing.T) {
	reader := fakeTaskReader{detail: beads.Issue{ID: "t-1"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/tasks/no-such-task", nil)
	req.SetPathValue("taskId", "no-such-task")
	rec := httptest.NewRecorder()
	TaskHandler(reader)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestTaskGraphHandlerReturnsCycles(t *testing.T) {
	reader := fakeTaskReader{graph: contract.TaskGraph{
		Nodes: []contract.TaskSummary{
			{ReadyIssue: beads.ReadyIssue{ID: "t-1"}, BoardStatus: "ready"},
			{ReadyIssue: beads.ReadyIssue{ID: "t-2"}, BoardStatus: "ready"},
		},
		Edges:  []contract.TaskEdge{{From: "t-1", To: "t-2", Type: "related"}, {From: "t-2", To: "t-1", Type: "related"}},
		Cycles: [][]string{{"t-1", "t-2", "t-1"}},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/task-graph", nil)
	rec := httptest.NewRecorder()
	TaskGraphHandler(reader)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out contract.TaskGraph
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Cycles) != 1 {
		t.Fatalf("Cycles = %+v, want 1", out.Cycles)
	}
}
