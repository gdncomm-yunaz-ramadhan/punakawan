package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/agent"
	"github.com/ygrip/punakawan/internal/capability"
)

// fakeToolPolicyRegistry is a minimal agent.AgentRegistry stub for
// checkToolPolicy unit tests that need exact, fabricated ToolPolicy shapes
// (e.g. a DeniedTools/AllowedTools overlap) rather than the real embedded
// manifests.
type fakeToolPolicyRegistry map[string]agent.RoleSpec

func (r fakeToolPolicyRegistry) List() []agent.RoleSpec { return nil }

func (r fakeToolPolicyRegistry) Get(id string) (agent.RoleSpec, error) {
	spec, ok := r[id]
	if !ok {
		return agent.RoleSpec{}, fmt.Errorf("unknown role %q", id)
	}
	return spec, nil
}

func (r fakeToolPolicyRegistry) Reload() error { return nil }

func newTestToolIndexWithCapabilities(mutates map[string]bool) *toolIndex {
	idx := newToolIndex()
	for name, m := range mutates {
		idx.Registry.Add(capability.Descriptor{Name: name, Source: capability.SourceMCP, Mutates: m})
	}
	return idx
}

// fakeSession returns a distinct *mcp.ServerSession identity for binding
// tests to key against - checkToolPolicy/bindRole/unbindRole only ever use
// pointer identity, never dereference the session's own fields (all
// unexported), so a bare zero-value literal is a valid, distinct key.
func fakeSession() *mcp.ServerSession {
	return &mcp.ServerSession{}
}

func TestCheckToolPolicyUnboundSessionUnaffected(t *testing.T) {
	idx := newTestToolIndexWithCapabilities(map[string]bool{"plan_save": true})
	idx.agents = fakeToolPolicyRegistry{"gareng": {ID: "gareng", ToolPolicy: agent.ToolPolicy{ReadOnly: true}}}
	if err := idx.checkToolPolicy(fakeSession(), "plan_save"); err != nil {
		t.Fatalf("checkToolPolicy on unbound session = %v, want nil (unrestricted)", err)
	}
}

func TestCheckToolPolicyNoAgentsConfiguredUnaffected(t *testing.T) {
	idx := newTestToolIndexWithCapabilities(map[string]bool{"plan_save": true})
	// idx.agents left nil.
	sess := fakeSession()
	idx.bindRole(sess, "gareng")
	if err := idx.checkToolPolicy(sess, "plan_save"); err != nil {
		t.Fatalf("checkToolPolicy with no agents configured = %v, want nil (unrestricted)", err)
	}
}

func TestCheckToolPolicyReadOnlyRoleRejectsMutatingTool(t *testing.T) {
	idx := newTestToolIndexWithCapabilities(map[string]bool{"plan_save": true, "plan_get": false})
	idx.agents = fakeToolPolicyRegistry{"gareng": {ID: "gareng", ToolPolicy: agent.ToolPolicy{ReadOnly: true}}}
	sess := fakeSession()
	idx.bindRole(sess, "gareng")

	if err := idx.checkToolPolicy(sess, "plan_save"); err == nil {
		t.Fatal("checkToolPolicy(plan_save) for read-only role = nil, want rejection")
	}
	if err := idx.checkToolPolicy(sess, "plan_get"); err != nil {
		t.Fatalf("checkToolPolicy(plan_get) for read-only role = %v, want nil (plan_get is read-only)", err)
	}
}

func TestCheckToolPolicyAllowedToolsIsAllowlist(t *testing.T) {
	idx := newTestToolIndexWithCapabilities(map[string]bool{"plan_get": false, "get_delivery": false})
	idx.agents = fakeToolPolicyRegistry{"gareng": {ID: "gareng", ToolPolicy: agent.ToolPolicy{AllowedTools: []string{"plan_get"}}}}
	sess := fakeSession()
	idx.bindRole(sess, "gareng")

	if err := idx.checkToolPolicy(sess, "plan_get"); err != nil {
		t.Fatalf("checkToolPolicy(plan_get) in AllowedTools = %v, want nil", err)
	}
	if err := idx.checkToolPolicy(sess, "get_delivery"); err == nil {
		t.Fatal("checkToolPolicy(get_delivery) not in AllowedTools = nil, want rejection")
	}
}

func TestCheckToolPolicyDeniedToolsWinsOverAllowedTools(t *testing.T) {
	idx := newTestToolIndexWithCapabilities(map[string]bool{"plan_save": true})
	idx.agents = fakeToolPolicyRegistry{"petruk": {ID: "petruk", ToolPolicy: agent.ToolPolicy{
		AllowedTools: []string{"plan_save"},
		DeniedTools:  []string{"plan_save"},
	}}}
	sess := fakeSession()
	idx.bindRole(sess, "petruk")

	if err := idx.checkToolPolicy(sess, "plan_save"); err == nil {
		t.Fatal("checkToolPolicy(plan_save) with plan_save in both Allowed and Denied = nil, want rejection (deny wins)")
	}
}

func TestUnbindRoleRemovesBinding(t *testing.T) {
	idx := newTestToolIndexWithCapabilities(map[string]bool{"plan_save": true})
	idx.agents = fakeToolPolicyRegistry{"gareng": {ID: "gareng", ToolPolicy: agent.ToolPolicy{ReadOnly: true}}}
	sess := fakeSession()
	idx.bindRole(sess, "gareng")
	if err := idx.checkToolPolicy(sess, "plan_save"); err == nil {
		t.Fatal("expected rejection while bound")
	}
	idx.unbindRole(sess)
	if err := idx.checkToolPolicy(sess, "plan_save"); err != nil {
		t.Fatalf("checkToolPolicy after unbindRole = %v, want nil (no binding)", err)
	}
}

// TestLiveEnforcementOverRealMCPConnection drives the real MCP wire
// protocol end to end over an in-memory transport (the same transport
// stdio and every real connection share the property that matters here:
// ServerSession.ID() returns "" - only Streamable HTTP populates it - so
// this also guards the binding mechanism against relying on that string).
// A session bound to gareng (using the real, embedded
// prompts/gareng/agent.yaml manifest - read_only:true,
// allowed:[plan_get, get_delivery, get_workflow, list_projects]) may call
// plan_get but is rejected calling upsert_project (mutating, not in
// gareng's allowed tools), matching gareng's real ToolPolicy exactly as
// assembleServer loads it.
func TestLiveEnforcementOverRealMCPConnection(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	server, idx, err := assembleServer(a)
	if err != nil {
		t.Fatalf("assembleServer: %v", err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })

	idx.bindRole(serverSession, "gareng")

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "upsert_project", Arguments: map[string]any{
		"slug": "x", "repository_url": "https://example.com/x.git",
	}})
	if err != nil {
		t.Fatalf("CallTool(upsert_project): %v", err)
	}
	if !res.IsError {
		t.Fatal("upsert_project for a gareng-bound session succeeded, want rejection (gareng is read-only and upsert_project is not in its allowed tools)")
	}

	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "plan_get", Arguments: map[string]any{"id": "p1"}})
	if err != nil {
		t.Fatalf("CallTool(plan_get): %v", err)
	}
	// plan_get is expected to fail for an unrelated reason (no such plan
	// exists) - not from the tool-policy rejection this test guards
	// against. Distinguish by content: a policy rejection's error text
	// names the role and "tool policy"/"read-only"; a not-found error does
	// not.
	if res.IsError {
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok && (strings.Contains(tc.Text, "tool policy") || strings.Contains(tc.Text, "read-only")) {
				t.Fatalf("plan_get for a gareng-bound session was rejected by tool policy, want it allowed (plan_get is in gareng's allowed tools): %s", tc.Text)
			}
		}
	}
}
