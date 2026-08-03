package knowledge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ygrip/punakawan/internal/hub"
	"github.com/ygrip/punakawan/internal/tools"
)

// ErrNotHubBacked is returned by ExportFromHub when root has no hub-ref: a
// legacy, non-hub project's .punakawan/knowledge is already the canonical
// store, not a snapshot of one, so there is nothing to export.
var ErrNotHubBacked = errors.New("knowledge: root is not on a hub; its .punakawan/knowledge is already the canonical store, not a snapshot to refresh")

// ExportFromHub snapshots root's hub-backed project database into
// root/.punakawan/knowledge, so it stays a normal, git-trackable Dolt
// repository even though the hub (ADR-0020) is now canonical storage. It is
// explicit and on-demand (no automatic mirroring on every write, by design)
// and safe to call repeatedly - each call fully replaces the previous
// snapshot with the project's current state.
//
// It works against a project database the hub server is actively serving,
// using Dolt's own live-backup primitive rather than copying files out from
// under a running server (verified empirically to risk a torn read):
// CALL DOLT_BACKUP registers a target, syncs the database's current commit
// graph to it, and is designed for exactly this - backing up a live
// database without stopping it. `dolt clone` from that backup then
// materializes it as an ordinary, standalone working repository, the same
// shape MigrateToHub's legacy directory and Open both expect. The working
// set is committed first (CommitWorkingSet) so nothing pending is left out
// of the snapshot.
func ExportFromHub(sup *tools.Supervisor, root string) error {
	ref, ok, err := hub.Lookup(root)
	if err != nil {
		return fmt.Errorf("knowledge: ExportFromHub: %w", err)
	}
	if !ok {
		return ErrNotHubBacked
	}

	store, err := OpenInHub(sup, ref.HubDir, ref.ProjectID)
	if err != nil {
		return fmt.Errorf("knowledge: ExportFromHub: open hub store: %w", err)
	}
	defer store.Close()

	if _, err := store.CommitWorkingSet("export snapshot"); err != nil {
		return fmt.Errorf("knowledge: ExportFromHub: commit working set: %w", err)
	}

	scratchDir := filepath.Join(root, ".punakawan")
	if err := os.MkdirAll(scratchDir, 0o755); err != nil {
		return fmt.Errorf("knowledge: ExportFromHub: create %s: %w", scratchDir, err)
	}
	suffix, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("knowledge: ExportFromHub: %w", err)
	}
	backupDir := filepath.Join(scratchDir, "export-backup-"+suffix)
	backupName := "punakawan-export-" + suffix
	defer os.RemoveAll(backupDir)

	if _, err := store.DB().Exec("CALL DOLT_BACKUP('add', ?, ?)", backupName, "file://"+backupDir); err != nil {
		return fmt.Errorf("knowledge: ExportFromHub: register backup target: %w", err)
	}
	defer func() { _, _ = store.DB().Exec("CALL DOLT_BACKUP('remove', ?)", backupName) }()
	if _, err := store.DB().Exec("CALL DOLT_BACKUP('sync', ?)", backupName); err != nil {
		return fmt.Errorf("knowledge: ExportFromHub: sync backup: %w", err)
	}

	cloneDir := filepath.Join(scratchDir, "export-clone-"+suffix)
	defer os.RemoveAll(cloneDir)
	res, err := sup.Run(context.Background(), tools.Spec{
		Name: "dolt",
		Args: []string{"clone", "file://" + backupDir, cloneDir},
		Dir:  scratchDir,
	})
	if err != nil {
		return fmt.Errorf("knowledge: ExportFromHub: clone backup: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("knowledge: ExportFromHub: clone backup failed: %s", res.Stderr)
	}

	destDir := filepath.Join(scratchDir, "knowledge")
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("knowledge: ExportFromHub: remove previous snapshot at %s: %w", destDir, err)
	}
	if err := os.Rename(cloneDir, destDir); err != nil {
		return fmt.Errorf("knowledge: ExportFromHub: replace snapshot at %s: %w", destDir, err)
	}
	return nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random suffix: %w", err)
	}
	return hex.EncodeToString(b), nil
}
