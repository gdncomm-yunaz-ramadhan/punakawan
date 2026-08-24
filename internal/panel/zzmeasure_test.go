package panel

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/panel/runtime"
	"github.com/ygrip/punakawan/internal/project"
)

const measurePrimary = "/Users/yunaz.ramadhan/Documents/PROJECT/punokawan"
const measureTarget = "share-hub"

func TestZZMeasure(t *testing.T) {
	if os.Getenv("MEASURE") == "" {
		t.Skip("set MEASURE=1")
	}
	ctx := context.Background()

	t0 := time.Now()
	a, err := app.Load(measurePrimary)
	if err != nil {
		t.Fatalf("app.Load primary: %v", err)
	}
	defer a.Close()
	t.Logf("app.Load(primary)            %v", time.Since(t0))

	t0 = time.Now()
	reg, err := registry.Open()
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	defer reg.Close()
	t.Logf("registry.Open                %v", time.Since(t0))

	entry, err := reg.Get(measureTarget)
	if err != nil {
		t.Fatalf("registry.Get: %v", err)
	}
	t.Logf("target path                  %s", entry.Path)

	readers := NewReaders(a, reg)
	ps := readers.Project.(*ProjectSource)

	// --- whole-handler shape: Summary then Get, as ProjectHandler does.
	t0 = time.Now()
	_, err = ps.Summary(ctx, measureTarget)
	dSummary := time.Since(t0)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	t0 = time.Now()
	_, err = ps.Get(ctx, measureTarget)
	dGet := time.Since(t0)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	t.Logf("COLD Summary                 %v", dSummary)
	t.Logf("COLD Get                     %v", dGet)

	// --- second time round (runtime now pooled, snapshot now cached)
	t0 = time.Now()
	_, _ = ps.Summary(ctx, measureTarget)
	t.Logf("WARM Summary                 %v", time.Since(t0))
	t0 = time.Now()
	_, _ = ps.Get(ctx, measureTarget)
	t.Logf("WARM Get                     %v", time.Since(t0))

	// --- stage breakdown inside Summary's cold path.
	mgr := runtime.NewManager(a.Workspace.ID, a)
	t0 = time.Now()
	rt, release, err := mgr.Acquire(ctx, entry.Id, entry.Path)
	dAcquire := time.Since(t0)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Logf("runtime.Acquire COLD         %v", dAcquire)
	defer release()

	t0 = time.Now()
	store, err := rt.App.OpenKnowledge()
	dOpenKnowledge := time.Since(t0)
	t.Logf("OpenKnowledge                %v (err=%v)", dOpenKnowledge, err)
	if err == nil {
		t0 = time.Now()
		recs, err := store.AllWithUpdatedAt()
		t.Logf("knowledge AllWithUpdatedAt   %v (n=%d err=%v)", time.Since(t0), len(recs), err)
	}

	t0 = time.Now()
	cur, err := rt.App.Workflow.Current()
	t.Logf("Workflow.Current             %v (n=%d err=%v)", time.Since(t0), len(cur), err)

	t0 = time.Now()
	_, err = project.Load(entry.Path)
	t.Logf("project.Load                 %v (err=%v)", time.Since(t0), err)

	t.Logf("repos                        %d", len(rt.App.Workspace.Repositories))
	t0 = time.Now()
	for _, r := range rt.App.Workspace.Repositories {
		p, err := rt.App.RepoPath(r.ID)
		if err != nil {
			continue
		}
		ts := time.Now()
		_, err = rt.App.Inspector.Status(ctx, p)
		t.Logf("  git status %-30s %v (err=%v)", r.ID, time.Since(ts), err)
	}
	t.Logf("git status TOTAL sequential   %v", time.Since(t0))
}
