package sources

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/redact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// defaultPreviewBytes and maxPreviewBytes bound EvidenceSource.Preview's
// reads, per Phase 6's exit criterion that large evidence must not block
// page loading: a caller that asks for more than maxPreviewBytes is
// silently capped rather than served (and possibly OOMing) the full file.
const (
	defaultPreviewBytes = 64 * 1024
	maxPreviewBytes     = 8 * 1024 * 1024
	// diffSummaryScanCap bounds how much of a diff Preview will read to
	// compute DiffSummary, independently of the text excerpt's own
	// offset/limit - the summary describes the whole diff, not just the
	// requested page of it, but still must not read an unbounded file.
	diffSummaryScanCap = 32 * 1024 * 1024
)

// binaryEvidenceTypes are served as a raw, size-capped blob rather than as
// redacted text, per §14.7's "screenshot previews": there is no text to
// redact in a PNG, and attempting to would corrupt it.
var binaryEvidenceTypes = map[protocol.EvidenceRecordType]bool{
	protocol.EvidenceRecordTypeScreenshot:      true,
	protocol.EvidenceRecordTypePlaywrightTrace: true,
}

// EvidenceSource implements contract.EvidenceReader over *app.App's
// per-run evidence.Ledger (the run's evidence manifest).
type EvidenceSource struct {
	App *app.App
}

func (e *EvidenceSource) checkWorkspace(workspaceID string) error {
	if workspaceID != e.App.Workspace.ID {
		return fmt.Errorf("sources: workspace %q is not available (only %q is): %w", workspaceID, e.App.Workspace.ID, contract.ErrWorkspaceUnavailable)
	}
	return nil
}

func (e *EvidenceSource) List(ctx context.Context, workspaceID, sessionID string) ([]protocol.EvidenceRecord, error) {
	if err := e.checkWorkspace(workspaceID); err != nil {
		return nil, err
	}
	ledger, err := evidence.OpenLedger(e.App.Workspace.Root, sessionID)
	if err != nil {
		return nil, fmt.Errorf("sources: list evidence for %q: %w", sessionID, err)
	}
	return ledger.List()
}

// Get scans every known run's ledger for evidenceID, since there is no
// global evidence index yet - only a per-run one. This is an O(runs)
// linear search; a later phase should add a workspace-wide evidence index
// if run counts make this too slow (§18's "Read evidence lazily" still
// holds, since each ledger read here is a bounded records.jsonl read).
func (e *EvidenceSource) Get(ctx context.Context, workspaceID, evidenceID string) (protocol.EvidenceRecord, error) {
	if err := e.checkWorkspace(workspaceID); err != nil {
		return protocol.EvidenceRecord{}, err
	}

	runs, err := e.App.Workflow.List()
	if err != nil {
		return protocol.EvidenceRecord{}, fmt.Errorf("sources: get evidence %q: %w", evidenceID, err)
	}

	seen := map[string]bool{}
	for _, run := range runs {
		if seen[run.Id] {
			continue
		}
		seen[run.Id] = true

		ledger, err := evidence.OpenLedger(e.App.Workspace.Root, run.Id)
		if err != nil {
			continue
		}
		recs, err := ledger.List()
		if err != nil {
			continue
		}
		for _, rec := range recs {
			if rec.Id == evidenceID {
				return rec, nil
			}
		}
	}
	return protocol.EvidenceRecord{}, fmt.Errorf("sources: evidence %q not found in any run", evidenceID)
}

// Preview reads evidenceID's backing artifact, per contract.EvidenceReader.
func (e *EvidenceSource) Preview(ctx context.Context, workspaceID, evidenceID string, offset, limit int64) (contract.EvidencePreview, error) {
	if err := e.checkWorkspace(workspaceID); err != nil {
		return contract.EvidencePreview{}, err
	}
	rec, err := e.Get(ctx, workspaceID, evidenceID)
	if err != nil {
		return contract.EvidencePreview{}, err
	}
	if rec.Path == nil {
		text := ""
		if rec.Summary != nil {
			text = *rec.Summary
		}
		return contract.EvidencePreview{Kind: "text", MimeType: "text/plain", Data: []byte(redact.Text(text)), TotalSize: int64(len(text))}, nil
	}

	path, err := safeEvidencePath(e.App.Workspace.Root, *rec.Path)
	if err != nil {
		return contract.EvidencePreview{}, err
	}

	if binaryEvidenceTypes[rec.Type] {
		return previewBinary(path)
	}
	return previewText(path, offset, limit, rec.Type)
}

// safeEvidencePath resolves recPath (an EvidenceRecord.Path) to an
// absolute, symlink-resolved path and rejects it unless it lies within
// workspaceRoot/.punakawan/evidence/ - the concrete enforcement of Phase
// 6's exit criterion "arbitrary workspace paths cannot be served". Every
// evidence artifact known to internal/evidence.Bundle is written under
// this directory; a path pointing anywhere else is treated as invalid
// rather than served, whether that came from a corrupted record or a
// future writer bug.
func safeEvidencePath(workspaceRoot, recPath string) (string, error) {
	evidenceRoot, err := filepath.Abs(filepath.Join(workspaceRoot, ".punakawan", "evidence"))
	if err != nil {
		return "", fmt.Errorf("sources: resolve evidence root: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(evidenceRoot)
	if err != nil {
		return "", fmt.Errorf("sources: resolve evidence root: %w", err)
	}

	absPath, err := filepath.Abs(recPath)
	if err != nil {
		return "", fmt.Errorf("sources: resolve evidence path: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("sources: resolve evidence path: %w", err)
	}

	if realPath != realRoot && !strings.HasPrefix(realPath, realRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("sources: evidence path %q escapes the workspace evidence directory", recPath)
	}
	return realPath, nil
}

// previewBinary reads at most maxPreviewBytes of path and returns it as an
// opaque blob, per §14.7's "screenshot previews". A file larger than the
// cap is truncated rather than fully read - it is a preview, not a
// download.
func previewBinary(path string) (contract.EvidencePreview, error) {
	f, err := os.Open(path)
	if err != nil {
		return contract.EvidencePreview{}, fmt.Errorf("sources: open evidence: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return contract.EvidencePreview{}, fmt.Errorf("sources: stat evidence: %w", err)
	}

	data := make([]byte, minInt64(info.Size(), maxPreviewBytes))
	n, err := io.ReadFull(f, data)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return contract.EvidencePreview{}, fmt.Errorf("sources: read evidence: %w", err)
	}

	mimeType := mime.TypeByExtension(filepath.Ext(path))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return contract.EvidencePreview{
		Kind:      "binary",
		MimeType:  mimeType,
		Data:      data[:n],
		TotalSize: info.Size(),
		Truncated: info.Size() > maxPreviewBytes,
	}, nil
}

// previewText reads a redacted, offset/limit-bounded excerpt of path, per
// §14.7's "ranged log loading". For diff evidence types it additionally
// scans (a separately bounded prefix of) the file to compute a
// contract.DiffSummary, independent of which page of raw text was
// requested.
func previewText(path string, offset, limit int64, evidenceType protocol.EvidenceRecordType) (contract.EvidencePreview, error) {
	f, err := os.Open(path)
	if err != nil {
		return contract.EvidencePreview{}, fmt.Errorf("sources: open evidence: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return contract.EvidencePreview{}, fmt.Errorf("sources: stat evidence: %w", err)
	}

	if limit <= 0 {
		limit = defaultPreviewBytes
	}
	if limit > maxPreviewBytes {
		limit = maxPreviewBytes
	}
	if offset < 0 {
		offset = 0
	}

	var text string
	var read int64
	if offset < info.Size() {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return contract.EvidencePreview{}, fmt.Errorf("sources: seek evidence: %w", err)
		}
		buf := make([]byte, limit)
		n, err := io.ReadFull(f, buf)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return contract.EvidencePreview{}, fmt.Errorf("sources: read evidence: %w", err)
		}
		read = int64(n)
		text = redact.Text(string(buf[:n]))
	}

	preview := contract.EvidencePreview{
		Kind:      "text",
		MimeType:  "text/plain",
		Data:      []byte(text),
		TotalSize: info.Size(),
		Offset:    offset,
		// Truncated compares against the raw bytes actually read, not
		// len(text): redaction can shorten text (a matched secret is
		// replaced by the shorter "[REDACTED]"), which would otherwise
		// make a fully-read small file look truncated.
		Truncated: offset+read < info.Size(),
	}

	if evidenceType == protocol.EvidenceRecordTypeGitDiff || evidenceType == protocol.EvidenceRecordTypeApiDiff {
		summary, err := diffSummary(path)
		if err != nil {
			return contract.EvidencePreview{}, err
		}
		preview.DiffSummary = &summary
	}
	return preview, nil
}

// diffSummary streams up to diffSummaryScanCap bytes of a unified diff at
// path and counts files touched and lines added/removed, per §14.7's
// "diff summaries" - without holding the whole diff in memory.
func diffSummary(path string) (contract.DiffSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return contract.DiffSummary{}, fmt.Errorf("sources: open evidence for diff summary: %w", err)
	}
	defer f.Close()

	limited := io.LimitReader(f, diffSummaryScanCap)
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var summary contract.DiffSummary
	for scanner.Scan() {
		line := scanner.Bytes()
		switch {
		case bytes.HasPrefix(line, []byte("+++ ")):
			summary.FilesChanged++
		case bytes.HasPrefix(line, []byte("+")) && !bytes.HasPrefix(line, []byte("+++")):
			summary.Insertions++
		case bytes.HasPrefix(line, []byte("-")) && !bytes.HasPrefix(line, []byte("---")):
			summary.Deletions++
		}
	}
	if err := scanner.Err(); err != nil {
		return contract.DiffSummary{}, fmt.Errorf("sources: scan evidence for diff summary: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		return contract.DiffSummary{}, fmt.Errorf("sources: stat evidence for diff summary: %w", err)
	}
	summary.Truncated = info.Size() > diffSummaryScanCap
	return summary, nil
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
