package delivery

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func strPtr(s string) *string { return &s }

func edge(from, to string) *protocol.DependencyEdge {
	return &protocol.DependencyEdge{
		FromTaskId: from,
		ToTaskId:   to,
		Type:       protocol.DependencyEdgeTypeRequires,
		Status:     protocol.DependencyEdgeStatusActive,
	}
}

func lane(id, taskID string, createdAt time.Time) *protocol.DeliveryLane {
	return &protocol.DeliveryLane{
		Id:           id,
		ParentTaskId: strPtr(taskID),
		Status:       protocol.DeliveryLaneStatusRunnable,
		CreatedAt:    createdAt,
	}
}

// TestChooseLanePrefersLongestCriticalPath builds a chain (a -> b -> c,
// meaning a depends on b depends on c) alongside an unrelated single
// task d. Finishing c unlocks the longest downstream chain, so its
// lane must be chosen over d's even though d's lane is older.
func TestChooseLanePrefersLongestCriticalPath(t *testing.T) {
	edges := []*protocol.DependencyEdge{edge("a", "b"), edge("b", "c")}
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	lanes := []*protocol.DeliveryLane{
		lane("lane-d", "d", older),
		lane("lane-c", "c", newer),
	}
	got := ChooseLane(lanes, edges)
	if got == nil || got.Id != "lane-c" {
		t.Fatalf("expected lane-c (longest critical path) chosen, got %+v", got)
	}
}

// TestChooseLaneBreaksTiesByUnlockCountThenAge builds a diamond (a and
// b both depend on shared, c depends on nothing) where shared has the
// same critical-path length as an unrelated task e, but unlocks two
// dependents instead of zero - shared must win the tie.
func TestChooseLaneBreaksTiesByUnlockCountThenAge(t *testing.T) {
	edges := []*protocol.DependencyEdge{edge("a", "shared"), edge("b", "shared")}
	now := time.Now()
	lanes := []*protocol.DeliveryLane{
		lane("lane-e", "e", now.Add(-time.Hour)),
		lane("lane-shared", "shared", now),
	}
	got := ChooseLane(lanes, edges)
	if got == nil || got.Id != "lane-shared" {
		t.Fatalf("expected lane-shared (higher unlock count) chosen, got %+v", got)
	}
}

// TestChooseLaneFallsBackToOldestFirst proves that two lanes with
// identical critical-path length and unlock count (both zero, both
// unrelated to any edge) resolve deterministically to the older one -
// no lane with no downstream value is starved forever behind newer
// arrivals of equal rank.
func TestChooseLaneFallsBackToOldestFirst(t *testing.T) {
	var edges []*protocol.DependencyEdge
	now := time.Now()
	lanes := []*protocol.DeliveryLane{
		lane("lane-new", "x", now),
		lane("lane-old", "y", now.Add(-time.Hour)),
	}
	got := ChooseLane(lanes, edges)
	if got == nil || got.Id != "lane-old" {
		t.Fatalf("expected lane-old (oldest, tie on rank) chosen, got %+v", got)
	}
}

// TestChooseLaneIgnoresNonRunnableLanes proves a leased/blocked/etc
// lane is never returned even if it would otherwise rank first.
func TestChooseLaneIgnoresNonRunnableLanes(t *testing.T) {
	edges := []*protocol.DependencyEdge{edge("a", "b")}
	leased := lane("lane-b", "b", time.Now())
	leased.Status = protocol.DeliveryLaneStatusLeased
	got := ChooseLane([]*protocol.DeliveryLane{leased}, edges)
	if got != nil {
		t.Fatalf("expected no runnable lane, got %+v", got)
	}
}

func TestChooseLaneReturnsNilWhenNoneRunnable(t *testing.T) {
	if got := ChooseLane(nil, nil); got != nil {
		t.Fatalf("expected nil for empty input, got %+v", got)
	}
}
