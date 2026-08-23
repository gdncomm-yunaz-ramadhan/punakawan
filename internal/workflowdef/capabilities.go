package workflowdef

// CapabilitySet is the set of capability identifiers a workflow definition is
// allowed to reference: MCP tool names (from internal/capability.Registry,
// populated live from internal/mcpserver's own registrations) unioned with
// per-adapter manifest operations. This type unions both into a single
// membership check for Validate.
type CapabilitySet struct {
	names map[string]struct{}
}

// NewCapabilitySet builds a CapabilitySet from the known MCP tool names and
// the (dynamic) adapter operation identifiers. Duplicates across the two
// sources are harmless — membership is by set.
func NewCapabilitySet(mcpNames []string, adapterOps []string) CapabilitySet {
	names := make(map[string]struct{}, len(mcpNames)+len(adapterOps))
	for _, n := range mcpNames {
		names[n] = struct{}{}
	}
	for _, op := range adapterOps {
		names[op] = struct{}{}
	}
	return CapabilitySet{names: names}
}

// Has reports whether name is a registered capability in this set.
func (c CapabilitySet) Has(name string) bool {
	_, ok := c.names[name]
	return ok
}

// Len reports how many distinct capabilities the set contains.
func (c CapabilitySet) Len() int { return len(c.names) }
