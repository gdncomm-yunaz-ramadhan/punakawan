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
// spawned adapter, e.g. secrets like ATLASSIAN_API_TOKEN - only these named
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
// the same way git locates .git. An explicit workspace.yaml is only needed
// for non-default setups (multiple repositories, relations, external/
// knowledge/tasks/policy/adapter overrides) - if none is found, Discover
// falls back to an implicit single-repository workspace rooted at the
// nearest .git, so punakawan can attach to any git-tracked project without
// per-project scaffolding. An error is returned only if neither a
// workspace.yaml nor a .git directory is found above startDir.
func Discover(startDir string) (*Workspace, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return nil, fmt.Errorf("workspace: resolve start dir: %w", err)
	}

	for d := dir; ; {
		candidate := filepath.Join(d, dirName, configFile)
		if _, err := os.Stat(candidate); err == nil {
			return Load(candidate, d)
		}

		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}

	gitRoot, err := findGitRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("workspace: no %s/%s found above %s, and no git repository found either", dirName, configFile, startDir)
	}
	return implicitWorkspace(gitRoot), nil
}

// findGitRoot walks upward from dir looking for .git (a directory for a
// normal clone, or a file for a worktree/submodule - either way, os.Stat
// succeeds).
func findGitRoot(dir string) (string, error) {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("workspace: no .git found above %s", dir)
		}
		d = parent
	}
}

// implicitWorkspace builds a zero-config Workspace for a plain git
// repository with no workspace.yaml: one repository (the git root itself,
// path "."), named after the root directory.
func implicitWorkspace(root string) *Workspace {
	id := filepath.Base(root)
	return &Workspace{
		Version: SupportedVersion,
		ID:      id,
		Name:    id,
		Repositories: []Repository{
			{ID: id, Path: ".", Roles: []string{"implementation"}},
		},
		Root: root,
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

// GlobalConfig holds user-level configuration that applies across every
// workspace on this machine - primarily adapter wiring (which adapter
// processes to spawn and which env vars to pass through to them). This
// lets adapters like Jira be set up once per machine instead of once per
// project: a project's own workspace.yaml only needs an `adapters:` entry
// when it wants to override or add to what's configured globally.
type GlobalConfig struct {
	Adapters map[string]AdapterConfig `yaml:"adapters,omitempty"`
}

// GlobalConfigPath returns the path LoadGlobalConfig reads from:
// <user config dir>/punakawan/config.yaml (e.g. ~/.config/punakawan on
// Linux, ~/Library/Application Support/punakawan on macOS, following
// os.UserConfigDir's platform conventions).
func GlobalConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("workspace: resolve user config dir: %w", err)
	}
	return filepath.Join(dir, "punakawan", "config.yaml"), nil
}

// LoadGlobalConfig reads the user-level config. A missing file is not an
// error - it returns an empty GlobalConfig, since global config is
// optional and every workspace must still function without one.
func LoadGlobalConfig() (*GlobalConfig, error) {
	path, err := GlobalConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadGlobalConfigFrom(path)
}

// LoadGlobalConfigFrom reads the user-level config from an explicit path,
// split out from LoadGlobalConfig so tests can exercise the parsing and
// missing-file-defaulting logic without touching this machine's real
// os.UserConfigDir().
func LoadGlobalConfigFrom(path string) (*GlobalConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &GlobalConfig{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: read %s: %w", path, err)
	}

	var cfg GlobalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("workspace: parse %s: %w", path, err)
	}
	return &cfg, nil
}

// MergeAdapters returns adapter specs with global as the base and the
// workspace's own Adapters overriding or adding entries by id - a project
// only needs to declare an adapter when it wants something different from
// (or in addition to) what is configured globally.
func (w *Workspace) MergeAdapters(global *GlobalConfig) map[string]AdapterConfig {
	merged := make(map[string]AdapterConfig, len(global.Adapters)+len(w.Adapters))
	for id, cfg := range global.Adapters {
		merged[id] = cfg
	}
	for id, cfg := range w.Adapters {
		merged[id] = cfg
	}
	return merged
}
