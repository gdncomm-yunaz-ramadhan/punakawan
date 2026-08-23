package approvals

// This file holds the one-time, read-and-rename legacy-import facet only. It
// is NOT a persistence path: canonical approval state lives exclusively in the
// shared SQLite kernel (see approvals.go). It touches the old pre-kernel JSONL
// file solely to migrate any data written before the upgrade, then leaves that
// file renamed in place as a backup. The storage-package consolidation guard
// deliberately excludes files named legacy_import.go for exactly this reason.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// legacyImportSuffix marks a pre-kernel legacy file that has been claimed
// for one-time import. After importing, the renamed file is deliberately
// left in place as a safety-net backup and is never deleted, so a later
// manual recovery is still possible and a second import run correctly
// no-ops (the original path no longer exists).
const legacyImportSuffix = ".imported"

// ImportLegacy performs the one-time, rename-based, concurrency-safe import
// of a pre-kernel append-only JSONL approvals file, run on first open of the
// store for a workspace. Before this subsystem moved onto the shared SQLite
// kernel it persisted to <workspaceRoot>/.punakawan/approvals/approvals.jsonl;
// any data written there before the upgrade would otherwise become invisible.
//
// It renames the legacy file to <path>.imported - an atomic operation that
// gives this process exclusive ownership of the contents: a racing second
// process attempting the same rename gets ENOENT and skips, which is what
// makes this safe without a separate lock. It then replays every line
// through Append in original file order (the append-only fold-to-latest-by-id
// semantics depend on later lines winning, so import order must match the
// original write order). The renamed file is left in place as a backup and
// is never deleted.
//
// It returns nil when there is nothing to import (no legacy file, or another
// process already claimed it). Any failure to claim, read, or decode the
// legacy file is returned as a NON-FATAL warning: the caller must still treat
// the store as open and usable. Losing the ability to import old data is much
// better than breaking the ability to open the store at all going forward.
func (s *Store) ImportLegacy(workspaceRoot string) error {
	legacyPath := filepath.Join(workspaceRoot, ".punakawan", "approvals", "approvals.jsonl")
	imported := legacyPath + legacyImportSuffix
	if err := os.Rename(legacyPath, imported); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("approvals: claim legacy file for import: %w", err)
	}

	f, err := os.Open(imported)
	if err != nil {
		return fmt.Errorf("approvals: open imported legacy file %s: %w", imported, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// Records can carry sizeable payloads; lift the scanner's line cap well
	// above the 64 KiB default so a large legacy record is not truncated.
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var rec protocol.ApprovalRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return fmt.Errorf("approvals: decode legacy line %d of %s: %w", lineNo, imported, err)
		}
		if err := s.Append(rec); err != nil {
			return fmt.Errorf("approvals: import legacy record %q: %w", rec.Id, err)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("approvals: read imported legacy file %s: %w", imported, err)
	}
	return nil
}
