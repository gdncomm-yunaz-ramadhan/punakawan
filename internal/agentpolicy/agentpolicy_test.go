package agentpolicy

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLoadDefaultsWhenAbsent(t *testing.T) {
	root := t.TempDir()
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Defaults()
	if !reflect.DeepEqual(*cfg, want) {
		t.Errorf("Load on an unconfigured root = %+v, want defaults %+v", *cfg, want)
	}
	if cfg.Capabilities != (DeclaredCapabilities{}) {
		t.Errorf("expected an unconfigured project to declare no capabilities, got %+v", cfg.Capabilities)
	}
}

func TestLoadRejectsUnsupportedVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, dirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath(root), []byte("version: punakawan.agentpolicy/v99\nrevision: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("expected an error loading an unsupported agent policy version")
	}
}

func TestSaveAndLoadRoundTrips(t *testing.T) {
	root := t.TempDir()
	cfg := Defaults()
	cfg.Capabilities = DeclaredCapabilities{Fork: true, ModelSelection: true}
	if err := Save(root, &cfg, SaveOptions{Now: time.Now().UTC(), Action: "init"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.Capabilities.Fork || !got.Capabilities.ModelSelection {
		t.Errorf("declared capabilities did not round-trip: %+v", got.Capabilities)
	}
	if got.Agents.Implementation.Strategy != "fork" {
		t.Errorf("implementation policy did not round-trip: %+v", got.Agents.Implementation)
	}
}

func TestSnapshotImmutabilityAndAudit(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

	cfg := Defaults()
	if err := Save(root, &cfg, SaveOptions{Now: now, Actor: "a1", Action: "init"}); err != nil {
		t.Fatalf("Save #1: %v", err)
	}

	cfg2, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	off := PurposePolicy{Model: "inherit", Reasoning: "high"}
	if err := Update(cfg2, Patch{Review: &off}, 0); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := Save(root, cfg2, SaveOptions{Now: now, Actor: "a2", Action: "update"}); err != nil {
		t.Fatalf("Save #2: %v", err)
	}

	snap0 := filepath.Join(root, dirName, subDir, versionsDir, "0.yaml")
	before, err := os.ReadFile(snap0)
	if err != nil {
		t.Fatalf("expected snapshot 0.yaml after two saves: %v", err)
	}

	cfg3, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	pinned := PurposePolicy{Model: "pinned-model"}
	if err := Update(cfg3, Patch{Orchestrator: &pinned}, 1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := Save(root, cfg3, SaveOptions{Now: now, Actor: "a3", Action: "update"}); err != nil {
		t.Fatalf("Save #3: %v", err)
	}

	after, err := os.ReadFile(snap0)
	if err != nil {
		t.Fatalf("read 0.yaml after later save: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("0.yaml changed after a later save; snapshots must be immutable")
	}
	if _, err := os.Stat(filepath.Join(root, dirName, subDir, versionsDir, "1.yaml")); err != nil {
		t.Errorf("expected snapshot 1.yaml after third save: %v", err)
	}

	recs := readAudit(t, root)
	if len(recs) != 3 {
		t.Fatalf("audit records = %d, want 3", len(recs))
	}
	if recs[0].OldRevision != 0 || recs[0].NewRevision != 0 || recs[0].Actor != "a1" || recs[0].Action != "init" {
		t.Errorf("audit[0] = %+v, unexpected", recs[0])
	}
	if recs[1].OldRevision != 0 || recs[1].NewRevision != 1 || recs[1].Actor != "a2" {
		t.Errorf("audit[1] = %+v, unexpected", recs[1])
	}
	if recs[2].OldRevision != 1 || recs[2].NewRevision != 2 || recs[2].Actor != "a3" {
		t.Errorf("audit[2] = %+v, unexpected", recs[2])
	}
	if !recs[0].Ts.Equal(now) {
		t.Errorf("audit[0].Ts = %v, want %v", recs[0].Ts, now)
	}
}

func TestSaveDefaultActor(t *testing.T) {
	root := t.TempDir()
	cfg := Defaults()
	if err := Save(root, &cfg, SaveOptions{Now: time.Now().UTC(), Action: "init"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	recs := readAudit(t, root)
	if len(recs) != 1 || recs[0].Actor != DefaultActor {
		t.Fatalf("expected one audit line with Actor=%q, got %+v", DefaultActor, recs)
	}
}

func TestUpdateRejectsStaleRevision(t *testing.T) {
	cfg := Defaults()
	pinned := PurposePolicy{Model: "cheaper"}
	if err := Update(&cfg, Patch{Implementation: &pinned}, 5); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected ErrRevisionConflict, got %v", err)
	}
}

func TestUpdateReplacesWholeSubConfig(t *testing.T) {
	cfg := Defaults()
	caps := DeclaredCapabilities{Fork: true}
	if err := Update(&cfg, Patch{Capabilities: &caps}, 0); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if cfg.Capabilities != caps {
		t.Errorf("expected a wholesale replacement, got %+v", cfg.Capabilities)
	}
	if cfg.Revision != 1 {
		t.Errorf("expected revision bumped to 1, got %d", cfg.Revision)
	}
}

func TestResetRestoresDefaults(t *testing.T) {
	cfg := Defaults()
	pinned := PurposePolicy{Model: "pinned"}
	if err := Update(&cfg, Patch{Orchestrator: &pinned}, 0); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := Reset(&cfg, 1); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	want := Defaults()
	if cfg.Agents != want.Agents || cfg.Capabilities != want.Capabilities {
		t.Errorf("Reset did not restore defaults: %+v", cfg)
	}
	if cfg.Revision != 2 {
		t.Errorf("expected revision bumped to 2 after reset, got %d", cfg.Revision)
	}
}

func TestPurposePolicyRejectsUnknownPurpose(t *testing.T) {
	cfg := Defaults()
	if _, err := cfg.PurposePolicy("gareng"); err == nil {
		t.Fatal("expected an error for an unrecognized purpose")
	}
}

func TestPurposePolicyReturnsEachPurpose(t *testing.T) {
	cfg := Defaults()
	got, err := cfg.PurposePolicy(PurposeOrchestrate)
	if err != nil || got != cfg.Agents.Orchestrator {
		t.Errorf("PurposePolicy(orchestrate) = %+v, %v; want %+v, nil", got, err, cfg.Agents.Orchestrator)
	}
	got, err = cfg.PurposePolicy(PurposeImplement)
	if err != nil || got != cfg.Agents.Implementation {
		t.Errorf("PurposePolicy(implement) = %+v, %v; want %+v, nil", got, err, cfg.Agents.Implementation)
	}
	got, err = cfg.PurposePolicy(PurposeReview)
	if err != nil || got != cfg.Agents.Review {
		t.Errorf("PurposePolicy(review) = %+v, %v; want %+v, nil", got, err, cfg.Agents.Review)
	}
}

func TestEffectiveOnlyNarrows(t *testing.T) {
	base := PurposePolicy{Model: "inherit", Reasoning: "high", Strategy: "fork", Type: "general-purpose", Isolated: false}

	if got := Effective(base, nil); got != base {
		t.Errorf("nil restriction changed the policy: got %+v, want %+v", got, base)
	}

	higher := "high"
	if got := Effective(base, &Restriction{Reasoning: &higher}); got.Reasoning != "high" {
		t.Errorf("expected reasoning to stay at high, got %q", got.Reasoning)
	}

	lower := "low"
	if got := Effective(base, &Restriction{Reasoning: &lower}); got.Reasoning != "low" {
		t.Errorf("expected reasoning clamped to low, got %q", got.Reasoning)
	}

	if got := Effective(base, &Restriction{ForceIsolated: true}); !got.Isolated {
		t.Errorf("expected ForceIsolated to set Isolated=true")
	}

	model := "pinned-model"
	if got := Effective(base, &Restriction{Model: &model}); got.Model != "pinned-model" {
		t.Errorf("expected model pinned to %q, got %q", model, got.Model)
	}
}

func TestResolverEffectivePolicyAppliesRestriction(t *testing.T) {
	root := t.TempDir()
	cfg := Defaults()
	if err := Save(root, &cfg, SaveOptions{Now: time.Now().UTC(), Action: "init"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	lower := "low"
	r := Resolver{
		Load: func(projectID string) (*Config, error) { return Load(root) },
		Restrictions: func(projectID, workflowID, purpose string) (*Restriction, error) {
			if workflowID == "strict-workflow" && purpose == PurposeImplement {
				return &Restriction{Reasoning: &lower}, nil
			}
			return nil, nil
		},
	}

	eff, err := r.EffectivePolicy("proj-1", "strict-workflow", PurposeImplement)
	if err != nil {
		t.Fatalf("EffectivePolicy: %v", err)
	}
	if eff.Reasoning != "low" {
		t.Errorf("expected reasoning clamped to low by the workflow restriction, got %q", eff.Reasoning)
	}

	unrestricted, err := r.EffectivePolicy("proj-1", "", PurposeImplement)
	if err != nil {
		t.Fatalf("EffectivePolicy: %v", err)
	}
	if unrestricted.Reasoning != "medium" {
		t.Errorf("expected the project's own medium reasoning with no workflow, got %q", unrestricted.Reasoning)
	}
}

func readAudit(t *testing.T, root string) []auditRecord {
	t.Helper()
	path := filepath.Join(root, dirName, subDir, auditFile)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open audit: %v", err)
	}
	defer f.Close()
	var recs []auditRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec auditRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("parse audit: %v", err)
		}
		recs = append(recs, rec)
	}
	return recs
}
