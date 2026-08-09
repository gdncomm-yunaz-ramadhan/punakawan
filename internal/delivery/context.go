// context.go assembles the bounded, hashed context a role stage
// receives when working a lane: the lane itself, its parent task, the
// exact pinned requirement source snapshots that task was built from,
// its project's delivery profile, and the exact base commit its
// worktree forked from. Retrieval-based context (ranked accepted
// knowledge, prior verified lessons) is deliberately not assembled
// here yet - the current knowledge store is still backed by Dolt and
// is itself being replaced by a dedicated SQLite migration, and wiring
// a role's context digest to a store about to be replaced would just
// be more code to throw away. Once that migration lands, its
// selections can be added into the digest computed here without
// changing this file's own shape.
package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// LaneContext is the bounded context one role stage receives for a
// lane: everything it needs is either pinned by id here or reachable
// through an id it contains, never a raw dump of unrelated project or
// conversation state.
type LaneContext struct {
	Lane       *protocol.DeliveryLane
	ParentTask *protocol.ParentTask
	Sources    []*protocol.RequirementSource
	Profile    *protocol.ProjectDeliveryProfile
	BaseSha    string
	// Digest is a hex sha256 over the exact combination of ids and
	// revisions above: lane id/revision, parent task id/revision,
	// each source's id/revision, profile id/revision, and base_sha.
	// Two calls that return the same digest saw exactly the same pinned
	// state; a role stage can record this digest alongside its output
	// so a later reviewer can confirm what context it actually worked
	// from.
	Digest string
}

// BuildLaneContext assembles laneID's bounded context: it fails closed
// (propagating ErrNotFound) if the lane is not yet routed to a parent
// task, or if any source id the parent task was built from no longer
// resolves to a real RequirementSource - a role stage is never handed
// a context with a dangling reference in it.
func (s *Store) BuildLaneContext(ctx context.Context, orchestrationID, laneID string) (*LaneContext, error) {
	lane, err := s.GetLane(ctx, orchestrationID, laneID)
	if err != nil {
		return nil, err
	}
	if lane.ParentTaskId == nil || *lane.ParentTaskId == "" {
		return nil, fmt.Errorf("delivery: lane %s has not been routed to a parent task yet", laneID)
	}

	task, err := s.GetParentTask(ctx, orchestrationID, *lane.ParentTaskId)
	if err != nil {
		return nil, err
	}

	sources := make([]*protocol.RequirementSource, 0, len(task.SourceIds))
	for _, sourceID := range task.SourceIds {
		source, err := s.GetRequirementSource(ctx, orchestrationID, sourceID)
		if err != nil {
			return nil, fmt.Errorf("delivery: resolve pinned source %s for task %s: %w", sourceID, task.Id, err)
		}
		sources = append(sources, source)
	}

	profile, err := s.GetDeliveryProfile(ctx, lane.ProjectId)
	if err != nil {
		return nil, err
	}

	baseSha := ""
	if lane.BaseSha != nil {
		baseSha = *lane.BaseSha
	}

	digest, err := laneContextDigest(lane, task, sources, profile, baseSha)
	if err != nil {
		return nil, err
	}

	return &LaneContext{
		Lane:       lane,
		ParentTask: task,
		Sources:    sources,
		Profile:    profile,
		BaseSha:    baseSha,
		Digest:     digest,
	}, nil
}

// laneContextDigest hashes exactly the ids and revisions a
// LaneContext pins, in a deterministic field order, so the same pinned
// state always produces the same digest regardless of map iteration
// or source ordering.
func laneContextDigest(lane *protocol.DeliveryLane, task *protocol.ParentTask, sources []*protocol.RequirementSource, profile *protocol.ProjectDeliveryProfile, baseSha string) (string, error) {
	type sourcePin struct {
		ID       string `json:"id"`
		Revision int    `json:"revision"`
	}
	pins := make([]sourcePin, 0, len(sources))
	for _, source := range sources {
		pins = append(pins, sourcePin{ID: source.Id, Revision: source.Revision})
	}

	payload := struct {
		LaneID          string      `json:"lane_id"`
		LaneRevision    int         `json:"lane_revision"`
		ParentTaskID    string      `json:"parent_task_id"`
		ParentRevision  int         `json:"parent_task_revision"`
		Sources         []sourcePin `json:"sources"`
		ProfileID       string      `json:"profile_id"`
		ProfileRevision int         `json:"profile_revision"`
		BaseSha         string      `json:"base_sha"`
	}{
		LaneID:          lane.Id,
		LaneRevision:    lane.Revision,
		ParentTaskID:    task.Id,
		ParentRevision:  task.Revision,
		Sources:         pins,
		ProfileID:       profile.Id,
		ProfileRevision: profile.Revision,
		BaseSha:         baseSha,
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("delivery: encode lane context digest input: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
