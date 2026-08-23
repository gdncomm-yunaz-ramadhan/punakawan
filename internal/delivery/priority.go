// priority.go picks which runnable lane a scheduler should hand out
// next, deterministically: longer downstream dependency chains go
// first (finishing them unblocks the most future work the soonest),
// then more directly-unlocked dependents, then oldest lane first so a
// lane with no downstream dependents at all still eventually runs
// instead of being starved forever by a stream of newer, higher-value
// arrivals.
package delivery

import (
	"sort"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// dependents maps each task to the tasks that directly depend on it
// (the reverse of edges' from-depends-on-to direction), considering
// only active, blocking edges.
func dependents(edges []*protocol.DependencyEdge) map[string][]string {
	out := map[string][]string{}
	for _, e := range edges {
		if e.Status != protocol.DependencyEdgeStatusActive || !blockingEdgeTypes[e.Type] {
			continue
		}
		out[e.ToTaskId] = append(out[e.ToTaskId], e.FromTaskId)
	}
	return out
}

// unlockCounts returns, for each task, how many other tasks (directly
// or transitively) depend on it - completing it unlocks that many.
func unlockCounts(edges []*protocol.DependencyEdge) map[string]int {
	deps := dependents(edges)
	counts := map[string]int{}
	for task := range deps {
		seen := map[string]bool{}
		var walk func(string)
		walk = func(t string) {
			for _, dep := range deps[t] {
				if !seen[dep] {
					seen[dep] = true
					walk(dep)
				}
			}
		}
		walk(task)
		counts[task] = len(seen)
	}
	return counts
}

// criticalPathLengths returns, for each task, the length of its longest
// downstream dependent chain. The dependency graph is acyclic by
// construction (AddDependencyEdge rejects any edge that would close a
// cycle), so plain memoized recursion terminates without a cycle guard.
func criticalPathLengths(edges []*protocol.DependencyEdge) map[string]int {
	deps := dependents(edges)
	memo := map[string]int{}
	var length func(string) int
	length = func(t string) int {
		if v, ok := memo[t]; ok {
			return v
		}
		best := 0
		for _, dep := range deps[t] {
			if l := 1 + length(dep); l > best {
				best = l
			}
		}
		memo[t] = best
		return best
	}
	out := map[string]int{}
	for task := range deps {
		out[task] = length(task)
	}
	return out
}

// RankLanes sorts lanes in place, best candidate first, given the
// orchestration's active dependency edges: longest critical path
// first, then highest unlock count, then oldest lane first (by
// CreatedAt, tie-broken by id) so two calls against identical input
// always agree and no lane is starved by an endless stream of
// equally-ranked newer arrivals. Lanes of any status may be passed in;
// only their relative order changes, so a caller that already
// filtered to runnable lanes gets a priority-ordered list back, not
// just the top pick.
func RankLanes(lanes []*protocol.DeliveryLane, edges []*protocol.DependencyEdge) {
	pathLengths := criticalPathLengths(edges)
	unlocks := unlockCounts(edges)
	taskOf := func(l *protocol.DeliveryLane) string {
		if l.ParentTaskId == nil {
			return ""
		}
		return *l.ParentTaskId
	}

	sort.SliceStable(lanes, func(i, j int) bool {
		a, b := lanes[i], lanes[j]
		if pl := pathLengths[taskOf(a)] - pathLengths[taskOf(b)]; pl != 0 {
			return pl > 0
		}
		if ul := unlocks[taskOf(a)] - unlocks[taskOf(b)]; ul != 0 {
			return ul > 0
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.Id < b.Id
	})
}

// ChooseLane picks the single best lane to lease next out of every
// currently-runnable lane in lanes, given the orchestration's active
// dependency edges, using the same ranking RankLanes applies. Returns
// nil if no lane is runnable.
func ChooseLane(lanes []*protocol.DeliveryLane, edges []*protocol.DependencyEdge) *protocol.DeliveryLane {
	var runnable []*protocol.DeliveryLane
	for _, l := range lanes {
		if l.Status == protocol.DeliveryLaneStatusRunnable {
			runnable = append(runnable, l)
		}
	}
	if len(runnable) == 0 {
		return nil
	}
	RankLanes(runnable, edges)
	return runnable[0]
}
