package timing

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCollectorRecordAndServerTiming(t *testing.T) {
	c := NewCollector()
	c.Record("registry", 2*time.Millisecond)
	c.Record("snapshot", 1*time.Millisecond)
	c.Record("aggregate", 3*time.Millisecond)

	got := c.ServerTiming()
	// Insertion order is preserved, per the package's documented ordering.
	want := "registry;dur=2,snapshot;dur=1,aggregate;dur=3"
	if got != want {
		t.Fatalf("ServerTiming() = %q, want %q", got, want)
	}
}

func TestCollectorRepeatedNameSums(t *testing.T) {
	c := NewCollector()
	c.Record("beads_list", 10*time.Millisecond)
	c.Record("beads_list", 5*time.Millisecond)
	c.Record("git_status", 4*time.Millisecond)

	got := c.ServerTiming()
	want := "beads_list;dur=15,git_status;dur=4"
	if got != want {
		t.Fatalf("ServerTiming() = %q, want %q", got, want)
	}
	// The summed name must appear once, in its first-seen position.
	if n := c.names(); len(n) != 2 || n[0] != "beads_list" || n[1] != "git_status" {
		t.Fatalf("names() = %v, want [beads_list git_status]", n)
	}
}

func TestProbeRecordsElapsed(t *testing.T) {
	c := NewCollector()
	stop := c.Probe("work")
	time.Sleep(2 * time.Millisecond)
	stop()

	st := c.ServerTiming()
	if !strings.HasPrefix(st, "work;dur=") {
		t.Fatalf("ServerTiming() = %q, want work;dur= prefix", st)
	}
}

func TestServerTimingEmptyIsBlank(t *testing.T) {
	c := NewCollector()
	if got := c.ServerTiming(); got != "" {
		t.Fatalf("ServerTiming() on empty = %q, want empty", got)
	}
}

func TestNilCollectorSafe(t *testing.T) {
	var c *Collector
	// None of these should panic on a nil receiver.
	c.Record("x", time.Millisecond)
	c.Probe("x")()
	if got := c.ServerTiming(); got != "" {
		t.Fatalf("nil ServerTiming() = %q, want empty", got)
	}
}

func TestPackageProbeNoOpWithoutCollector(t *testing.T) {
	ctx := context.Background()
	// No collector attached: Probe must be a harmless no-op.
	stop := Probe(ctx, "workspace_list")
	stop()
	if _, ok := FromContext(ctx); ok {
		t.Fatal("FromContext on a bare context should report no collector")
	}
}

func TestPackageProbeRecordsIntoContextCollector(t *testing.T) {
	c := NewCollector()
	ctx := WithCollector(context.Background(), c)

	got, ok := FromContext(ctx)
	if !ok || got != c {
		t.Fatalf("FromContext = (%v, %v), want the attached collector", got, ok)
	}

	func() {
		defer Probe(ctx, "workspace_list")()
		time.Sleep(time.Millisecond)
	}()

	if st := c.ServerTiming(); !strings.HasPrefix(st, "workspace_list;dur=") {
		t.Fatalf("ServerTiming() = %q, want workspace_list;dur= prefix", st)
	}
}

func TestSanitizeNameInHeader(t *testing.T) {
	c := NewCollector()
	c.Record("bad name,here", time.Millisecond)
	st := c.ServerTiming()
	if strings.ContainsAny(strings.SplitN(st, ";", 2)[0], " ,") {
		t.Fatalf("metric name not sanitized: %q", st)
	}
	if !strings.HasPrefix(st, "bad_name_here;dur=") {
		t.Fatalf("ServerTiming() = %q, want sanitized bad_name_here", st)
	}
}

func TestConcurrentProbesAccumulate(t *testing.T) {
	c := NewCollector()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Record("parallel", time.Millisecond)
		}()
	}
	wg.Wait()
	// 50 x 1ms == 50ms, exercised under -race to prove the mutex holds.
	if got := c.sortedNames(); len(got) != 1 || got[0] != "parallel" {
		t.Fatalf("sortedNames() = %v, want [parallel]", got)
	}
	if st := c.ServerTiming(); st != "parallel;dur=50" {
		t.Fatalf("ServerTiming() = %q, want parallel;dur=50", st)
	}
}
