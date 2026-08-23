package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/workspace"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// These benchmarks are the reproducible baseline for the release gate's
// process-level resource SLOs (cold-open latency, indexed query latency,
// incremental idle RSS, steady idle CPU), all measured against a project
// holding sloFixtureRecordCount real knowledge records - not a mock store.
// Run with: go test ./internal/app/... -bench=. -benchmem -run=^$
//
// BenchmarkColdOpen and BenchmarkIndexedQuery report a p95, not a mean,
// because the SLO itself is stated as a p95: a handful of slow outliers
// (the first query after a page cache miss, a GC pause during Load) are
// exactly what a mean would hide and a p95 is meant to catch.

const sloFixtureRecordCount = 10000

// sloFixtureTopics seed sloFixtureRecordCount records across a spread of
// distinct subjects so BenchmarkIndexedQuery's FTS5 queries see a realistic
// mix of common and rare terms instead of every row being a near-duplicate
// of the next.
var sloFixtureTopics = []string{
	"Warehouse capacity threshold",
	"Checkout latency regression",
	"Payment gateway timeout",
	"Cache invalidation delay",
	"Inventory sync failure",
	"Order fulfillment backlog",
	"Shipping label generation error",
	"Customer authentication lockout",
	"Search index staleness",
	"Database connection pool exhaustion",
	"API rate limit breach",
	"Webhook delivery retry",
	"Currency conversion mismatch",
	"Tax calculation discrepancy",
	"Refund processing delay",
	"Promotion code validation bug",
	"Session token expiry",
	"Image upload compression",
	"Notification queue backpressure",
	"Audit log rotation",
}

// sloFixtureQueries are the representative search terms BenchmarkIndexedQuery
// cycles through: a mix of multi-word phrases pulled straight from the
// fixture titles (common, high-hit-count queries) and single words that
// appear in every record's content (worst-case-selectivity queries), so the
// p95 isn't dominated by one query shape.
var sloFixtureQueries = []string{
	"checkout latency regression",
	"payment gateway timeout",
	"warehouse capacity threshold",
	"inventory sync failure",
	"cache invalidation delay",
	"database connection pool exhaustion",
	"webhook delivery retry",
	"session token expiry",
	"regression",
	"timeout",
	"mitigation",
	"root cause analysis",
}

// sloFixtureOnce/-Root/-Err memoize the 10k-record project fixture across
// every Benchmark function in this file (and across -bench=. re-invocations
// within one test binary run): seeding 10k records is a one-time setup cost,
// not part of what any of these benchmarks measure, so it must not be redone
// per Benchmark function.
var (
	sloFixtureOnce sync.Once
	sloFixtureRoot string
	sloFixtureErr  error
)

// setupSLOFixture returns the root directory of a project already holding
// sloFixtureRecordCount knowledge records and a rebuilt search index,
// building it once for the whole test binary run.
func setupSLOFixture(b *testing.B) string {
	b.Helper()
	sloFixtureOnce.Do(func() {
		sloFixtureRoot, sloFixtureErr = buildSLOFixture()
	})
	if sloFixtureErr != nil {
		b.Fatalf("build SLO fixture: %v", sloFixtureErr)
	}
	return sloFixtureRoot
}

// buildSLOFixture creates a real project directory (a .punakawan/workspace.yaml,
// the same shape as test/fixtures/workspace) and points the shared storage
// kernel at an isolated data directory for the lifetime of this test binary,
// via the same PUNAKAWAN_DATA_DIR override every other test in this repo
// uses (see internal/storage/path.go) - plain os.Setenv rather than
// b.Setenv/t.Setenv, since this must survive across every Benchmark function
// in the package, not just the one that happens to build the fixture first.
func buildSLOFixture() (string, error) {
	dataDir, err := os.MkdirTemp("", "punakawan-slo-datadir-")
	if err != nil {
		return "", fmt.Errorf("create data dir: %w", err)
	}
	if err := os.Setenv("PUNAKAWAN_DATA_DIR", dataDir); err != nil {
		return "", fmt.Errorf("set PUNAKAWAN_DATA_DIR: %w", err)
	}

	root, err := os.MkdirTemp("", "punakawan-slo-project-")
	if err != nil {
		return "", fmt.Errorf("create project dir: %w", err)
	}
	punDir := filepath.Join(root, ".punakawan")
	if err := os.MkdirAll(punDir, 0o755); err != nil {
		return "", fmt.Errorf("create .punakawan: %w", err)
	}
	workspaceYAML := fmt.Sprintf("version: %s\nid: slo-fixture\nname: SLO Fixture\n\nrepositories:\n  - id: slo-fixture\n    path: .\n",
		workspace.SupportedVersion)
	if err := os.WriteFile(filepath.Join(punDir, "workspace.yaml"), []byte(workspaceYAML), 0o644); err != nil {
		return "", fmt.Errorf("write workspace.yaml: %w", err)
	}

	a, err := Load(root)
	if err != nil {
		return "", fmt.Errorf("Load: %w", err)
	}
	defer a.Close()

	store, err := a.OpenKnowledge()
	if err != nil {
		return "", fmt.Errorf("OpenKnowledge: %w", err)
	}
	db, err := a.OpenStorage(context.Background())
	if err != nil {
		return "", fmt.Errorf("OpenStorage: %w", err)
	}

	records := generateFixtureRecords(sloFixtureRecordCount)
	if err := seedKnowledgeRecords(context.Background(), db, a.Workspace.ID, records); err != nil {
		return "", fmt.Errorf("seed records: %w", err)
	}

	ix, err := a.OpenSearchIndex()
	if err != nil {
		return "", fmt.Errorf("OpenSearchIndex: %w", err)
	}
	if err := search.Rebuild(store, ix); err != nil {
		return "", fmt.Errorf("Rebuild index: %w", err)
	}

	return root, nil
}

// generateFixtureRecords builds count realistic, individually valid (per
// knowledge.Validate) knowledge records spread across sloFixtureTopics.
func generateFixtureRecords(count int) []protocol.KnowledgeRecord {
	now := time.Now().UTC()
	records := make([]protocol.KnowledgeRecord, count)
	for i := 0; i < count; i++ {
		topic := sloFixtureTopics[i%len(sloFixtureTopics)]
		variant := i / len(sloFixtureTopics)
		content := fmt.Sprintf(
			"Investigation into %s affecting project systems. Record variant %d covers reproduction steps, root cause analysis, and mitigation notes for %s.",
			strings.ToLower(topic), variant, topic)
		summary := fmt.Sprintf("%s - variant %d", topic, variant)
		records[i] = protocol.KnowledgeRecord{
			Id:      fmt.Sprintf("pkw:req/slo-fixture/rec-%05d", i),
			Type:    protocol.KnowledgeRecordTypeRequirement,
			Status:  "active",
			Title:   fmt.Sprintf("%s #%d", topic, variant),
			Content: &content,
			Summary: &summary,
			Tags:    []string{"slo-fixture"},
			Source: protocol.KnowledgeRecordSource{
				Provider:    "test",
				RetrievedAt: now,
			},
			Extraction: protocol.KnowledgeRecordExtraction{
				Method: protocol.KnowledgeRecordExtractionMethodManual,
			},
			Validity: protocol.KnowledgeRecordValidity{
				State: protocol.KnowledgeRecordValidityStateObserved,
			},
		}
	}
	return records
}

// seedKnowledgeRecords writes every record in one transaction using exactly
// the same upsert knowledge.Store.Put issues (internal/knowledge/service.go),
// rather than calling Put sloFixtureRecordCount times. Put's one-fsync'd
// transaction per call is the right cost model for a single write in
// production, but paying that cost 10000 times over is purely fixture setup,
// not something any of these benchmarks are measuring - so it is batched
// into the one transaction Put already uses per call, just carrying more
// rows.
func seedKnowledgeRecords(ctx context.Context, db *storage.DB, projectID string, records []protocol.KnowledgeRecord) error {
	for _, rec := range records {
		if err := knowledge.Validate(rec); err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(knowledge.TimeLayout)
	return db.Write(ctx, "slo-fixture-seed", "seed slo benchmark fixture records", func(tx *sql.Tx) error {
		for _, rec := range records {
			data, err := json.Marshal(rec)
			if err != nil {
				return fmt.Errorf("marshal %s: %w", rec.Id, err)
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO knowledge_records (project_id, id, type, status, validity_state, data, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, id) DO UPDATE SET
  type = excluded.type, status = excluded.status, validity_state = excluded.validity_state,
  data = excluded.data, updated_at = excluded.updated_at`,
				projectID, rec.Id, string(rec.Type), rec.Status, string(rec.Validity.State), string(data), now); err != nil {
				return fmt.Errorf("insert %s: %w", rec.Id, err)
			}
		}
		return nil
	})
}

// BenchmarkColdOpen measures the cost of bringing a knowledge-heavy command
// (see cmd/punakawan/knowledge_cmd.go, which always calls both) up from
// nothing to ready-to-search against the 10k-record fixture: Load, then
// OpenKnowledge, then OpenSearchIndex. Load alone opens nothing (the
// storage kernel and search index are both lazy), so it would understate the
// real "can this command now do useful work" cost; this benchmark measures
// the sequence a real command actually pays.
func BenchmarkColdOpen(b *testing.B) {
	root := setupSLOFixture(b)

	samples := make([]time.Duration, 0, b.N)
	for i := 0; i < b.N; i++ {
		start := time.Now()
		a, err := Load(root)
		if err != nil {
			b.Fatalf("Load: %v", err)
		}
		if _, err := a.OpenKnowledge(); err != nil {
			b.Fatalf("OpenKnowledge: %v", err)
		}
		if _, err := a.OpenSearchIndex(); err != nil {
			b.Fatalf("OpenSearchIndex: %v", err)
		}
		samples = append(samples, time.Since(start))
		if err := a.Close(); err != nil {
			b.Fatalf("Close: %v", err)
		}
	}
	reportP95(b, samples, "cold-open")
}

// BenchmarkIndexedQuery measures FTS5 search latency against the same
// pre-built 10k-record index, cycling through sloFixtureQueries so the p95
// reflects a realistic mix of query shapes rather than one repeated (and
// therefore page-cache-warmed in an unrealistic way) query.
func BenchmarkIndexedQuery(b *testing.B) {
	root := setupSLOFixture(b)

	a, err := Load(root)
	if err != nil {
		b.Fatalf("Load: %v", err)
	}
	defer a.Close()
	store, err := a.OpenKnowledge()
	if err != nil {
		b.Fatalf("OpenKnowledge: %v", err)
	}
	ix, err := a.OpenSearchIndex()
	if err != nil {
		b.Fatalf("OpenSearchIndex: %v", err)
	}

	samples := make([]time.Duration, 0, b.N)
	for i := 0; i < b.N; i++ {
		q := sloFixtureQueries[i%len(sloFixtureQueries)]
		start := time.Now()
		if _, err := search.Search(store, ix, search.Request{Query: q, Limit: 20}); err != nil {
			b.Fatalf("Search(%q): %v", q, err)
		}
		samples = append(samples, time.Since(start))
	}
	reportP95(b, samples, "indexed-query")
}

// idleSettleWindow is how long BenchmarkIdleResourceFootprint waits after
// Load/OpenKnowledge/OpenSearchIndex before sampling the "before idle"
// baseline, so goroutines those calls started (e.g. the sqlite driver's own
// setup) get a chance to finish rather than being counted as idle-window
// CPU usage they didn't actually spend idling.
const idleSettleWindow = 200 * time.Millisecond

// idleMeasureWindow is the fixed wall-clock window steady idle CPU is
// measured over. Long enough that scheduler noise and the settle sleep's own
// tail don't dominate the ratio, short enough to keep this benchmark's own
// runtime reasonable.
const idleMeasureWindow = 3 * time.Second

// BenchmarkIdleResourceFootprint reports two single-shot resource metrics
// rather than a per-op timing, following the same shape as
// internal/storage/bench_test.go's BenchmarkDatabaseSizeAndRSS: it does not
// loop over b.N because RSS and CPU-while-idle are properties of one process
// lifetime, not a repeatable unit of work.
//
//   - incremental-rss-MiB: process RSS measured immediately before Load vs.
//     after Load+OpenKnowledge+OpenSearchIndex and letting the process settle
//     and idle - the AC5 "incremental idle RSS" number.
//   - idle-cpu-percent: CPU time consumed during idleMeasureWindow of pure
//     idle (no requests in flight) as a percentage of that wall-clock window
//   - the AC5 "steady idle CPU" number.
//
// RSS has no portable Go stdlib accessor, so it shells out to `ps -o rss=`
// (present on both macOS and Linux); CPU time comes from
// syscall.Getrusage(RUSAGE_SELF), summing user+system time, which is also
// unix-only. Both are fine for this repo's actual test/CI machines
// (macOS/Linux); see the doc comments on processRSSKB and cpuSeconds for the
// portability caveat.
func BenchmarkIdleResourceFootprint(b *testing.B) {
	root := setupSLOFixture(b)

	runtime.GC()
	rssBefore, err := processRSSKB()
	if err != nil {
		b.Fatalf("RSS before open: %v", err)
	}

	a, err := Load(root)
	if err != nil {
		b.Fatalf("Load: %v", err)
	}
	if _, err := a.OpenKnowledge(); err != nil {
		b.Fatalf("OpenKnowledge: %v", err)
	}
	if _, err := a.OpenSearchIndex(); err != nil {
		b.Fatalf("OpenSearchIndex: %v", err)
	}

	time.Sleep(idleSettleWindow)
	runtime.GC()

	cpuBefore, err := cpuSeconds()
	if err != nil {
		b.Fatalf("cpuSeconds before idle: %v", err)
	}
	wallBefore := time.Now()

	time.Sleep(idleMeasureWindow)

	cpuAfter, err := cpuSeconds()
	if err != nil {
		b.Fatalf("cpuSeconds after idle: %v", err)
	}
	wallAfter := time.Now()

	runtime.GC()
	rssAfter, err := processRSSKB()
	if err != nil {
		b.Fatalf("RSS after idle: %v", err)
	}

	if err := a.Close(); err != nil {
		b.Fatalf("Close: %v", err)
	}

	incrementalRSSMiB := float64(rssAfter-rssBefore) / 1024.0
	idleCPUPercent := (cpuAfter - cpuBefore) / wallAfter.Sub(wallBefore).Seconds() * 100

	b.ReportMetric(incrementalRSSMiB, "incremental-rss-MiB")
	b.ReportMetric(idleCPUPercent, "idle-cpu-percent")
	b.Logf("rss before=%dKiB after=%dKiB incremental=%.2fMiB; idle cpu=%.4f%% over %s",
		rssBefore, rssAfter, incrementalRSSMiB, idleCPUPercent, wallAfter.Sub(wallBefore))
}

// reportP95 sorts samples and reports the 95th-percentile (nearest-rank)
// latency in milliseconds as a benchmark metric, plus a human-readable
// summary line at -v.
func reportP95(b *testing.B, samples []time.Duration, label string) {
	b.Helper()
	if len(samples) == 0 {
		return
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	idx := len(sorted) * 95 / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	p95 := sorted[idx]

	b.ReportMetric(float64(p95.Microseconds())/1000.0, label+"-p95-ms")
	b.Logf("%s: n=%d p50=%s p95=%s max=%s", label, len(sorted), sorted[len(sorted)/2], p95, sorted[len(sorted)-1])
}

// processRSSKB reports this process's current resident set size in KiB.
// Linux exposes VmRSS directly in /proc/self/status; every other unix
// (macOS, in practice, for this repo's dev/CI machines) falls back to
// shelling out to `ps -o rss=`, which reports the same unit on both
// platforms. Not supported on Windows.
func processRSSKB() (int64, error) {
	if runtime.GOOS == "linux" {
		if kb, err := linuxVMRSSKB(); err == nil {
			return kb, nil
		}
	}
	return psRSSKB(os.Getpid())
}

func linuxVMRSSKB() (int64, error) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("unexpected VmRSS line %q", line)
		}
		return strconv.ParseInt(fields[1], 10, 64)
	}
	return 0, fmt.Errorf("VmRSS not found in /proc/self/status")
}

func psRSSKB(pid int) (int64, error) {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, fmt.Errorf("ps -o rss= -p %d: %w", pid, err)
	}
	return strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
}

// cpuSeconds reports this process's total (user+system) CPU time consumed
// so far, via getrusage(2) - the standard way to measure process CPU time
// without instrumenting every code path. Unix-only (Windows has no
// RUSAGE_SELF), same caveat as processRSSKB.
func cpuSeconds() (float64, error) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, fmt.Errorf("getrusage: %w", err)
	}
	return timevalSeconds(ru.Utime) + timevalSeconds(ru.Stime), nil
}

func timevalSeconds(tv syscall.Timeval) float64 {
	return float64(tv.Sec) + float64(tv.Usec)/1e6
}
