package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// ContentHash returns a schema-compliant "sha256:<hex>" digest (§7.3,
// content_hash pattern ^sha256:[0-9a-f]{64}$) of data. Callers use this to
// compute the *current* hash of whatever a record's source actually is (a
// file on disk, an API response body, etc.); locating and fetching that
// source is provider-specific and out of scope here, since no provider
// integrations exist in this repo yet.
func ContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CheckStale implements the "Staleness checks" mitigation from §11 (Risk:
// Agent-generated knowledge becomes stale or false): it compares a record's
// stored source.content_hash against the currentHash of its source as
// re-fetched by the caller. If the hashes differ, the record no longer
// reflects its source, so its validity.state is transitioned to stale
// (§7.4) and persisted via Put. If they match, the record is left
// untouched and stale=false is returned.
//
// If the record has no stored content_hash, there is no baseline to compare
// currentHash against, so CheckStale returns stale=false, nil rather than
// guessing: absence of a hash means staleness was never measurable for this
// record, not that it is known to be fresh, but this method cannot
// distinguish those without a comparison point, so it declines to mutate
// validity.state.
func (s *Store) CheckStale(id string, currentHash string) (bool, error) {
	rec, err := s.Get(id)
	if err != nil {
		return false, fmt.Errorf("knowledge: check stale %s: %w", id, err)
	}

	if rec.Source.ContentHash == nil {
		return false, nil
	}

	if *rec.Source.ContentHash == currentHash {
		return false, nil
	}

	rec.Validity.State = protocol.KnowledgeRecordValidityStateStale
	if err := s.Put(rec); err != nil {
		return false, fmt.Errorf("knowledge: mark %s stale: %w", id, err)
	}
	return true, nil
}
