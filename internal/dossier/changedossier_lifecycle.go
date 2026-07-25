package dossier

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// ErrIllegalTransition is returned by Advance when the requested status change
// is not one of §39's legal edges.
var ErrIllegalTransition = errors.New("dossier: illegal status transition")

// ErrBlockingFindings is the sentinel wrapped by every Finalize refusal, so
// callers can test errors.Is(err, ErrBlockingFindings) without depending on
// the concrete BlockingError type or its message.
var ErrBlockingFindings = errors.New("dossier: blocking findings prevent completion")

// BlockingError reports the specific reasons Finalize refused to complete a
// dossier (§36/§59: "blocking issues prevent verification"). It carries the
// human-readable blocker list so a caller can surface each one, and unwraps to
// ErrBlockingFindings for sentinel matching.
type BlockingError struct {
	Blockers []string
}

func (e *BlockingError) Error() string {
	return fmt.Sprintf("dossier: %d blocking finding(s) prevent completion: %s",
		len(e.Blockers), strings.Join(e.Blockers, "; "))
}

func (e *BlockingError) Unwrap() error { return ErrBlockingFindings }

// legalForward is §39's linear lifecycle: each status may advance only to the
// single status listed here. The two universal escapes (any->disputed,
// any->superseded) are handled separately in Advance because they apply from
// every state, not just these edges.
var legalForward = map[protocol.ChangeDossierStatus]protocol.ChangeDossierStatus{
	protocol.ChangeDossierStatusDraft:                protocol.ChangeDossierStatusContextReady,
	protocol.ChangeDossierStatusContextReady:         protocol.ChangeDossierStatusPlanned,
	protocol.ChangeDossierStatusPlanned:              protocol.ChangeDossierStatusImplementing,
	protocol.ChangeDossierStatusImplementing:         protocol.ChangeDossierStatusAwaitingVerification,
	protocol.ChangeDossierStatusAwaitingVerification: protocol.ChangeDossierStatusVerified,
	protocol.ChangeDossierStatusVerified:             protocol.ChangeDossierStatusCompleted,
}

// legalTransition reports whether from->to is one of §39's allowed edges:
// either the single forward step for `from`, or the universal escape to
// disputed or superseded (allowed from any state).
func legalTransition(from, to protocol.ChangeDossierStatus) bool {
	if to == protocol.ChangeDossierStatusDisputed || to == protocol.ChangeDossierStatusSuperseded {
		return true
	}
	next, ok := legalForward[from]
	return ok && next == to
}

// Advance moves the dossier's status to `to`, enforcing §39's legal order. It
// loads the current dossier, rejects an illegal transition with
// ErrIllegalTransition (nothing is mutated), and otherwise persists the new
// status through Put - which snapshots the pre-advance state into versions/.
// Advancing to the status it is already at is not a legal edge and is
// rejected, so a no-op save never silently records a snapshot.
func Advance(root, id string, to protocol.ChangeDossierStatus) error {
	d, err := readCurrent(root, id)
	if err != nil {
		return err
	}
	if !legalTransition(d.Status, to) {
		return fmt.Errorf("dossier: %s -> %s: %w", d.Status, to, ErrIllegalTransition)
	}
	d.Status = to
	_, err = Put(root, d)
	return err
}

// Finalize advances the dossier to completed, but only when it is free of
// blocking findings: no unresolved contradictions, no missing required plan
// items or unapproved material deviations (§36), and no disputed or rejected
// claims. When any blocker exists it returns a *BlockingError (which unwraps to
// ErrBlockingFindings) listing every reason and leaves the dossier untouched.
// When clean it defers to Advance, so the verified->completed edge is still
// enforced: Finalize cannot skip the lifecycle, only gate its last step.
func Finalize(root, id string) error {
	loaded, err := Get(root, id)
	if err != nil {
		return err
	}
	if blockers := finalizeBlockers(loaded); len(blockers) > 0 {
		return &BlockingError{Blockers: blockers}
	}
	return Advance(root, id, protocol.ChangeDossierStatusCompleted)
}

// finalizeBlockers collects every reason a dossier may not be completed. It is
// the dossier-only blockingFindings plus the claim-derived blockers (disputed
// or rejected claims), which live in sibling records and so are only visible
// once the dossier's claims are loaded.
func finalizeBlockers(loaded Loaded) []string {
	blockers := blockingFindings(loaded.Dossier)
	for _, c := range loaded.Claims {
		switch c.Status {
		case protocol.DossierClaimStatusDisputed:
			blockers = append(blockers, fmt.Sprintf("claim %q is disputed", c.Id))
		case protocol.DossierClaimStatusRejected:
			blockers = append(blockers, fmt.Sprintf("claim %q is rejected", c.Id))
		}
	}
	return blockers
}

// blockingFindings returns the blocking issues derivable from the dossier
// itself (independent of its sibling claim records), per §36: unresolved
// contradictions, missing required plan items, unapproved material deviations,
// and any verification dimension marked disputed or rejected. It is the basis
// for the §38 "Blocking findings" summary indicator.
func blockingFindings(d protocol.ChangeDossier) []string {
	var out []string
	if d.Contradictions != nil {
		for _, c := range d.Contradictions.Unresolved {
			out = append(out, fmt.Sprintf("unresolved contradiction %q", c))
		}
	}
	if d.PlanConformance != nil {
		if d.PlanConformance.Missing != nil && *d.PlanConformance.Missing > 0 {
			out = append(out, fmt.Sprintf("%d missing plan item(s)", *d.PlanConformance.Missing))
		}
		// Approved deviations are allowed (§36); unapproved ones are
		// unexplained material deviations and block verification.
		for _, dev := range d.PlanConformance.DeliberateDeviations {
			if dev.Approved == nil || !*dev.Approved {
				out = append(out, fmt.Sprintf("unapproved deviation %q", dev.Item))
			}
		}
	}
	for dim, status := range d.Verification {
		if status == string(protocol.DossierClaimStatusDisputed) ||
			status == string(protocol.DossierClaimStatusRejected) {
			out = append(out, fmt.Sprintf("verification %q is %s", dim, status))
		}
	}
	return out
}

// hasBlockingFindings reports whether the dossier itself carries any blocking
// finding (§36). It considers only the dossier, not its sibling claim records;
// Finalize layers the claim-derived blockers on top.
func hasBlockingFindings(d protocol.ChangeDossier) bool {
	return len(blockingFindings(d)) > 0
}

// Conformance returns the plan-conformance totals (§36) - implemented, partial,
// and missing item counts - defaulting each to 0 when the dossier has no
// plan_conformance block or leaves a count unset.
func Conformance(d protocol.ChangeDossier) (implemented, partial, missing int) {
	pc := d.PlanConformance
	if pc == nil {
		return 0, 0, 0
	}
	if pc.Implemented != nil {
		implemented = *pc.Implemented
	}
	if pc.Partial != nil {
		partial = *pc.Partial
	}
	if pc.Missing != nil {
		missing = *pc.Missing
	}
	return implemented, partial, missing
}
