// Package hub is the seam that decides whether a project's knowledge store
// lives behind its own dedicated Dolt sql-server (the legacy, still-default
// path) or as one database within a shared "hub" server serving many
// projects from one process (ADR-0020).
//
// Adoption is always explicit, never automatic for an already-existing
// project: a project only uses the hub once something writes its pointer
// file (the future migration tool). Lookup mirrors internal/beads's
// ProjectInitialized - a pure, cheap filesystem check with no caching and no
// side effects - so callers can branch on it inline.
package hub

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const refFile = "hub-ref.yaml"

// Ref points a project at its database within a shared hub server: HubDir is
// the hub's --data-dir (shared across every project pointed at it); ProjectID
// names this project's subdirectory/database within it. ProjectID is
// recorded explicitly here (not re-derived from workspace.yaml's id each
// time) so it stays stable even if the workspace's own id later changes.
type Ref struct {
	HubDir    string `yaml:"hub_dir"`
	ProjectID string `yaml:"project_id"`
}

func path(root string) string {
	return filepath.Join(root, ".punakawan", refFile)
}

// Lookup reports whether root has a hub pointer and, if so, returns it. A
// missing file is the normal, expected case for every project that has not
// been (or does not need to be) moved into a hub - it is not an error.
func Lookup(root string) (Ref, bool, error) {
	data, err := os.ReadFile(path(root))
	if os.IsNotExist(err) {
		return Ref{}, false, nil
	}
	if err != nil {
		return Ref{}, false, fmt.Errorf("hub: read %s: %w", path(root), err)
	}
	var ref Ref
	if err := yaml.Unmarshal(data, &ref); err != nil {
		return Ref{}, false, fmt.Errorf("hub: parse %s: %w", path(root), err)
	}
	if strings.TrimSpace(ref.HubDir) == "" || strings.TrimSpace(ref.ProjectID) == "" {
		return Ref{}, false, fmt.Errorf("hub: %s is missing hub_dir or project_id", path(root))
	}
	return ref, true, nil
}

// Write persists ref at root, creating .punakawan/ if needed. Intended for
// the migration tool that moves an already-live project's data into a hub;
// callers are responsible for having actually moved the data first - Write
// only records the pointer, it does not move anything itself.
func Write(root string, ref Ref) error {
	if strings.TrimSpace(ref.HubDir) == "" || strings.TrimSpace(ref.ProjectID) == "" {
		return fmt.Errorf("hub: Write: both hub_dir and project_id are required")
	}
	dir := filepath.Join(root, ".punakawan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("hub: create %s: %w", dir, err)
	}
	data, err := yaml.Marshal(ref)
	if err != nil {
		return fmt.Errorf("hub: encode ref: %w", err)
	}
	if err := os.WriteFile(path(root), data, 0o644); err != nil {
		return fmt.Errorf("hub: write %s: %w", path(root), err)
	}
	return nil
}
