// publish.go turns a verified lane into a published pull request. A lane's
// pr_number/pr_url/pr_provider/pr_repo_slug fields are the durable record of
// that (folded into DeliveryLane by reduce.go's lane.pr_published case) - a
// lane never opens a second pull request for the same attempt, no matter
// how many times PublishPullRequest is retried with a fresh idempotency
// key. Nothing in this file ever calls a merge or close endpoint; publishing
// a pull request and merging it are deliberately never the same operation.
package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// PublishPRRequest is what PublishPullRequest asks a PRProvider to publish.
type PublishPRRequest struct {
	RepoSlug   string
	BaseBranch string
	HeadBranch string
	Title      string
	Body       string
}

// PRProvider publishes one pull request for a PublishPRRequest, returning
// the provider's own number and a link to it. It never merges or closes
// anything - that capability deliberately does not exist anywhere in this
// domain.
type PRProvider interface {
	Publish(ctx context.Context, req PublishPRRequest) (number int, url string, err error)
}

// namedPRProvider is an optional capability a PRProvider implementation may
// add to identify which pr_provider enum value its published pull requests
// should be recorded under. PublishPullRequest falls back to "generic" for
// a PRProvider that does not implement this, rather than guessing or
// hardcoding a single provider's name for every implementation.
type namedPRProvider interface {
	ProviderName() protocol.DeliveryLanePrProvider
}

// PublishPullRequest publishes laneID's pull request via provider and
// records the result, or - if a pull request was already published for
// this lane's current attempt - returns the lane unchanged without calling
// provider at all. That check is domain-level and load-bearing, not
// redundant with idempotencyKey: a crash-and-retry that presents a
// different idempotencyKey after a process restart would otherwise have
// nothing else stopping it from opening a second pull request.
func (s *Store) PublishPullRequest(ctx context.Context, idempotencyKey, orchestrationID, laneID, leaseToken string, provider PRProvider, req PublishPRRequest, expectedRevision int) (*protocol.DeliveryLane, error) {
	lane, err := s.GetLane(ctx, orchestrationID, laneID)
	if err != nil {
		return nil, err
	}
	if lane.Revision != expectedRevision {
		return nil, ErrRevisionConflict
	}
	if lane.PrNumber != nil {
		return lane, nil
	}
	if lane.LeaseToken == nil || *lane.LeaseToken != leaseToken {
		return nil, ErrLeaseTokenMismatch
	}
	if lane.Status != protocol.DeliveryLaneStatusLeased && lane.Status != protocol.DeliveryLaneStatusRunning {
		return nil, ErrLaneNotRunnable
	}

	number, url, err := provider.Publish(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("delivery: publish pull request for lane %s: %w", laneID, err)
	}
	providerName := protocol.DeliveryLanePrProviderGeneric
	if np, ok := provider.(namedPRProvider); ok {
		providerName = np.ProviderName()
	}

	writeErr := s.db.Write(ctx, idempotencyKey, "publish pull request "+laneID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		current, err := reduceLane(orchestrationID, laneID, events)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if current.PrNumber != nil {
			return nil
		}

		encoded, err := json.Marshal(map[string]interface{}{
			"provider":  string(providerName),
			"repo_slug": req.RepoSlug,
			"number":    number,
			"url":       url,
		})
		if err != nil {
			return fmt.Errorf("delivery: encode pr published payload: %w", err)
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeLanePrPublished), Payload: string(encoded),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
		return nil, writeErr
	}
	return s.GetLane(ctx, orchestrationID, laneID)
}
