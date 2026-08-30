package roleconfig

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
	"gopkg.in/yaml.v3"
)

func TestLoadAbsentReturnsDefaults(t *testing.T) {
	root := t.TempDir()

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != SupportedVersion {
		t.Errorf("Version = %d, want %d", cfg.Version, SupportedVersion)
	}
	if cfg.Revision != 0 {
		t.Errorf("Revision = %d, want 0", cfg.Revision)
	}

	for _, role := range AllRoles {
		rp, err := RoleOf(cfg, role)
		if err != nil {
			t.Fatalf("RoleOf(%s): %v", role, err)
		}
		if rp.Style != defaultStyle {
			t.Errorf("%s.Style = %q, want %q", role, rp.Style, defaultStyle)
		}
		if rp.Instructions != "" {
			t.Errorf("%s.Instructions = %q, want empty", role, rp.Instructions)
		}
	}
}

func TestSaveLoadRoundTripSetsVersion(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)

	cfg := Defaults()
	cfg.Version = 0 // Save must (re)set it to SupportedVersion.

	newStyle := protocol.RolePreferenceStyleStrict
	newInstructions := "Prefer reversible migrations."
	if err := Update(&cfg, Petruk, Patch{
		Style:        &newStyle,
		Instructions: &newInstructions,
	}, 0); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := Save(root, &cfg, SaveOptions{Now: now, Actor: "tester", Action: "update", Role: string(Petruk)}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if cfg.Version != SupportedVersion {
		t.Errorf("after Save, Version = %d, want %d", cfg.Version, SupportedVersion)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != SupportedVersion {
		t.Errorf("loaded Version = %d, want %d", got.Version, SupportedVersion)
	}
	if got.Revision != 1 {
		t.Errorf("loaded Revision = %d, want 1", got.Revision)
	}
	if got.Roles.Petruk.Style != protocol.RolePreferenceStyleStrict {
		t.Errorf("loaded Petruk.Style = %q, want strict", got.Roles.Petruk.Style)
	}
	if got.Roles.Petruk.Instructions != newInstructions {
		t.Errorf("loaded Petruk.Instructions = %q, want %q", got.Roles.Petruk.Instructions, newInstructions)
	}
	// A non-touched role's preference stays at the default.
	if got.Roles.Semar.Style != defaultStyle {
		t.Errorf("loaded Semar.Style = %q, want unchanged default %q", got.Roles.Semar.Style, defaultStyle)
	}
}

func TestOptimisticLockingUpdate(t *testing.T) {
	cfg := Defaults()
	cfg.Revision = 3

	before := cfg.Roles.Semar
	style := protocol.RolePreferenceStyleStrict
	// Stale base -> conflict, no mutation.
	err := Update(&cfg, Semar, Patch{Style: &style}, 2)
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale Update err = %v, want ErrRevisionConflict", err)
	}
	if cfg.Revision != 3 {
		t.Errorf("Revision = %d after conflict, want 3", cfg.Revision)
	}
	if !reflect.DeepEqual(cfg.Roles.Semar, before) {
		t.Errorf("Semar mutated on conflict: %+v != %+v", cfg.Roles.Semar, before)
	}

	// Correct base -> bumps revision by exactly 1.
	if err := Update(&cfg, Semar, Patch{Style: &style}, 3); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if cfg.Revision != 4 {
		t.Errorf("Revision = %d, want 4", cfg.Revision)
	}
	if cfg.Roles.Semar.Style != protocol.RolePreferenceStyleStrict {
		t.Errorf("Semar.Style = %q, want strict", cfg.Roles.Semar.Style)
	}
}

func TestOptimisticLockingReset(t *testing.T) {
	cfg := Defaults()
	cfg.Revision = 5

	before := cfg.Roles.Gareng
	if err := Reset(&cfg, Gareng, 4); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale Reset err = %v, want ErrRevisionConflict", err)
	}
	if cfg.Revision != 5 {
		t.Errorf("Revision = %d after conflict, want 5", cfg.Revision)
	}
	if !reflect.DeepEqual(cfg.Roles.Gareng, before) {
		t.Errorf("Gareng mutated on conflict")
	}

	if err := Reset(&cfg, Gareng, 5); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if cfg.Revision != 6 {
		t.Errorf("Revision = %d, want 6", cfg.Revision)
	}
}

func TestUpdateStyleValidation(t *testing.T) {
	cfg := Defaults()

	bad := protocol.RolePreferenceStyle("chaotic")
	err := Update(&cfg, Semar, Patch{Style: &bad}, 0)
	if !errors.Is(err, ErrInvalidStyle) {
		t.Fatalf("invalid style err = %v, want ErrInvalidStyle", err)
	}
	if cfg.Revision != 0 {
		t.Errorf("Revision bumped on invalid style: %d", cfg.Revision)
	}

	for _, s := range []protocol.RolePreferenceStyle{
		protocol.RolePreferenceStyleStrict,
		protocol.RolePreferenceStyleBalanced,
		protocol.RolePreferenceStyleCreative,
	} {
		c := Defaults()
		if err := Update(&c, Semar, Patch{Style: &s}, 0); err != nil {
			t.Errorf("valid style %q rejected: %v", s, err)
		}
	}
}

func TestUpdateInstructionsBound(t *testing.T) {
	cfg := Defaults()

	tooLong := strings.Repeat("a", maxInstructionsLen+1)
	err := Update(&cfg, Semar, Patch{Instructions: &tooLong}, 0)
	if !errors.Is(err, ErrInstructionsTooLong) {
		t.Fatalf("over-bound instructions err = %v, want ErrInstructionsTooLong", err)
	}
	if cfg.Revision != 0 {
		t.Errorf("Revision bumped on over-bound instructions: %d", cfg.Revision)
	}

	exact := strings.Repeat("a", maxInstructionsLen)
	if err := Update(&cfg, Semar, Patch{Instructions: &exact}, 0); err != nil {
		t.Errorf("exactly-bound instructions rejected: %v", err)
	}
}

func TestUnknownRole(t *testing.T) {
	cfg := Defaults()

	if _, err := RoleOf(&cfg, Role("togog")); !errors.Is(err, ErrUnknownRole) {
		t.Errorf("RoleOf unknown err = %v, want ErrUnknownRole", err)
	}
	style := protocol.RolePreferenceStyleStrict
	if err := Update(&cfg, Role("togog"), Patch{Style: &style}, 0); !errors.Is(err, ErrUnknownRole) {
		t.Errorf("Update unknown err = %v, want ErrUnknownRole", err)
	}
	if err := Reset(&cfg, Role("togog"), 0); !errors.Is(err, ErrUnknownRole) {
		t.Errorf("Reset unknown err = %v, want ErrUnknownRole", err)
	}
}

func TestResetRestoresDefaults(t *testing.T) {
	cfg := Defaults()

	// Change Petruk away from its defaults.
	newStyle := protocol.RolePreferenceStyleStrict
	newInstructions := "Always add a migration rollback."
	if err := Update(&cfg, Petruk, Patch{
		Style:        &newStyle,
		Instructions: &newInstructions,
	}, 0); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if cfg.Revision != 1 {
		t.Fatalf("Revision = %d, want 1", cfg.Revision)
	}

	if err := Reset(&cfg, Petruk, 1); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if cfg.Revision != 2 {
		t.Errorf("Revision = %d after Reset, want 2", cfg.Revision)
	}
	if !reflect.DeepEqual(cfg.Roles.Petruk, defaultRole(Petruk)) {
		t.Errorf("Petruk after Reset = %+v, want %+v", cfg.Roles.Petruk, defaultRole(Petruk))
	}
}

func TestSnapshotImmutabilityAndAudit(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

	// Save #1: persist rev 0. No pre-existing file, so nothing is snapshotted.
	cfg := Defaults()
	if err := Save(root, &cfg, SaveOptions{Now: now, Actor: "a1", Action: "init", Role: ""}); err != nil {
		t.Fatalf("Save #1: %v", err)
	}

	// Save #2: mutate to rev 1. The on-disk rev-0 file is snapshotted to 0.yaml.
	cfg2, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	style := protocol.RolePreferenceStyleStrict
	if err := Update(cfg2, Semar, Patch{Style: &style}, 0); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := Save(root, cfg2, SaveOptions{Now: now, Actor: "a2", Action: "update", Role: string(Semar)}); err != nil {
		t.Fatalf("Save #2: %v", err)
	}

	snap0 := filepath.Join(root, dirName, subDir, versionsDir, "0.yaml")
	before, err := os.ReadFile(snap0)
	if err != nil {
		t.Fatalf("expected snapshot 0.yaml after two saves: %v", err)
	}

	// Save #3: mutate to rev 2. Snapshots rev-1 to 1.yaml; 0.yaml stays intact.
	cfg3, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	creative := protocol.RolePreferenceStyleCreative
	if err := Update(cfg3, Semar, Patch{Style: &creative}, 1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := Save(root, cfg3, SaveOptions{Now: now, Actor: "a3", Action: "update", Role: string(Semar)}); err != nil {
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

	// Audit: one line per save with the expected old/new revisions and fields.
	recs := readAudit(t, root)
	if len(recs) != 3 {
		t.Fatalf("audit records = %d, want 3", len(recs))
	}
	if recs[0].OldRevision != 0 || recs[0].NewRevision != 0 || recs[0].Actor != "a1" || recs[0].Action != "init" {
		t.Errorf("audit[0] = %+v, unexpected", recs[0])
	}
	if recs[1].OldRevision != 0 || recs[1].NewRevision != 1 || recs[1].Actor != "a2" || recs[1].Action != "update" || recs[1].Role != string(Semar) {
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

func TestBackfillMissingRoles(t *testing.T) {
	root := t.TempDir()

	// Build a valid-version file whose Bagong sub-object is zero-valued, as an
	// older or hand-edited file would leave it.
	cfg := Defaults()
	cfg.Roles.Bagong = protocol.RolePreference{}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, dirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath(root), data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got.Roles.Bagong, defaultRole(Bagong)) {
		t.Errorf("Bagong not backfilled: got %+v, want %+v", got.Roles.Bagong, defaultRole(Bagong))
	}
	// A populated role is left as-is.
	if !reflect.DeepEqual(got.Roles.Semar, defaultRole(Semar)) {
		t.Errorf("Semar changed unexpectedly: %+v", got.Roles.Semar)
	}
}

// TestLegacyVersionMigrationPreservesStyleOnly proves that reading a
// pre-version-2 roles.yaml (the previous punakawan.roles/v1 shape, complete
// with enabled/mode/capabilities that never enforced any real behavior)
// preserves each role's style and discards everything else, and that the
// next Save persists the migrated file at SupportedVersion.
func TestLegacyVersionMigrationPreservesStyleOnly(t *testing.T) {
	root := t.TempDir()
	legacy := `version: punakawan.roles/v1
revision: 4
roles:
  semar:
    enabled: true
    style: strict
    mode: execute
    capabilities:
      workflows: true
  gareng:
    enabled: false
    style: creative
    mode: assist
    capabilities: {}
  petruk:
    enabled: true
    style: balanced
    mode: execute
    capabilities: {}
  bagong:
    enabled: true
    style: strict
    mode: propose
    capabilities: {}
`
	if err := os.MkdirAll(filepath.Join(root, dirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath(root), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load legacy: %v", err)
	}
	if got.Roles.Semar.Style != protocol.RolePreferenceStyleStrict {
		t.Errorf("Semar.Style = %q, want strict (preserved from legacy)", got.Roles.Semar.Style)
	}
	if got.Roles.Gareng.Style != protocol.RolePreferenceStyleCreative {
		t.Errorf("Gareng.Style = %q, want creative (preserved from legacy)", got.Roles.Gareng.Style)
	}
	if got.Roles.Semar.Instructions != "" {
		t.Errorf("Semar.Instructions = %q, want empty (never persisted pre-migration)", got.Roles.Semar.Instructions)
	}

	// The next Save persists the migrated file at SupportedVersion, and it no
	// longer carries enabled/mode/capabilities at all.
	if err := Save(root, got, SaveOptions{Now: time.Now().UTC(), Action: "migrate"}); err != nil {
		t.Fatalf("Save after migration: %v", err)
	}
	raw, err := os.ReadFile(configPath(root))
	if err != nil {
		t.Fatalf("read migrated file: %v", err)
	}
	if strings.Contains(string(raw), "capabilities") || strings.Contains(string(raw), "mode:") || strings.Contains(string(raw), "enabled:") {
		t.Errorf("migrated file still carries a never-enforced field:\n%s", raw)
	}
	reloaded, err := Load(root)
	if err != nil {
		t.Fatalf("reload after migration: %v", err)
	}
	if reloaded.Version != SupportedVersion {
		t.Errorf("reloaded Version = %d, want %d", reloaded.Version, SupportedVersion)
	}
}

func TestIsRole(t *testing.T) {
	for _, r := range AllRoles {
		if !IsRole(string(r)) {
			t.Errorf("IsRole(%q) = false, want true", r)
		}
	}
	if IsRole("togog") {
		t.Errorf("IsRole(togog) = true, want false")
	}
}

// mustResolve resolves cfg's role preference by name and renders it through
// PromptGuidance, failing the test on an unknown role.
func mustResolve(t *testing.T, cfg Preferences, role string) string {
	t.Helper()
	rp, err := roleIn(&cfg, Role(role))
	if err != nil {
		t.Fatalf("mustResolve(%s): %v", role, err)
	}
	return PromptGuidance(Role(role), *rp)
}

// TestRolePreferenceProducesConcretePromptGuidance proves a role's resolved
// prompt carries the fixed style guidance plus the free-text instruction, and
// never mentions permission or approval - prompt preferences shape wording
// only, they do not gate or authorize anything.
func TestRolePreferenceProducesConcretePromptGuidance(t *testing.T) {
	cfg := Preferences{Semar: RolePreference{Style: "strict", Instructions: "Prefer reversible migrations."}}
	prompt := mustResolve(t, cfg, "semar")
	if !strings.Contains(prompt, "Verify every required input") {
		t.Errorf("prompt missing strict style guidance:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Prefer reversible migrations.") {
		t.Errorf("prompt missing free-text instructions:\n%s", prompt)
	}
	if strings.Contains(prompt, "permission") {
		t.Errorf("prompt must never mention permission:\n%s", prompt)
	}
	if strings.Contains(prompt, "approval") {
		t.Errorf("prompt must never mention approval:\n%s", prompt)
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
