package evidence

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Ledger is an append-only JSONL log of protocol.EvidenceRecords for a
// single run, mirroring Journal's structure. It exists because a task's
// evidence today is discoverable only by knowing the bundle's file-naming
// convention (diff.patch, tests.json, api-diff.json) - there is no
// structured list of what evidence a task actually produced
// (punokawan-s12). RecordArtifact is the usual way to add an entry: it
// hashes a file already written into a Bundle and appends the resulting
// EvidenceRecord here.
type Ledger struct {
	path string
	mu   sync.Mutex
}

// OpenLedger ensures .punakawan/evidence/<runID>/ exists and returns a
// Ledger backed by records.jsonl within it.
func OpenLedger(workspaceRoot, runID string) (*Ledger, error) {
	dir := filepath.Join(workspaceRoot, ".punakawan", "evidence", runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("evidence: create %s: %w", dir, err)
	}
	return &Ledger{path: filepath.Join(dir, "records.jsonl")}, nil
}

// Append writes one record to the ledger, in the order artifacts were
// recorded. It does not dedupe: rerunning e.g. run_tests mid-task appends a
// second test-report record rather than replacing the first, so ForTask
// returns the full history of a task's evidence, not just its latest.
func (l *Ledger) Append(rec protocol.EvidenceRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("evidence: open %s: %w", l.path, err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(rec); err != nil {
		return fmt.Errorf("evidence: encode record %s: %w", rec.Id, err)
	}
	return nil
}

// ForTask returns every record in the ledger whose TaskId matches taskID,
// in append order.
func (l *Ledger) ForTask(taskID string) ([]protocol.EvidenceRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("evidence: open %s: %w", l.path, err)
	}
	defer f.Close()

	var out []protocol.EvidenceRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec protocol.EvidenceRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("evidence: decode record: %w", err)
		}
		if rec.TaskId != nil && *rec.TaskId == taskID {
			out = append(out, rec)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("evidence: scan %s: %w", l.path, err)
	}
	return out, nil
}

// RecordArtifact hashes the file at bundle.Path(name) and appends an
// EvidenceRecord of the given kind for it to l. Callers use this
// immediately after writing a bundle artifact (diff.patch, tests.json,
// api-diff.json, ...) so that artifact becomes enumerable via ForTask
// instead of only discoverable by a caller that already knows its file
// name.
func RecordArtifact(l *Ledger, runID, taskID string, kind protocol.EvidenceRecordType, bundle *Bundle, name string, now time.Time) (protocol.EvidenceRecord, error) {
	path := bundle.Path(name)
	data, err := os.ReadFile(path)
	if err != nil {
		return protocol.EvidenceRecord{}, fmt.Errorf("evidence: read %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	hash := "sha256:" + hex.EncodeToString(sum[:])

	rec := protocol.EvidenceRecord{
		Id:          fmt.Sprintf("ev-%s-%s-%s-%d", runID, taskID, kind, now.UnixNano()),
		RunId:       runID,
		TaskId:      &taskID,
		Type:        kind,
		Path:        &path,
		ContentHash: &hash,
		CreatedAt:   now,
	}
	if err := l.Append(rec); err != nil {
		return protocol.EvidenceRecord{}, err
	}
	return rec, nil
}
