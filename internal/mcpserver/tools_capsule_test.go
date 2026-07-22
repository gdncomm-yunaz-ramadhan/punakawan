package mcpserver

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestRequestCapsuleEndToEnd(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	var out map[string]any
	callTool(t, cs, "request_capsule", map[string]any{
		"task_id":       "bd-task-1",
		"role":          "petruk",
		"objective":     "implement the refund flow",
		"allowed_tools": []string{"write_file"},
	}, &out)

	if out["task_id"] != "bd-task-1" || out["role"] != "petruk" || out["objective"] != "implement the refund flow" {
		t.Fatalf("request_capsule output = %+v, want task_id/role/objective set", out)
	}
	id, _ := out["id"].(string)
	if id == "" {
		t.Fatalf("output has no id: %+v", out)
	}
	if out["digest"] == "" {
		t.Fatalf("output has no digest: %+v", out)
	}

	got, err := a.Capsules.Get(id)
	if err != nil {
		t.Fatalf("Capsules.Get(%q): %v", id, err)
	}
	if got.Objective != "implement the refund flow" {
		t.Fatalf("persisted Objective = %q, want %q", got.Objective, "implement the refund flow")
	}
}

func TestRequestCapsuleRejectsForbiddenToolForRole(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "request_capsule",
		Arguments: map[string]any{
			"task_id":       "bd-task-1",
			"role":          "bagong",
			"objective":     "verify the refund flow",
			"allowed_tools": []string{"write_file"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result granting write_file to a bagong capsule")
	}
}

func TestRequireCapsuleForRoleRejectsMissingCapsule(t *testing.T) {
	a := newTestApp(t)
	if _, err := requireCapsuleForRole(a, "no-such-capsule", protocol.ContextCapsuleRoleGareng); err == nil {
		t.Fatal("expected an error for a capsule id that was never issued")
	}
}

func TestRequireCapsuleForRoleRejectsRoleMismatch(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)
	capsuleID := requestTestCapsule(t, cs, "bd-task-1", "petruk")

	if _, err := requireCapsuleForRole(a, capsuleID, protocol.ContextCapsuleRoleGareng); err == nil {
		t.Fatal("expected an error using a petruk capsule where a gareng capsule was required")
	}
}

func TestRequireCapsuleForRoleRejectsTamperedDigest(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)
	capsuleID := requestTestCapsule(t, cs, "bd-task-1", "gareng")

	c, err := a.Capsules.Get(capsuleID)
	if err != nil {
		t.Fatalf("Capsules.Get: %v", err)
	}
	// Store.Get returns the first record matching an id, so overwriting
	// capsuleID wouldn't be observed; write the tampered content under a
	// fresh id instead to simulate a record whose stored digest no longer
	// matches its own content.
	c.Id = "cap-tampered"
	c.Objective = "a different objective than what was digested"
	if err := a.Capsules.Put(c); err != nil {
		t.Fatalf("Capsules.Put: %v", err)
	}

	if _, err := requireCapsuleForRole(a, "cap-tampered", protocol.ContextCapsuleRoleGareng); err == nil {
		t.Fatal("expected a digest-mismatch error for a capsule edited after issuance")
	}
}
