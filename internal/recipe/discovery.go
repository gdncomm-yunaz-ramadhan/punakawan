package recipe

import (
	"fmt"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// DiscoveryState is §8's guided-discovery state machine.
type DiscoveryState string

const (
	DiscoveryMissing               DiscoveryState = "missing"
	DiscoveryCollectingRules       DiscoveryState = "collecting_rules"
	DiscoveryCompiling             DiscoveryState = "compiling"
	DiscoveryTesting               DiscoveryState = "testing"
	DiscoveryPresentingResults     DiscoveryState = "presenting_results"
	DiscoveryCorrecting            DiscoveryState = "correcting"
	DiscoveryRetesting             DiscoveryState = "retesting"
	DiscoveryAccepted              DiscoveryState = "accepted"
	DiscoveryStored                DiscoveryState = "stored"
	DiscoveryExecutingOriginalTask DiscoveryState = "executing_original_task"

	// Exits, per §8. SavedAsDraft is not in the plan's ASCII diagram (which
	// only draws the happy path) but is one of §9's explicit user options
	// ("...or save as draft") and KNOW-RECIPE-024's own task list, so it is
	// modeled as a sibling exit alongside the diagram's named ones.
	DiscoveryCancelled           DiscoveryState = "cancelled"
	DiscoveryRejected            DiscoveryState = "rejected"
	DiscoverySavedAsDraft        DiscoveryState = "saved_as_draft"
	DiscoveryUnresolved          DiscoveryState = "unresolved"
	DiscoveryProviderUnavailable DiscoveryState = "provider_unavailable"
	DiscoveryPolicyBlocked       DiscoveryState = "policy_blocked"
)

// Terminal reports whether s is one of §8's exits or the successful end
// of the loop (EXECUTING_ORIGINAL_TASK) - a session in any of these
// states will never advance further.
func (s DiscoveryState) Terminal() bool {
	switch s {
	case DiscoveryExecutingOriginalTask, DiscoveryCancelled, DiscoveryRejected,
		DiscoverySavedAsDraft, DiscoveryUnresolved, DiscoveryProviderUnavailable, DiscoveryPolicyBlocked:
		return true
	}
	return false
}

// discoveryTransitions is the state machine's edge list. Every state
// change goes through transition(), which rejects any target not listed
// here - an invalid jump (e.g. presenting results while still MISSING)
// is a programming error, not a silent no-op.
var discoveryTransitions = map[DiscoveryState][]DiscoveryState{
	DiscoveryMissing:           {DiscoveryCollectingRules, DiscoveryCancelled},
	DiscoveryCollectingRules:   {DiscoveryCollectingRules, DiscoveryCompiling, DiscoveryCancelled, DiscoveryUnresolved},
	DiscoveryCompiling:         {DiscoveryTesting, DiscoveryPolicyBlocked, DiscoveryCancelled},
	DiscoveryTesting:           {DiscoveryPresentingResults, DiscoveryProviderUnavailable, DiscoveryCancelled},
	DiscoveryPresentingResults: {DiscoveryAccepted, DiscoveryCorrecting, DiscoveryRejected, DiscoverySavedAsDraft, DiscoveryCancelled},
	DiscoveryCorrecting:        {DiscoveryRetesting, DiscoverySavedAsDraft, DiscoveryCancelled, DiscoveryUnresolved},
	DiscoveryRetesting:         {DiscoveryPresentingResults, DiscoveryProviderUnavailable, DiscoveryCancelled},
	DiscoveryAccepted:          {DiscoveryStored},
	DiscoveryStored:            {DiscoveryExecutingOriginalTask},
}

// DiscoveryTransition is one edge taken by a session, kept for an
// auditable/resumable history - §8's "checkpoints accepted answers so an
// interrupted session can resume."
type DiscoveryTransition struct {
	From DiscoveryState
	To   DiscoveryState
	Note string
	At   time.Time
}

// DiscoverySession is one resumable run of the guided-discovery loop. It
// is a plain, JSON-serializable value: this package does not own where a
// session is persisted (workflow checkpoints, a knowledge draft record,
// or any other mechanism already assumed to exist per §1.2's "session
// checkpoints") - only how it advances. A caller loads the last saved
// value, calls the relevant method, and saves the result right back.
type DiscoverySession struct {
	ID          string
	Capability  string
	Intent      string
	WorkspaceID string

	State DiscoveryState

	// Answers accumulates every resolved constraint collected from the
	// user, keyed by whatever prompt key the caller used to ask for it
	// (e.g. "project", "board", "component_rule"). MissingConstraints is
	// the mechanism behind §8's "ask only for unresolved constraints":
	// a resumed session skips any key already present here.
	Answers map[string]interface{}

	// Exclusions/MustInclude are the negative/positive examples the user
	// supplies while correcting a candidate (§9's "exclude a result,
	// identify a missing result"), consumed by Validator.Validate's
	// step 9.
	Exclusions  []string
	MustInclude []string

	// Candidate/LastCompiled/LastReport hold the most recent
	// compile+validate attempt, so PRESENTING_RESULTS/CORRECTING can show
	// what actually ran without recompiling.
	Candidate    *protocol.KnowledgeRecordRetrievalRecipe
	LastCompiled *CompiledQuery
	LastReport   *ValidationReport

	History []DiscoveryTransition
}

// NewDiscoverySession starts a session in MISSING for the given typed
// operation request (§6's OperationRequest, minus the parts Resolver
// already answered by finding no reusable candidate).
func NewDiscoverySession(id, capability, intent, workspaceID string) *DiscoverySession {
	return &DiscoverySession{
		ID:          id,
		Capability:  capability,
		Intent:      intent,
		WorkspaceID: workspaceID,
		State:       DiscoveryMissing,
		Answers:     map[string]interface{}{},
	}
}

func (s *DiscoverySession) transition(to DiscoveryState, note string, now time.Time) error {
	for _, allowed := range discoveryTransitions[s.State] {
		if allowed == to {
			s.History = append(s.History, DiscoveryTransition{From: s.State, To: to, Note: note, At: now})
			s.State = to
			return nil
		}
	}
	return fmt.Errorf("recipe: discovery: invalid transition %s -> %s", s.State, to)
}

// MissingConstraints returns which of required's constraint keys have no
// recorded answer yet. required is caller-supplied because this phase
// has no capability-to-required-constraints registry of its own.
func (s *DiscoverySession) MissingConstraints(required []string) []string {
	var missing []string
	for _, k := range required {
		if _, ok := s.Answers[k]; !ok {
			missing = append(missing, k)
		}
	}
	return missing
}

// Answer records one resolved constraint, entering COLLECTING_RULES on
// the session's first answer.
func (s *DiscoverySession) Answer(key string, value interface{}, now time.Time) error {
	if s.State != DiscoveryMissing && s.State != DiscoveryCollectingRules {
		return fmt.Errorf("recipe: discovery: cannot record an answer in state %s", s.State)
	}
	if s.State == DiscoveryMissing {
		if err := s.transition(DiscoveryCollectingRules, "collecting rules", now); err != nil {
			return err
		}
	}
	s.Answers[key] = value
	return nil
}

// BeginCompiling moves from COLLECTING_RULES to COMPILING once every
// required constraint has an answer.
func (s *DiscoverySession) BeginCompiling(now time.Time) error {
	return s.transition(DiscoveryCompiling, "compiling candidate recipe", now)
}

// SetCandidate records the recipe compiled from Answers so far. Valid in
// COMPILING (the first attempt) or RETESTING (after a correction);
// unlike BeginCompiling/BeginTesting it does not itself change state -
// RETESTING already represents "compile and dry-run again" as one step.
func (s *DiscoverySession) SetCandidate(rr *protocol.KnowledgeRecordRetrievalRecipe, cq CompiledQuery) error {
	if s.State != DiscoveryCompiling && s.State != DiscoveryRetesting {
		return fmt.Errorf("recipe: discovery: cannot set a candidate in state %s", s.State)
	}
	s.Candidate = rr
	s.LastCompiled = &cq
	return nil
}

// BeginTesting moves from COMPILING to TESTING once a candidate has been
// compiled, before the provider dry run runs.
func (s *DiscoverySession) BeginTesting(now time.Time) error {
	if s.Candidate == nil {
		return fmt.Errorf("recipe: discovery: cannot begin testing without a compiled candidate")
	}
	return s.transition(DiscoveryTesting, "dry-running compiled candidate", now)
}

// SetReport records the dry-run/validation result and moves to
// PRESENTING_RESULTS, from either TESTING (first attempt) or RETESTING
// (after a correction).
func (s *DiscoverySession) SetReport(report ValidationReport, now time.Time) error {
	if s.State != DiscoveryTesting && s.State != DiscoveryRetesting {
		return fmt.Errorf("recipe: discovery: cannot set a report in state %s", s.State)
	}
	s.LastReport = &report
	return s.transition(DiscoveryPresentingResults, "results ready for review", now)
}

// Correct moves from PRESENTING_RESULTS to CORRECTING: the user asked to
// change conditions, exclude a result, or add a missing example.
func (s *DiscoverySession) Correct(now time.Time) error {
	return s.transition(DiscoveryCorrecting, "user requested corrections", now)
}

// Exclude records a result key the user says should not have matched,
// consumed by the next Validator.Validate call's negative-example check.
func (s *DiscoverySession) Exclude(key string) {
	s.Exclusions = append(s.Exclusions, key)
}

// AddMissingExample records a result key the user expected to match but
// didn't, consumed the same way as Exclude.
func (s *DiscoverySession) AddMissingExample(key string) {
	s.MustInclude = append(s.MustInclude, key)
}

// BeginRetesting moves from CORRECTING to RETESTING once the user's
// corrections have been folded into Answers/Exclusions/MustInclude. The
// caller recompiles (SetCandidate) and re-validates (SetReport) from
// this state, closing the "retest until accepted, rejected, saved as
// draft, or cancelled" loop as many times as needed.
func (s *DiscoverySession) BeginRetesting(now time.Time) error {
	return s.transition(DiscoveryRetesting, "retesting corrected candidate", now)
}

// Accept moves from PRESENTING_RESULTS to ACCEPTED. Only a passed
// validation report may be accepted - §9's "a recipe does not become
// verified just because Jira accepts the JQL," extended here to "just
// because the user is impatient."
func (s *DiscoverySession) Accept(acceptedBy string, now time.Time) error {
	if s.LastReport == nil || s.LastReport.Status != protocol.KnowledgeRecordRetrievalRecipeValidationStatusPassed {
		return fmt.Errorf("recipe: discovery: cannot accept without a passed validation report")
	}
	return s.transition(DiscoveryAccepted, fmt.Sprintf("accepted by %s", acceptedBy), now)
}

// Store moves from ACCEPTED to STORED. The caller is expected to have
// already persisted the accepted recipe (Repository.CreateVersion) with
// its validity.state set to verified before calling this - Store only
// records that the loop's persistence step happened.
func (s *DiscoverySession) Store(now time.Time) error {
	return s.transition(DiscoveryStored, "recipe persisted", now)
}

// ResumeOriginalTask moves from STORED to EXECUTING_ORIGINAL_TASK, per
// §16: "the original task remains active during discovery. Knowledge
// creation is a prerequisite subtask, not a separate forgotten
// conversation."
func (s *DiscoverySession) ResumeOriginalTask(now time.Time) error {
	return s.transition(DiscoveryExecutingOriginalTask, "resuming original task", now)
}

// Reject, SaveAsDraft, Cancel, MarkUnresolved, MarkProviderUnavailable,
// and MarkPolicyBlocked end the loop via one of §8's named exits.
func (s *DiscoverySession) Reject(reason string, now time.Time) error {
	return s.transition(DiscoveryRejected, reason, now)
}

func (s *DiscoverySession) SaveAsDraft(reason string, now time.Time) error {
	return s.transition(DiscoverySavedAsDraft, reason, now)
}

func (s *DiscoverySession) Cancel(reason string, now time.Time) error {
	return s.transition(DiscoveryCancelled, reason, now)
}

func (s *DiscoverySession) MarkUnresolved(reason string, now time.Time) error {
	return s.transition(DiscoveryUnresolved, reason, now)
}

func (s *DiscoverySession) MarkProviderUnavailable(reason string, now time.Time) error {
	return s.transition(DiscoveryProviderUnavailable, reason, now)
}

func (s *DiscoverySession) MarkPolicyBlocked(reason string, now time.Time) error {
	return s.transition(DiscoveryPolicyBlocked, reason, now)
}
