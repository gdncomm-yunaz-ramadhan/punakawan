package learning

import (
	"testing"
	"time"
)

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
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
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
}
