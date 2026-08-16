package learning

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
)

// newTestStore opens the shared SQLite storage kernel in a temp dir and scopes
// a Store to a fixed test project id, mirroring approvals/taskstore test setup.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, "test-project")
}

func TestNormalizeKey(t *testing.T) {
	if NormalizeKey("Payout.Retry.Max_Attempts") != NormalizeKey("payout retry max attempts") {
		t.Fatal("normalization should collapse separators + case")
	}
	if NormalizeKey("a-b_c.d") != "a b c d" {
		t.Fatalf("got %q", NormalizeKey("a-b_c.d"))
	}
}

func TestFingerprintsDeterministicAndDistinct(t *testing.T) {
	if MetadataFingerprint("p", "test.command") != MetadataFingerprint("p", "TEST COMMAND") {
		t.Fatal("metadata fingerprint should normalize key")
	}
	if MetadataFingerprint("p", "a") == MetadataFingerprint("p", "b") {
		t.Fatal("distinct keys must differ")
	}
	if WorkflowFingerprint("p", []string{"a:x", "b:y"}) == WorkflowFingerprint("p", []string{"b:y", "a:x"}) {
		t.Fatal("step order is significant in the workflow graph fingerprint")
	}
	if KnowledgeFingerprint("p", "decision", "s", "h1") == KnowledgeFingerprint("p", "decision", "s", "h2") {
		t.Fatal("content hash must affect the knowledge fingerprint")
	}
}

func TestStoreDedupAnchor(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	p := Proposal{Id: "learn-1", ArtifactType: TypeMetadata, TargetId: "k", Fingerprint: "fp-1", SupportCount: 1, Status: StatusPending, CreatedAt: now, UpdatedAt: now}
	if err := s.Append(p); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.FindPendingByFingerprint("fp-1")
	if err != nil || !ok || got.Id != "learn-1" {
		t.Fatalf("dedup anchor not found: %v %v %+v", err, ok, got)
	}
	// An accepted proposal is no longer a dedup anchor.
	p.Status = StatusAccepted
	p.UpdatedAt = now.Add(time.Second)
	if err := s.Append(p); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.FindPendingByFingerprint("fp-1"); ok {
		t.Fatal("accepted proposal should not anchor dedup")
	}

	// Get returns the folded-latest state; List folds history to one row per id.
	got, ok, err = s.Get("learn-1")
	if err != nil || !ok {
		t.Fatalf("Get: %v ok=%v", err, ok)
	}
	if got.Status != StatusAccepted {
		t.Fatalf("Get returned stale status %q, want accepted", got.Status)
	}
	all, err := s.List()
	if err != nil || len(all) != 1 {
		t.Fatalf("List folded = %d rows (err %v), want 1", len(all), err)
	}
}

// TestProposalClassificationValues confirms a proposal round-trips each
// recognized Classification value, and that the validation/auto-accept
// helpers treat an inferred or unset classification as reviewable-only
// while a detected fact or explicit user correction may auto-accept.
func TestProposalClassificationValues(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	for i, c := range []string{ClassificationDetectedFact, ClassificationUserCorrection, ClassificationInferred} {
		id := fmt.Sprintf("learn-c-%d", i)
		p := Proposal{
			Id: id, ArtifactType: TypeMetadata, TargetId: "k", Fingerprint: fmt.Sprintf("fp-c-%d", i),
			SupportCount: 1, Status: StatusPending, Classification: c, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.Append(p); err != nil {
			t.Fatalf("append classification %q: %v", c, err)
		}
		got, ok, err := s.Get(id)
		if err != nil || !ok {
			t.Fatalf("Get(%s): ok=%v err=%v", id, ok, err)
		}
		if got.Classification != c {
			t.Fatalf("Classification = %q, want %q", got.Classification, c)
		}
	}

	if !ValidClassification(ClassificationDetectedFact) || !ValidClassification(ClassificationUserCorrection) || !ValidClassification(ClassificationInferred) {
		t.Fatal("all three classification constants must be valid")
	}
	if ValidClassification("") || ValidClassification("bogus") {
		t.Fatal("empty/unrecognized classification must not be valid")
	}
	if !AutoAcceptable(ClassificationDetectedFact) || !AutoAcceptable(ClassificationUserCorrection) {
		t.Fatal("a detected fact or an explicit user correction must be auto-acceptable")
	}
	if AutoAcceptable(ClassificationInferred) || AutoAcceptable("") || AutoAcceptable("bogus") {
		t.Fatal("inferred, unset, and unrecognized classifications must never be auto-acceptable")
	}
}

// TestAcceptedProposalRecordsProfileRevision confirms ProfileRevision
// round-trips through Append/Get, so an acceptance can be tied to the
// project-profile revision it was accepted against.
func TestAcceptedProposalRecordsProfileRevision(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	p := Proposal{
		Id: "learn-pr", ArtifactType: TypeMetadata, TargetId: "k", Fingerprint: "fp-pr",
		SupportCount: 1, Status: StatusAccepted, Classification: ClassificationDetectedFact,
		ProfileRevision: 7, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.Append(p); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get("learn-pr")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if got.Status != StatusAccepted || got.ProfileRevision != 7 {
		t.Fatalf("got %+v, want status accepted with profile_revision 7", got)
	}
}

// TestRollbackAppendsRolledBackRowPreservingHistory confirms Rollback follows
// this store's append-only idiom: it appends a fresh row rather than editing
// history, so List folds to the new rolled_back state while the raw table
// still holds the prior accepted row underneath it (mirroring
// TestImportLegacyImportsAndRenames' later-line-wins fold check).
func TestRollbackAppendsRolledBackRowPreservingHistory(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	p := Proposal{
		Id: "learn-rb", ArtifactType: TypeMetadata, TargetId: "k", Fingerprint: "fp-rb",
		SupportCount: 1, Status: StatusAccepted, ProfileRevision: 3, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.Append(p); err != nil {
		t.Fatal(err)
	}

	if err := s.Rollback("learn-rb", "learn-restored"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	got, ok, err := s.Get("learn-rb")
	if err != nil || !ok {
		t.Fatalf("Get after rollback: ok=%v err=%v", ok, err)
	}
	if got.Status != StatusRolledBack {
		t.Fatalf("Status = %q, want %q", got.Status, StatusRolledBack)
	}
	if got.SupersededBy == nil || *got.SupersededBy != "learn-restored" {
		t.Fatalf("SupersededBy = %v, want *\"learn-restored\"", got.SupersededBy)
	}

	all, err := s.List()
	if err != nil || len(all) != 1 {
		t.Fatalf("List folded = %d rows (err %v), want 1", len(all), err)
	}

	// The prior accepted row must still be physically present in the
	// append-only history underneath the fold - Rollback appends, it never
	// edits.
	rows, err := s.db.Reader().Query(
		`SELECT data FROM learning_proposals WHERE project_id = ? AND id = ? ORDER BY seq ASC`, s.projectID, "learn-rb")
	if err != nil {
		t.Fatalf("raw history query: %v", err)
	}
	defer rows.Close()
	var raw []string
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			t.Fatal(err)
		}
		raw = append(raw, data)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 {
		t.Fatalf("raw history rows = %d, want 2 (accepted then rolled_back)", len(raw))
	}
	if !strings.Contains(raw[0], `"status":"accepted"`) {
		t.Fatalf("first row lost its accepted status: %s", raw[0])
	}
	if !strings.Contains(raw[1], `"status":"rolled_back"`) {
		t.Fatalf("second row is not rolled_back: %s", raw[1])
	}

	if err := s.Rollback("does-not-exist", ""); err == nil {
		t.Fatal("Rollback of an unknown id must error")
	}
}

func TestListEmpty(t *testing.T) {
	s := newTestStore(t)
	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if all != nil {
		t.Fatalf("List on empty store = %+v, want nil", all)
	}
	if _, ok, err := s.Get("nope"); err != nil || ok {
		t.Fatalf("Get on empty store = ok %v (err %v), want not found", ok, err)
	}
}

// TestProjectScopingPreventsLeakage confirms two Stores over one *storage.DB
// with distinct project ids never see each other's proposals on any read path.
func TestProjectScopingPreventsLeakage(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	a := New(db, "project-a")
	b := New(db, "project-b")

	now := time.Now()
	p := Proposal{Id: "learn-1", ArtifactType: TypeMetadata, TargetId: "k", Fingerprint: "fp-1", SupportCount: 1, Status: StatusPending, CreatedAt: now, UpdatedAt: now}
	if err := a.Append(p); err != nil {
		t.Fatalf("Append in A: %v", err)
	}

	// B sees nothing on any read path.
	if list, err := b.List(); err != nil || len(list) != 0 {
		t.Fatalf("project B List = %+v (err %v), want empty", list, err)
	}
	if _, ok, err := b.Get("learn-1"); err != nil || ok {
		t.Fatalf("project B Get = ok %v (err %v), want not found", ok, err)
	}
	if _, ok, err := b.FindPendingByFingerprint("fp-1"); err != nil || ok {
		t.Fatalf("project B FindPendingByFingerprint = ok %v (err %v), want not found", ok, err)
	}

	// A still sees its own proposal.
	if list, err := a.List(); err != nil || len(list) != 1 {
		t.Fatalf("project A List = %+v (err %v), want 1", list, err)
	}
	if _, ok, err := a.FindPendingByFingerprint("fp-1"); err != nil || !ok {
		t.Fatalf("project A FindPendingByFingerprint = ok %v (err %v), want found", ok, err)
	}
}
