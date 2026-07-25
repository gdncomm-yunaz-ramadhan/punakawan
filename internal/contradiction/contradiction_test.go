package contradiction

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
	"gopkg.in/yaml.v3"
)

// sample builds a minimal valid contradiction for id with the given subject
// key. Two claims satisfy the schema's minItems:2 on claims.
func sample(id, subjectKey string, sev protocol.ContradictionSeverity) protocol.Contradiction {
	return protocol.Contradiction{
		Id:        id,
		ProjectId: "proj",
		Title:     "Disagreement about " + subjectKey,
		Severity:  sev,
		Status:    protocol.ContradictionStatusDetected,
		Subject: protocol.ContradictionSubject{
			Type: protocol.ContradictionSubjectTypeConfiguration,
			Key:  &subjectKey,
		},
		Claims: []protocol.ContradictionClaimsElem{
			{Source: protocol.ContradictionClaimsElemSource{Type: protocol.ContradictionClaimsElemSourceTypeRepository}, Statement: "A"},
			{Source: protocol.ContradictionClaimsElemSource{Type: protocol.ContradictionClaimsElemSourceTypeConfluence}, Statement: "B"},
		},
	}
}

func TestPutGetRoundTripAndStamps(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := Put(root, sample("c1", "payout.retry.max", protocol.ContradictionSeverityMaterial), PutOptions{Now: now}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := Get(root, "c1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Version != Version {
		t.Errorf("Version = %q, want %q", got.Version, Version)
	}
	if got.CreatedAt == nil || !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}
	if got.UpdatedAt == nil || !got.UpdatedAt.Equal(now) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, now)
	}

	// A second write preserves CreatedAt but advances UpdatedAt.
	later := now.Add(time.Hour)
	got.Title = "updated"
	if err := Put(root, *got, PutOptions{Now: later}); err != nil {
		t.Fatalf("Put update: %v", err)
	}
	got2, err := Get(root, "c1")
	if err != nil {
		t.Fatalf("Get 2: %v", err)
	}
	if !got2.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt drifted: %v, want %v", got2.CreatedAt, now)
	}
	if !got2.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", got2.UpdatedAt, later)
	}
}

func TestListEmptyWhenAbsent(t *testing.T) {
	list, err := List(t.TempDir())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list == nil || len(list) != 0 {
		t.Fatalf("List = %#v, want empty non-nil slice", list)
	}
}

func TestListAndIndexRefresh(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"a", "b", "c"} {
		if err := Put(root, sample(id, "key."+id, protocol.ContradictionSeverityMinor), PutOptions{}); err != nil {
			t.Fatalf("Put %s: %v", id, err)
		}
	}
	list, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List len = %d, want 3", len(list))
	}

	// The index must summarize all three records.
	data, err := os.ReadFile(filepath.Join(root, ".punakawan", "contradictions", "index.yaml"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var doc indexDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse index: %v", err)
	}
	if doc.Version != Version {
		t.Errorf("index version = %q, want %q", doc.Version, Version)
	}
	if len(doc.Contradictions) != 3 {
		t.Fatalf("index entries = %d, want 3", len(doc.Contradictions))
	}
	for _, e := range doc.Contradictions {
		if e.ID == "" || e.Subject.Key == nil || e.UpdatedAt == nil {
			t.Errorf("index entry incomplete: %+v", e)
		}
	}
}

func TestTransitionsLegalChain(t *testing.T) {
	c := sample("c", "k", protocol.ContradictionSeverityMaterial)
	chain := []protocol.ContradictionStatus{
		protocol.ContradictionStatusTriaged,
		protocol.ContradictionStatusNeedsClarification,
		protocol.ContradictionStatusResolutionProposed,
		protocol.ContradictionStatusResolved,
	}
	for _, to := range chain {
		if err := Transition(&c, to); err != nil {
			t.Fatalf("Transition to %s: %v", to, err)
		}
		if c.Status != to {
			t.Fatalf("Status = %s, want %s", c.Status, to)
		}
	}
}

func TestTransitionsEscapeHatches(t *testing.T) {
	// any -> accepted_divergence
	c := sample("c", "k", protocol.ContradictionSeverityMinor)
	c.Status = protocol.ContradictionStatusTriaged
	if err := Transition(&c, protocol.ContradictionStatusAcceptedDivergence); err != nil {
		t.Fatalf("triaged -> accepted_divergence: %v", err)
	}
	// accepted_divergence -> superseded is allowed; -> anything else is not.
	if err := Transition(&c, protocol.ContradictionStatusSuperseded); err != nil {
		t.Fatalf("accepted_divergence -> superseded: %v", err)
	}
	// superseded is terminal.
	if err := Transition(&c, protocol.ContradictionStatusResolved); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("superseded -> resolved err = %v, want ErrIllegalTransition", err)
	}
}

func TestTransitionsIllegal(t *testing.T) {
	c := sample("c", "k", protocol.ContradictionSeverityMinor) // detected
	// Skipping straight to resolved is not a legal single step.
	if err := Transition(&c, protocol.ContradictionStatusResolved); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("detected -> resolved err = %v, want ErrIllegalTransition", err)
	}
	if c.Status != protocol.ContradictionStatusDetected {
		t.Fatalf("Status mutated on illegal transition: %s", c.Status)
	}
}

func TestProposeResolveAcceptDivergence(t *testing.T) {
	root := t.TempDir()
	// Walk detected -> triaged -> needs_clarification so ProposeResolution's
	// required predecessor state exists.
	c := sample("c1", "k", protocol.ContradictionSeverityMaterial)
	c.Status = protocol.ContradictionStatusNeedsClarification
	if err := Put(root, c, PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := ProposeResolution(root, "c1", "use 5 retries", "aligns with SLA", true); err != nil {
		t.Fatalf("ProposeResolution: %v", err)
	}
	got, _ := Get(root, "c1")
	if got.Status != protocol.ContradictionStatusResolutionProposed {
		t.Fatalf("status = %s, want resolution_proposed", got.Status)
	}
	if got.Resolution == nil || got.Resolution.ProposedStatement == nil || *got.Resolution.ProposedStatement != "use 5 retries" {
		t.Fatalf("proposed statement not recorded: %+v", got.Resolution)
	}
	if got.Resolution.RequiresHumanConfirmation == nil || !*got.Resolution.RequiresHumanConfirmation {
		t.Fatalf("requires_human_confirmation not recorded")
	}

	if err := Resolve(root, "c1", "5 retries", "alice"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	got, _ = Get(root, "c1")
	if got.Status != protocol.ContradictionStatusResolved {
		t.Fatalf("status = %s, want resolved", got.Status)
	}
	if got.Resolution.ResolvedStatement == nil || *got.Resolution.ResolvedStatement != "5 retries" {
		t.Fatalf("resolved statement not recorded")
	}
	if got.Resolution.ResolvedBy == nil || *got.Resolution.ResolvedBy != "alice" || got.Resolution.ResolvedAt == nil {
		t.Fatalf("resolved_by/at not recorded: %+v", got.Resolution)
	}

	// AcceptDivergence from a fresh detected record.
	d := sample("c2", "k2", protocol.ContradictionSeverityMinor)
	if err := Put(root, d, PutOptions{}); err != nil {
		t.Fatalf("Put c2: %v", err)
	}
	if err := AcceptDivergence(root, "c2", "bob"); err != nil {
		t.Fatalf("AcceptDivergence: %v", err)
	}
	got, _ = Get(root, "c2")
	if got.Status != protocol.ContradictionStatusAcceptedDivergence {
		t.Fatalf("status = %s, want accepted_divergence", got.Status)
	}
	if got.Resolution == nil || got.Resolution.ResolvedBy == nil || *got.Resolution.ResolvedBy != "bob" {
		t.Fatalf("accepted-by not recorded: %+v", got.Resolution)
	}
}

func TestDefaultBlockingPerSeverity(t *testing.T) {
	cases := map[protocol.ContradictionSeverity]bool{
		protocol.ContradictionSeverityInformational: false,
		protocol.ContradictionSeverityMinor:         false,
		protocol.ContradictionSeverityMaterial:      false,
		protocol.ContradictionSeverityCritical:      true,
	}
	for sev, want := range cases {
		if got := DefaultBlocking(sev); got != want {
			t.Errorf("DefaultBlocking(%s) = %v, want %v", sev, got, want)
		}
	}
}

func TestOpenBlocking(t *testing.T) {
	root := t.TempDir()
	blocking := true
	notBlocking := false

	// open + blocking -> counts
	open := sample("open-block", "a.key", protocol.ContradictionSeverityCritical)
	open.Blocking = &blocking
	// open but not blocking -> excluded
	warn := sample("open-warn", "b.key", protocol.ContradictionSeverityMinor)
	warn.Blocking = &notBlocking
	// blocking but resolved -> excluded
	resolved := sample("resolved-block", "c.key", protocol.ContradictionSeverityCritical)
	resolved.Blocking = &blocking
	resolved.Status = protocol.ContradictionStatusResolved
	// blocking but accepted divergence -> excluded
	accepted := sample("accepted-block", "d.key", protocol.ContradictionSeverityCritical)
	accepted.Blocking = &blocking
	accepted.Status = protocol.ContradictionStatusAcceptedDivergence

	for _, c := range []protocol.Contradiction{open, warn, resolved, accepted} {
		if err := Put(root, c, PutOptions{}); err != nil {
			t.Fatalf("Put(%s): %v", c.Id, err)
		}
	}

	got, err := OpenBlocking(root)
	if err != nil {
		t.Fatalf("OpenBlocking: %v", err)
	}
	if len(got) != 1 || got[0].Id != "open-block" {
		ids := make([]string, len(got))
		for i, c := range got {
			ids[i] = c.Id
		}
		t.Fatalf("OpenBlocking = %v, want [open-block]", ids)
	}

	if !IsResolvedStatus(protocol.ContradictionStatusResolved) {
		t.Error("resolved should be a resolved status")
	}
	if IsResolvedStatus(protocol.ContradictionStatusDetected) {
		t.Error("detected should not be a resolved status")
	}
}

func TestNormalizeKey(t *testing.T) {
	cases := map[string]string{
		"payout.retry.max_attempts":     "payout retry max attempts",
		"  Payout.Retry.Max_Attempts  ": "payout retry max attempts",
		"payout/retry/max attempts":     "payout retry max attempts",
		"UPPER":                         "upper",
		"":                              "",
		"---":                           "",
	}
	for in, want := range cases {
		if got := NormalizeKey(in); got != want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFindCandidatesDedup(t *testing.T) {
	root := t.TempDir()
	if err := Put(root, sample("c1", "Payout.Retry.Max_Attempts", protocol.ContradictionSeverityMaterial), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// A different subject key that must not match.
	if err := Put(root, sample("c2", "timeout.seconds", protocol.ContradictionSeverityMinor), PutOptions{}); err != nil {
		t.Fatalf("Put c2: %v", err)
	}

	// Same logical key, different spelling -> should find c1.
	found, err := FindCandidates(root, string(protocol.ContradictionSubjectTypeConfiguration), "payout retry max attempts")
	if err != nil {
		t.Fatalf("FindCandidates: %v", err)
	}
	if len(found) != 1 || found[0].Id != "c1" {
		t.Fatalf("FindCandidates = %+v, want exactly c1", found)
	}

	// Wrong subject type -> no match even with the same key.
	none, err := FindCandidates(root, string(protocol.ContradictionSubjectTypeApiOperation), "payout retry max attempts")
	if err != nil {
		t.Fatalf("FindCandidates (wrong type): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("FindCandidates wrong type = %+v, want none", none)
	}
}
