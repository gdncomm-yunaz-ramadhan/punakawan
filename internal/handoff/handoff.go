// Package handoff loads, persists, and validates a Punakawan project's Handoff
// Capsules - compact, resumable snapshots of in-progress work, per
// punakawan-role-config-distinguished-improvements-plan.md Part V §40-43. A
// capsule lets work continue across agent clients, model providers, sessions,
// machines, and people WITHOUT depending on conversation transcript history: it
// references existing objects (plan, tasks, contradictions, evidence, dossier)
// by id rather than copying them, so it stays small.
//
// Like internal/project, internal/roleconfig, and the change-dossier store,
// this package is stateless and keyed by a workspace root rather than being an
// *app.App field, because capsules are project-scoped. Each capsule is a single
// file at <root>/.punakawan/handoffs/<id>.yaml, written temp-file + rename so a
// crash mid-write can never leave a half-written capsule. Reads never fail
// purely because a capsule is absent (mirrors project.Load): a missing capsule
// is synthesized as an empty one carrying just the requested id.
//
// Resume validation (§42) deliberately takes an injected ValidationDeps set of
// lookup funcs rather than importing the plan store, role config, contradiction
// registry, git, and evidence packages directly. That keeps this package
// dependency-free and unit-testable: the caller (the panel/MCP layer) wires the
// real lookups; tests wire fakes.
package handoff

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
	"gopkg.in/yaml.v3"
)

const (
	// SupportedVersion is the only handoff schema version understood, matching
	// pkg/protocol's `punakawan.handoff/v1` version const. Create stamps it so
	// the generated UnmarshalJSON never rejects our own output.
	SupportedVersion = "punakawan.handoff/v1"

	dirName      = ".punakawan"
	handoffsDir  = "handoffs"
	fileNameGlob = ".yaml"
)

// ErrHandoffNotFound is available for callers that must distinguish an absent
// capsule; the read path (Get/List) never returns it, synthesizing an empty
// capsule instead so a fresh workspace need not be special-cased.
var ErrHandoffNotFound = errors.New("handoff: not found")

func handoffsRoot(root string) string { return filepath.Join(root, dirName, handoffsDir) }
func handoffPath(root, id string) string {
	return filepath.Join(handoffsRoot(root), id+fileNameGlob)
}

// Create persists a new capsule: it stamps the schema version and, when the
// caller left it unset, a CreatedAt of now, then writes the file atomically.
func Create(root string, h protocol.HandoffCapsule) (protocol.HandoffCapsule, error) {
	h.Version = SupportedVersion
	if h.CreatedAt == nil {
		now := time.Now().UTC()
		h.CreatedAt = &now
	}
	if err := write(root, h); err != nil {
		return protocol.HandoffCapsule{}, err
	}
	return h, nil
}

// Get reads the capsule with id id. A capsule that has never been written is a
// normal, empty state, not an error: the returned capsule carries just the id
// and the schema version.
func Get(root, id string) (protocol.HandoffCapsule, error) {
	data, err := os.ReadFile(handoffPath(root, id))
	if os.IsNotExist(err) {
		return protocol.HandoffCapsule{Version: SupportedVersion, Id: id}, nil
	}
	if err != nil {
		return protocol.HandoffCapsule{}, fmt.Errorf("handoff: read %s: %w", handoffPath(root, id), err)
	}
	var h protocol.HandoffCapsule
	if err := yaml.Unmarshal(data, &h); err != nil {
		return protocol.HandoffCapsule{}, fmt.Errorf("handoff: parse %s: %w", handoffPath(root, id), err)
	}
	return h, nil
}

// List returns the capsule ids present in this workspace, sorted. A workspace
// that has never held a capsule (no handoffs directory yet) yields an empty
// slice, not an error. Non-YAML and directory entries are ignored.
func List(root string) ([]string, error) {
	entries, err := os.ReadDir(handoffsRoot(root))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("handoff: list handoffs: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if id, ok := strings.CutSuffix(e.Name(), fileNameGlob); ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// Supersede marks the capsule superseded so it cannot resume silently
// (§43/acceptance): a superseded capsule always validates to the superseded
// status. It loads, sets Superseded=true, and rewrites the file.
func Supersede(root, id string) error {
	h, err := Get(root, id)
	if err != nil {
		return err
	}
	t := true
	h.Superseded = &t
	return write(root, h)
}

func write(root string, h protocol.HandoffCapsule) error {
	if err := os.MkdirAll(handoffsRoot(root), 0o755); err != nil {
		return fmt.Errorf("handoff: mkdir %s: %w", handoffsRoot(root), err)
	}
	out, err := yaml.Marshal(h)
	if err != nil {
		return fmt.Errorf("handoff: marshal: %w", err)
	}
	return atomicWrite(handoffPath(root, h.Id), out)
}

// atomicWrite writes data to a sibling temp file then renames it over path, so
// a reader never observes a partially written file.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".handoff-*.tmp")
	if err != nil {
		return fmt.Errorf("handoff: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("handoff: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("handoff: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("handoff: rename temp over %s: %w", path, err)
	}
	return nil
}
