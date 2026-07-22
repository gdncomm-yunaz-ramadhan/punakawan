// Package approvals persists approval records as append-only JSONL, per
// punakawan-go-typescript-detailed-plan.md §16.2, §6.1.
package approvals

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// agentRoleIdentifiers are the four caller roles protocol.ApprovalRecord's
// requested_by enum allows. §16.2 documents approved_by as "user", never a
// requesting role - an approved_by value that matches one of these is the
// concrete self-approval pattern reported in punakawan-d3s ("I ran it
// myself by mistake"): an agent with shell access to the approvals CLI
// reaches for its own role name (or any of the other three) rather than a
// human identifying themselves, since that is the value already sitting in
// its context. This does not authenticate that the caller is genuinely
// human - a local CLI with no session/credential has no way to do that -
// it only closes the specific reported pattern of an agent echoing an
// agent-shaped identifier back as the approver.
var agentRoleIdentifiers = map[string]bool{
	string(protocol.ApprovalRecordRequestedBySemar):  true,
	string(protocol.ApprovalRecordRequestedByGareng): true,
	string(protocol.ApprovalRecordRequestedByPetruk): true,
	string(protocol.ApprovalRecordRequestedByBagong): true,
}

// IsAgentRoleIdentifier reports whether approvedBy is one of the four agent
// role identifiers rather than a human name, case- and whitespace-
// insensitively. Shared with internal/gitops's own inline Resolve
// equivalent (see its doc comment for why that one isn't migrated to call
// this package outright), so the two approval paths reject the same
// pattern identically.
func IsAgentRoleIdentifier(approvedBy string) bool {
	return agentRoleIdentifiers[strings.ToLower(strings.TrimSpace(approvedBy))]
}

// Store appends and reads approval records for a workspace. History is
// append-only: resolving a request appends a new record with the same Id
// rather than mutating the original, so Current folds to the latest record
// per id while List returns full history.
type Store struct {
	path string
	mu   sync.Mutex
}

// Open ensures .punakawan/approvals/ exists under workspaceRoot and returns
// a Store backed by approvals.jsonl within it.
func Open(workspaceRoot string) (*Store, error) {
	dir := filepath.Join(workspaceRoot, ".punakawan", "approvals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("approvals: create %s: %w", dir, err)
	}
	return &Store{path: filepath.Join(dir, "approvals.jsonl")}, nil
}

// Append writes a new approval record (a request, an approval, or a denial).
func (s *Store) Append(rec protocol.ApprovalRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("approvals: open %s: %w", s.path, err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(rec); err != nil {
		return fmt.Errorf("approvals: encode record: %w", err)
	}
	return nil
}

// List returns the full append-only history of approval records.
func (s *Store) List() ([]protocol.ApprovalRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("approvals: open %s: %w", s.path, err)
	}
	defer f.Close()

	var records []protocol.ApprovalRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec protocol.ApprovalRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("approvals: decode record: %w", err)
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("approvals: scan %s: %w", s.path, err)
	}
	return records, nil
}

// Current folds the append-only history to the latest record per id.
func (s *Store) Current() (map[string]protocol.ApprovalRecord, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	latest := make(map[string]protocol.ApprovalRecord, len(all))
	for _, r := range all {
		latest[r.Id] = r
	}
	return latest, nil
}

// Pending returns approval records whose latest state is still pending.
func (s *Store) Pending() ([]protocol.ApprovalRecord, error) {
	current, err := s.Current()
	if err != nil {
		return nil, err
	}
	var pending []protocol.ApprovalRecord
	for _, r := range current {
		if r.Status == protocol.ApprovalRecordStatusPending {
			pending = append(pending, r)
		}
	}
	return pending, nil
}

// Resolve marks the approval record identified by id as approved or denied,
// appending a new record with the same id per the append-only history
// convention (see the Store doc comment). This is the generic entry point
// punakawan's approvals CLI uses: it resolves purely by id, status, and
// approver, with no notion of which domain (worktree creation, adapter
// operation, ...) requested it - §16's approval record has no such domain
// concept either. gitops.WorktreeManager.Approve/Deny and
// adapters.Gate.Approve/Deny each keep their own inline equivalent rather
// than being migrated to call this - they predate it, are already tested,
// and this method's already-resolved guard (below) is a deliberately
// stricter contract not worth risking against their existing behavior.
func (s *Store) Resolve(id string, status protocol.ApprovalRecordStatus, approvedBy string) error {
	current, err := s.Current()
	if err != nil {
		return err
	}
	rec, ok := current[id]
	if !ok {
		return fmt.Errorf("approvals: no request %q; it must be requested before it can be resolved", id)
	}
	if rec.Status != protocol.ApprovalRecordStatusPending {
		return fmt.Errorf("approvals: request %q is already %s", id, rec.Status)
	}
	if IsAgentRoleIdentifier(approvedBy) {
		return fmt.Errorf("approvals: approved_by %q looks like an agent role, not a human identifying themselves; §16.2 requires a human name here - re-run with --by <your actual name>", approvedBy)
	}

	now := time.Now().UTC()
	rec.Status = status
	rec.ApprovedBy = &approvedBy
	rec.ResolvedAt = &now
	return s.Append(rec)
}
