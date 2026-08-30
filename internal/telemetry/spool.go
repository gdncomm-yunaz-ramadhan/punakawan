package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// spoolDirName is the directory name under ${PUNAKAWAN_DATA_DIR} every
// hook event is durably recorded into before any ingestion is attempted -
// so a hook that fires while the daemon (or, today, this CLI process) is
// unreachable never silently loses the event.
const spoolDirName = "telemetry-spool"

// quarantineDirName holds a spool file this process could not even parse
// as a SpoolRecord - most likely a partially-written file from a process
// that died mid-write despite the write-to-temp+rename discipline (e.g. a
// killed process during the initial os.WriteFile of the temp file, before
// any rename ever happened), or a genuinely malformed hook payload. Moving
// it out of the spool directory means a draining loop's own decode error
// can never wedge every later, healthy event behind it.
const quarantineDirName = "telemetry-spool-quarantine"

// SpoolRecord is exactly what one hook invocation durably records before
// attempting ingestion: enough to retry the same ingestion later without
// re-deriving anything from a session marker or client payload that may
// have since changed.
type SpoolRecord struct {
	EventID    string           `json:"event_id"`
	ClientKind string           `json:"client_kind"`
	EventName  string           `json:"event_name"`
	Begin      *BeginRequest    `json:"begin,omitempty"`
	Snapshot   *SnapshotRequest `json:"snapshot,omitempty"`
	Finalize   *FinalizeRequest `json:"finalize,omitempty"`
}

// SpoolDir returns (creating if absent) the directory spool files live
// under inside dataDir (${PUNAKAWAN_DATA_DIR}).
func SpoolDir(dataDir string) (string, error) {
	dir := filepath.Join(dataDir, spoolDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("telemetry: create spool dir %s: %w", dir, err)
	}
	return dir, nil
}

func quarantineDir(dataDir string) (string, error) {
	dir := filepath.Join(dataDir, quarantineDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("telemetry: create spool quarantine dir %s: %w", dir, err)
	}
	return dir, nil
}

// WriteSpoolRecord durably records rec under dataDir's spool directory as
// <event-id>.json: write to a temp file in the same directory, fsync that
// file, atomically rename it into place, then fsync the containing
// directory so the rename itself is durable. A second call for the same
// rec.EventID overwrites in place via the same discipline - writing is
// idempotent by filename.
func WriteSpoolRecord(dataDir string, rec SpoolRecord) error {
	if strings.TrimSpace(rec.EventID) == "" {
		return errors.New("telemetry: spool record requires a non-empty event_id")
	}
	dir, err := SpoolDir(dataDir)
	if err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("telemetry: encode spool record: %w", err)
	}

	final := filepath.Join(dir, rec.EventID+".json")
	tmp := filepath.Join(dir, "."+rec.EventID+".json.tmp")

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("telemetry: open spool temp file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("telemetry: write spool temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("telemetry: fsync spool temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("telemetry: close spool temp file: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("telemetry: rename spool temp file into place: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("telemetry: fsync spool dir: %w", err)
	}
	return nil
}

// syncDir fsyncs a directory so a prior rename into it is itself durable,
// not just the renamed file's own content. Best-effort on platforms where
// opening a directory for fsync is not supported (e.g. Windows) - the
// write-to-temp+rename step above is still atomic there, it is only the
// directory-entry durability guarantee that becomes best-effort.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	// Best-effort: some platforms (notably Windows) reject fsync on a
	// directory handle entirely. The write-to-temp+atomic-rename step
	// above is already durable/atomic for the file's own content there;
	// only the extra guarantee that the rename's directory entry itself
	// survived a crash is unavailable, and a hook must never fail over
	// that.
	_ = d.Sync()
	return nil
}

// PendingSpoolFiles lists every non-quarantined spool file's path under
// dataDir, oldest first (ULID-prefixed event ids sort lexicographically by
// creation order), for a drain pass to work through in order.
func PendingSpoolFiles(dataDir string) ([]string, error) {
	dir, err := SpoolDir(dataDir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("telemetry: read spool dir %s: %w", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

// ReadSpoolRecord decodes one spool file. A decode failure means the file
// is malformed (most likely a crash during an initial, pre-rename write
// despite the write-to-temp discipline, or external corruption) - the
// caller should quarantine it via QuarantineSpoolFile rather than retrying
// forever.
func ReadSpoolRecord(path string) (SpoolRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SpoolRecord{}, fmt.Errorf("telemetry: read spool file %s: %w", path, err)
	}
	var rec SpoolRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return SpoolRecord{}, fmt.Errorf("telemetry: decode spool file %s: %w", path, err)
	}
	return rec, nil
}

// RemoveSpoolFile deletes path after its ingestion has succeeded. Deleting
// an already-absent file (e.g. two concurrent drain passes raced) is not
// an error.
func RemoveSpoolFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("telemetry: remove spool file %s: %w", path, err)
	}
	return nil
}

// QuarantineSpoolFile moves a spool file this process could not decode
// out of the spool directory, so a drain loop's malformed-file error can
// never block every other pending file behind it.
func QuarantineSpoolFile(dataDir, path string) error {
	dir, err := quarantineDir(dataDir)
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, filepath.Base(path))
	if err := os.Rename(path, dest); err != nil {
		return fmt.Errorf("telemetry: quarantine spool file %s: %w", path, err)
	}
	return nil
}

// Ingest applies rec against store: Begin, IngestSnapshot, or Finalize,
// whichever of rec's optional fields is set (exactly one is expected per
// record - see clienthooks' event mappings). It is the single seam both
// the synchronous "attempt immediate ingestion after spooling" path and a
// later drain pass call through, so both apply identical semantics.
func (rec SpoolRecord) Ingest(ctx context.Context, store *Store) error {
	switch {
	case rec.Begin != nil:
		_, err := store.Begin(ctx, *rec.Begin)
		return err
	case rec.Snapshot != nil:
		_, err := store.IngestSnapshot(ctx, *rec.Snapshot)
		return err
	case rec.Finalize != nil:
		_, _, err := store.Finalize(ctx, *rec.Finalize)
		return err
	default:
		return fmt.Errorf("telemetry: spool record %s names no action", rec.EventID)
	}
}

// DrainSpool applies every pending spool file (oldest first) against
// store, removing each one that ingests successfully and quarantining
// each one that fails to even decode. It returns the count that ingested
// successfully; an ingestion error (as opposed to a decode error) stops
// the drain and returns it, leaving that file and everything after it in
// place for the next drain pass - a transient failure (e.g. the database
// briefly unavailable) must not be mistaken for permanently malformed
// input.
func DrainSpool(ctx context.Context, dataDir string, store *Store) (int, error) {
	paths, err := PendingSpoolFiles(dataDir)
	if err != nil {
		return 0, err
	}
	drained := 0
	for _, path := range paths {
		rec, err := ReadSpoolRecord(path)
		if err != nil {
			if qerr := QuarantineSpoolFile(dataDir, path); qerr != nil {
				return drained, fmt.Errorf("telemetry: quarantine malformed spool file after decode error (%v): %w", err, qerr)
			}
			continue
		}
		if err := rec.Ingest(ctx, store); err != nil {
			return drained, fmt.Errorf("telemetry: ingest spool file %s: %w", path, err)
		}
		if err := RemoveSpoolFile(path); err != nil {
			return drained, err
		}
		drained++
	}
	return drained, nil
}
