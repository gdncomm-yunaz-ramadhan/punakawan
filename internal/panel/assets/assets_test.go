package assets

import (
	"io/fs"
	"strings"
	"testing"
)

// TestEmbeddedBundleIncludesIndex guards against shipping a binary whose
// embedded dist/ was never rebuilt from the real Vite/Svelte output, per
// performance plan Phase 0's "confirm whether the rebuilt binary includes the
// latest embedded panel assets." index.html must always be present (both the
// placeholder and a real build carry one).
func TestEmbeddedBundleIncludesIndex(t *testing.T) {
	sub, err := fs.Sub(Dist, DistDir)
	if err != nil {
		t.Fatalf("fs.Sub(%q): %v", DistDir, err)
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		t.Fatalf("embedded bundle is missing index.html: %v", err)
	}
}

// TestEmbeddedBundleHasBuiltJS asserts the embedded FS contains at least one
// hashed JS bundle under assets/ (e.g. assets/index-<hash>.js) - the signal
// that a real `pnpm --filter @punakawan/panel build` produced the dist/, not
// just the checked-in placeholder page. If this fails, the binary was built
// against a stale/unbuilt frontend and the panel would serve a placeholder.
func TestEmbeddedBundleHasBuiltJS(t *testing.T) {
	sub, err := fs.Sub(Dist, DistDir)
	if err != nil {
		t.Fatalf("fs.Sub(%q): %v", DistDir, err)
	}

	var jsFiles []string
	err = fs.WalkDir(sub, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An absent assets/ dir means only the placeholder shipped.
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".js") {
			jsFiles = append(jsFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("embedded bundle has no assets/ dir with built JS (stale/unbuilt frontend?): %v", err)
	}
	if len(jsFiles) == 0 {
		t.Fatal("embedded bundle has assets/ but no *.js bundle; frontend build likely did not run")
	}
}
