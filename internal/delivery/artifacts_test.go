package delivery

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestPutArtifactHashIsServerComputedAndStable covers AC1/AC2: the hash
// is always computed from bytes (never accepted from a caller), and
// three independent invocations of the same content read back
// identically and validly.
func TestPutArtifactHashIsServerComputedAndStable(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	data := []byte("combined stdout+stderr of one test invocation")

	var hashes []string
	for i := 0; i < 3; i++ {
		hash, err := s.PutArtifact(ctx, data, "text/plain")
		if err != nil {
			t.Fatalf("PutArtifact (invocation %d): %v", i, err)
		}
		hashes = append(hashes, hash)
	}
	for i, h := range hashes {
		if h != hashes[0] {
			t.Fatalf("invocation %d hash %q differs from %q", i, h, hashes[0])
		}
	}
	if !strings.HasPrefix(hashes[0], "sha256:") || len(hashes[0]) != len("sha256:")+64 {
		t.Fatalf("unexpected hash shape: %q", hashes[0])
	}

	got, err := s.GetArtifact(hashes[0])
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("readback mismatch: got %q want %q", got, data)
	}
}

// TestGetArtifactUnknownHashNotFound ensures a hash nothing ever wrote
// fails closed rather than fabricating empty content.
func TestGetArtifactUnknownHashNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetArtifact("sha256:" + strings.Repeat("0", 64))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestRecordAndListArtifactsAcrossDimensions covers AC5: artifacts must
// be enumerable by orchestration, project, lane, and parent task.
func TestRecordAndListArtifactsAcrossDimensions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "artifact-project")
	task := createTestTask(t, s, orch.Id, "task")
	lane, err := s.CreateLane(ctx, "lane-1", NewID(), orch.Id, proj.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}

	hash, err := s.PutArtifact(ctx, []byte("go test -v ./... output"), "text/plain")
	if err != nil {
		t.Fatalf("PutArtifact: %v", err)
	}
	ref := ArtifactRef{
		OrchestrationID: orch.Id,
		ProjectID:       proj.Id,
		LaneID:          lane.Id,
		ParentTaskID:    task.Id,
		Kind:            protocol.EvidenceArtifactKindTest,
		Producer:        "go test",
	}
	rec, err := s.RecordArtifact(ctx, "record-1", NewID(), ref, hash)
	if err != nil {
		t.Fatalf("RecordArtifact: %v", err)
	}
	if rec.ContentHash != hash || rec.Kind != protocol.EvidenceArtifactKindTest || rec.MediaType != "text/plain" {
		t.Fatalf("unexpected recorded artifact: %+v", rec)
	}
	if rec.LaneId == nil || *rec.LaneId != lane.Id {
		t.Fatalf("expected lane id preserved, got %+v", rec.LaneId)
	}

	cases := []ArtifactFilter{
		{OrchestrationID: orch.Id},
		{ProjectID: proj.Id},
		{LaneID: lane.Id},
		{ParentTaskID: task.Id},
	}
	for _, filter := range cases {
		list, err := s.ListArtifacts(ctx, filter)
		if err != nil {
			t.Fatalf("ListArtifacts(%+v): %v", filter, err)
		}
		if len(list) != 1 || list[0].Id != rec.Id {
			t.Fatalf("ListArtifacts(%+v): expected [%s], got %+v", filter, rec.Id, list)
		}
	}

	if _, err := s.RecordArtifact(ctx, "record-missing", NewID(), ref, "sha256:"+strings.Repeat("1", 64)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound recording against an unstored hash, got %v", err)
	}
}

// TestConcurrentPutArtifactSameContentIsSafe covers AC6: many goroutines
// writing identical content at once must never observe a partial file
// or fail because of the race, since the atomic temp+rename write means
// whichever goroutine wins still leaves valid, complete bytes at dest.
func TestConcurrentPutArtifactSameContentIsSafe(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	data := []byte(strings.Repeat("concurrent-write-content ", 500))

	const workers = 16
	var wg sync.WaitGroup
	errs := make([]error, workers)
	hashes := make([]string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hashes[i], errs[i] = s.PutArtifact(ctx, data, "text/plain")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: PutArtifact: %v", i, err)
		}
		if hashes[i] != hashes[0] {
			t.Fatalf("worker %d hash %q differs from %q", i, hashes[i], hashes[0])
		}
	}
	got, err := s.GetArtifact(hashes[0])
	if err != nil {
		t.Fatalf("GetArtifact after concurrent writes: %v", err)
	}
	if string(got) != string(data) {
		t.Fatal("readback after concurrent writes does not match original content")
	}
}
