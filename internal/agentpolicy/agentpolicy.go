// Package agentpolicy loads, persists, and versions a Punakawan project's
// agent execution policy: which model, reasoning effort, dispatch strategy,
// and context isolation apply to orchestration, implementation, and review
// work, plus the project operator's own honest declaration of which of
// those knobs their connected MCP client (a Claude Code session or similar)
// can actually enforce. Its canonical file lives at
// <workspaceRoot>/.punakawan/agents.yaml.
//
// Punakawan never calls a model itself - this package only stores and
// resolves an operator's stated preferences and capability declaration, the
// same way internal/roleconfig stores per-role permission/personality
// settings without ever acting as one of the roles. It deliberately mirrors
// internal/roleconfig's Load/Save/versioned-snapshot/optimistic-locking
// mechanics file-for-file rather than inventing a second way to persist
// small, project-scoped, operator-editable YAML: both packages need the
// same guarantee that a prior revision is never lost and that a save
// carries a matching audit trail.
package agentpolicy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// SupportedVersion is the only agents.yaml schema version understood.
	SupportedVersion = "punakawan.agentpolicy/v1"

	dirName     = ".punakawan"
	configFile  = "agents.yaml"
	subDir      = "agentpolicy"
	versionsDir = "versions"
	auditFile   = "audit.jsonl"

	// DefaultActor is the audit actor recorded when a caller does not
	// specify one - panel mutations have no per-request identity on a
	// local panel.
	DefaultActor = "panel"
)

// Purpose names one of the three kinds of agent work this policy
// configures. Declared as plain strings (matching the values an
// AgentRunner's own Purpose type carries) rather than importing that type,
// so this package stays free of any dependency on internal/agentrunner -
// agentrunner depends on agentpolicy for its config lookups, not the other
// way around.
const (
	PurposeOrchestrate = "orchestrate"
	PurposeImplement   = "implement"
	PurposeReview      = "review"
)

// DeclaredCapabilities is a project operator's own statement of which
// agent-execution capabilities their connected harness actually supports.
// Punakawan has no way to detect this itself - it never invokes a model or
// spawns a subagent - so every field here is exactly what the operator
// wrote down, nothing inferred or assumed. The zero value (every field
// false) is the safe default for a project that has never declared
// anything: "unknown" must read as "not supported," not as "probably fine."
type DeclaredCapabilities struct {
	// Fork reports whether the harness can dispatch forked/parallel workers.
	Fork bool `yaml:"fork"`
	// ModelSelection reports whether the harness can honor a request to use
	// a specific model rather than whatever it defaults to.
	ModelSelection bool `yaml:"model_selection"`
	// ReasoningControl reports whether the harness can honor a requested
	// reasoning effort level.
	ReasoningControl bool `yaml:"reasoning_control"`
	// IsolatedContext reports whether the harness can run a request in a
	// context isolated from the conversation that triggered it.
	IsolatedContext bool `yaml:"isolated_context"`
}

// PurposePolicy is the model/reasoning/strategy/isolation setting for one
// purpose. Every field is a free-form label the operator assigns meaning
// to (e.g. Model "inherit" or "cheaper" are conventions this package does
// not interpret) except Isolated, which is a plain boolean since isolation
// either applies or it does not.
type PurposePolicy struct {
	// Model is which model this purpose should use, e.g. "inherit" (use
	// whatever the calling session already has) or a specific model label
	// the operator's harness understands.
	Model string `yaml:"model,omitempty"`
	// Reasoning is the requested reasoning effort, e.g. "low", "medium", or
	// "high". Free-form: only Effective's ceiling logic in resolver.go
	// assumes these three specific labels order low<medium<high, and falls
	// back safely (as if "low") for anything else.
	Reasoning string `yaml:"reasoning,omitempty"`
	// Strategy is how work for this purpose should be dispatched, e.g.
	// "fork" for a forked subagent. Empty means no particular strategy is
	// requested.
	Strategy string `yaml:"strategy,omitempty"`
	// Type is which kind of agent should handle this purpose, e.g.
	// "general-purpose". Empty means no particular type is requested.
	Type string `yaml:"type,omitempty"`
	// Isolated reports whether this purpose's work should run in a context
	// isolated from whatever triggered it.
	Isolated bool `yaml:"isolated,omitempty"`
}

// AgentsConfig holds the three purposes' policies, keyed by name to match
// the on-disk YAML shape directly (agents.orchestrator/implementation/
// review) rather than a generic map, so a hand-edited file with a typoed
// purpose name fails to parse loudly instead of silently vanishing into an
// unused map key.
type AgentsConfig struct {
	Orchestrator   PurposePolicy `yaml:"orchestrator"`
	Implementation PurposePolicy `yaml:"implementation"`
	Review         PurposePolicy `yaml:"review"`
}

// Config is a project's persisted agent execution policy.
type Config struct {
	Version      string               `yaml:"version"`
	Revision     int                  `yaml:"revision"`
	Capabilities DeclaredCapabilities `yaml:"capabilities"`
	Agents       AgentsConfig         `yaml:"agents"`
}

// PurposePolicy returns the configured policy for purpose ("orchestrate",
// "implement", or "review"), or an error for anything else - there is no
// sensible default policy to fall back to for a purpose this package does
// not recognize.
func (c *Config) PurposePolicy(purpose string) (PurposePolicy, error) {
	switch purpose {
	case PurposeOrchestrate:
		return c.Agents.Orchestrator, nil
	case PurposeImplement:
		return c.Agents.Implementation, nil
	case PurposeReview:
		return c.Agents.Review, nil
	default:
		return PurposePolicy{}, fmt.Errorf("agentpolicy: unknown purpose %q", purpose)
	}
}

// Defaults returns the recommended configuration at revision 0: every
// declared capability off (a project that has never stated what its
// harness supports gets the fail-closed answer, not an optimistic guess),
// and the per-purpose policy the plan itself gives as the recommended
// starting point.
func Defaults() Config {
	return Config{
		Version:      SupportedVersion,
		Revision:     0,
		Capabilities: DeclaredCapabilities{},
		Agents: AgentsConfig{
			Orchestrator: PurposePolicy{
				Model:     "inherit",
				Reasoning: "high",
			},
			Implementation: PurposePolicy{
				Strategy:  "fork",
				Model:     "cheaper",
				Reasoning: "medium",
			},
			Review: PurposePolicy{
				Type:      "general-purpose",
				Isolated:  true,
				Model:     "inherit",
				Reasoning: "high",
			},
		},
	}
}

func configPath(root string) string {
	return filepath.Join(root, dirName, configFile)
}

// Load reads <root>/.punakawan/agents.yaml. If the file is absent it
// returns Defaults() at revision 0: a project that has never edited its
// agent policy is a normal state, not an error (mirrors
// internal/roleconfig.Load).
func Load(root string) (*Config, error) {
	path := configPath(root)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		d := Defaults()
		return &d, nil
	}
	if err != nil {
		return nil, fmt.Errorf("agentpolicy: read %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("agentpolicy: parse %s: %w", path, err)
	}
	if cfg.Version != SupportedVersion {
		return nil, fmt.Errorf("agentpolicy: unsupported version %q (want %q)", cfg.Version, SupportedVersion)
	}
	return &cfg, nil
}

// SaveOptions carries the audit context for one Save. Now and Actor are
// injected (not read from the wall clock or an ambient identity) so tests
// can assert exact audit lines; an empty Actor defaults to DefaultActor and
// a zero Now defaults to time.Now().UTC().
type SaveOptions struct {
	Now    time.Time
	Actor  string
	Action string // "update" | "reset" (free-form; recorded verbatim)
}

type auditRecord struct {
	Ts          time.Time `json:"ts"`
	Actor       string    `json:"actor"`
	Action      string    `json:"action"`
	OldRevision int       `json:"old_revision"`
	NewRevision int       `json:"new_revision"`
}

// Save atomically persists cfg to <root>/.punakawan/agents.yaml. The
// current on-disk file (if any) is first snapshotted immutably to
// agentpolicy/versions/<oldRevision>.yaml and an audit line is appended to
// agentpolicy/audit.jsonl; the write itself is temp-file + rename so a
// crash mid-write can never leave a half-written agents.yaml.
func Save(root string, cfg *Config, opts SaveOptions) error {
	if cfg == nil {
		return fmt.Errorf("agentpolicy: save nil configuration")
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
		return fmt.Errorf("agentpolicy: mkdir %s: %w", baseDir, err)
	}
	path := configPath(root)

	oldRevision := 0
	if existing, err := os.ReadFile(path); err == nil {
		var prev Config
		if uerr := yaml.Unmarshal(existing, &prev); uerr == nil {
			oldRevision = prev.Revision
		}
		if serr := snapshotVersion(root, oldRevision, existing); serr != nil {
			return serr
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("agentpolicy: read %s: %w", path, err)
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("agentpolicy: marshal: %w", err)
	}
	if err := atomicWrite(path, out); err != nil {
		return err
	}

	return appendAudit(root, auditRecord{
		Ts:          now,
		Actor:       actor,
		Action:      opts.Action,
		OldRevision: oldRevision,
		NewRevision: cfg.Revision,
	})
}

func snapshotVersion(root string, rev int, data []byte) error {
	dir := filepath.Join(root, dirName, subDir, versionsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("agentpolicy: mkdir %s: %w", dir, err)
	}
	dst := filepath.Join(dir, fmt.Sprintf("%d.yaml", rev))
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if os.IsExist(err) {
		return nil // immutable: already snapshotted
	}
	if err != nil {
		return fmt.Errorf("agentpolicy: snapshot %s: %w", dst, err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("agentpolicy: write snapshot %s: %w", dst, err)
	}
	return nil
}

func appendAudit(root string, rec auditRecord) error {
	dir := filepath.Join(root, dirName, subDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("agentpolicy: mkdir %s: %w", dir, err)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("agentpolicy: marshal audit: %w", err)
	}
	path := filepath.Join(dir, auditFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("agentpolicy: open audit %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("agentpolicy: append audit: %w", err)
	}
	return nil
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".agents-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("agentpolicy: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("agentpolicy: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("agentpolicy: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("agentpolicy: rename temp over %s: %w", path, err)
	}
	return nil
}
