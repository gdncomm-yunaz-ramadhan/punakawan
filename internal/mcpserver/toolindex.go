package mcpserver

import (
	"fmt"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ygrip/punakawan/internal/agent"
	"github.com/ygrip/punakawan/internal/capability"
)

// toolIndex embeds the capability registry so MCP registration and workflow
// capability validation share one source of truth. It also carries the
// optional live tool-policy enforcement state: agents resolves a bound
// session's role, and roleBindings is the *mcp.ServerSession -> roleID
// table a delivery session binds/unbinds itself into (see
// tools_sessions.go). Both are nil-safe - a toolIndex with no agents
// configured enforces nothing, preserving every call path that predates
// this.
//
// Bindings are keyed by *mcp.ServerSession pointer identity, not
// ServerSession.ID(): ID() returns "" for stdio and in-memory transports
// (it only resolves for a transport implementing the SDK's internal
// hasSessionID, which today is Streamable HTTP only - see
// mcp/server.go:1092) - but Punakawan's only production transport today is
// stdio. Pointer identity is stable for a connection's lifetime regardless
// of transport, so it works uniformly everywhere ID() would return "".
type toolIndex struct {
	*capability.Registry
	agents       agent.AgentRegistry
	roleBindings sync.Map // *mcp.ServerSession -> roleID string
}

func newToolIndex() *toolIndex {
	return &toolIndex{
		Registry: capability.NewRegistry(),
	}
}

// toolIndexFrom wraps an already-populated capability.Registry.
func toolIndexFrom(reg *capability.Registry) *toolIndex {
	return &toolIndex{
		Registry: reg,
	}
}

// bindRole records that session is now acting as roleID, so subsequent
// tool calls on that same MCP connection are checked against roleID's
// ToolPolicy. Called once a delivery session's declared Participant
// resolves to a known role (tools_sessions.go's startDeliverySessionHandler).
func (t *toolIndex) bindRole(session *mcp.ServerSession, roleID string) {
	if t == nil || session == nil {
		return
	}
	t.roleBindings.Store(session, roleID)
}

// unbindRole removes session's role binding, if any. Called when a
// delivery session finalizes (tools_sessions.go's
// finalizeDeliverySessionHandler). A session that disconnects without ever
// finalizing just leaves one harmless stale entry - this is advisory
// defense-in-depth, not a security boundary, so no reaping mechanism exists
// beyond this explicit unbind.
func (t *toolIndex) unbindRole(session *mcp.ServerSession) {
	if t == nil || session == nil {
		return
	}
	t.roleBindings.Delete(session)
}

// checkToolPolicy enforces toolName against session's bound role, if any.
// No binding (the overwhelming majority of calls, including every call on a
// connection that never started a delivery session) or no agents registry
// configured means unrestricted - nil returned. An unresolvable roleID
// (should not happen once bound, since binding itself required a
// successful agents.Get) also fails open rather than blocking every
// subsequent call on the session.
func (t *toolIndex) checkToolPolicy(session *mcp.ServerSession, toolName string) error {
	if t == nil || t.agents == nil || session == nil {
		return nil
	}
	roleIDValue, ok := t.roleBindings.Load(session)
	if !ok {
		return nil
	}
	roleID := roleIDValue.(string)
	spec, err := t.agents.Get(roleID)
	if err != nil {
		return nil
	}
	policy := spec.ToolPolicy
	for _, denied := range policy.DeniedTools {
		if denied == toolName {
			return fmt.Errorf("mcpserver: role %q may not call tool %q: denied by tool policy", roleID, toolName)
		}
	}
	if len(policy.AllowedTools) > 0 {
		allowed := false
		for _, name := range policy.AllowedTools {
			if name == toolName {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("mcpserver: role %q may not call tool %q: not in allowed tools", roleID, toolName)
		}
	}
	if policy.ReadOnly {
		if desc, ok := t.Registry.Lookup(toolName); ok && desc.Mutates {
			return fmt.Errorf("mcpserver: role %q is read-only and may not call mutating tool %q", roleID, toolName)
		}
	}
	return nil
}
