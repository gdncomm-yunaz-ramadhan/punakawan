// artifacts.go implements a content-addressed evidence store: bytes are
// addressed by server-computed sha256 and never overwritten; invocation
// records are addressed by ULID and reference
// those bytes rather than a mutable path. Both tables are insert-only -
// there is no event log here, unlike the mutable entities in store.go.
package delivery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// artifactsRoot returns the local directory blob bytes are stored
// under: a sibling "<db-file>-artifacts" directory next to whatever
// database file this Store's kernel was opened against. Deriving it
// from db.Path() (rather than the global storage.DataDir) keeps each
// opened database's blobs colocated with it - required for test
// isolation (each test opens its own temp-dir database) and for
// correctness if more than one database is ever opened in one process.
func (s *Store) artifactsRoot() (string, error) {
	root := s.db.Path() + "-artifacts"
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("delivery: create artifacts root: %w", err)
	}
	return root, nil
}

// blobPath shards by the hash's first two hex characters (after the
// "sha256:" prefix) so the directory never holds an unbounded flat list
// of files. Both segments are plain hex, so the path is inherently
// Windows-safe: no colon, no path separator embedded in an id.
func blobPath(root, contentHash string) (string, error) {
	hex := trimHashPrefix(contentHash)
	if len(hex) != 64 {
		return "", fmt.Errorf("delivery: malformed content hash %q", contentHash)
	}
	return filepath.Join(root, hex[:2], hex), nil
}

func trimHashPrefix(contentHash string) string {
	const prefix = "sha256:"
	if len(contentHash) > len(prefix) && contentHash[:len(prefix)] == prefix {
		return contentHash[len(prefix):]
	}
	return contentHash
}

// PutArtifact writes bytes to the content-addressed store, computing
// the hash itself - a caller-supplied hash is never trusted or even
// accepted as a parameter. Writing already-present content is a
// harmless no-op (deduplication is inherent to content addressing).
// The write is atomic: bytes land at a temp path first, then an atomic
// rename places them at their final, hash-derived path, so a concurrent
// writer of the same content can never observe a partial file.
func (s *Store) PutArtifact(ctx context.Context, data []byte, mediaType string) (string, error) {
	sum := sha256.Sum256(data)
	contentHash := "sha256:" + hex.EncodeToString(sum[:])

	root, err := s.artifactsRoot()
	if err != nil {
		return "", err
	}
	dest, err := blobPath(root, contentHash)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(dest); err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("delivery: stat artifact blob: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return "", fmt.Errorf("delivery: create artifact shard dir: %w", err)
		}
		tmp, err := os.CreateTemp(filepath.Dir(dest), "blob-*.tmp")
		if err != nil {
			return "", fmt.Errorf("delivery: create temp artifact file: %w", err)
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			return "", fmt.Errorf("delivery: write temp artifact file: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return "", fmt.Errorf("delivery: close temp artifact file: %w", err)
		}
		if err := os.Rename(tmp.Name(), dest); err != nil {
			// A concurrent writer of the identical content may have won
			// the race; since content is hash-addressed, that is fine as
			// long as the destination now exists.
			if _, statErr := os.Stat(dest); statErr != nil {
				return "", fmt.Errorf("delivery: finalize artifact file: %w", err)
			}
		}
	}

	now := time.Now().UTC()
	err = s.db.Write(ctx, "blob-"+contentHash, "store artifact blob "+contentHash, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO artifact_blobs (content_hash, media_type, byte_size, stored_at) VALUES (?, ?, ?, ?)`,
			contentHash, mediaType, len(data), now.Format(timeLayout),
		)
		return err
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return "", err
	}
	return contentHash, nil
}

// GetArtifact reads bytes back from the content-addressed store and
// re-verifies the hash on read, so silent on-disk corruption is
// detected rather than trusted.
func (s *Store) GetArtifact(contentHash string) ([]byte, error) {
	root, err := s.artifactsRoot()
	if err != nil {
		return nil, err
	}
	path, err := blobPath(root, contentHash)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("delivery: read artifact blob: %w", err)
	}
	sum := sha256.Sum256(data)
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != contentHash {
		return nil, fmt.Errorf("delivery: artifact blob %s is corrupt (bytes hash to %s)", contentHash, got)
	}
	return data, nil
}

// ArtifactRef identifies where an invocation happened; laneID and
// parentTaskID are optional (a project-level check may not belong to a
// specific lane or task).
type ArtifactRef struct {
	OrchestrationID string
	ProjectID       string
	LaneID          string
	ParentTaskID    string
	Kind            protocol.EvidenceArtifactKind
	Producer        string
}

// RecordArtifact inserts one immutable invocation record referencing an
// already-stored content hash. id must be minted by the caller (NewID())
// so retries with the same id and idempotencyKey are harmless, matching
// the rest of this package's write convention.
func (s *Store) RecordArtifact(ctx context.Context, idempotencyKey, id string, ref ArtifactRef, contentHash string) (*protocol.EvidenceArtifact, error) {
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "record artifact "+id, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM artifact_blobs WHERE content_hash = ?`, contentHash).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO evidence_artifacts (id, orchestration_id, project_id, lane_id, parent_task_id, kind, content_hash, producer, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, ref.OrchestrationID, ref.ProjectID, nullableString(ref.LaneID), nullableString(ref.ParentTaskID),
			string(ref.Kind), contentHash, ref.Producer, now.Format(timeLayout),
		)
		return err
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetArtifactRecord(ctx, id)
}

// GetArtifactRecord fails closed (ErrNotFound) for an unknown id.
func (s *Store) GetArtifactRecord(ctx context.Context, id string) (*protocol.EvidenceArtifact, error) {
	row := s.db.Reader().QueryRowContext(ctx, `
		SELECT a.id, a.orchestration_id, a.project_id, a.lane_id, a.parent_task_id, a.kind, a.content_hash, b.media_type, b.byte_size, a.producer, a.created_at
		FROM evidence_artifacts a JOIN artifact_blobs b ON b.content_hash = a.content_hash
		WHERE a.id = ?`, id)
	return scanArtifact(row)
}

// ArtifactFilter enumerates artifacts by any combination of scope
// fields; a zero field is not filtered on.
type ArtifactFilter struct {
	OrchestrationID string
	ProjectID       string
	LaneID          string
	ParentTaskID    string
}

// ListArtifacts enumerates artifacts matching filter: enumerable by
// orchestration, project, lane, parent, and invocation.
func (s *Store) ListArtifacts(ctx context.Context, filter ArtifactFilter) ([]*protocol.EvidenceArtifact, error) {
	query := `
		SELECT a.id, a.orchestration_id, a.project_id, a.lane_id, a.parent_task_id, a.kind, a.content_hash, b.media_type, b.byte_size, a.producer, a.created_at
		FROM evidence_artifacts a JOIN artifact_blobs b ON b.content_hash = a.content_hash WHERE 1=1`
	var args []interface{}
	if filter.OrchestrationID != "" {
		query += " AND a.orchestration_id = ?"
		args = append(args, filter.OrchestrationID)
	}
	if filter.ProjectID != "" {
		query += " AND a.project_id = ?"
		args = append(args, filter.ProjectID)
	}
	if filter.LaneID != "" {
		query += " AND a.lane_id = ?"
		args = append(args, filter.LaneID)
	}
	if filter.ParentTaskID != "" {
		query += " AND a.parent_task_id = ?"
		args = append(args, filter.ParentTaskID)
	}
	query += " ORDER BY a.created_at ASC"

	rows, err := s.db.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("delivery: list artifacts: %w", err)
	}
	defer rows.Close()

	var out []*protocol.EvidenceArtifact
	for rows.Next() {
		a, err := scanArtifactRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...interface{}) error
}

func scanArtifact(row scannable) (*protocol.EvidenceArtifact, error) {
	a, err := scanArtifactRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func scanArtifactRow(row scannable) (*protocol.EvidenceArtifact, error) {
	var a protocol.EvidenceArtifact
	var laneID, parentTaskID, producer, createdAt sql.NullString
	if err := row.Scan(&a.Id, &a.OrchestrationId, &a.ProjectId, &laneID, &parentTaskID, &a.Kind, &a.ContentHash, &a.MediaType, &a.ByteSize, &producer, &createdAt); err != nil {
		return nil, err
	}
	if laneID.Valid {
		a.LaneId = &laneID.String
	}
	if parentTaskID.Valid {
		a.ParentTaskId = &parentTaskID.String
	}
	if producer.Valid && producer.String != "" {
		a.Producer = &producer.String
	}
	t, err := time.Parse(timeLayout, createdAt.String)
	if err != nil {
		return nil, fmt.Errorf("delivery: parse created_at for artifact %s: %w", a.Id, err)
	}
	a.CreatedAt = t
	return &a, nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
