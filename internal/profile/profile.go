// Package profile loads the repo-owned profile
// (<workspaceRoot>/.punakawan/repo-profile.yaml) that punokawan-14yn.9 AC6
// requires: "Repository-owned configuration wins over conflicting learned
// state and the conflict is visible."
//
// This package deliberately holds only the read-only repo-owned layer and
// (in merge.go) the two-layer merge/conflict logic between it and the
// global overlay that learned facts already get written into
// (internal/project's Project.Metadata, materialized by
// internal/learning's MetadataAdapter when a project_metadata proposal is
// accepted). It is not a general precedence engine: exactly two hardcoded
// layers, key-presence-wins.
//
// RepoProfile is intentionally read-only from this package's perspective —
// there is no Save/mutation API here, mirroring internal/policy's
// policy.yaml, which is likewise only ever loaded, never written, by
// running code. A repo-profile.yaml is meant to be authored and
// git-committed by a human (or generated once by a separate tool), the same
// way policy.yaml and .editorconfig are.
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// SupportedVersion is the only repo-profile.yaml schema version
	// understood.
	SupportedVersion = "punakawan.repo-profile/v1"

	dirName    = ".punakawan"
	configFile = "repo-profile.yaml"
)

// Entry is one repo-owned key/value fact. Value may be a string, number,
// bool, or structured map/slice, mirroring
// internal/project.MetadataEntry's intentionally generic shape so the same
// key can be compared against a learning.Proposal's TargetId without any
// per-field code.
type Entry struct {
	Key   string `yaml:"key" json:"key"`
	Value any    `yaml:"value" json:"value"`
}

// RepoProfile is a workspace's repo-owned profile: explicit facts a human
// has pinned in git, which must win over anything internal/learning later
// infers or is told for the same key (AC6).
type RepoProfile struct {
	Entries []Entry
}

// repoProfileFile is the on-disk YAML shape.
type repoProfileFile struct {
	Version string  `yaml:"version"`
	Entries []Entry `yaml:"entries"`
}

// configPath returns <root>/.punakawan/repo-profile.yaml.
func configPath(root string) string {
	return filepath.Join(root, dirName, configFile)
}

// Load reads <root>/.punakawan/repo-profile.yaml. If the file does not
// exist, an empty RepoProfile is returned (no entries, so every key falls
// through to the global overlay) rather than an error — a workspace that
// has never pinned a repo-owned fact is a normal state, matching
// internal/policy.Load's and internal/project.Load's same
// missing-file-is-not-an-error convention.
func Load(root string) (*RepoProfile, error) {
	path := configPath(root)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &RepoProfile{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("profile: read %s: %w", path, err)
	}

	var file repoProfileFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("profile: parse %s: %w", path, err)
	}
	if file.Version != "" && file.Version != SupportedVersion {
		return nil, fmt.Errorf("profile: unsupported version %q (want %q)", file.Version, SupportedVersion)
	}
	return &RepoProfile{Entries: file.Entries}, nil
}

// Value returns the repo-owned entry for key (case-insensitive, matching
// internal/project's MetadataFor convention) and whether it exists.
func (rp *RepoProfile) Value(key string) (any, bool) {
	if rp == nil {
		return nil, false
	}
	target := strings.ToLower(strings.TrimSpace(key))
	for _, e := range rp.Entries {
		if strings.ToLower(strings.TrimSpace(e.Key)) == target {
			return e.Value, true
		}
	}
	return nil, false
}
