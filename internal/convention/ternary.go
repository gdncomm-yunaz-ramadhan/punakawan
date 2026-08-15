package convention

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/learning"
)

// ternaryOccurrenceThreshold is the minimum number of ternary-emulation-helper
// call sites DetectNoTernaryConvention requires before it proposes a
// no-ternary convention (punokawan-14yn.9 AC4's literal test case). Three is
// not derived from any formula - it exists so a single stray match (a
// coincidentally-named unrelated function, a match inside a comment or string
// literal that the regex below cannot distinguish from real code) cannot
// alone trigger a proposal, while keeping the bar low enough that this stays
// an honestly-scoped demonstration heuristic rather than a tuned classifier.
const ternaryOccurrenceThreshold = 3

// NoTernaryConventionID is this detector's fixed convention key
// (learning.Proposal.TargetId, ArtifactType: learning.TypeConvention). There
// is exactly one convention this detector can ever propose, so the id is a
// constant rather than something derived per call.
const NoTernaryConventionID = "no-ternary"

// ternaryHelperPattern matches an invocation of one of a small, fixed set of
// function names commonly used to emulate a ternary operator in Go (which has
// no native one): Ternary(cond, a, b), and IIf(cond, a, b) ("immediate if",
// the classic VB/Excel/Delphi name for the same idiom).
//
// This is a narrow, honest heuristic, not a general expression analyzer, and
// it does not claim otherwise:
//   - It is a plain regex over source text, one line at a time - not an AST
//     walk. It cannot tell whether a match sits inside a comment or a string
//     literal, is a local variable/field shadowing one of these names, or
//     belongs to an unrelated function that just happens to share the name.
//   - It only recognizes these two specific identifiers. Any other spelling a
//     codebase might use for the same idiom (cond, iif, Choose, a private
//     lowercase ternary helper, an inline closure) goes completely undetected.
//   - It says nothing about ordinary if/else or map-based lookups used for
//     the same purpose - only this one named-helper-call idiom.
//
// It exists solely to prove AC4's dormant-proposal-to-approved-visibility
// pipeline end-to-end against one concrete, explainable example. It is
// deliberately not, and is not meant to become, a linter.
var ternaryHelperPattern = regexp.MustCompile(`\b(Ternary|IIf)\s*\(`)

// TernaryHit is one line in a scanned tree matching ternaryHelperPattern.
type TernaryHit struct {
	File string
	Line int
}

// ScanTernaryUsage walks repoPath's *.go files, skipping the same
// vendor/node_modules/.git directories Extract skips (skipDirs, extract.go),
// and returns every line matching ternaryHelperPattern in deterministic
// file-then-line order. A file that cannot be opened or read is skipped
// rather than failing the whole scan, since a single unreadable file (a
// broken symlink, a permissions quirk) should not prevent detection over the
// rest of the tree.
func ScanTernaryUsage(repoPath string) ([]TernaryHit, error) {
	var hits []TernaryHit
	err := filepath.WalkDir(repoPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != repoPath && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, openErr := os.Open(path)
		if openErr != nil {
			return nil
		}
		defer f.Close()

		rel, relErr := filepath.Rel(repoPath, path)
		if relErr != nil {
			rel = path
		}

		lineNo := 0
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lineNo++
			if ternaryHelperPattern.MatchString(scanner.Text()) {
				hits = append(hits, TernaryHit{File: rel, Line: lineNo})
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("convention: scan ternary usage: %w", err)
	}
	return hits, nil
}

// DetectNoTernaryConvention scans repoPath for the ternary-emulation idiom
// (ternaryHelperPattern) and, if the number of matches meets
// ternaryOccurrenceThreshold, builds a candidate learning.Proposal
// recommending a project convention of avoiding it: Status: StatusPending and
// Classification: ClassificationInferred, so per AC4 it stays reviewable-only
// and invisible to a role's rendered context until a human approves it (see
// roleconfig.LearnedFactsBlock). found==false means "below threshold", not an
// error - the caller decides whether that is worth reporting.
//
// This performs no store I/O itself; RecordNoTernaryConvention below is the
// side-effecting wrapper that dedups and appends the candidate to a project's
// learning.Store.
func DetectNoTernaryConvention(repoPath, projectID string) (proposal learning.Proposal, found bool, err error) {
	hits, err := ScanTernaryUsage(repoPath)
	if err != nil {
		return learning.Proposal{}, false, err
	}
	if len(hits) < ternaryOccurrenceThreshold {
		return learning.Proposal{}, false, nil
	}

	evidence := make([]string, 0, len(hits))
	for _, h := range hits {
		evidence = append(evidence, fmt.Sprintf("%s:%d", h.File, h.Line))
	}

	id, err := randomConventionID()
	if err != nil {
		return learning.Proposal{}, false, err
	}
	now := time.Now().UTC()
	return learning.Proposal{
		Id:             id,
		ArtifactType:   learning.TypeConvention,
		TargetId:       NoTernaryConventionID,
		Fingerprint:    learning.ConventionFingerprint(projectID, NoTernaryConventionID),
		Rationale:      fmt.Sprintf("detected %d use(s) of a ternary-emulation helper (Ternary/IIf); recommend a project convention of avoiding ternary-style expressions in this codebase", len(hits)),
		EvidenceIds:    evidence,
		SupportCount:   len(hits),
		Status:         learning.StatusPending,
		Classification: learning.ClassificationInferred,
		Confidence:     0.5,
		CreatedBy:      "convention.DetectNoTernaryConvention",
		CreatedAt:      now,
		UpdatedAt:      now,
	}, true, nil
}

// RecordNoTernaryConvention runs DetectNoTernaryConvention and, if it found
// enough evidence, appends the candidate to store - or, if a pending proposal
// with the same fingerprint already exists (a prior detection run over the
// same repository), reinforces it by absorbing the new evidence and support
// count instead of opening a duplicate, mirroring
// internal/mcpserver/tools_pushbranch.go's recordDetectedGitCapabilities
// dedup-by-fingerprint idiom (a non-MCP-tool-initiated Append). It never
// appends an already-accepted proposal: per AC4, an inferred convention
// always starts, and stays, pending until a human review says otherwise -
// that is FindPendingByFingerprint's job, not a generic "any status" lookup.
func RecordNoTernaryConvention(store *learning.Store, repoPath, projectID string) (proposal learning.Proposal, found bool, err error) {
	candidate, found, err := DetectNoTernaryConvention(repoPath, projectID)
	if err != nil || !found {
		return learning.Proposal{}, found, err
	}

	existing, ok, err := store.FindPendingByFingerprint(candidate.Fingerprint)
	if err != nil {
		return learning.Proposal{}, false, err
	}
	if ok {
		existing.EvidenceIds = candidate.EvidenceIds
		existing.SupportCount = candidate.SupportCount
		existing.Rationale = candidate.Rationale
		existing.UpdatedAt = candidate.UpdatedAt
		if err := store.Append(existing); err != nil {
			return learning.Proposal{}, false, err
		}
		return existing, true, nil
	}

	if err := store.Append(candidate); err != nil {
		return learning.Proposal{}, false, err
	}
	return candidate, true, nil
}

// randomConventionID mints a fresh local id for a new convention proposal,
// mirroring internal/learning.Store's own writeKey helper (unexported there,
// so not reusable directly from this package).
func randomConventionID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("convention: generate proposal id: %w", err)
	}
	return "conv-" + hex.EncodeToString(b[:]), nil
}
