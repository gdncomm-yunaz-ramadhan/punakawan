// Package planimport is the one-time importer that carries every plan
// version the retired filesystem PlanStore (formerly
// internal/artifact.PlanStore) ever wrote under one project's
// <workspaceRoot>/.punakawan/plans/<id>/versions/<n>.md layout into
// internal/plan's plan_revisions, so nothing written through that older
// store is lost when it is deleted.
package planimport

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ygrip/punakawan/internal/plan"
)

// ImportRecord maps one imported legacy plan version onto the canonical
// plan_revisions row it now lives at.
type ImportRecord struct {
	WorkspaceID     string
	LegacyPlanID    string
	LegacyVersion   int
	CanonicalPlanID string
	Revision        int
}

// CanonicalPlanID deterministically derives the global plan_revisions
// plan_id a legacy (workspaceID, legacyPlanID) pair imports onto.
//
// Rather than persisting a mapping table that only a detected id
// collision would populate, this always workspace-prefixes the legacy
// id - a pure, total function of its two inputs that produces the same
// canonical id whether or not any other workspace's legacy plan ever
// used the same directory name. This needs no durable mapping store of
// its own (idempotency for "was this exact version already imported"
// already comes from Store.SaveWithKey's own idempotency-key ledger,
// keyed on (workspace_id, legacy_plan_id, legacy_version) below), and it
// can never collide, which a conditionally-applied mapping only
// populated on detected collisions still could if two workspaces
// imported in an interleaved, non-obvious order.
func CanonicalPlanID(workspaceID, legacyPlanID string) string {
	return workspaceID + ":" + legacyPlanID
}

// Import walks workspaceRoot's legacy .punakawan/plans/<id>/versions/<n>.md
// directories and imports every plan's every version, in ascending
// version order, into plans' plan_revisions via SaveWithKey. It is
// idempotent on (workspaceID, legacy plan id, legacy version): calling it
// again over the same directory creates no new revisions and returns the
// same ImportRecords. A workspace root with no .punakawan/plans directory
// yet (nothing was ever written through the legacy store) is a normal
// empty result, not an error.
func Import(ctx context.Context, plans *plan.Store, workspaceID, workspaceRoot string) ([]ImportRecord, error) {
	plansRoot := filepath.Join(workspaceRoot, ".punakawan", "plans")
	entries, err := os.ReadDir(plansRoot)
	if os.IsNotExist(err) {
		return []ImportRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("planimport: list %s: %w", plansRoot, err)
	}

	var legacyIDs []string
	for _, e := range entries {
		if e.IsDir() {
			legacyIDs = append(legacyIDs, e.Name())
		}
	}
	sort.Strings(legacyIDs)

	records := make([]ImportRecord, 0, len(legacyIDs))
	for _, legacyID := range legacyIDs {
		versionsDir := filepath.Join(plansRoot, legacyID, "versions")
		versions, err := legacyVersions(versionsDir)
		if err != nil {
			return nil, err
		}
		canonicalID := CanonicalPlanID(workspaceID, legacyID)
		for i, version := range versions {
			content, err := os.ReadFile(filepath.Join(versionsDir, fmt.Sprintf("%d.md", version)))
			if err != nil {
				return nil, fmt.Errorf("planimport: read %s version %d: %w", legacyID, version, err)
			}
			key := fmt.Sprintf("planimport:%s:%s:%d", workspaceID, legacyID, version)
			if _, err := plans.SaveWithKey(ctx, key, plan.Plan{
				ID:             canonicalID,
				Objective:      legacyObjective(legacyID, version),
				LegacyMarkdown: string(content),
			}); err != nil {
				return nil, fmt.Errorf("planimport: save %s version %d: %w", legacyID, version, err)
			}
			// Every version this importer ever writes for canonicalID is
			// appended in this same ascending order, on the very first call
			// that ever sees it - so the revision legacyVersion maps to is
			// always its own 1-based position in that ascending list,
			// whether this is a fresh import (Save actually wrote it) or a
			// replay (Save's duplicate-key fallback returned the lineage's
			// current head, which is not necessarily this version's own
			// revision once later versions exist).
			records = append(records, ImportRecord{
				WorkspaceID: workspaceID, LegacyPlanID: legacyID, LegacyVersion: version,
				CanonicalPlanID: canonicalID, Revision: i + 1,
			})
		}
	}
	return records, nil
}

func legacyObjective(legacyID string, version int) string {
	return fmt.Sprintf("%s (imported v%d)", legacyID, version)
}

func legacyVersions(dir string) ([]int, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("planimport: list %s: %w", dir, err)
	}
	var versions []int
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		versions = append(versions, n)
	}
	sort.Ints(versions)
	return versions, nil
}
