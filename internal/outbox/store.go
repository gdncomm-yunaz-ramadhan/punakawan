package outbox

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
)

// timeLayout is a fixed-width RFC3339 variant (unlike time.RFC3339Nano,
// which trims trailing fractional zeros): Claim's predicate compares
// next_attempt_at/claim_until against "now" as plain TEXT, so every
// timestamp this package writes must sort lexicographically the same way
// it sorts chronologically, or an expired claim could be missed.
const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// ErrNotFound is returned when an operation names an intent id that does
// not exist.
var ErrNotFound = errors.New("outbox: intent not found")

// ErrNotClaimedByWorker is returned by Succeed/Retry/MarkAmbiguous when the
// intent is not currently claimed by the calling worker - either another
// worker holds it, or it already reached a terminal or reconciling state.
var ErrNotClaimedByWorker = errors.New("outbox: intent is not claimed by this worker")

// Store is the durable provider-write outbox over the shared storage
// kernel. It is provider-neutral: every method operates on the same
// provider_write_intents row shape regardless of which adapter or
// operation an intent names.
type Store struct {
	db *storage.DB
}

// New wraps db. db is the same shared kernel handle every other domain
// store in this codebase (delivery.Store, plan.Store, ...) is built over.
func New(db *storage.DB) *Store {
	return &Store{db: db}
}

func writeKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("outbox: generate write key: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

const intentColumns = `id, orchestration_id, execution_id, session_id, adapter_id, operation, target_key,
	payload_json, operation_fingerprint, status, claim_owner, claim_until, attempt_count,
	next_attempt_at, external_id, provider_request_id, last_error_code, last_error_redacted,
	created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanIntent(row rowScanner) (Intent, error) {
	var i Intent
	var status string
	var claimUntil, nextAttemptAt sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(
		&i.ID, &i.OrchestrationID, &i.ExecutionID, &i.SessionID, &i.AdapterID, &i.Operation, &i.TargetKey,
		&i.PayloadJSON, &i.OperationFingerprint, &status, &i.ClaimOwner, &claimUntil, &i.AttemptCount,
		&nextAttemptAt, &i.ExternalID, &i.ProviderRequestID, &i.LastErrorCode, &i.LastErrorRedacted,
		&createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Intent{}, ErrNotFound
		}
		return Intent{}, fmt.Errorf("outbox: scan intent: %w", err)
	}
	i.Status = Status(status)
	var err error
	if i.CreatedAt, err = time.Parse(timeLayout, createdAt); err != nil {
		return Intent{}, fmt.Errorf("outbox: parse intent %s created_at: %w", i.ID, err)
	}
	if i.UpdatedAt, err = time.Parse(timeLayout, updatedAt); err != nil {
		return Intent{}, fmt.Errorf("outbox: parse intent %s updated_at: %w", i.ID, err)
	}
	if claimUntil.Valid {
		t, err := time.Parse(timeLayout, claimUntil.String)
		if err != nil {
			return Intent{}, fmt.Errorf("outbox: parse intent %s claim_until: %w", i.ID, err)
		}
		i.ClaimUntil = &t
	}
	if nextAttemptAt.Valid {
		t, err := time.Parse(timeLayout, nextAttemptAt.String)
		if err != nil {
			return Intent{}, fmt.Errorf("outbox: parse intent %s next_attempt_at: %w", i.ID, err)
		}
		i.NextAttemptAt = &t
	}
	return i, nil
}

// Enqueue durably records intent. Enqueue is idempotent on
// intent.OperationFingerprint, not on a caller-supplied idempotency key: if
// a row with the same fingerprint already exists, Enqueue returns that row
// unchanged instead of creating a competing second attempt at the same
// logical effect. intent.ID, Status, AttemptCount, and the claim/timestamp
// fields are always assigned here and ignore whatever the caller set.
func (s *Store) Enqueue(ctx context.Context, intent Intent) (Intent, error) {
	intent.AdapterID = strings.TrimSpace(intent.AdapterID)
	intent.Operation = strings.TrimSpace(intent.Operation)
	intent.OperationFingerprint = strings.TrimSpace(intent.OperationFingerprint)
	if intent.AdapterID == "" || intent.Operation == "" || intent.OperationFingerprint == "" {
		return Intent{}, fmt.Errorf("outbox: enqueue requires adapter_id, operation, and operation_fingerprint")
	}
	if strings.TrimSpace(intent.PayloadJSON) == "" {
		intent.PayloadJSON = "{}"
	}
	if intent.ID == "" {
		id, err := writeKey()
		if err != nil {
			return Intent{}, err
		}
		intent.ID = "pwi-" + id
	}
	now := time.Now().UTC()
	intent.Status = StatusPending
	intent.ClaimOwner = ""
	intent.ClaimUntil = nil
	intent.AttemptCount = 0
	intent.NextAttemptAt = nil
	intent.CreatedAt = now
	intent.UpdatedAt = now

	key, err := writeKey()
	if err != nil {
		return Intent{}, err
	}
	writeErr := s.db.Write(ctx, key, "enqueue provider write intent "+intent.OperationFingerprint, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO provider_write_intents (`+intentColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(operation_fingerprint) DO NOTHING`,
			intent.ID, intent.OrchestrationID, intent.ExecutionID, intent.SessionID, intent.AdapterID,
			intent.Operation, intent.TargetKey, intent.PayloadJSON, intent.OperationFingerprint,
			string(intent.Status), intent.ClaimOwner, nil, intent.AttemptCount, nil,
			intent.ExternalID, intent.ProviderRequestID, intent.LastErrorCode, intent.LastErrorRedacted,
			now.Format(timeLayout), now.Format(timeLayout))
		return err
	})
	if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
		return Intent{}, fmt.Errorf("outbox: enqueue %s: %w", intent.OperationFingerprint, writeErr)
	}
	return s.GetByFingerprint(ctx, intent.OperationFingerprint)
}

// Claim atomically leases at most one intent whose status is pending, due
// retrying (NextAttemptAt <= now), reconciling, or an expired claim
// (ClaimUntil <= now) to workerID, extending its claim through now+lease.
// It returns (nil, nil) when nothing is currently claimable - that is a
// normal empty result, not an error. A worker may only resolve
// (Succeed/Retry/MarkAmbiguous) a row its own claim owns.
//
// A reconciling row is claimable immediately (never time-gated) because
// nothing else currently owns it, but claiming it is not license to retry
// its write blindly: LastAttemptOutcome tells the caller whether the
// attempt that made it reconciling was ambiguous, so internal/providerwrite
// can run operation-specific reconciliation before ever considering a
// second write attempt.
func (s *Store) Claim(ctx context.Context, workerID string, now time.Time, lease time.Duration) (*Intent, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return nil, fmt.Errorf("outbox: claim requires a worker id")
	}
	now = now.UTC()
	claimUntil := now.Add(lease).Format(timeLayout)
	nowText := now.Format(timeLayout)

	key, err := writeKey()
	if err != nil {
		return nil, err
	}
	var claimed *Intent
	writeErr := s.db.Write(ctx, key, "claim provider write intent for "+workerID, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `
			UPDATE provider_write_intents
			SET status = 'claimed', claim_owner = ?, claim_until = ?, attempt_count = attempt_count + 1, updated_at = ?
			WHERE id = (
				SELECT id FROM provider_write_intents
				WHERE status = 'pending'
				   OR (status = 'retrying' AND next_attempt_at IS NOT NULL AND next_attempt_at <= ?)
				   OR (status = 'claimed' AND claim_until IS NOT NULL AND claim_until <= ?)
				   OR status = 'reconciling'
				ORDER BY created_at, id
				LIMIT 1
			)
			RETURNING `+intentColumns,
			workerID, claimUntil, nowText, nowText, nowText)
		i, err := scanIntent(row)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		claimed = &i
		return nil
	})
	if writeErr != nil {
		if errors.Is(writeErr, storage.ErrDuplicateWrite) {
			// A fresh random key can never already exist; treat this
			// defensively as "nothing claimed" rather than panic.
			return nil, nil
		}
		return nil, fmt.Errorf("outbox: claim for %s: %w", workerID, writeErr)
	}
	return claimed, nil
}

// ClaimByID is Claim narrowed to one specific, already-known intent id -
// used by a synchronous caller (internal/providerwrite.ExecuteNow) that just
// enqueued exactly this intent and wants to execute that one immediately,
// rather than whatever Claim's normal oldest-first predicate would have
// picked from the whole table. It returns (nil, nil) when intentID is not
// currently claimable (already claimed by someone else, or not in a
// claimable status).
func (s *Store) ClaimByID(ctx context.Context, intentID, workerID string, now time.Time, lease time.Duration) (*Intent, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return nil, fmt.Errorf("outbox: claim requires a worker id")
	}
	now = now.UTC()
	claimUntil := now.Add(lease).Format(timeLayout)
	nowText := now.Format(timeLayout)

	key, err := writeKey()
	if err != nil {
		return nil, err
	}
	var claimed *Intent
	writeErr := s.db.Write(ctx, key, "claim provider write intent "+intentID+" for "+workerID, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `
			UPDATE provider_write_intents
			SET status = 'claimed', claim_owner = ?, claim_until = ?, attempt_count = attempt_count + 1, updated_at = ?
			WHERE id = ?
			  AND (
				status = 'pending'
				OR (status = 'retrying' AND next_attempt_at IS NOT NULL AND next_attempt_at <= ?)
				OR (status = 'claimed' AND claim_until IS NOT NULL AND claim_until <= ?)
				OR status = 'reconciling'
			  )
			RETURNING `+intentColumns,
			workerID, claimUntil, nowText, intentID, nowText, nowText)
		i, err := scanIntent(row)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		claimed = &i
		return nil
	})
	if writeErr != nil {
		if errors.Is(writeErr, storage.ErrDuplicateWrite) {
			return nil, nil
		}
		return nil, fmt.Errorf("outbox: claim %s for %s: %w", intentID, workerID, writeErr)
	}
	return claimed, nil
}

// verifyOwnedClaim is shared by Succeed/Retry/MarkAmbiguous: each may only
// resolve a row that is currently claimed by workerID.
func verifyOwnedClaim(ctx context.Context, tx *sql.Tx, intentID, workerID string) (Intent, error) {
	current, err := scanIntent(tx.QueryRowContext(ctx, `SELECT `+intentColumns+` FROM provider_write_intents WHERE id = ?`, intentID))
	if err != nil {
		return Intent{}, err
	}
	if current.Status != StatusClaimed || current.ClaimOwner != workerID {
		return Intent{}, ErrNotClaimedByWorker
	}
	return current, nil
}

func insertAttempt(ctx context.Context, tx *sql.Tx, intentID, workerID string, attempt int, outcome, providerRequestID, diagnostic string, startedAt, finishedAt time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO provider_write_attempts (intent_id, attempt, worker_id, started_at, finished_at, outcome, provider_request_id, diagnostic_redacted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		intentID, attempt, workerID, startedAt.Format(timeLayout), finishedAt.Format(timeLayout), outcome, providerRequestID, diagnostic)
	return err
}

// Succeed marks intentID's currently claimed attempt as landed exactly
// once, recording its externalID/requestID and every granular Effect the
// successful attempt produced. Succeed is terminal: the row can never be
// claimed again afterward.
func (s *Store) Succeed(ctx context.Context, intentID, workerID, externalID, requestID string, effects []Effect) (Intent, error) {
	key, err := writeKey()
	if err != nil {
		return Intent{}, err
	}
	now := time.Now().UTC()
	writeErr := s.db.Write(ctx, key, "succeed provider write intent "+intentID, func(tx *sql.Tx) error {
		current, err := verifyOwnedClaim(ctx, tx, intentID, workerID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE provider_write_intents
			SET status = 'succeeded', claim_owner = '', claim_until = NULL, next_attempt_at = NULL,
			    external_id = ?, provider_request_id = ?, updated_at = ?
			WHERE id = ?`,
			strings.TrimSpace(externalID), strings.TrimSpace(requestID), now.Format(timeLayout), intentID); err != nil {
			return err
		}
		if err := insertAttempt(ctx, tx, intentID, workerID, current.AttemptCount, "succeeded", requestID, "", now, now); err != nil {
			return err
		}
		for _, effect := range effects {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO provider_effects (intent_id, effect_key, external_id, completed_at)
				VALUES (?, ?, ?, ?)
				ON CONFLICT(intent_id, effect_key) DO NOTHING`,
				intentID, effect.EffectKey, effect.ExternalID, now.Format(timeLayout)); err != nil {
				return err
			}
		}
		return nil
	})
	if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
		return Intent{}, fmt.Errorf("outbox: succeed %s: %w", intentID, writeErr)
	}
	return s.Get(ctx, intentID)
}

// Retry marks intentID's currently claimed attempt as retryable, scheduling
// the next claim attempt at "at" and recording code/redacted for operator
// visibility. redacted must never carry secrets or raw provider bodies.
func (s *Store) Retry(ctx context.Context, intentID, workerID, code, redacted string, at time.Time) (Intent, error) {
	key, err := writeKey()
	if err != nil {
		return Intent{}, err
	}
	now := time.Now().UTC()
	writeErr := s.db.Write(ctx, key, "retry provider write intent "+intentID, func(tx *sql.Tx) error {
		current, err := verifyOwnedClaim(ctx, tx, intentID, workerID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE provider_write_intents
			SET status = 'retrying', claim_owner = '', claim_until = NULL, next_attempt_at = ?,
			    last_error_code = ?, last_error_redacted = ?, updated_at = ?
			WHERE id = ?`,
			at.UTC().Format(timeLayout), strings.TrimSpace(code), redacted, now.Format(timeLayout), intentID); err != nil {
			return err
		}
		return insertAttempt(ctx, tx, intentID, workerID, current.AttemptCount, "retryable", "", redacted, now, now)
	})
	if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
		return Intent{}, fmt.Errorf("outbox: retry %s: %w", intentID, writeErr)
	}
	return s.Get(ctx, intentID)
}

// MarkAmbiguous records that intentID's currently claimed attempt could not
// tell whether the remote write applied. The intent moves to reconciling,
// never back to pending/retrying directly - only an explicit reconciliation
// decision (Succeed, once reconciliation confirms the effect landed, or
// Retry, once it confirms it did not) may move it further, so a lost
// response can never cause a blind replay.
func (s *Store) MarkAmbiguous(ctx context.Context, intentID, workerID, requestID, redacted string) (Intent, error) {
	key, err := writeKey()
	if err != nil {
		return Intent{}, err
	}
	now := time.Now().UTC()
	writeErr := s.db.Write(ctx, key, "mark provider write intent ambiguous "+intentID, func(tx *sql.Tx) error {
		current, err := verifyOwnedClaim(ctx, tx, intentID, workerID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE provider_write_intents
			SET status = 'reconciling', claim_owner = '', claim_until = NULL,
			    provider_request_id = ?, last_error_redacted = ?, updated_at = ?
			WHERE id = ?`,
			strings.TrimSpace(requestID), redacted, now.Format(timeLayout), intentID); err != nil {
			return err
		}
		return insertAttempt(ctx, tx, intentID, workerID, current.AttemptCount, "ambiguous", requestID, redacted, now, now)
	})
	if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
		return Intent{}, fmt.Errorf("outbox: mark ambiguous %s: %w", intentID, writeErr)
	}
	return s.Get(ctx, intentID)
}

// Cancel withdraws intentID unless it already succeeded: a succeeded
// intent's external effect already happened and cancellation cannot undo
// it. Cancelling an already-cancelled intent is a no-op that returns the
// same row. Once cancelled, the row's claim predicate never matches again,
// so no worker can claim it afterward.
func (s *Store) Cancel(ctx context.Context, intentID, reason string) (Intent, error) {
	key, err := writeKey()
	if err != nil {
		return Intent{}, err
	}
	now := time.Now().UTC()
	writeErr := s.db.Write(ctx, key, "cancel provider write intent "+intentID, func(tx *sql.Tx) error {
		current, err := scanIntent(tx.QueryRowContext(ctx, `SELECT `+intentColumns+` FROM provider_write_intents WHERE id = ?`, intentID))
		if err != nil {
			return err
		}
		if current.Status == StatusSucceeded || current.Status == StatusCancelled {
			return nil
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE provider_write_intents
			SET status = 'cancelled', claim_owner = '', claim_until = NULL, next_attempt_at = NULL,
			    last_error_redacted = ?, updated_at = ?
			WHERE id = ?`,
			strings.TrimSpace(reason), now.Format(timeLayout), intentID)
		return err
	})
	if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
		return Intent{}, fmt.Errorf("outbox: cancel %s: %w", intentID, writeErr)
	}
	return s.Get(ctx, intentID)
}

// Get returns intentID's current durable state.
func (s *Store) Get(ctx context.Context, intentID string) (Intent, error) {
	return scanIntent(s.db.Reader().QueryRowContext(ctx, `SELECT `+intentColumns+` FROM provider_write_intents WHERE id = ?`, intentID))
}

// GetByFingerprint returns the intent enqueued under fingerprint, if any.
func (s *Store) GetByFingerprint(ctx context.Context, fingerprint string) (Intent, error) {
	return scanIntent(s.db.Reader().QueryRowContext(ctx, `SELECT `+intentColumns+` FROM provider_write_intents WHERE operation_fingerprint = ?`, fingerprint))
}

// LastAttemptOutcome returns the outcome recorded for intentID's most
// recent attempt (before the currently in-flight one, if any), and whether
// any attempt has been recorded at all. A worker that just claimed a row
// uses this to tell a fresh pending/due-retrying row (no prior attempt, or
// a prior "retryable" outcome - safe to execute the write directly) apart
// from one reclaimed out of reconciling (prior outcome "ambiguous" - must
// run operation-specific reconciliation before ever considering a second
// write attempt).
func (s *Store) LastAttemptOutcome(ctx context.Context, intentID string) (outcome string, found bool, err error) {
	row := s.db.Reader().QueryRowContext(ctx,
		`SELECT outcome FROM provider_write_attempts WHERE intent_id = ? ORDER BY attempt DESC LIMIT 1`, intentID)
	if err := row.Scan(&outcome); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("outbox: last attempt outcome for %s: %w", intentID, err)
	}
	return outcome, true, nil
}

// ListEffects returns every granular effect recorded for intentID.
func (s *Store) ListEffects(ctx context.Context, intentID string) ([]Effect, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT intent_id, effect_key, external_id, completed_at FROM provider_effects WHERE intent_id = ? ORDER BY effect_key`, intentID)
	if err != nil {
		return nil, fmt.Errorf("outbox: list effects for %s: %w", intentID, err)
	}
	defer rows.Close()
	out := []Effect{}
	for rows.Next() {
		var e Effect
		var completedAt string
		if err := rows.Scan(&e.IntentID, &e.EffectKey, &e.ExternalID, &completedAt); err != nil {
			return nil, fmt.Errorf("outbox: scan effect: %w", err)
		}
		if e.CompletedAt, err = time.Parse(timeLayout, completedAt); err != nil {
			return nil, fmt.Errorf("outbox: parse effect completed_at: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("outbox: list effects for %s: %w", intentID, err)
	}
	return out, nil
}
