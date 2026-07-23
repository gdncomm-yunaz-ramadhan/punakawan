package revision

import (
	"context"
	"os/exec"
	"strconv"
	"testing"

	"github.com/ygrip/punakawan/internal/tools"
)

func requireBd(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed")
	}
}

func newTestDispatcher(t *testing.T) *BDDispatcher {
	t.Helper()
	requireBd(t)
	dir := t.TempDir()
	sup := tools.New(dir)
	res, err := sup.Run(context.Background(), tools.Spec{
		Name: "bd",
		Args: []string{"init", "--non-interactive", "--prefix", "revtest", "--skip-agents", "--skip-hooks", "-q"},
		Dir:  dir,
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("bd init: err=%v exit=%d stderr=%s", err, res.ExitCode, res.Stderr)
	}
	return &BDDispatcher{Supervisor: sup, WorkspaceRoot: dir}
}

func sampleRequest(id string) Request {
	return Request{
		RequestID:         id,
		ReviewID:          "review-1",
		ArtifactType:      "plan",
		ArtifactID:        "plan-panel",
		BaseVersion:       3,
		BaseRevisionHash:  "sha256:aaaa",
		ReviewTitle:       "Panel architecture revision",
		ReviewInstruction: "Focus on the security section.",
		CommentCount:      2,
	}
}

func TestBDDispatcherCreatesParentAndSevenChildren(t *testing.T) {
	d := newTestDispatcher(t)
	req := sampleRequest("revision-abc123")

	ref, err := d.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if ref.RunID != req.RequestID || ref.ParentTaskID != req.RequestID {
		t.Fatalf("ref = %+v, want both ids to equal %q", ref, req.RequestID)
	}

	for i := 1; i <= len(childTasks); i++ {
		exists, err := d.taskExists(context.Background(), req.RequestID+"."+strconv.Itoa(i))
		if err != nil {
			t.Fatalf("taskExists child %d: %v", i, err)
		}
		if !exists {
			t.Fatalf("child task %d was not created", i)
		}
	}
}

func TestBDDispatcherIsIdempotent(t *testing.T) {
	d := newTestDispatcher(t)
	req := sampleRequest("revision-def456")

	first, err := d.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}
	second, err := d.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("second Dispatch: %v", err)
	}
	if first != second {
		t.Fatalf("first = %+v, second = %+v, want identical run references", first, second)
	}
}

func TestIdempotencyKeyIsStableAndDistinctPerInput(t *testing.T) {
	a := IdempotencyKey("review-1", "sha256:aaaa", "sha256:bbbb", 1)
	b := IdempotencyKey("review-1", "sha256:aaaa", "sha256:bbbb", 1)
	if a != b {
		t.Fatalf("IdempotencyKey is not deterministic: %q != %q", a, b)
	}
	c := IdempotencyKey("review-1", "sha256:aaaa", "sha256:bbbb", 2)
	if a == c {
		t.Fatal("IdempotencyKey did not change when sequence changed")
	}
}
