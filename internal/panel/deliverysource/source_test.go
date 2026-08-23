package deliverysource

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/daemon"
)

func newTestSource(t *testing.T) *Source {
	t.Helper()
	dir := t.TempDir()
	paths := daemon.Paths{
		LockPath:  filepath.Join(dir, "daemon.lock"),
		TokenPath: filepath.Join(dir, "daemon.token"),
		PortPath:  filepath.Join(dir, "daemon.port"),
		DBPath:    filepath.Join(dir, "punakawan.db"),
	}
	d, err := daemon.Run(context.Background(), "127.0.0.1", "0", paths)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	go d.Serve()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		d.Shutdown(ctx)
	})

	client, err := daemon.Discover(paths)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	return &Source{Client: client}
}

func TestSourceListDeliveriesEmptyByDefault(t *testing.T) {
	src := newTestSource(t)
	list, err := src.ListDeliveries(context.Background())
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("list = %+v, want empty", list)
	}
}

func TestSourceGetDeliveryViewUnknownIDIs404(t *testing.T) {
	src := newTestSource(t)
	_, err := src.GetDeliveryView(context.Background(), "no-such-orchestration", 0)
	if err == nil {
		t.Fatal("expected an error for an unknown orchestration id")
	}
	var statusErr *daemon.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v, want a *daemon.StatusError", err)
	}
	if statusErr.Status != http.StatusNotFound {
		t.Fatalf("Status = %d, want 404", statusErr.Status)
	}
}

func TestSourceAnswerDeliveryQuestionRequiresProviderOrRouting(t *testing.T) {
	src := newTestSource(t)
	_, err := src.AnswerDeliveryQuestion(context.Background(), "no-such-orchestration", daemon.AnswerDeliveryQuestionRequest{Reference: "ref-1"})
	if err == nil {
		t.Fatal("expected an error: neither provider nor parent_task_id/project_id set")
	}
	var statusErr *daemon.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v, want a *daemon.StatusError", err)
	}
	if statusErr.Status != http.StatusBadRequest {
		t.Fatalf("Status = %d, want 400", statusErr.Status)
	}
}
