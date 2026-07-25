package dossier

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
	"gopkg.in/yaml.v3"
)

// ErrSelfVerification is returned by VerifyClaim/DisputeClaim when the
// verifying role is the same as the claim's producing role. §34's rule "a role
// cannot verify its own claim" is the whole point of the claim model: evidence
// is only trustworthy when the checker is independent of the producer.
var ErrSelfVerification = errors.New("dossier: a role cannot verify its own claim")

// ErrClaimNotFound is returned by VerifyClaim/DisputeClaim when no claim with
// the given id has been recorded for the dossier.
var ErrClaimNotFound = errors.New("dossier: claim not found")

// AddClaim appends claim to the dossier's append-only claims.jsonl. The
// claim's dossier_id is forced to dossierID so a record can never disagree
// with the log it lives in, and an empty status defaults to "claimed" (§2.3's
// first rung above draft: asserted but not yet supported by evidence). The log
// is append-only; later verification adds new lines that fold-latest read
// resolves, rather than rewriting history.
func AddClaim(root, dossierID string, claim protocol.DossierClaim) (protocol.DossierClaim, error) {
	claim.DossierId = ptr(dossierID)
	if claim.Status == "" {
		claim.Status = protocol.DossierClaimStatusClaimed
	}
	if err := appendClaim(root, dossierID, claim); err != nil {
		return protocol.DossierClaim{}, err
	}
	return claim, nil
}

// VerifyClaim records byRole's verification of claimID: it loads the claim's
// latest state, refuses self-verification (ErrSelfVerification), then appends a
// new claim record with status "verified" and a verification block naming the
// checker, the result, the note, and the time. Because claims.jsonl is
// append-only and read fold-latest, this new line supersedes the prior state
// without mutating it.
func VerifyClaim(root, dossierID, claimID, byRole, note string) (protocol.DossierClaim, error) {
	return recordVerification(root, dossierID, claimID, byRole, note,
		protocol.DossierClaimStatusVerified, protocol.DossierClaimVerificationResultVerified)
}

// DisputeClaim mirrors VerifyClaim but records a dispute: status "disputed"
// and result "disputed". A disputed claim is a blocking finding for Finalize.
func DisputeClaim(root, dossierID, claimID, byRole, note string) (protocol.DossierClaim, error) {
	return recordVerification(root, dossierID, claimID, byRole, note,
		protocol.DossierClaimStatusDisputed, protocol.DossierClaimVerificationResultDisputed)
}

// recordVerification is the shared body of VerifyClaim/DisputeClaim: the two
// differ only in the status and result they stamp, so the load, the
// producer!=verifier guard, and the append are written once here.
func recordVerification(root, dossierID, claimID, byRole, note string,
	status protocol.DossierClaimStatus, result protocol.DossierClaimVerificationResult) (protocol.DossierClaim, error) {

	claims, err := readClaims(root, dossierID)
	if err != nil {
		return protocol.DossierClaim{}, err
	}
	var claim protocol.DossierClaim
	found := false
	for _, c := range claims {
		if c.Id == claimID {
			claim = c
			found = true
			break
		}
	}
	if !found {
		return protocol.DossierClaim{}, fmt.Errorf("dossier: %w: %q", ErrClaimNotFound, claimID)
	}
	if string(claim.Producer.Role) == byRole {
		return protocol.DossierClaim{}, fmt.Errorf("dossier: role %q: %w", byRole, ErrSelfVerification)
	}

	now := time.Now().UTC()
	claim.Status = status
	claim.Verification = &protocol.DossierClaimVerification{
		Role:   ptr(protocol.DossierClaimVerificationRole(byRole)),
		Result: ptr(result),
		At:     &now,
	}
	if note != "" {
		claim.Verification.Note = ptr(note)
	}
	if err := appendClaim(root, dossierID, claim); err != nil {
		return protocol.DossierClaim{}, err
	}
	return claim, nil
}

// appendClaim appends one JSON line to the dossier's claims.jsonl, creating the
// dossier directory as needed. JSON (not YAML) is used for the log because it
// is naturally one-record-per-line and matches the .jsonl extension.
func appendClaim(root, dossierID string, claim protocol.DossierClaim) error {
	if err := os.MkdirAll(dossierDir(root, dossierID), 0o755); err != nil {
		return fmt.Errorf("dossier: mkdir %s: %w", dossierDir(root, dossierID), err)
	}
	line, err := json.Marshal(claim)
	if err != nil {
		return fmt.Errorf("dossier: marshal claim: %w", err)
	}
	f, err := os.OpenFile(claimsPath(root, dossierID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("dossier: open claims log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("dossier: append claim: %w", err)
	}
	return nil
}

// readClaims reads claims.jsonl and folds it to the latest record per claim id,
// preserving first-seen order. The log is append-only, so a claim that was
// added and later verified appears as two lines; the last line for an id wins.
// A missing log is a normal empty state (an empty, non-nil slice), never an
// error.
func readClaims(root, dossierID string) ([]protocol.DossierClaim, error) {
	f, err := os.Open(claimsPath(root, dossierID))
	if os.IsNotExist(err) {
		return []protocol.DossierClaim{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dossier: open claims log: %w", err)
	}
	defer f.Close()

	latest := map[string]protocol.DossierClaim{}
	var order []string
	sc := bufio.NewScanner(f)
	// Claims can carry long statements/notes; grow the scanner's buffer well
	// past bufio's 64KiB default line cap so a large record is never silently
	// truncated into a parse error.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var c protocol.DossierClaim
		if err := json.Unmarshal(line, &c); err != nil {
			return nil, fmt.Errorf("dossier: parse claim line: %w", err)
		}
		if _, seen := latest[c.Id]; !seen {
			order = append(order, c.Id)
		}
		latest[c.Id] = c
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("dossier: scan claims log: %w", err)
	}

	out := make([]protocol.DossierClaim, 0, len(order))
	for _, id := range order {
		out = append(out, latest[id])
	}
	return out, nil
}

// AddEvidence writes ev as evidence/<id>.yaml, forcing its dossier_id to
// dossierID. Per §35 this package accepts whatever sha256 the caller supplies
// for an artifact and does not compute it: hashing artifact bytes is the
// caller's responsibility (out of scope here), and the store only persists the
// evidence record as given.
func AddEvidence(root, dossierID string, ev protocol.DossierEvidence) (protocol.DossierEvidence, error) {
	ev.DossierId = ptr(dossierID)
	out, err := yaml.Marshal(ev)
	if err != nil {
		return protocol.DossierEvidence{}, fmt.Errorf("dossier: marshal evidence: %w", err)
	}
	if err := atomicWrite(evidencePath(root, dossierID, ev.Id), out); err != nil {
		return protocol.DossierEvidence{}, err
	}
	return ev, nil
}

// readEvidence reads every evidence/<id>.yaml for the dossier, sorted by id for
// a deterministic order. A missing evidence directory is a normal empty state,
// not an error.
func readEvidence(root, dossierID string) ([]protocol.DossierEvidence, error) {
	dir := filepath.Join(dossierDir(root, dossierID), evidenceSub)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []protocol.DossierEvidence{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dossier: list evidence: %w", err)
	}
	out := make([]protocol.DossierEvidence, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("dossier: read evidence %s: %w", e.Name(), err)
		}
		var ev protocol.DossierEvidence
		if err := yaml.Unmarshal(data, &ev); err != nil {
			return nil, fmt.Errorf("dossier: parse evidence %s: %w", e.Name(), err)
		}
		out = append(out, ev)
	}
	return out, nil
}
