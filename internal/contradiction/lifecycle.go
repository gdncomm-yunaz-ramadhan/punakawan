package contradiction

import (
	"fmt"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// linearNext maps each status to the single forward step allowed in §18's
// resolution chain (detected -> triaged -> needs_clarification ->
// resolution_proposed -> resolved). Statuses with no forward step map to the
// empty value. The two escape hatches (any -> accepted_divergence, any ->
// superseded) are handled specially in Transition rather than duplicated into
// every row here, so the chain stays readable as exactly one path.
var linearNext = map[protocol.ContradictionStatus]protocol.ContradictionStatus{
	protocol.ContradictionStatusDetected:           protocol.ContradictionStatusTriaged,
	protocol.ContradictionStatusTriaged:            protocol.ContradictionStatusNeedsClarification,
	protocol.ContradictionStatusNeedsClarification: protocol.ContradictionStatusResolutionProposed,
	protocol.ContradictionStatusResolutionProposed: protocol.ContradictionStatusResolved,
}

// Transition moves c.Status to `to` if and only if §18's lifecycle DAG permits
// it, mutating c in place on success and returning ErrIllegalTransition
// otherwise. The DAG is the linear resolution chain plus two escape hatches
// reachable from any non-terminal state: accepted_divergence (a divergence we
// choose to live with) and superseded (the contradiction no longer applies).
// superseded is terminal - nothing leaves it, so a stale record can never be
// silently revived - and accepted_divergence can only move on to superseded.
func Transition(c *protocol.Contradiction, to protocol.ContradictionStatus) error {
	if c == nil {
		return fmt.Errorf("contradiction: transition nil record")
	}
	from := c.Status

	switch from {
	case protocol.ContradictionStatusSuperseded:
		// Terminal: a superseded contradiction is done for good.
		return illegal(from, to)
	case protocol.ContradictionStatusAcceptedDivergence:
		// An accepted divergence can only later be superseded (e.g. the code
		// changed so the divergence no longer exists); it cannot re-enter the
		// resolution chain.
		if to == protocol.ContradictionStatusSuperseded {
			c.Status = to
			return nil
		}
		return illegal(from, to)
	}

	// From any non-terminal state, the two escape hatches are always legal.
	if to == protocol.ContradictionStatusSuperseded || to == protocol.ContradictionStatusAcceptedDivergence {
		c.Status = to
		return nil
	}
	// Otherwise only the single forward step in the resolution chain is legal.
	if next, ok := linearNext[from]; ok && next == to {
		c.Status = to
		return nil
	}
	return illegal(from, to)
}

func illegal(from, to protocol.ContradictionStatus) error {
	return fmt.Errorf("contradiction: %s -> %s: %w", from, to, ErrIllegalTransition)
}

// ProposeResolution records a proposed resolution on the contradiction and
// advances it to resolution_proposed (§18). requiresHuman marks resolutions
// that a human must confirm before Resolve may be called - a machine-proposed
// statement is a suggestion, not a decision.
func ProposeResolution(root, id, proposed, rationale string, requiresHuman bool) error {
	c, err := Get(root, id)
	if err != nil {
		return err
	}
	if err := Transition(c, protocol.ContradictionStatusResolutionProposed); err != nil {
		return err
	}
	if c.Resolution == nil {
		c.Resolution = &protocol.ContradictionResolution{}
	}
	c.Resolution.ProposedStatement = &proposed
	c.Resolution.Rationale = &rationale
	c.Resolution.RequiresHumanConfirmation = &requiresHuman
	return Put(root, *c, PutOptions{})
}

// Resolve records the confirmed statement and who confirmed it, and advances
// the contradiction to resolved (§18). It only succeeds from
// resolution_proposed, so a resolution is always preceded by an explicit
// proposal that could be reviewed.
func Resolve(root, id, statement, by string) error {
	c, err := Get(root, id)
	if err != nil {
		return err
	}
	if err := Transition(c, protocol.ContradictionStatusResolved); err != nil {
		return err
	}
	now := time.Now().UTC()
	if c.Resolution == nil {
		c.Resolution = &protocol.ContradictionResolution{}
	}
	c.Resolution.ResolvedStatement = &statement
	c.Resolution.ResolvedBy = &by
	c.Resolution.ResolvedAt = &now
	return Put(root, *c, PutOptions{})
}

// AcceptDivergence records that the disagreement is a divergence the project
// chooses to live with, attributing the decision to `by`, and advances the
// contradiction to accepted_divergence. Unlike Resolve this is reachable from
// any non-terminal state - a divergence can be accepted without first walking
// the full resolution chain.
func AcceptDivergence(root, id, by string) error {
	c, err := Get(root, id)
	if err != nil {
		return err
	}
	if err := Transition(c, protocol.ContradictionStatusAcceptedDivergence); err != nil {
		return err
	}
	now := time.Now().UTC()
	if c.Resolution == nil {
		c.Resolution = &protocol.ContradictionResolution{}
	}
	c.Resolution.ResolvedBy = &by
	c.Resolution.ResolvedAt = &now
	return Put(root, *c, PutOptions{})
}

// DefaultBlocking returns the default `blocking` flag for a severity, per §19:
// only critical contradictions block progress by default; everything else is
// recorded/warned but does not halt work unless a role explicitly marks it
// blocking. Callers use this to seed Contradiction.Blocking at detection time.
func DefaultBlocking(sev protocol.ContradictionSeverity) bool {
	return sev == protocol.ContradictionSeverityCritical
}

// resolvedStatuses are the terminal states in which a contradiction no longer
// halts progress: resolved (settled), accepted_divergence (chosen to live
// with), superseded (no longer applies). Any other status is "open".
var resolvedStatuses = map[protocol.ContradictionStatus]bool{
	protocol.ContradictionStatusResolved:           true,
	protocol.ContradictionStatusAcceptedDivergence: true,
	protocol.ContradictionStatusSuperseded:         true,
}

// IsResolvedStatus reports whether a status is terminal/non-blocking (§18).
func IsResolvedStatus(s protocol.ContradictionStatus) bool {
	return resolvedStatuses[s]
}

// OpenBlocking returns the project's contradictions that are both unresolved
// (IsResolvedStatus is false) and marked blocking. Semar must not finalize a
// plan while any of these exist (§22/CONTRA-008); callers surface them to the
// human rather than silently proceeding.
func OpenBlocking(root string) ([]protocol.Contradiction, error) {
	all, err := List(root)
	if err != nil {
		return nil, err
	}
	var out []protocol.Contradiction
	for _, c := range all {
		if IsResolvedStatus(c.Status) {
			continue
		}
		if c.Blocking != nil && *c.Blocking {
			out = append(out, c)
		}
	}
	return out, nil
}
