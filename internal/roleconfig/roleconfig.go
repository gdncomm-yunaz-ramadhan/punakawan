// Package roleconfig loads, persists, and versions a Punakawan project's
// four-role configuration (Semar coordinates, Gareng challenges, Petruk plans
// and builds, Bagong verifies), per
// punakawan-role-config-distinguished-improvements-plan.md Part I. Its
// canonical file lives at <workspaceRoot>/.punakawan/roles.yaml. The
// user-facing surface is intentionally small - enabled, style, mode, and a
// short list of role-specific capability toggles - so users never configure
// confidence matrices or numeric thresholds (§2.1).
//
// This package mirrors internal/project's stateless Load/Save-on-a-root model
// (not an *app.App field) because role config is project-scoped and is read
// from non-primary projects too. Save snapshots the prior revision immutably
// and appends an audit line, exactly like internal/project.
package roleconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
	"gopkg.in/yaml.v3"
)

const (
	// SupportedVersion is the only roles.yaml schema version understood,
	// matching protocol/roleconfig.schema.json's version const.
	SupportedVersion = "punakawan.roles/v1"

	dirName     = ".punakawan"
	configFile  = "roles.yaml"
	subDir      = "roles"
	versionsDir = "versions"
	auditFile   = "audit.jsonl"

	// DefaultActor is the audit actor recorded when a caller does not specify
	// one - panel mutations have no per-request identity on a local panel.
	DefaultActor = "panel"
)

// Role is one of the four fixed Punakawan roles. It is a string so it flows
// straight into path values, prompts, and the generated protocol structs
// without conversion churn.
type Role string

const (
	Semar  Role = "semar"
	Gareng Role = "gareng"
	Petruk Role = "petruk"
	Bagong Role = "bagong"
)

// AllRoles is the fixed set, in the plan's canonical order.
var AllRoles = []Role{Semar, Gareng, Petruk, Bagong}

// IsRole reports whether s names one of the four roles.
func IsRole(s string) bool {
	switch Role(s) {
	case Semar, Gareng, Petruk, Bagong:
		return true
	}
	return false
}

// configPath returns <root>/.punakawan/roles.yaml.
func configPath(root string) string {
	return filepath.Join(root, dirName, configFile)
}

// rolesFile is the on-disk YAML shape. It is exactly protocol.RoleConfiguration
// (whose yaml tags are load-bearing); the alias keeps the persistence type and
// the API type identical so there is no drift between what is saved and what is
// served.
type rolesFile = protocol.RoleConfiguration

// Load reads <root>/.punakawan/roles.yaml. If the file is absent it returns the
// recommended Defaults() at revision 0: a project that has never had its roles
// edited is a normal state, not an error (mirrors internal/project.Load). Any
// role sub-object missing from an on-disk file is backfilled from defaults so a
// partial or older file still yields a complete, usable configuration.
func Load(root string) (*protocol.RoleConfiguration, error) {
	path := configPath(root)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		d := Defaults()
		return &d, nil
	}
	if err != nil {
		return nil, fmt.Errorf("roleconfig: read %s: %w", path, err)
	}

	var rf rolesFile
	if err := yaml.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("roleconfig: parse %s: %w", path, err)
	}
	if rf.Version != SupportedVersion {
		return nil, fmt.Errorf("roleconfig: unsupported version %q (want %q)", rf.Version, SupportedVersion)
	}
	backfillMissingRoles(&rf)
	return &rf, nil
}

// backfillMissingRoles replaces any role sub-object left zero-valued by an
// older or hand-edited file (empty style/mode means it was never populated)
// with that role's recommended default, so Load always returns four complete
// roles.
func backfillMissingRoles(rf *protocol.RoleConfiguration) {
	d := Defaults()
	fill := func(dst *protocol.RoleConfig, def protocol.RoleConfig) {
		if dst.Style == "" || dst.Mode == "" || dst.Capabilities == nil {
			*dst = def
		}
	}
	fill(&rf.Roles.Semar, d.Roles.Semar)
	fill(&rf.Roles.Gareng, d.Roles.Gareng)
	fill(&rf.Roles.Petruk, d.Roles.Petruk)
	fill(&rf.Roles.Bagong, d.Roles.Bagong)
}

// SaveOptions carries the audit context for one Save. Now and Actor are
// injected (not read from the wall clock or an ambient identity) so tests can
// assert exact audit lines; an empty Actor defaults to DefaultActor and a zero
// Now defaults to time.Now().UTC().
type SaveOptions struct {
	Now    time.Time
	Actor  string
	Action string // "update" | "reset" (free-form; recorded verbatim)
	Role   string // role the action touched, if any
}

type auditRecord struct {
	Ts          time.Time `json:"ts"`
	Actor       string    `json:"actor"`
	Action      string    `json:"action"`
	Role        string    `json:"role,omitempty"`
	OldRevision int       `json:"old_revision"`
	NewRevision int       `json:"new_revision"`
}

// Save atomically persists cfg to <root>/.punakawan/roles.yaml. The current
// on-disk file (if any) is first snapshotted immutably to
// roles/versions/<oldRevision>.yaml and an audit line is appended to
// roles/audit.jsonl; the write itself is temp-file + rename so a crash
// mid-write can never leave a half-written roles.yaml.
func Save(root string, cfg *protocol.RoleConfiguration, opts SaveOptions) error {
	if cfg == nil {
		return fmt.Errorf("roleconfig: save nil configuration")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	actor := opts.Actor
	if actor == "" {
		actor = DefaultActor
	}
	cfg.Version = SupportedVersion

	baseDir := filepath.Join(root, dirName)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("roleconfig: mkdir %s: %w", baseDir, err)
	}
	path := configPath(root)

	oldRevision := 0
	if existing, err := os.ReadFile(path); err == nil {
		var prev rolesFile
		if uerr := yaml.Unmarshal(existing, &prev); uerr == nil {
			oldRevision = prev.Revision
		}
		if serr := snapshotVersion(root, oldRevision, existing); serr != nil {
			return serr
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("roleconfig: read %s: %w", path, err)
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("roleconfig: marshal: %w", err)
	}
	if err := atomicWrite(path, out); err != nil {
		return err
	}

	return appendAudit(root, auditRecord{
		Ts:          now,
		Actor:       actor,
		Action:      opts.Action,
		Role:        opts.Role,
		OldRevision: oldRevision,
		NewRevision: cfg.Revision,
	})
}

func snapshotVersion(root string, rev int, data []byte) error {
	dir := filepath.Join(root, dirName, subDir, versionsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("roleconfig: mkdir %s: %w", dir, err)
	}
	dst := filepath.Join(dir, fmt.Sprintf("%d.yaml", rev))
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if os.IsExist(err) {
		return nil // immutable: already snapshotted
	}
	if err != nil {
		return fmt.Errorf("roleconfig: snapshot %s: %w", dst, err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("roleconfig: write snapshot %s: %w", dst, err)
	}
	return nil
}

func appendAudit(root string, rec auditRecord) error {
	dir := filepath.Join(root, dirName, subDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("roleconfig: mkdir %s: %w", dir, err)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("roleconfig: marshal audit: %w", err)
	}
	path := filepath.Join(dir, auditFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("roleconfig: open audit %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("roleconfig: append audit: %w", err)
	}
	return nil
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".roles-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("roleconfig: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("roleconfig: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("roleconfig: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("roleconfig: rename temp over %s: %w", path, err)
	}
	return nil
}

// RoleOf returns a pointer to the sub-config for role within cfg, or an error
// if role is not one of the four. The pointer aliases cfg's field so callers
// mutate in place.
func RoleOf(cfg *protocol.RoleConfiguration, role Role) (*protocol.RoleConfig, error) {
	switch role {
	case Semar:
		return &cfg.Roles.Semar, nil
	case Gareng:
		return &cfg.Roles.Gareng, nil
	case Petruk:
		return &cfg.Roles.Petruk, nil
	case Bagong:
		return &cfg.Roles.Bagong, nil
	}
	return nil, fmt.Errorf("roleconfig: unknown role %q: %w", role, ErrUnknownRole)
}
