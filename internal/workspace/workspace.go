// Package workspace loads and discovers Punakawan workspace configuration
// (.punakawan/workspace.yaml), per punakawan-go-typescript-detailed-plan.md §6.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	// SupportedVersion is the only workspace.yaml schema version understood.
	SupportedVersion = "punakawan.workspace/v1"

	dirName    = ".punakawan"
	configFile = "workspace.yaml"
)

// Repository is a single repository declared in the workspace.
type Repository struct {
	ID    string   `yaml:"id"`
	Path  string   `yaml:"path"`
	Roles []string `yaml:"roles,omitempty"`
}

// Relation links two declared repositories (e.g. "tests", "deploys").
type Relation struct {
	From string `yaml:"from"`
	Type string `yaml:"type"`
	To   string `yaml:"to"`
}

type JiraConfig struct {
	Project string `yaml:"project"`
}

type ConfluenceConfig struct {
	Spaces []string `yaml:"spaces,omitempty"`
}

type External struct {
	Jira       *JiraConfig       `yaml:"jira,omitempty"`
	Confluence *ConfluenceConfig `yaml:"confluence,omitempty"`
}

type KnowledgeConfig struct {
	Store    string `yaml:"store,omitempty"`
	Database string `yaml:"database,omitempty"`
}

type TasksConfig struct {
	Provider string `yaml:"provider,omitempty"`
	Path     string `yaml:"path,omitempty"`
}

type PolicyConfig struct {
	File string `yaml:"file,omitempty"`
}

// AdapterConfig declares how to spawn one adapter process (§5.1's stdio
// JSON-RPC transport). EnvPassthrough names additional environment
// variables (beyond the process's default allowlist) to copy into the
// spawned adapter, e.g. secrets like ATLASSIAN_MCP_TOKEN - only these named
// variables are copied, not the full parent environment, per §11.4/§15.2's
// secret-lease philosophy.
type AdapterConfig struct {
	Command        string   `yaml:"command"`
	Args           []string `yaml:"args,omitempty"`
	EnvPassthrough []string `yaml:"env_passthrough,omitempty"`
}

// Workspace is the parsed contents of workspace.yaml plus its resolved root.
type Workspace struct {
	Version      string                   `yaml:"version"`
	ID           string                   `yaml:"id"`
	Name         string                   `yaml:"name"`
	Repositories []Repository             `yaml:"repositories"`
	Relations    []Relation               `yaml:"relations,omitempty"`
	External     External                 `yaml:"external,omitempty"`
	Knowledge    KnowledgeConfig          `yaml:"knowledge,omitempty"`
	Tasks        TasksConfig              `yaml:"tasks,omitempty"`
	Policy       PolicyConfig             `yaml:"policy,omitempty"`
	Adapters     map[string]AdapterConfig `yaml:"adapters,omitempty"`

	// Root is the directory containing .punakawan/. Not part of the YAML.
	Root string `yaml:"-"`
}

// Discover walks upward from startDir looking for .punakawan/workspace.yaml,
// the same way git locates .git. It returns an error if none is found before
// reaching the filesystem root.
func Discover(startDir string) (*Workspace, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return nil, fmt.Errorf("workspace: resolve start dir: %w", err)
	}

	for {
		candidate := filepath.Join(dir, dirName, configFile)
		if _, err := os.Stat(candidate); err == nil {
			return Load(candidate, dir)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("workspace: no %s/%s found above %s", dirName, configFile, startDir)
		}
		dir = parent
	}
}

// Load reads and validates a workspace.yaml file at path, with root as the
// workspace's base directory (the directory containing .punakawan/).
func Load(path, root string) (*Workspace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("workspace: read %s: %w", path, err)
	}

	var ws Workspace
	if err := yaml.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("workspace: parse %s: %w", path, err)
	}
	ws.Root = root

	if ws.Version != SupportedVersion {
		return nil, fmt.Errorf("workspace: unsupported version %q (want %q)", ws.Version, SupportedVersion)
	}
	if ws.ID == "" {
		return nil, fmt.Errorf("workspace: missing id")
	}
	if len(ws.Repositories) == 0 {
		return nil, fmt.Errorf("workspace: no repositories declared")
	}
	seen := make(map[string]bool, len(ws.Repositories))
	for _, r := range ws.Repositories {
		if r.ID == "" {
			return nil, fmt.Errorf("workspace: repository with empty id")
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("workspace: duplicate repository id %q", r.ID)
		}
		seen[r.ID] = true
	}

	return &ws, nil
}

// RepositoryPath returns the absolute path to a declared repository by id.
func (w *Workspace) RepositoryPath(id string) (string, error) {
	for _, r := range w.Repositories {
		if r.ID == id {
			return filepath.Join(w.Root, r.Path), nil
		}
	}
	return "", fmt.Errorf("workspace: unknown repository %q", id)
}

// PolicyPath returns the absolute path to the workspace's policy file,
// defaulting to .punakawan/policy.yaml if not explicitly configured.
func (w *Workspace) PolicyPath() string {
	if w.Policy.File != "" {
		return filepath.Join(w.Root, w.Policy.File)
	}
	return filepath.Join(w.Root, dirName, "policy.yaml")
}

// JiraWorkflowPath returns the absolute path to the workspace's Jira
// workflow config file (.punakawan/jira-workflow.yaml). Unlike PolicyPath,
// this has no override field: jiraworkflow.Load already treats a missing
// file as a safe empty default, so there is no need for this location
// itself to be configurable too.
func (w *Workspace) JiraWorkflowPath() string {
	return filepath.Join(w.Root, dirName, "jira-workflow.yaml")
}
