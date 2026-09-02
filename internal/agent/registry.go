package agent

import (
	"fmt"
	"io/fs"

	"gopkg.in/yaml.v3"

	"github.com/ygrip/punakawan/prompts"
)

// AgentRegistry is the read side of Punakawan's role specifications.
// Implementations are expected to load once at construction (the
// embedded manifest set never changes at runtime) and serve List/Get from
// memory thereafter.
type AgentRegistry interface {
	// List returns every loaded RoleSpec, in no particular guaranteed
	// order.
	List() []RoleSpec
	// Get returns the RoleSpec for id, or an error if id names no loaded
	// role.
	Get(id string) (RoleSpec, error)
	// Reload re-reads every manifest from its original source. For the
	// embedded-FS-backed implementation this is mostly symmetry with the
	// AgentRegistry interface (an embed.FS is baked into the binary at
	// build time and cannot change while the process runs) rather than a
	// live-refresh mechanism.
	Reload() error
}

// manifestPaths lists the 4 fixed role manifests, sibling to each role's
// existing prompt.md, in prompts.FS.
var manifestPaths = []string{
	"semar/agent.yaml",
	"gareng/agent.yaml",
	"petruk/agent.yaml",
	"bagong/agent.yaml",
}

// registry is the concrete, embedded-FS-backed AgentRegistry.
type registry struct {
	fsys  fs.FS
	paths []string
	specs map[string]RoleSpec
}

// NewRegistry loads the 4 real role manifests from prompts.FS. It fails if
// any manifest is missing or malformed - a role registry that silently
// serves fewer than 4 roles is worse than a startup error, since every
// caller of List/Get already assumes all 4 roles are present.
func NewRegistry() (AgentRegistry, error) {
	r := &registry{fsys: prompts.FS, paths: manifestPaths}
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *registry) List() []RoleSpec {
	out := make([]RoleSpec, 0, len(r.specs))
	for _, s := range r.specs {
		out = append(out, s)
	}
	return out
}

func (r *registry) Get(id string) (RoleSpec, error) {
	spec, ok := r.specs[id]
	if !ok {
		return RoleSpec{}, fmt.Errorf("agent: unknown role %q", id)
	}
	return spec, nil
}

func (r *registry) Reload() error {
	specs, err := loadSpecs(r.fsys, r.paths)
	if err != nil {
		return err
	}
	r.specs = specs
	return nil
}

// loadSpecs reads and parses each of paths from fsys into a RoleSpec,
// keyed by RoleSpec.ID. Factored out from NewRegistry/Reload so tests can
// exercise manifest parsing against a fabricated fs.FS (e.g.
// testing/fstest.MapFS) without depending on the real embedded prompt
// manifests.
func loadSpecs(fsys fs.FS, paths []string) (map[string]RoleSpec, error) {
	specs := make(map[string]RoleSpec, len(paths))
	for _, path := range paths {
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil, fmt.Errorf("agent: read manifest %s: %w", path, err)
		}
		var m manifestFile
		if err := yaml.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("agent: parse manifest %s: %w", path, err)
		}
		spec := m.toRoleSpec()
		if spec.ID == "" {
			return nil, fmt.Errorf("agent: manifest %s: missing id", path)
		}
		specs[spec.ID] = spec
	}
	return specs, nil
}

// manifestFile is prompts/<role>/agent.yaml's on-disk shape.
type manifestFile struct {
	ID           string          `yaml:"id"`
	Name         string          `yaml:"name"`
	Version      manifestVersion `yaml:"version"`
	Description  string          `yaml:"description"`
	Instructions string          `yaml:"instructions"`
	OutputSchema string          `yaml:"output_schema"`
	Capabilities []string        `yaml:"capabilities"`
	Tools        manifestTools   `yaml:"tools"`
	Execution    manifestExec    `yaml:"execution"`
}

type manifestTools struct {
	ReadOnly bool     `yaml:"read_only"`
	Allowed  []string `yaml:"allowed"`
	Denied   []string `yaml:"denied"`
}

type manifestExec struct {
	CanMutate        bool `yaml:"can_mutate"`
	RequiresEvidence bool `yaml:"requires_evidence"`
	ParallelSafe     bool `yaml:"parallel_safe"`
}

func (m manifestFile) toRoleSpec() RoleSpec {
	return RoleSpec{
		ID:           m.ID,
		Name:         m.Name,
		Description:  m.Description,
		Version:      string(m.Version),
		Instructions: m.Instructions,
		Capabilities: m.Capabilities,
		ToolPolicy: ToolPolicy{
			AllowedTools: m.Tools.Allowed,
			DeniedTools:  m.Tools.Denied,
			ReadOnly:     m.Tools.ReadOnly,
		},
		OutputSchemaID: m.OutputSchema,
		ExecutionPolicy: ExecutionPolicy{
			CanMutate:        m.Execution.CanMutate,
			RequiresEvidence: m.Execution.RequiresEvidence,
			ParallelSafe:     m.Execution.ParallelSafe,
		},
	}
}

// manifestVersion accepts either a YAML integer (e.g. `version: 1`) or a
// string (e.g. `version: "1"`) so manifest authors don't have to
// remember to quote it, while RoleSpec.Version stays a plain string (it
// is surfaced verbatim, e.g. into telemetry, never arithmetically
// compared).
type manifestVersion string

func (v *manifestVersion) UnmarshalYAML(value *yaml.Node) error {
	*v = manifestVersion(value.Value)
	return nil
}
