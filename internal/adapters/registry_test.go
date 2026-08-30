package adapters

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const prototypeAdapterPath = "../../packages/adapter-sdk/dist/prototypeAdapter.js"

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	if _, err := os.Stat(prototypeAdapterPath); err != nil {
		t.Skipf("prototype adapter not built (%s): %v; run `pnpm --filter @punakawan/adapter-sdk build` first", prototypeAdapterPath, err)
	}
	specs := map[string]AdapterSpec{
		"prototype": {Command: "node", Args: []string{prototypeAdapterPath}},
	}
	return NewRegistry(specs)
}

func TestRegistryGateStartsAndFetchesManifest(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	g, err := r.Gate(ctx, "prototype")
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if g.adapterID != "prototype" {
		t.Fatalf("adapterID = %q, want prototype", g.adapterID)
	}
	if g.manifest.Id != "prototype" {
		t.Fatalf("manifest.Id = %q, want prototype", g.manifest.Id)
	}
	if _, ok := g.manifest.Operations["sleep"]; !ok {
		t.Fatalf("manifest.Operations = %+v, want sleep present", g.manifest.Operations)
	}

	if _, err := g.Call(ctx, "run-1", "sleep", map[string]any{"ms": 0}); err != nil {
		t.Fatalf("Call sleep: %v", err)
	}
}

func TestRegistryGateMemoizes(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	g1, err := r.Gate(ctx, "prototype")
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	g2, err := r.Gate(ctx, "prototype")
	if err != nil {
		t.Fatalf("Gate (second call): %v", err)
	}
	if g1 != g2 {
		t.Fatal("expected second Gate call to return the same memoized instance")
	}
	if len(r.clients) != 1 {
		t.Fatalf("clients = %d, want 1 (no duplicate process spawned)", len(r.clients))
	}
}

func TestRegistryGateUnknownAdapterID(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	if _, err := r.Gate(ctx, "does-not-exist"); err == nil {
		t.Fatal("expected error for unknown adapter id")
	}
}

// TestRegistryGateRespawnsAfterCrash guards against the bug reported in
// practice: once an adapter process crashes, every future call to that
// adapter used to fail identically ("broken pipe") until the whole
// punakawan process restarted, since Gate memoized the now-dead Client
// forever. Gate must instead detect the death and transparently respawn.
func TestRegistryGateRespawnsAfterCrash(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	g1, err := r.Gate(ctx, "prototype")
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if _, err := g1.Call(ctx, "run-1", "sleep", map[string]any{"ms": 0}); err != nil {
		t.Fatalf("Call sleep before crash: %v", err)
	}

	client := r.clients["prototype"]
	if err := client.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitUntilDead(t, client)

	g2, err := r.Gate(ctx, "prototype")
	if err != nil {
		t.Fatalf("Gate after crash: %v", err)
	}
	if g1 == g2 {
		t.Fatal("expected a fresh Gate after the previous process crashed, got the same memoized instance")
	}
	if _, err := g2.Call(ctx, "run-1", "sleep", map[string]any{"ms": 0}); err != nil {
		t.Fatalf("Call sleep on respawned process: %v", err)
	}
}

// fakeManifestScript is a minimal JSON-RPC responder that answers
// "capabilities" with whatever manifest JSON was passed as its sole
// argument, and "initialize"/"shutdown" trivially - used to drive
// Registry.Gate's manifest validation with hand-crafted (including
// deliberately invalid) manifests, without needing a real adapter build.
const fakeManifestScript = `
process.stdin.setEncoding('utf8');
const manifest = JSON.parse(process.argv[1]);
let buf = '';
process.stdin.on('data', (chunk) => {
  buf += chunk;
  let idx;
  while ((idx = buf.indexOf('\n')) >= 0) {
    const line = buf.slice(0, idx);
    buf = buf.slice(idx + 1);
    if (!line.trim()) continue;
    const req = JSON.parse(line);
    if (req.method === 'cancel') continue;
    if (req.method === 'capabilities') {
      process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: req.id, result: manifest }) + '\n');
    } else if (req.id !== undefined) {
      process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: req.id, result: {} }) + '\n');
    }
  }
});
`

// validFakeManifest is a manifest fakeManifestScript can serve that passes
// every validateManifest check on its own, so each test below only needs
// to break exactly one aspect of it.
func validFakeManifest(id string) map[string]any {
	return map[string]any{
		"id": id, "name": id, "version": "0.1.0", "protocol": punakawanAdapterProtocol,
		"runtime": "node", "provides": []string{"jira"},
		"permissions": map[string]any{
			"network":    map[string]any{"hosts": []string{}},
			"filesystem": map[string]any{"read": []string{}, "write": []string{}},
			"secrets":    []string{},
		},
		"operations": map[string]any{
			"noop": map[string]any{
				"side_effect": false,
				"description": "Return the supplied value without side effects.",
				"input_schema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"value": map[string]any{}},
				},
			},
		},
	}
}

func newFakeManifestRegistry(t *testing.T, adapterID string, manifest map[string]any) *Registry {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found on PATH")
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	specs := map[string]AdapterSpec{
		adapterID: {Command: "node", Args: []string{"-e", fakeManifestScript, "--", string(encoded)}},
	}
	return NewRegistry(specs)
}

// TestRegistryGateRejectsMismatchedManifestID guards the "manifest.id ==
// configured adapter id" contract: an adapter answering for a different id
// than the one it was configured under must never be wired up, since
// nothing else in the stack re-checks this once a Gate exists.
func TestRegistryGateRejectsMismatchedManifestID(t *testing.T) {
	manifest := validFakeManifest("some-other-adapter")
	r := newFakeManifestRegistry(t, "fake", manifest)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	_, err := r.Gate(ctx, "fake")
	if err == nil {
		t.Fatal("expected a manifest id mismatch to be rejected")
	}
	if !strings.Contains(err.Error(), "manifest id") {
		t.Fatalf("error = %q, want it to mention the manifest id mismatch", err.Error())
	}
}

// TestRegistryGateRejectsWrongProtocol guards manifest.protocol ==
// punakawan.adapter/v1.
func TestRegistryGateRejectsWrongProtocol(t *testing.T) {
	manifest := validFakeManifest("fake")
	manifest["protocol"] = "some.other.protocol/v1"
	r := newFakeManifestRegistry(t, "fake", manifest)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	if _, err := r.Gate(ctx, "fake"); err == nil {
		t.Fatal("expected a wrong protocol version to be rejected")
	}
}

// TestRegistryGateRejectsMissingOperationDescription guards "every
// operation has description": a JSON schema requiring the field is not
// enough by itself, since the generated Go decode does not enforce
// per-operation required fields the way it does for the manifest's own
// top-level fields.
func TestRegistryGateRejectsMissingOperationDescription(t *testing.T) {
	manifest := validFakeManifest("fake")
	manifest["operations"] = map[string]any{
		"noop": map[string]any{
			"side_effect": false,
			"input_schema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"value": map[string]any{}},
			},
		},
	}
	r := newFakeManifestRegistry(t, "fake", manifest)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	_, err := r.Gate(ctx, "fake")
	if err == nil {
		t.Fatal("expected a missing operation description to be rejected")
	}
	if !strings.Contains(err.Error(), "no description") {
		t.Fatalf("error = %q, want it to mention the missing description", err.Error())
	}
}

// TestRegistryGateRejectsInvalidInputSchema guards "every operation has ...
// object input_schema": a schema that does not describe a JSON object (or
// omits input_schema entirely) must be rejected, since a call's params are
// always an object.
func TestRegistryGateRejectsInvalidInputSchema(t *testing.T) {
	manifest := validFakeManifest("fake")
	manifest["operations"] = map[string]any{
		"noop": map[string]any{
			"side_effect":  false,
			"description":  "Return the supplied value without side effects.",
			"input_schema": map[string]any{"type": "string"},
		},
	}
	r := newFakeManifestRegistry(t, "fake", manifest)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	_, err := r.Gate(ctx, "fake")
	if err == nil {
		t.Fatal("expected a non-object input_schema to be rejected")
	}
	if !strings.Contains(err.Error(), "input_schema") {
		t.Fatalf("error = %q, want it to mention the invalid input_schema", err.Error())
	}
}

// TestRegistryGateRejectsUndeclaredSecret guards "declared secret names are
// a subset of the host-owned spec": an adapter asking for a secret this
// host never agreed to pass it must be rejected at initialization, not
// discovered only once that secret happens to be missing from its
// environment.
func TestRegistryGateRejectsUndeclaredSecret(t *testing.T) {
	manifest := validFakeManifest("fake")
	manifest["permissions"] = map[string]any{
		"network":    map[string]any{"hosts": []string{}},
		"filesystem": map[string]any{"read": []string{}, "write": []string{}},
		"secrets":    []string{"SOME_UNAUTHORIZED_TOKEN"},
	}
	r := newFakeManifestRegistry(t, "fake", manifest)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	_, err := r.Gate(ctx, "fake")
	if err == nil {
		t.Fatal("expected an undeclared secret to be rejected")
	}
	if !strings.Contains(err.Error(), "SOME_UNAUTHORIZED_TOKEN") {
		t.Fatalf("error = %q, want it to name the unauthorized secret", err.Error())
	}
}

// TestRegistryGateAcceptsValidManifest is the control for the rejection
// tests above: a manifest that satisfies every check must still work.
func TestRegistryGateAcceptsValidManifest(t *testing.T) {
	manifest := validFakeManifest("fake")
	r := newFakeManifestRegistry(t, "fake", manifest)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	g, err := r.Gate(ctx, "fake")
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if _, err := g.Call(ctx, "run-1", "noop", map[string]any{"value": "hi"}); err != nil {
		t.Fatalf("Call noop: %v", err)
	}
}

func waitUntilDead(t *testing.T, c *Client) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !c.Dead() {
		if time.Now().After(deadline) {
			t.Fatal("client not marked Dead within 2s of Kill")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRegistryCloseShutsDownProcess(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	g, err := r.Gate(ctx, "prototype")
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if _, err := g.Call(ctx, "run-1", "sleep", map[string]any{"ms": 0}); err != nil {
		t.Fatalf("Call sleep before close: %v", err)
	}

	if err := r.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := g.Call(ctx, "run-1", "sleep", map[string]any{"ms": 0}); err == nil {
		t.Fatal("expected Call to fail after Close shut down the adapter process")
	}
}
