// Package policy loads and evaluates capability policy
// (.punakawan/policy.yaml), per punakawan-go-typescript-detailed-plan.md §15-§16.
package policy

import (
	"fmt"
	"os"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// Level is one of the four canonical policy levels (§16.3). YAML files may
// also use the informal §15.1 spellings ("allowed"/"approval"/"denied"),
// which are normalized to these on load.
type Level string

const (
	LevelDeny                 Level = "deny"
	LevelRequireApproval      Level = "require-approval"
	LevelAllow                Level = "allow"
	LevelAllowWithConstraints Level = "allow-with-constraints"
)

// UnmarshalYAML normalizes both the §15.1 example vocabulary and the §16.3
// canonical vocabulary to the same four Level values.
func (l *Level) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	switch s {
	case "allowed", "allow":
		*l = LevelAllow
	case "approval", "require-approval":
		*l = LevelRequireApproval
	case "denied", "deny":
		*l = LevelDeny
	case "allow-with-constraints":
		*l = LevelAllowWithConstraints
	default:
		return fmt.Errorf("policy: unknown level %q", s)
	}
	return nil
}

type FilesystemPolicy struct {
	Read  []string `yaml:"read,omitempty"`
	Write []string `yaml:"write,omitempty"`
	Deny  []string `yaml:"deny,omitempty"`
}

type GitPolicy struct {
	Commit             Level `yaml:"commit"`
	Push               Level `yaml:"push"`
	ForcePush          Level `yaml:"force_push"`
	DefaultBranchWrite Level `yaml:"default_branch_write"`
}

type ExternalPolicy struct {
	JiraRead        Level `yaml:"jira_read"`
	JiraWrite       Level `yaml:"jira_write"`
	ConfluenceRead  Level `yaml:"confluence_read"`
	ConfluenceWrite Level `yaml:"confluence_write"`
}

type BrowserPolicy struct {
	ControlledProfile Level  `yaml:"controlled_profile"`
	ExistingProfile   Level  `yaml:"existing_profile"`
	RecordInputs      string `yaml:"record_inputs"`
}

type ExecutionPolicy struct {
	Network        string `yaml:"network"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	MaxOutputBytes int64  `yaml:"max_output_bytes"`
}

type Capabilities struct {
	Filesystem FilesystemPolicy `yaml:"filesystem"`
	Git        GitPolicy        `yaml:"git"`
	External   ExternalPolicy   `yaml:"external"`
	Browser    BrowserPolicy    `yaml:"browser"`
	Execution  ExecutionPolicy  `yaml:"execution"`
}

// ApprovalsPolicy controls how broad one human approval is, independent of
// which operations require one (that's Capabilities' job). "run" (default)
// scopes an adapter-write approval to a single run_id, as before. "day"
// shares one approval across every run against the same adapter within a
// calendar UTC day, so resuming the same ticket/task across multiple runs
// on the same day doesn't re-prompt for an approval that was, in practice,
// already granted for the same unit of work (punokawan-cy8).
type ApprovalsPolicy struct {
	Scope string `yaml:"scope"`
}

// Policy is a workspace's loaded capability policy.
type Policy struct {
	Capabilities Capabilities    `yaml:"capabilities"`
	Approvals    ApprovalsPolicy `yaml:"approvals"`
}

// Default returns a conservative built-in policy used when no policy.yaml
// exists yet: git pushes and default-branch writes are denied, external
// writes require approval, execution is network-restricted.
func Default() *Policy {
	return &Policy{
		Approvals: ApprovalsPolicy{Scope: "run"},
		Capabilities: Capabilities{
			Git: GitPolicy{
				Commit:             LevelAllow,
				Push:               LevelRequireApproval,
				ForcePush:          LevelDeny,
				DefaultBranchWrite: LevelDeny,
			},
			External: ExternalPolicy{
				JiraRead:        LevelAllow,
				JiraWrite:       LevelRequireApproval,
				ConfluenceRead:  LevelAllow,
				ConfluenceWrite: LevelRequireApproval,
			},
			Browser: BrowserPolicy{
				ControlledProfile: LevelAllow,
				ExistingProfile:   LevelRequireApproval,
				RecordInputs:      "sanitized",
			},
			Execution: ExecutionPolicy{
				Network:        "restricted",
				TimeoutSeconds: 600,
				MaxOutputBytes: 1_000_000,
			},
		},
	}
}

// Load reads a policy.yaml file. If path does not exist, Default() is
// returned so a workspace without an explicit policy file still behaves
// safely rather than failing to start.
func Load(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("policy: read %s: %w", path, err)
	}

	p := Default()
	if err := yaml.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("policy: parse %s: %w", path, err)
	}
	return p, nil
}

// AllowsFilesystemWrite reports whether writing to relPath is permitted:
// not matched by any deny pattern, and matched by a write pattern (or no
// write patterns are configured, in which case writes default to allowed
// within the workspace unless explicitly denied).
func (p *Policy) AllowsFilesystemWrite(relPath string) (bool, error) {
	for _, pattern := range p.Capabilities.Filesystem.Deny {
		matched, err := doublestar.Match(pattern, relPath)
		if err != nil {
			return false, fmt.Errorf("policy: invalid deny pattern %q: %w", pattern, err)
		}
		if matched {
			return false, nil
		}
	}
	if len(p.Capabilities.Filesystem.Write) == 0 {
		return true, nil
	}
	for _, pattern := range p.Capabilities.Filesystem.Write {
		matched, err := doublestar.Match(pattern, relPath)
		if err != nil {
			return false, fmt.Errorf("policy: invalid write pattern %q: %w", pattern, err)
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}
