// Package approvals persists approval records in the shared SQLite storage
// kernel (internal/storage, punokawan-14yn.16), scoped to one project. History
// is append-only: resolving a request appends a new record with the same Id
// rather than mutating the original, so Current folds to the latest record per
// id while List returns the full history.
package approvals

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
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

// Store appends and reads approval records for one project within the shared
// storage kernel. Schema migration happens once, centrally, when the kernel
// opens (internal/storage/migrations/0009_approvals.sql) - a Store never
// creates its own tables. History is append-only: resolving a request appends
// a new record with the same Id rather than mutating the original, so Current
// folds to the latest record per id while List returns full history.
type Store struct {
	db        *storage.DB
	projectID string
}

// New wraps db, scoping every read and write to projectID.
func New(db *storage.DB, projectID string) *Store {
	return &Store{db: db, projectID: projectID}
}

// writeKey returns a fresh random idempotency key. Append is a genuine append -
// the same record id may be written more than once (a request, then its
// resolution) and each such write must always take effect - so every call
// wants a unique key rather than the kernel's replay dedup collapsing them.
func writeKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("approvals: generate write key: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Append writes a new approval record (a request, an approval, or a denial).
func (s *Store) Append(rec protocol.ApprovalRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("approvals: encode record: %w", err)
	}

	ctx := context.Background()
	key, err := writeKey()
	if err != nil {
		return err
	}
	err = s.db.Write(ctx, key, "append approval "+rec.Id, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO approvals (project_id, id, data) VALUES (?, ?, ?)`,
			s.projectID, rec.Id, string(data)); err != nil {
			return fmt.Errorf("approvals: append %s: %w", rec.Id, err)
		}
		return nil
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return err
	}
	return nil
}

// List returns the full append-only history of approval records, in the order
// they were written.
func (s *Store) List() ([]protocol.ApprovalRecord, error) {
	rows, err := s.db.Reader().Query(
		`SELECT data FROM approvals WHERE project_id = ? ORDER BY seq ASC`, s.projectID)
	if err != nil {
		return nil, fmt.Errorf("approvals: list: %w", err)
	}
	defer rows.Close()

	var records []protocol.ApprovalRecord
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("approvals: scan record: %w", err)
		}
		var rec protocol.ApprovalRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, fmt.Errorf("approvals: decode record: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("approvals: iterate records: %w", err)
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
