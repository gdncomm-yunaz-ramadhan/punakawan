package mcpserver

import "github.com/ygrip/punakawan/internal/capability"

// toolIndex embeds the capability registry so MCP registration and workflow
// capability validation share one source of truth.
type toolIndex struct {
	*capability.Registry
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
