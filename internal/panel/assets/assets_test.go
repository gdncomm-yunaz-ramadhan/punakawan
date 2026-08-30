package assets

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// TestEmbeddedBundleRendersDeliveryTitle is a runtime smoke test, not just a
// presence check: it loads the built index.html's module entrypoint into a
// headless jsdom DOM at /deliveries with a seeded, non-empty delivery
// standing in for the panel's own GET /api/v1/deliveries, and asserts the
// delivery's title renders without a runtime exception. The two tests above
// only confirm *a* bundle was built; this one would also catch a build that
// mounts but throws, or a frontend/backend schema mismatch the compiler
// cannot see.
//
// jsdom (unlike a real browser) never executes `<script type="module">`
// tags at all, so the smoke script below does not rely on jsdom's own script
// runner: it installs the jsdom-produced window's properties as Node
// globals (the same technique tools like `global-jsdom` use) and then lets
// Node's own ESM loader `import()` the built module directly from disk,
// which does understand real import/export syntax.
//
// Requires `node` (with the web/panel workspace's jsdom devDependency
// already installed) on PATH; skips otherwise, matching this repo's existing
// tolerance for optional external tooling (e.g. requireDolt in
// internal/panel/server/server_test.go).
func TestEmbeddedBundleRendersDeliveryTitle(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}

	sub, err := fs.Sub(Dist, DistDir)
	if err != nil {
		t.Fatalf("fs.Sub(%q): %v", DistDir, err)
	}
	if _, err := fs.Stat(sub, "assets"); err != nil {
		t.Skip("embedded bundle has no assets/ (placeholder dist; run `pnpm --filter @punakawan/panel build` first)")
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// DistDir ("dist") is a real on-disk directory next to this file, and
	// go:embed embeds it verbatim - exercising it directly is exercising
	// exactly what ends up in the binary.
	distDir := filepath.Join(filepath.Dir(thisFile), DistDir)

	// node's ESM resolver picks up bare imports (jsdom) relative to the
	// importing file's own directory, not the process's cwd - the smoke
	// script has to live inside web/panel so it resolves the workspace's
	// already-installed jsdom devDependency instead of failing to find it.
	webPanelDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "web", "panel")
	if _, err := os.Stat(webPanelDir); err != nil {
		t.Fatalf("could not locate web/panel at %s: %v", webPanelDir, err)
	}

	scriptPath := filepath.Join(webPanelDir, ".assets-smoke-test.mjs")
	if err := os.WriteFile(scriptPath, []byte(assetSmokeScript), 0o600); err != nil {
		t.Fatalf("write smoke script: %v", err)
	}
	defer os.Remove(scriptPath)

	const wantTitle = "Smoke test delivery for the embedded panel bundle"
	cmd := exec.Command("node", scriptPath, distDir, wantTitle)
	cmd.Dir = webPanelDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("headless render failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("headless render did not confirm the title rendered:\n%s", out)
	}
}

// assetSmokeScript builds a jsdom window for the built index.html at
// /deliveries, projects that window's own properties onto Node's globals so
// the bundle sees a normal browser environment, stubs only the two API
// calls the deliveries list route makes (system info, delivery list), then
// imports the built module entrypoint directly from disk and polls the
// rendered DOM for the seeded title. Any uncaught error or unhandled
// rejection from the app fails the run immediately instead of being
// swallowed.
const assetSmokeScript = `
import { JSDOM } from "jsdom";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const distDir = process.argv[2];
const wantTitle = process.argv[3];

const html = fs.readFileSync(path.join(distDir, "index.html"), "utf8");
const dom = new JSDOM(html, { url: "http://localhost/deliveries", pretendToBeVisual: true });
const window = dom.window;

// Names Node itself owns and that must never be replaced by the jsdom
// window's version - notably its own setTimeout family, since jsdom's
// timer implementations call back into the *global* timer functions and
// would recurse into themselves forever if those were overwritten.
const RESERVED = new Set([
  "global", "process", "Buffer", "require", "module", "exports", "__dirname", "__filename",
  "setTimeout", "clearTimeout", "setInterval", "clearInterval", "queueMicrotask", "setImmediate", "clearImmediate",
]);

function setGlobal(name, value) {
  try {
    Object.defineProperty(global, name, { value, configurable: true, writable: true });
  } catch {
    // Node already defines this as a non-configurable global - leave it be.
  }
}

setGlobal("window", window);
for (const name of Object.getOwnPropertyNames(window)) {
  if (RESERVED.has(name)) continue;
  try {
    setGlobal(name, window[name]);
  } catch {
    // Some window accessors throw when read outside a real browser frame -
    // irrelevant to this smoke test, skip them.
  }
}
setGlobal("requestAnimationFrame", window.requestAnimationFrame || ((cb) => setTimeout(cb, 0)));
setGlobal("ResizeObserver", window.ResizeObserver || class { observe() {} unobserve() {} disconnect() {} });
setGlobal("IntersectionObserver", window.IntersectionObserver || class { observe() {} unobserve() {} disconnect() {} });
window.matchMedia = window.matchMedia || function () {
  return { matches: false, addListener() {}, removeListener() {} };
};
setGlobal("matchMedia", window.matchMedia);

const runtimeErrors = [];
process.on("unhandledRejection", (e) => runtimeErrors.push(String((e && e.stack) || e)));
window.addEventListener("error", (e) => {
  runtimeErrors.push(String((e && e.error && e.error.stack) || (e && e.message) || e));
});

const fetchImpl = async (input) => {
  const url = String(input);
  const ok = (body) => ({ ok: true, status: 200, json: async () => body });
  if (url === "/api/v1/system") return ok({ version: "smoke-test" });
  if (url === "/api/v1/deliveries") {
    return ok({
      items: [
        {
          id: "orc-smoke-1",
          title: wantTitle,
          status: "active",
          projects: [],
          usage: {
            input_tokens: 0,
            output_tokens: 0,
            cache_tokens: 0,
            tool_calls: 0,
            elapsed_ms: 0,
            estimated_costs: {},
            pricing_complete: false,
          },
          updated_at: new Date().toISOString(),
          cancellable: false,
          projection_revision: 1,
        },
      ],
      snapshot_revision: 1,
    });
  }
  return { ok: false, status: 404, json: async () => ({ error: "not found" }) };
};
setGlobal("fetch", fetchImpl);
window.fetch = fetchImpl;

const scriptTagMatch = html.match(/<script[^>]*type="module"[^>]*src="([^"]+)"/);
if (!scriptTagMatch) {
  console.error("NO_MODULE_SCRIPT_TAG_FOUND");
  process.exit(1);
}
const entryURL = pathToFileURL(path.join(distDir, scriptTagMatch[1])).href;

try {
  await import(entryURL);
} catch (e) {
  runtimeErrors.push("importError: " + ((e && e.stack) || e));
}

let found = false;
for (let i = 0; i < 50 && !found; i++) {
  if (runtimeErrors.length > 0) break;
  if (window.document.body.textContent.includes(wantTitle)) {
    found = true;
  } else {
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
}

if (runtimeErrors.length > 0) {
  console.error("RUNTIME_ERROR: " + runtimeErrors.join(" | "));
  process.exit(1);
}
if (!found) {
  console.error("TITLE_NOT_FOUND. Body was:\n" + window.document.body.textContent);
  process.exit(1);
}
console.log("OK");
process.exit(0);
`
