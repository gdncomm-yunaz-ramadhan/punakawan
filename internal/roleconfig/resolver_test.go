package roleconfig

import (
	"context"
	"errors"
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

func modePtr(m protocol.RoleConfigMode) *protocol.RoleConfigMode { return &m }

func TestEffectiveNilRestriction(t *testing.T) {
	rc := protocol.RoleConfig{
		Enabled:      true,
		Style:        protocol.RoleConfigStyleBalanced,
		Mode:         protocol.RoleConfigModeExecute,
		Capabilities: map[string]bool{"a": true, "b": false},
	}
	eff := Effective(rc, nil)
	if eff.Enabled != rc.Enabled || eff.Style != rc.Style || eff.Mode != rc.Mode {
		t.Fatalf("effective scalars diverged: %+v vs %+v", eff, rc)
	}
	if len(eff.Capabilities) != len(rc.Capabilities) {
		t.Fatalf("capabilities len = %d, want %d", len(eff.Capabilities), len(rc.Capabilities))
	}
	for k, v := range rc.Capabilities {
		if eff.Capabilities[k] != v {
			t.Errorf("capability %q = %v, want %v", k, eff.Capabilities[k], v)
		}
	}
	// The effective map must be a copy, not an alias of rc's map.
	eff.Capabilities["a"] = false
	if !rc.Capabilities["a"] {
		t.Errorf("mutating effective capabilities leaked into project config")
	}
}

func TestEffectiveModeOnlyReduces(t *testing.T) {
	// propose ceiling clamps an execute project mode down to propose.
	execRC := protocol.RoleConfig{Enabled: true, Mode: protocol.RoleConfigModeExecute, Capabilities: map[string]bool{}}
	eff := Effective(execRC, &Restriction{Mode: modePtr(protocol.RoleConfigModePropose)})
	if eff.Mode != protocol.RoleConfigModePropose {
		t.Errorf("execute clamped by propose ceiling = %q, want propose", eff.Mode)
	}

	// execute ceiling does NOT raise a propose project mode.
	propRC := protocol.RoleConfig{Enabled: true, Mode: protocol.RoleConfigModePropose, Capabilities: map[string]bool{}}
	eff = Effective(propRC, &Restriction{Mode: modePtr(protocol.RoleConfigModeExecute)})
	if eff.Mode != protocol.RoleConfigModePropose {
		t.Errorf("propose raised by execute ceiling = %q, want propose (never raised)", eff.Mode)
	}
}

func TestEffectiveCapabilitiesOnlyReduce(t *testing.T) {
	rc := protocol.RoleConfig{
		Enabled:      true,
		Mode:         protocol.RoleConfigModeExecute,
		Capabilities: map[string]bool{"on": true, "off": false},
	}
	// {cap:false} switches an on capability off.
	eff := Effective(rc, &Restriction{Capabilities: map[string]bool{"on": false}})
	if eff.Capabilities["on"] {
		t.Errorf("restriction false did not switch 'on' off")
	}
	// {cap:true} does NOT enable a project-disabled capability.
	eff = Effective(rc, &Restriction{Capabilities: map[string]bool{"off": true}})
	if eff.Capabilities["off"] {
		t.Errorf("restriction true wrongly enabled a project-disabled capability")
	}
}

func TestAuthorizeFailsClosed(t *testing.T) {
	base := EffectiveRoleConfig{
		Enabled:      true,
		Mode:         protocol.RoleConfigModeExecute,
		Capabilities: map[string]bool{"cap": true},
	}

	// Disabled role -> denied.
	disabled := base
	disabled.Enabled = false
	assertNotAuthorized(t, Authorize(disabled, "cap", protocol.RoleConfigModeExecute), "disabled role")

	// Effective mode below needed -> denied.
	lowMode := base
	lowMode.Mode = protocol.RoleConfigModeAssist
	assertNotAuthorized(t, Authorize(lowMode, "", protocol.RoleConfigModeExecute), "mode below needed")

	// Disabled capability -> denied.
	assertNotAuthorized(t, Authorize(base, "missing", protocol.RoleConfigModeExecute), "disabled capability")

	// Happy path: enabled, mode >= needed, capability on -> nil.
	if err := Authorize(base, "cap", protocol.RoleConfigModeExecute); err != nil {
		t.Errorf("happy path Authorize = %v, want nil", err)
	}
	// Capability empty (not gated) is allowed when mode suffices.
	if err := Authorize(base, "", protocol.RoleConfigModePropose); err != nil {
		t.Errorf("ungated Authorize = %v, want nil", err)
	}
}

func TestAuthorizeModeRankOrdering(t *testing.T) {
	// needed=propose passes when eff=execute, fails when eff=assist.
	execEff := EffectiveRoleConfig{Enabled: true, Mode: protocol.RoleConfigModeExecute, Capabilities: map[string]bool{}}
	if err := Authorize(execEff, "", protocol.RoleConfigModePropose); err != nil {
		t.Errorf("execute vs needed=propose = %v, want nil", err)
	}
	assistEff := EffectiveRoleConfig{Enabled: true, Mode: protocol.RoleConfigModeAssist, Capabilities: map[string]bool{}}
	assertNotAuthorized(t, Authorize(assistEff, "", protocol.RoleConfigModePropose), "assist vs needed=propose")

	// propose satisfies needed=propose (>= boundary), and assist satisfies assist.
	propEff := EffectiveRoleConfig{Enabled: true, Mode: protocol.RoleConfigModePropose, Capabilities: map[string]bool{}}
	if err := Authorize(propEff, "", protocol.RoleConfigModePropose); err != nil {
		t.Errorf("propose vs needed=propose = %v, want nil", err)
	}
	if err := Authorize(assistEff, "", protocol.RoleConfigModeAssist); err != nil {
		t.Errorf("assist vs needed=assist = %v, want nil", err)
	}
}

// TestStyleDoesNotChangeAuthorization guards the guardrail that a role's tone
// (Style) is presentation only: switching strict/balanced/creative must not
// change what the role is permitted to do. Authorization depends on Enabled,
// Mode, and the capability set — never on Style.
func TestStyleDoesNotChangeAuthorization(t *testing.T) {
	styles := []protocol.RoleConfigStyle{
		protocol.RoleConfigStyleStrict,
		protocol.RoleConfigStyleBalanced,
		protocol.RoleConfigStyleCreative,
	}
	checks := []struct {
		capability string
		needed     protocol.RoleConfigMode
	}{
		{"modify_files", protocol.RoleConfigModeExecute},
		{"modify_files", protocol.RoleConfigModePropose},
		{"absent", protocol.RoleConfigModeExecute},
		{"", protocol.RoleConfigModeAssist},
	}

	var baseline []bool
	for i, style := range styles {
		eff := EffectiveRoleConfig{
			Enabled:      true,
			Style:        style,
			Mode:         protocol.RoleConfigModePropose,
			Capabilities: map[string]bool{"modify_files": true},
		}
		var got []bool
		for _, c := range checks {
			got = append(got, Authorize(eff, c.capability, c.needed) == nil)
		}
		if i == 0 {
			baseline = got
			continue
		}
		for j := range got {
			if got[j] != baseline[j] {
				t.Errorf("style %q changed authorization for %+v: got %v, want %v (baseline strict)",
					style, checks[j], got[j], baseline[j])
			}
		}
	}
}

func TestPromptBlock(t *testing.T) {
	eff := EffectiveRoleConfig{
		Enabled: true,
		Style:   protocol.RoleConfigStyleStrict,
		Mode:    protocol.RoleConfigModePropose,
		Capabilities: map[string]bool{
			"zeta":  true,
			"alpha": true,
			"gamma": false,
			"beta":  false,
		},
	}
	block := PromptBlock(Gareng, eff, nil)

	if !strings.Contains(block, "Role configuration (gareng):") {
		t.Errorf("missing header:\n%s", block)
	}
	if !strings.Contains(block, "- Style: strict") {
		t.Errorf("missing style:\n%s", block)
	}
	if !strings.Contains(block, "- Mode: propose") {
		t.Errorf("missing mode:\n%s", block)
	}
	for _, cap := range []string{"alpha", "zeta", "beta", "gamma"} {
		if !strings.Contains(block, "  - "+cap) {
			t.Errorf("missing capability %q:\n%s", cap, block)
		}
	}
	if !strings.Contains(block, "- Disabled:") {
		t.Errorf("missing Disabled section:\n%s", block)
	}
	// Enabled capabilities are sorted: alpha before zeta.
	if strings.Index(block, "- alpha") > strings.Index(block, "- zeta") {
		t.Errorf("enabled capabilities not sorted:\n%s", block)
	}
	// Disabled capabilities are sorted: beta before gamma.
	if strings.Index(block, "- beta") > strings.Index(block, "- gamma") {
		t.Errorf("disabled capabilities not sorted:\n%s", block)
	}
	// Enabled section precedes Disabled section.
	if strings.Index(block, "- Enabled:") > strings.Index(block, "- Disabled:") {
		t.Errorf("Enabled must precede Disabled:\n%s", block)
	}

	// One-line mode reminder per mode.
	reminders := map[protocol.RoleConfigMode]string{
		protocol.RoleConfigModeAssist:  "You may read and analyze only; you may not make durable changes.",
		protocol.RoleConfigModePropose: "You may propose durable changes but may not execute them.",
		protocol.RoleConfigModeExecute: "You may execute enabled actions, under project policy and human approval.",
	}
	for mode, want := range reminders {
		e := eff
		e.Mode = mode
		b := PromptBlock(Gareng, e, nil)
		if !strings.Contains(b, want) {
			t.Errorf("mode %q reminder missing %q:\n%s", mode, want, b)
		}
	}
}

func TestPromptBlockNoDisabledSection(t *testing.T) {
	eff := EffectiveRoleConfig{
		Enabled:      true,
		Style:        protocol.RoleConfigStyleBalanced,
		Mode:         protocol.RoleConfigModeExecute,
		Capabilities: map[string]bool{"only": true},
	}
	block := PromptBlock(Semar, eff, nil)
	if strings.Contains(block, "- Disabled:") {
		t.Errorf("unexpected Disabled section when nothing is disabled:\n%s", block)
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

	eff := EffectiveRoleConfig{Enabled: true, Style: protocol.RoleConfigStyleBalanced, Mode: protocol.RoleConfigModePropose}

	proposals, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	block := PromptBlock(Petruk, eff, proposals)
	if strings.Contains(block, "Learned project facts:") || strings.Contains(block, pending.Rationale) {
		t.Fatalf("pending proposal must stay inactive until approved (AC4), got:\n%s", block)
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
	block = PromptBlock(Petruk, eff, proposals)
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

	eff := EffectiveRoleConfig{Enabled: true, Style: protocol.RoleConfigStyleBalanced, Mode: protocol.RoleConfigModePropose}

	proposals, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, role := range []Role{Petruk, Bagong} {
		block := PromptBlock(role, eff, proposals)
		if strings.Contains(block, "Learned project facts:") || strings.Contains(block, proposal.Rationale) {
			t.Fatalf("%s: pending inferred convention must stay inactive until approved (AC4), got:\n%s", role, block)
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
		block := PromptBlock(role, eff, proposals)
		if !strings.Contains(block, "Learned project facts:") {
			t.Fatalf("%s: accepted convention missing heading, got:\n%s", role, block)
		}
		if !strings.Contains(block, proposal.Rationale) {
			t.Fatalf("%s: accepted convention content missing, got:\n%s", role, block)
		}
	}
}

func TestResolverEffectiveEndToEnd(t *testing.T) {
	fixed := Defaults() // Semar defaults to execute mode.
	r := Resolver{
		Load: func(projectID string) (*protocol.RoleConfiguration, error) {
			c := fixed
			return &c, nil
		},
		Restrictions: func(projectID, workflowID string, role Role) (*Restriction, error) {
			return &Restriction{Mode: modePtr(protocol.RoleConfigModePropose)}, nil
		},
	}

	eff, err := r.Effective("proj", "wf-1", Semar)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if eff.Mode != protocol.RoleConfigModePropose {
		t.Errorf("resolver effective mode = %q, want propose (clamped from execute)", eff.Mode)
	}

	// Empty workflowID -> no restriction applied, project mode preserved.
	eff, err = r.Effective("proj", "", Semar)
	if err != nil {
		t.Fatalf("Effective no workflow: %v", err)
	}
	if eff.Mode != protocol.RoleConfigModeExecute {
		t.Errorf("resolver effective mode without workflow = %q, want execute", eff.Mode)
	}
}

func assertNotAuthorized(t *testing.T, err error, ctx string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: Authorize = nil, want ErrNotAuthorized", ctx)
		return
	}
	var nae ErrNotAuthorized
	if !errors.As(err, &nae) {
		t.Errorf("%s: err = %v, want ErrNotAuthorized", ctx, err)
	}
}
