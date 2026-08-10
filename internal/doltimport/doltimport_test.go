package doltimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/hub"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// requireDolt skips a test when the dolt binary is not on PATH. This tool's
// entire job is reading real Dolt data, so exercising it against real dolt is
// legitimate - but the binary is not guaranteed in every environment.
func requireDolt(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt binary not found on PATH; skipping real-dolt import test")
	}
}

// dolt runs a single dolt command in dir, failing the test on error. When
// cfgDir is non-empty it is passed as --doltcfg-dir (the hub layout).
func doltCmd(t *testing.T, dir, cfgDir string, args ...string) {
	t.Helper()
	full := args
	if cfgDir != "" {
		full = append([]string{"--doltcfg-dir", cfgDir}, args...)
	}
	cmd := exec.Command("dolt", full...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dolt %v: %v: %s", full, err, out)
	}
}

// newDoltStore builds a throwaway Dolt repo at dir with the exact legacy
// knowledge schema and returns it ready for inserts. cfgDir, when set, models
// a hub's shared .doltcfg.
func newDoltStore(t *testing.T, dir, cfgDir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doltCmd(t, dir, cfgDir, "init", "--name", "Test", "--email", "t@e.com")
	doltCmd(t, dir, cfgDir, "sql", "-q", `CREATE TABLE knowledge_records (
		id VARCHAR(255) PRIMARY KEY, type VARCHAR(64), status VARCHAR(64),
		validity_state VARCHAR(32), data JSON, updated_at DATETIME);`)
	doltCmd(t, dir, cfgDir, "sql", "-q", `CREATE TABLE knowledge_relations (
		from_id VARCHAR(255), type VARCHAR(64), to_id VARCHAR(255));`)
}

// insertRecord inserts one knowledge_records row. data must be a valid JSON
// literal; the caller controls whether it is valid/decodable.
func insertRecord(t *testing.T, dir, cfgDir, id, data, updatedAt string) {
	t.Helper()
	q := "INSERT INTO knowledge_records (id, type, status, validity_state, data, updated_at) VALUES ('" +
		id + "','requirement','active','observed','" + data + "','" + updatedAt + "');"
	doltCmd(t, dir, cfgDir, "sql", "-q", q)
}

func insertRelation(t *testing.T, dir, cfgDir, from, typ, to string) {
	t.Helper()
	q := "INSERT INTO knowledge_relations (from_id, type, to_id) VALUES ('" + from + "','" + typ + "','" + to + "');"
	doltCmd(t, dir, cfgDir, "sql", "-q", q)
}

// validRecordJSON is a record that passes knowledge.Validate, with no embedded
// relations.
func validRecordJSON(id, title string) string {
	return validRecordJSONRel(id, title, "")
}

// validRecordJSONRel is like validRecordJSON but, when relTarget is non-empty,
// carries one embedded depends-on relation to it.
func validRecordJSONRel(id, title, relTarget string) string {
	rels := ""
	if relTarget != "" {
		rels = `,"relations":[{"type":"depends-on","target":"` + relTarget + `"}]`
	}
	return `{"id":"` + id + `","type":"requirement","status":"active","title":"` + title + `",` +
		`"source":{"provider":"dolt","retrieved_at":"2024-01-02T03:04:05Z"},` +
		`"extraction":{"method":"manual"},` +
		`"validity":{"state":"observed"}` + rels + `}`
}

func openKernel(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "punakawan.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

const proj = "demo-project"

// --- Discovery: pure, no dolt required ---

func TestDiscoverNone(t *testing.T) {
	src, err := Discover(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if src.Kind != KindNone {
		t.Fatalf("want KindNone, got %q", src.Kind)
	}
}

func TestDiscoverLegacy(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".punakawan", "knowledge", ".dolt"), 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if src.Kind != KindLegacy {
		t.Fatalf("want KindLegacy, got %q", src.Kind)
	}
	if src.Dir != filepath.Join(root, ".punakawan", "knowledge") {
		t.Fatalf("unexpected dir %q", src.Dir)
	}
	if src.DoltCfgDir != "" {
		t.Fatalf("legacy source must not set DoltCfgDir, got %q", src.DoltCfgDir)
	}
}

func TestDiscoverHubTakesPrecedenceOverLegacy(t *testing.T) {
	root := t.TempDir()
	hubDir := t.TempDir()
	// Both a hub pointer AND a stale legacy dir present: hub must win.
	if err := os.MkdirAll(filepath.Join(root, ".punakawan", "knowledge", ".dolt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := hub.Write(root, hub.Ref{HubDir: hubDir, ProjectID: "proj-x"}); err != nil {
		t.Fatal(err)
	}
	src, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if src.Kind != KindHub {
		t.Fatalf("want KindHub, got %q", src.Kind)
	}
	if src.Dir != filepath.Join(hubDir, "proj-x") {
		t.Fatalf("unexpected hub dir %q", src.Dir)
	}
	if src.DoltCfgDir != filepath.Join(hubDir, ".doltcfg") {
		t.Fatalf("unexpected doltcfg dir %q", src.DoltCfgDir)
	}
	if src.SourceDB != "proj-x" {
		t.Fatalf("unexpected source db %q", src.SourceDB)
	}
}

// --- Fake-querier fixtures (no dolt binary required) ---
//
// These drive runWithQuerier directly through the same fake-Querier
// injection point the real dolt querier is wired through in Run, so they
// exercise the exact decode/apply/verify path without needing a dolt binary.

func legacySource(dir string) Source {
	return Source{Kind: KindLegacy, Dir: dir, SourceDB: "knowledge"}
}

// fakeQuerier returns a Querier that answers the knowledge_relations count
// query with relCount and any other query with rows, regardless of the exact
// SQL text.
func fakeQuerier(rows []map[string]json.RawMessage, relCount int) Querier {
	return func(ctx context.Context, sqlStr string) ([]map[string]json.RawMessage, error) {
		if strings.Contains(sqlStr, "COUNT(*)") {
			return []map[string]json.RawMessage{{"n": json.RawMessage(fmt.Sprintf("%d", relCount))}}, nil
		}
		return rows, nil
	}
}

// fakeKnowledgeRow builds one row shaped like the dolt querier's
// "SELECT id, type, status, validity_state, data, updated_at FROM
// knowledge_records" result: dataJSON must already be valid JSON text (the
// record's data blob), the rest are plain strings the fake JSON-encodes.
func fakeKnowledgeRow(id, dataJSON, updatedAt string) map[string]json.RawMessage {
	quotedID, _ := json.Marshal(id)
	quotedUpdatedAt, _ := json.Marshal(updatedAt)
	return map[string]json.RawMessage{
		"id":             json.RawMessage(quotedID),
		"type":           json.RawMessage(`"requirement"`),
		"status":         json.RawMessage(`"active"`),
		"validity_state": json.RawMessage(`"observed"`),
		"data":           json.RawMessage(dataJSON),
		"updated_at":     json.RawMessage(quotedUpdatedAt),
	}
}

// TestRunMissingTableFixture pins the behavior when the source Dolt store is
// missing an expected table (e.g. an unmigrated or corrupted legacy repo):
// runWithQuerier's very first read is the knowledge_relations count, so a
// "no such table" failure there must surface as a clear error - not a panic,
// and not a silent zero-count "success" that would let an empty import look
// like a real one - and must mutate nothing.
func TestRunMissingTableFixture(t *testing.T) {
	db := openKernel(t)
	q := func(ctx context.Context, sqlStr string) ([]map[string]json.RawMessage, error) {
		return nil, fmt.Errorf("no such table: knowledge_relations")
	}
	_, err := runWithQuerier(context.Background(), db, proj, legacySource(t.TempDir()), true, q)
	if err == nil {
		t.Fatal("expected an error when a source table is missing, got nil")
	}
	if !strings.Contains(err.Error(), "no such table") {
		t.Fatalf("expected the error to surface the missing-table cause, got: %v", err)
	}
	if n := countKernel(t, db); n != 0 {
		t.Fatalf("a failed inventory read must mutate nothing, got %d rows", n)
	}
}

// TestRunDuplicateIDFixture pins the current, deliberately-unchanged behavior
// when the source returns two rows sharing the same id: the destination
// table's own (project_id, id) primary key means the second row's
// INSERT...ON CONFLICT DO UPDATE overwrites the first, so exactly one row
// lands and it holds the last row's data (last one wins). The per-record
// upsert loop, however, increments RecordsImported once per row it processes
// regardless of whether that row's id collided with another id in the same
// batch, so RecordsImported reports 2, not the 1 distinct row actually
// stored. This is today's real behavior, not a design goal - the test exists
// to catch a silent change to it, not to bless it.
func TestRunDuplicateIDFixture(t *testing.T) {
	db := openKernel(t)
	rows := []map[string]json.RawMessage{
		fakeKnowledgeRow("pkw:req/demo/DUP-1", validRecordJSON("pkw:req/demo/DUP-1", "First"), "2024-03-04 05:06:07"),
		fakeKnowledgeRow("pkw:req/demo/DUP-1", validRecordJSON("pkw:req/demo/DUP-1", "Second"), "2024-03-05 06:07:08"),
	}
	rep, err := runWithQuerier(context.Background(), db, proj, legacySource(t.TempDir()), true, fakeQuerier(rows, 0))
	if err != nil {
		t.Fatal(err)
	}
	if rep.RecordsImported != 2 {
		t.Fatalf("want RecordsImported=2 (current non-deduplicating count), got %d", rep.RecordsImported)
	}
	if n := countKernel(t, db); n != 1 {
		t.Fatalf("want exactly 1 stored row for the duplicated id, got %d", n)
	}
	store := knowledge.New(db, proj)
	got, err := store.Get("pkw:req/demo/DUP-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Second" {
		t.Fatalf("want last-one-wins (title %q from the second row), got %q", "Second", got.Title)
	}
}

// TestVerifyImportedDetectsMissingRowAndRollsBack directly exercises the
// tx-scoped verifyImported function that the rollback fix introduced: given a
// transaction where one expected id was deliberately never inserted (modeling
// a mid-import failure that a post-commit-only check could not have caught in
// time), it must return an error naming that id rather than silently
// succeeding. Because this call happens from inside applyRecords' db.Write
// callback, returning that error makes db.Write roll back the entire
// transaction - so even the sibling row that WAS successfully inserted must
// not survive. That is the core of the fix: nothing partial or unverified is
// ever committed to the live kernel.
func TestVerifyImportedDetectsMissingRowAndRollsBack(t *testing.T) {
	db := openKernel(t)
	ctx := context.Background()
	expected := []decoded{
		{rec: protocol.KnowledgeRecord{Id: "pkw:req/demo/PRESENT"}},
		{rec: protocol.KnowledgeRecord{Id: "pkw:req/demo/MISSING"}},
	}

	err := db.Write(ctx, "verify-missing-row-test", "test: verify catches a missing row", func(tx *sql.Tx) error {
		// Insert only the first expected id, deliberately leaving the second
		// missing - so verifyImported must catch it.
		if _, err := tx.ExecContext(ctx, `
INSERT INTO knowledge_records (project_id, id, type, status, validity_state, data, updated_at)
VALUES (?, ?, 'requirement', 'active', 'observed', '{}', '2024-01-01T00:00:00.000000000Z')`,
			proj, "pkw:req/demo/PRESENT"); err != nil {
			return err
		}
		return verifyImported(ctx, tx, proj, expected)
	})
	if err == nil {
		t.Fatal("expected verifyImported to fail for an id that was never inserted")
	}
	if !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("expected the error to name the missing id, got: %v", err)
	}
	if n := countKernel(t, db); n != 0 {
		t.Fatalf("a failed verify must roll back everything in its transaction (including PRESENT), got %d rows", n)
	}
}

// TestRunInterruptedImportRollsBackWholeBatch pins the transactional
// all-or-nothing guarantee applyRecords documents: a mid-batch failure must
// roll back every record in that apply, not just the one that failed. The
// failure is forced by a genuine SQL error rather than a test-only hook -
// the third record embeds the same relation twice, and the knowledge_relations
// insert has no ON CONFLICT clause, so its second identical relation row
// violates that table's own primary key. knowledge.Validate does not check
// for duplicate embedded relations, so this record decodes and validates
// cleanly and only fails once applyRecords tries to index it.
func TestRunInterruptedImportRollsBackWholeBatch(t *testing.T) {
	db := openKernel(t)
	badRecordJSON := `{"id":"pkw:req/demo/BAD-1","type":"requirement","status":"active","title":"Bad",` +
		`"source":{"provider":"dolt","retrieved_at":"2024-01-02T03:04:05Z"},` +
		`"extraction":{"method":"manual"},` +
		`"validity":{"state":"observed"},` +
		`"relations":[{"type":"depends-on","target":"pkw:req/demo/X"},{"type":"depends-on","target":"pkw:req/demo/X"}]}`
	rows := []map[string]json.RawMessage{
		fakeKnowledgeRow("pkw:req/demo/OK-1", validRecordJSON("pkw:req/demo/OK-1", "First"), "2024-03-04 05:06:07"),
		fakeKnowledgeRow("pkw:req/demo/OK-2", validRecordJSON("pkw:req/demo/OK-2", "Second"), "2024-03-04 05:06:08"),
		fakeKnowledgeRow("pkw:req/demo/BAD-1", badRecordJSON, "2024-03-04 05:06:09"),
	}
	_, err := runWithQuerier(context.Background(), db, proj, legacySource(t.TempDir()), true, fakeQuerier(rows, 0))
	if err == nil {
		t.Fatal("expected an error from the duplicate-relation insert, got nil")
	}
	// The two good records precede the bad one in the same apply/transaction;
	// asserting the kernel holds zero rows (not 2) proves the whole batch
	// rolled back, not just the failing record.
	if n := countKernel(t, db); n != 0 {
		t.Fatalf("a mid-batch failure must roll back the whole batch, got %d rows", n)
	}
}

// --- Real-dolt end-to-end ---

func TestImportEmptySource(t *testing.T) {
	requireDolt(t)
	dir := filepath.Join(t.TempDir(), "knowledge")
	newDoltStore(t, dir, "")
	db := openKernel(t)

	rep, err := Run(context.Background(), db, proj, legacySource(dir), true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.SourceRecordCount != 0 || rep.RecordsImported != 0 {
		t.Fatalf("expected empty import, got %+v", rep)
	}
	if !rep.IntegrityOK {
		t.Fatal("integrity check should pass on empty apply")
	}
}

func TestImportValidRecords(t *testing.T) {
	requireDolt(t)
	dir := filepath.Join(t.TempDir(), "knowledge")
	newDoltStore(t, dir, "")
	insertRecord(t, dir, "", "pkw:req/demo/R-1", validRecordJSONRel("pkw:req/demo/R-1", "First", "pkw:req/demo/DEP-1"), "2024-03-04 05:06:07")
	insertRecord(t, dir, "", "pkw:req/demo/R-2", validRecordJSON("pkw:req/demo/R-2", "Second"), "2024-03-05 06:07:08")
	insertRelation(t, dir, "", "pkw:req/demo/R-1", "depends-on", "pkw:req/demo/DEP-1")
	db := openKernel(t)

	rep, err := Run(context.Background(), db, proj, legacySource(dir), true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.SourceRecordCount != 2 || rep.RecordsImported != 2 {
		t.Fatalf("want 2 imported, got %+v", rep)
	}
	// Relations are derived from each record's embedded list, not from Dolt's
	// separate knowledge_relations index: only R-1 carries an embedded edge,
	// so exactly one relation is imported (the standalone Dolt relation row is
	// deliberately not copied - the embedded list is canonical).
	if rep.RelationsImported != 1 {
		t.Fatalf("want 1 relation imported, got %d", rep.RelationsImported)
	}
	// The source knowledge_relations table (one row) is still inventoried.
	if rep.SourceRelationCount != 1 {
		t.Fatalf("want source relation count 1, got %d", rep.SourceRelationCount)
	}
	if !rep.IntegrityOK {
		t.Fatal("integrity should pass")
	}

	// Records round-trip through the kernel's own read path.
	store := knowledge.New(db, proj)
	got, err := store.Get("pkw:req/demo/R-1")
	if err != nil {
		t.Fatalf("Get R-1: %v", err)
	}
	if got.Title != "First" {
		t.Fatalf("title mismatch: %q", got.Title)
	}
	// Reverse relation lookup sees the imported edge.
	related, err := store.Related("pkw:req/demo/DEP-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(related) != 1 || related[0].Id != "pkw:req/demo/R-1" {
		t.Fatalf("expected R-1 to relate to DEP-1, got %+v", related)
	}

	// Source updated_at is preserved (not clobbered with now), normalized to
	// the kernel's fixed-width layout.
	var storedUpdatedAt string
	if err := db.Reader().QueryRow(`SELECT updated_at FROM knowledge_records WHERE project_id=? AND id=?`, proj, "pkw:req/demo/R-1").Scan(&storedUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if storedUpdatedAt != "2024-03-04T05:06:07.000000000Z" {
		t.Fatalf("updated_at not preserved/normalized: %q", storedUpdatedAt)
	}
}

func TestImportSkipsMalformedAndInvalid(t *testing.T) {
	requireDolt(t)
	dir := filepath.Join(t.TempDir(), "knowledge")
	newDoltStore(t, dir, "")
	// Valid.
	insertRecord(t, dir, "", "pkw:req/demo/OK-1", validRecordJSON("pkw:req/demo/OK-1", "Good"), "2024-03-04 05:06:07")
	// Decodes as JSON but fails knowledge.Validate: id does not match the
	// pkw: scheme and required provenance fields are absent.
	insertRecord(t, dir, "", "bad-id", `{"id":"bad-id","type":"requirement","title":"x"}`, "2024-03-04 05:06:07")
	// Valid JSON object but empty record: fails Validate (bad id, missing provenance).
	insertRecord(t, dir, "", "pkw:req/demo/EMPTY", `{}`, "2024-03-04 05:06:07")
	db := openKernel(t)

	rep, err := Run(context.Background(), db, proj, legacySource(dir), true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.SourceRecordCount != 3 {
		t.Fatalf("want 3 source rows, got %d", rep.SourceRecordCount)
	}
	if rep.RecordsImported != 1 {
		t.Fatalf("want 1 imported, got %d", rep.RecordsImported)
	}
	if len(rep.Skipped) != 2 {
		t.Fatalf("want 2 skipped, got %d: %+v", len(rep.Skipped), rep.Skipped)
	}
	// A skip is reported, not fatal, and the good record still landed.
	store := knowledge.New(db, proj)
	if _, err := store.Get("pkw:req/demo/OK-1"); err != nil {
		t.Fatalf("valid record should have imported despite siblings being skipped: %v", err)
	}
}

func TestImportIsIdempotent(t *testing.T) {
	requireDolt(t)
	dir := filepath.Join(t.TempDir(), "knowledge")
	newDoltStore(t, dir, "")
	insertRecord(t, dir, "", "pkw:req/demo/R-1", validRecordJSON("pkw:req/demo/R-1", "First"), "2024-03-04 05:06:07")
	db := openKernel(t)
	ctx := context.Background()

	if _, err := Run(ctx, db, proj, legacySource(dir), true); err != nil {
		t.Fatal(err)
	}
	firstData, firstUpdated := readRow(t, db, "pkw:req/demo/R-1")

	// Second apply against the same source: the id already exists, so it is
	// reported as overwritten, but the stored row is byte-identical.
	rep2, err := Run(ctx, db, proj, legacySource(dir), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep2.Overwritten) != 1 {
		t.Fatalf("second run should report 1 overwrite, got %d", len(rep2.Overwritten))
	}
	secondData, secondUpdated := readRow(t, db, "pkw:req/demo/R-1")
	if firstData != secondData || firstUpdated != secondUpdated {
		t.Fatalf("re-run changed stored state:\n data %q -> %q\n updated %q -> %q", firstData, secondData, firstUpdated, secondUpdated)
	}
	// Still exactly one row: no duplication.
	if n := countKernel(t, db); n != 1 {
		t.Fatalf("expected 1 kernel row after idempotent re-run, got %d", n)
	}
}

func TestDryRunMutatesNothing(t *testing.T) {
	requireDolt(t)
	dir := filepath.Join(t.TempDir(), "knowledge")
	newDoltStore(t, dir, "")
	insertRecord(t, dir, "", "pkw:req/demo/R-1", validRecordJSON("pkw:req/demo/R-1", "First"), "2024-03-04 05:06:07")
	db := openKernel(t)
	ctx := context.Background()

	rep1, err := Run(ctx, db, proj, legacySource(dir), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep1.Applied {
		t.Fatal("dry run must not be marked applied")
	}
	if rep1.SourceRecordCount != 1 {
		t.Fatalf("dry run should still count source: %+v", rep1)
	}
	// The destination is untouched by a dry run.
	if n := countKernel(t, db); n != 0 {
		t.Fatalf("dry run mutated the kernel: %d rows present", n)
	}
	// Running dry-run twice yields the same inventory.
	rep2, err := Run(ctx, db, proj, legacySource(dir), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.SourceRecordCount != rep1.SourceRecordCount || len(rep2.Skipped) != len(rep1.Skipped) {
		t.Fatalf("dry run not stable: %+v vs %+v", rep1, rep2)
	}
	if n := countKernel(t, db); n != 0 {
		t.Fatalf("second dry run mutated the kernel: %d rows", n)
	}
}

func TestImportHubBacked(t *testing.T) {
	requireDolt(t)
	hubDir := t.TempDir()
	cfgDir := filepath.Join(hubDir, ".doltcfg")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projDir := filepath.Join(hubDir, "proj-x")
	newDoltStore(t, projDir, cfgDir)
	insertRecord(t, projDir, cfgDir, "pkw:req/demo/H-1", validRecordJSON("pkw:req/demo/H-1", "HubRec"), "2024-03-04 05:06:07")

	// Build the workspace hub pointer and discover through it, exercising the
	// real hub discovery path end to end.
	root := t.TempDir()
	if err := hub.Write(root, hub.Ref{HubDir: hubDir, ProjectID: "proj-x"}); err != nil {
		t.Fatal(err)
	}
	src, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if src.Kind != KindHub || src.DoltCfgDir != cfgDir {
		t.Fatalf("hub discovery wrong: %+v", src)
	}

	db := openKernel(t)
	rep, err := Run(context.Background(), db, proj, src, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RecordsImported != 1 {
		t.Fatalf("want 1 hub record imported, got %+v", rep)
	}
	store := knowledge.New(db, proj)
	if _, err := store.Get("pkw:req/demo/H-1"); err != nil {
		t.Fatalf("hub record should have imported: %v", err)
	}
}

// TestApplyWritesManifest confirms a successful apply persists the report as
// a manifest file next to the kernel database, and that a dry run writes no
// such file.
func TestApplyWritesManifest(t *testing.T) {
	requireDolt(t)
	dir := filepath.Join(t.TempDir(), "knowledge")
	newDoltStore(t, dir, "")
	insertRecord(t, dir, "", "pkw:req/demo/R-1", validRecordJSON("pkw:req/demo/R-1", "First"), "2024-03-04 05:06:07")
	db := openKernel(t)
	ctx := context.Background()

	// A dry run must not write a manifest.
	if _, err := Run(ctx, db, proj, legacySource(dir), false); err != nil {
		t.Fatal(err)
	}
	path := manifestPath(db, proj)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dry run should not write a manifest, stat err: %v", err)
	}

	rep, err := Run(ctx, db, proj, legacySource(dir), true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("manifest not written at %s: %v", path, err)
	}
	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("manifest did not unmarshal as Report: %v", err)
	}
	if got.RecordsImported != rep.RecordsImported || got.RecordsImported != 1 {
		t.Fatalf("manifest RecordsImported mismatch: got %d, want 1", got.RecordsImported)
	}
	if got.DestProjectID != proj {
		t.Fatalf("manifest DestProjectID mismatch: got %q, want %q", got.DestProjectID, proj)
	}
}

func readRow(t *testing.T, db *storage.DB, id string) (data, updatedAt string) {
	t.Helper()
	if err := db.Reader().QueryRow(`SELECT data, updated_at FROM knowledge_records WHERE project_id=? AND id=?`, proj, id).Scan(&data, &updatedAt); err != nil {
		t.Fatal(err)
	}
	return data, updatedAt
}

func countKernel(t *testing.T, db *storage.DB) int {
	t.Helper()
	var n int
	if err := db.Reader().QueryRow(`SELECT COUNT(*) FROM knowledge_records WHERE project_id=?`, proj).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
