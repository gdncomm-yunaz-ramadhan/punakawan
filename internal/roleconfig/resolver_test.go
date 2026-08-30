package roleconfig

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/convention"
	"github.com/ygrip/punakawan/internal/learning"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestPromptGuidanceEveryStyleIsConcreteAndDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range []protocol.RolePreferenceStyle{
		protocol.RolePreferenceStyleStrict,
		protocol.RolePreferenceStyleBalanced,
		protocol.RolePreferenceStyleCreative,
	} {
		block := PromptGuidance(Petruk, protocol.RolePreference{Style: s})
		if !strings.Contains(block, "Role prompt preferences (petruk):") {
			t.Errorf("style %q: missing header:\n%s", s, block)
		}
		if seen[block] {
			t.Errorf("style %q rendered guidance identical to a prior style", s)
		}
		seen[block] = true
	}
}

func TestPromptGuidanceOmitsEmptyInstructions(t *testing.T) {
	block := PromptGuidance(Semar, protocol.RolePreference{Style: protocol.RolePreferenceStyleBalanced})
	lines := strings.Split(strings.TrimSpace(block), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + one guidance line with no instructions line, got:\n%s", block)
	}
}

func TestPromptGuidanceAppendsInstructionsAfterStyle(t *testing.T) {
	block := PromptGuidance(Bagong, protocol.RolePreference{
		Style:        protocol.RolePreferenceStyleCreative,
		Instructions: "Flag any missing test coverage explicitly.",
	})
	styleIdx := strings.Index(block, "Explore multiple viable approaches")
	instrIdx := strings.Index(block, "Flag any missing test coverage explicitly.")
	if styleIdx < 0 || instrIdx < 0 || instrIdx < styleIdx {
		t.Errorf("expected fixed style guidance before free-text instructions:\n%s", block)
	}
}

// newTestLearningStore opens the shared storage kernel in a temp dir, mirroring
// internal/learning's own test setup, so this package can exercise the real
// Store-level accept path (Append with Status: learning.StatusAccepted)
// without any dependency on internal/learning's test helpers.
func newTestLearningStore(t *testing.T) *learning.Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return learning.New(db, "test-project")
}

// TestPromptBlockLearnedFactsGatedByAcceptance proves an inferred proposal
// stays dormant until approved: a proposal sitting in pending must not
// appear in a rendered PromptBlock, and the same proposal id, once
// transitioned to accepted at the Store level, must appear.
func TestPromptBlockLearnedFactsGatedByAcceptance(t *testing.T) {
	store := newTestLearningStore(t)
	now := time.Now().UTC()

	pending := learning.Proposal{
		Id:             "learn-1",
		ArtifactType:   learning.TypeMetadata,
		TargetId:       "no-ternary",
		Fingerprint:    "fp-1",
		Rationale:      "avoid ternary-style expressions in this codebase",
		SupportCount:   1,
		Status:         learning.StatusPending,
		Classification: learning.ClassificationInferred,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := store.Append(pending); err != nil {
		t.Fatalf("append pending: %v", err)
	}

	pref := protocol.RolePreference{Style: protocol.RolePreferenceStyleBalanced}

	proposals, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	block := PromptBlock(Petruk, pref, proposals)
	if strings.Contains(block, "Learned project facts:") || strings.Contains(block, pending.Rationale) {
		t.Fatalf("pending proposal must stay inactive until approved, got:\n%s", block)
	}

	// Transition to accepted at the Store level - the real (if currently
	// only) production path that writes this is
	// internal/mcpserver/tools_workflowdef_save.go's recordWorkflowJudgment,
	// which appends a fresh row with the same id and Status: StatusAccepted;
	// mirror that shape directly against the store here.
	accepted := pending
	accepted.Status = learning.StatusAccepted
	accepted.UpdatedAt = now.Add(time.Minute)
	if err := store.Append(accepted); err != nil {
		t.Fatalf("append accepted: %v", err)
	}

	proposals, err = store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	block = PromptBlock(Petruk, pref, proposals)
	if !strings.Contains(block, "Learned project facts:") {
		t.Fatalf("accepted proposal missing heading, got:\n%s", block)
	}
	if !strings.Contains(block, pending.Rationale) {
		t.Fatalf("accepted proposal content missing, got:\n%s", block)
	}
}

// TestPromptBlockNoTernaryConventionDetectorEndToEnd proves the dormant-to-
// approved pipeline end to end, run against the real detector rather than a
// hand-built proposal fixture: a repository containing the
// ternary-emulation idiom enough times crosses convention.DetectNoTernary
// Convention's threshold and produces a pending, inferred proposal that stays
// invisible in both Petruk's and Bagong's rendered role context; once that
// same proposal is transitioned to accepted at the Store level - the same
// "no real approval UI exists yet" simulation
// TestPromptBlockLearnedFactsGatedByAcceptance above uses - it appears in
// both roles' rendered context.
func TestPromptBlockNoTernaryConventionDetectorEndToEnd(t *testing.T) {
	const projectID = "test-project"
	store := newTestLearningStore(t)

	// Fixture repo: one Go file using the ternary-emulation helper idiom
	// three times (Ternary/Ternary/IIf), meeting the detector's threshold.
	repoDir := t.TempDir()
	fixture := `package fixture

func demo() int {
	a := Ternary(true, 1, 2)
	b := Ternary(false, 3, 4)
	c := IIf(true, 5, 6)
	return a + b + c
}
`
	if err := os.WriteFile(filepath.Join(repoDir, "demo.go"), []byte(fixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	proposal, found, err := convention.RecordNoTernaryConvention(store, repoDir, projectID)
	if err != nil {
		t.Fatalf("RecordNoTernaryConvention: %v", err)
	}
	if !found {
		t.Fatalf("expected the fixture's 3 ternary-helper call sites to cross the detection threshold")
	}
	if proposal.Status != learning.StatusPending {
		t.Fatalf("newly detected convention proposal Status = %q, want %q", proposal.Status, learning.StatusPending)
	}
	if proposal.Classification != learning.ClassificationInferred {
		t.Fatalf("newly detected convention proposal Classification = %q, want %q", proposal.Classification, learning.ClassificationInferred)
	}
	if proposal.ArtifactType != learning.TypeConvention {
		t.Fatalf("newly detected convention proposal ArtifactType = %q, want %q", proposal.ArtifactType, learning.TypeConvention)
	}

	pref := protocol.RolePreference{Style: protocol.RolePreferenceStyleBalanced}

	proposals, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, role := range []Role{Petruk, Bagong} {
		block := PromptBlock(role, pref, proposals)
		if strings.Contains(block, "Learned project facts:") || strings.Contains(block, proposal.Rationale) {
			t.Fatalf("%s: pending inferred convention must stay inactive until approved, got:\n%s", role, block)
		}
	}

	// Simulate approval: no review/accept UI exists yet, so - exactly like
	// TestPromptBlockLearnedFactsGatedByAcceptance above - transition the
	// proposal directly at the Store level.
	accepted := proposal
	accepted.Status = learning.StatusAccepted
	accepted.UpdatedAt = proposal.UpdatedAt.Add(time.Minute)
	if err := store.Append(accepted); err != nil {
		t.Fatalf("append accepted: %v", err)
	}

	proposals, err = store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, role := range []Role{Petruk, Bagong} {
		block := PromptBlock(role, pref, proposals)
		if !strings.Contains(block, "Learned project facts:") {
			t.Fatalf("%s: accepted convention missing heading, got:\n%s", role, block)
		}
		if !strings.Contains(block, proposal.Rationale) {
			t.Fatalf("%s: accepted convention content missing, got:\n%s", role, block)
		}
	}
}

func TestResolverGet(t *testing.T) {
	fixed := Defaults()
	strict := protocol.RolePreferenceStyleStrict
	fixed.Roles.Semar.Style = strict

	r := Resolver{
		Load: func(projectID string) (*protocol.RolePreferences, error) {
			c := fixed
			return &c, nil
		},
	}

	pref, err := r.Get("proj", Semar)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if pref.Style != strict {
		t.Errorf("resolved style = %q, want strict", pref.Style)
	}

	if _, err := r.Get("proj", Role("togog")); err == nil {
		t.Errorf("Get(unknown role) = nil error, want ErrUnknownRole")
	}
}
