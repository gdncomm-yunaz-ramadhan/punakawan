package roleconfig

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
	"gopkg.in/yaml.v3"
)

// expectedStyleMode mirrors the plan §7 recommended posture per role. It is
// duplicated here (not read from the source table) so the test independently
// pins the documented defaults.
var expectedStyleMode = map[Role]struct {
	style protocol.RoleConfigStyle
	mode  protocol.RoleConfigMode
}{
	Semar:  {protocol.RoleConfigStyleBalanced, protocol.RoleConfigModeExecute},
	Gareng: {protocol.RoleConfigStyleStrict, protocol.RoleConfigModePropose},
	Petruk: {protocol.RoleConfigStyleCreative, protocol.RoleConfigModeExecute},
	Bagong: {protocol.RoleConfigStyleStrict, protocol.RoleConfigModePropose},
}

func TestLoadAbsentReturnsDefaults(t *testing.T) {
	root := t.TempDir()

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != SupportedVersion {
		t.Errorf("Version = %q, want %q", cfg.Version, SupportedVersion)
	}
	if cfg.Revision != 0 {
		t.Errorf("Revision = %d, want 0", cfg.Revision)
	}

	for _, role := range AllRoles {
		rc, err := RoleOf(cfg, role)
		if err != nil {
			t.Fatalf("RoleOf(%s): %v", role, err)
		}
		if !rc.Enabled {
			t.Errorf("%s.Enabled = false, want true", role)
		}
		want := expectedStyleMode[role]
		if rc.Style != want.style {
			t.Errorf("%s.Style = %q, want %q", role, rc.Style, want.style)
		}
		if rc.Mode != want.mode {
			t.Errorf("%s.Mode = %q, want %q", role, rc.Mode, want.mode)
		}
		owned := OwnedCapabilities(role)
		if len(rc.Capabilities) != len(owned) {
			t.Errorf("%s has %d capabilities, want %d (%v)", role, len(rc.Capabilities), len(owned), owned)
		}
		for _, key := range owned {
			on, ok := rc.Capabilities[key]
			if !ok {
				t.Errorf("%s missing owned capability %q", role, key)
				continue
			}
			if !on {
				t.Errorf("%s.Capabilities[%q] = false, want true (all owned default on)", role, key)
			}
		}
	}
}

func TestSaveLoadRoundTripSetsVersion(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)

	cfg := Defaults()
	cfg.Version = "" // Save must (re)set it to SupportedVersion.

	// Mutate: flip a Petruk capability off and change its style.
	newStyle := protocol.RoleConfigStyleStrict
	if err := Update(&cfg, Petruk, Patch{
		Style:        &newStyle,
		Capabilities: map[string]bool{"modify_files": false},
	}, 0); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := Save(root, &cfg, SaveOptions{Now: now, Actor: "tester", Action: "update", Role: string(Petruk)}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if cfg.Version != SupportedVersion {
		t.Errorf("after Save, Version = %q, want %q", cfg.Version, SupportedVersion)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != SupportedVersion {
		t.Errorf("loaded Version = %q, want %q", got.Version, SupportedVersion)
	}
	if got.Revision != 1 {
		t.Errorf("loaded Revision = %d, want 1", got.Revision)
	}
	if got.Roles.Petruk.Style != protocol.RoleConfigStyleStrict {
		t.Errorf("loaded Petruk.Style = %q, want strict", got.Roles.Petruk.Style)
	}
	if got.Roles.Petruk.Capabilities["modify_files"] {
		t.Errorf("loaded Petruk.modify_files = true, want false")
	}
	// A non-touched capability of the same role stays on.
	if !got.Roles.Petruk.Capabilities["plans"] {
		t.Errorf("loaded Petruk.plans = false, want true (untouched)")
	}
}

func TestOptimisticLockingUpdate(t *testing.T) {
	cfg := Defaults()
	cfg.Revision = 3

	before := cfg.Roles.Semar
	enabled := false
	// Stale base -> conflict, no mutation.
	err := Update(&cfg, Semar, Patch{Enabled: &enabled}, 2)
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
	if err := Update(&cfg, Semar, Patch{Enabled: &enabled}, 3); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if cfg.Revision != 4 {
		t.Errorf("Revision = %d, want 4", cfg.Revision)
	}
	if cfg.Roles.Semar.Enabled {
		t.Errorf("Semar.Enabled = true, want false")
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

	bad := protocol.RoleConfigStyle("chaotic")
	err := Update(&cfg, Semar, Patch{Style: &bad}, 0)
	if !errors.Is(err, ErrInvalidStyle) {
		t.Fatalf("invalid style err = %v, want ErrInvalidStyle", err)
	}
	if cfg.Revision != 0 {
		t.Errorf("Revision bumped on invalid style: %d", cfg.Revision)
	}

	for _, s := range []protocol.RoleConfigStyle{
		protocol.RoleConfigStyleStrict,
		protocol.RoleConfigStyleBalanced,
		protocol.RoleConfigStyleCreative,
	} {
		c := Defaults()
		if err := Update(&c, Semar, Patch{Style: &s}, 0); err != nil {
			t.Errorf("valid style %q rejected: %v", s, err)
		}
	}
}

func TestUpdateModeValidation(t *testing.T) {
	cfg := Defaults()

	bad := protocol.RoleConfigMode("god")
	err := Update(&cfg, Semar, Patch{Mode: &bad}, 0)
	if !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("invalid mode err = %v, want ErrInvalidMode", err)
	}
	if cfg.Revision != 0 {
		t.Errorf("Revision bumped on invalid mode: %d", cfg.Revision)
	}

	for _, m := range []protocol.RoleConfigMode{
		protocol.RoleConfigModeAssist,
		protocol.RoleConfigModePropose,
		protocol.RoleConfigModeExecute,
	} {
		c := Defaults()
		if err := Update(&c, Semar, Patch{Mode: &m}, 0); err != nil {
			t.Errorf("valid mode %q rejected: %v", m, err)
		}
	}
}

func TestUpdateCapabilityOwnership(t *testing.T) {
	cfg := Defaults()
	before := cfg.Roles.Gareng

	// modify_files is Petruk's capability, not Gareng's.
	err := Update(&cfg, Gareng, Patch{Capabilities: map[string]bool{"modify_files": true}}, 0)
	if !errors.Is(err, ErrUnownedCapability) {
		t.Fatalf("unowned capability err = %v, want ErrUnownedCapability", err)
	}
	if cfg.Revision != 0 {
		t.Errorf("Revision bumped on unowned capability: %d", cfg.Revision)
	}
	if !reflect.DeepEqual(cfg.Roles.Gareng, before) {
		t.Errorf("Gareng mutated on unowned capability")
	}

	// A genuinely owned capability merges; siblings untouched.
	if err := Update(&cfg, Gareng, Patch{Capabilities: map[string]bool{"contradictions": false}}, 0); err != nil {
		t.Fatalf("Update owned capability: %v", err)
	}
	if cfg.Roles.Gareng.Capabilities["contradictions"] {
		t.Errorf("contradictions = true, want false")
	}
	if !cfg.Roles.Gareng.Capabilities["security_checks"] {
		t.Errorf("security_checks = false, want true (untouched merge)")
	}
	if cfg.Revision != 1 {
		t.Errorf("Revision = %d, want 1", cfg.Revision)
	}
}

func TestUnknownRole(t *testing.T) {
	cfg := Defaults()

	if _, err := RoleOf(&cfg, Role("togog")); !errors.Is(err, ErrUnknownRole) {
		t.Errorf("RoleOf unknown err = %v, want ErrUnknownRole", err)
	}
	enabled := false
	if err := Update(&cfg, Role("togog"), Patch{Enabled: &enabled}, 0); !errors.Is(err, ErrUnknownRole) {
		t.Errorf("Update unknown err = %v, want ErrUnknownRole", err)
	}
	if err := Reset(&cfg, Role("togog"), 0); !errors.Is(err, ErrUnknownRole) {
		t.Errorf("Reset unknown err = %v, want ErrUnknownRole", err)
	}
}

func TestResetRestoresDefaults(t *testing.T) {
	cfg := Defaults()

	// Change Petruk away from its defaults.
	newStyle := protocol.RoleConfigStyleStrict
	newMode := protocol.RoleConfigModeAssist
	if err := Update(&cfg, Petruk, Patch{
		Style:        &newStyle,
		Mode:         &newMode,
		Capabilities: map[string]bool{"modify_files": false, "plans": false},
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
	enabled := false
	if err := Update(cfg2, Semar, Patch{Enabled: &enabled}, 0); err != nil {
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
	style := protocol.RoleConfigStyleCreative
	if err := Update(cfg3, Semar, Patch{Style: &style}, 1); err != nil {
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
	cfg.Roles.Bagong = protocol.RoleConfig{}
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

func TestEnabledToggleRoundTrips(t *testing.T) {
	root := t.TempDir()

	cfg := Defaults()
	off := false
	if err := Update(&cfg, Bagong, Patch{Enabled: &off}, 0); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := Save(root, &cfg, SaveOptions{Now: time.Now().UTC(), Action: "update", Role: string(Bagong)}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Roles.Bagong.Enabled {
		t.Errorf("Bagong.Enabled = true after round trip, want false")
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
