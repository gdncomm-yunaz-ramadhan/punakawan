// Package doltimport performs the one-way migration of a project's durable
// knowledge from its legacy Dolt store into the live shared SQLite kernel
// (internal/storage). Knowledge is the only subsystem that ever lived in Dolt;
// every other store was migrated straight from a file into the kernel,
// so this is the last real data migration.
//
// Dolt is invoked only as a short-lived supervised import tool: each query is
// a single `dolt sql -q "..." -r json` process, never a long-running
// sql-server. The kernel is already the single live database, so an import
// writes directly into its existing knowledge_records/knowledge_relations
// tables (scoped by the destination project id) inside one transaction - there
// is no separate staging database to build and no wholesale cutover to
// perform, only the remaining Dolt-resident data to fold in.
package doltimport

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ygrip/punakawan/internal/hub"
)

// SourceKind names where a workspace's Dolt knowledge lives, or that it has
// none to import.
type SourceKind string

const (
	// KindNone means the workspace has neither a hub pointer nor a legacy
	// per-project Dolt store: there is nothing to import.
	KindNone SourceKind = "none"
	// KindHub means the workspace's knowledge database lives inside a shared
	// hub server's data directory (ADR-0020), selected by a hub-ref pointer.
	KindHub SourceKind = "hub"
	// KindLegacy means the workspace has its own dedicated Dolt repository
	// under .punakawan/knowledge.
	KindLegacy SourceKind = "legacy"
)

// Source describes the discovered Dolt knowledge source for one workspace.
type Source struct {
	Kind SourceKind
	// Dir is the working directory to run dolt in: the workspace's own
	// .punakawan/knowledge for a legacy store, or <hubDir>/<projectID> for a
	// hub-backed one.
	Dir string
	// DoltCfgDir is the value for dolt's --doltcfg-dir flag. It is set only
	// for hub-backed sources, where many database directories share one
	// .doltcfg at the hub root and dolt refuses to guess between them.
	DoltCfgDir string
	// SourceDB names the source database - the hub's project subdirectory for
	// a hub-backed source, or "knowledge" for a legacy one. Informational,
	// surfaced in the report; it is not the destination scope.
	SourceDB string
}

// Discover determines the Dolt knowledge source for the workspace rooted at
// root. A hub pointer takes precedence: adoption into a hub is the explicit,
// forward path, so if hub-ref.yaml is present the hub database is the source
// even when a stale legacy .dolt directory also lingers under the workspace.
func Discover(root string) (Source, error) {
	ref, ok, err := hub.Lookup(root)
	if err != nil {
		return Source{}, err
	}
	if ok {
		return Source{
			Kind:       KindHub,
			Dir:        filepath.Join(ref.HubDir, ref.ProjectID),
			DoltCfgDir: filepath.Join(ref.HubDir, ".doltcfg"),
			SourceDB:   ref.ProjectID,
		}, nil
	}

	legacy := filepath.Join(root, ".punakawan", "knowledge")
	fi, err := os.Stat(filepath.Join(legacy, ".dolt"))
	switch {
	case err == nil && fi.IsDir():
		return Source{Kind: KindLegacy, Dir: legacy, SourceDB: "knowledge"}, nil
	case err != nil && !os.IsNotExist(err):
		return Source{}, fmt.Errorf("doltimport: stat legacy store %s: %w", legacy, err)
	}
	return Source{Kind: KindNone}, nil
}
