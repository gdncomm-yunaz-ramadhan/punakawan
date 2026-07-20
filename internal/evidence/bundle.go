package evidence

import (
	"fmt"
	"os"
	"path/filepath"
)

// Bundle is the directory skeleton for one task's evidence, per §17.2.
// Populating task.yaml, commands.jsonl, diff.patch, tests.json,
// api-diff.json, provenance.yaml, and summary.yaml is later execution-runtime
// work (Milestone 6); this only creates the layout and exposes Path.
type Bundle struct {
	Dir string
}

// NewBundle creates .punakawan/evidence/<runID>/<taskID>/ with its logs/,
// screenshots/, and traces/ subdirectories, per §17.2.
func NewBundle(workspaceRoot, runID, taskID string) (*Bundle, error) {
	dir := filepath.Join(workspaceRoot, ".punakawan", "evidence", runID, taskID)
	for _, sub := range []string{"logs", "screenshots", "traces"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("evidence: create %s: %w", sub, err)
		}
	}
	return &Bundle{Dir: dir}, nil
}

// Path returns the absolute path for a named file within the bundle (e.g.
// "task.yaml", "diff.patch", "logs/build.log").
func (b *Bundle) Path(name string) string {
	return filepath.Join(b.Dir, name)
}
