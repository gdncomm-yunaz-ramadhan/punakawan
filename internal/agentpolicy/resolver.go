package agentpolicy

// Restriction is a workflow definition's constraint on top of a project's
// purpose policy, mirroring internal/roleconfig.Restriction's role for role
// permissions: every field, if set, narrows the effective policy - it can
// pin a specific model/strategy/type in place of whatever the project
// configured, force isolation on, or cap reasoning at a lower effort - but
// it can never loosen anything the project itself configured. A nil
// Restriction means "no workflow restriction", exactly like a nil
// roleconfig.Restriction means no workflow narrows a role's permissions.
type Restriction struct {
	Model     *string
	Reasoning *string
	Strategy  *string
	Type      *string
	// ForceIsolated, if true, sets the effective policy's Isolated to true.
	// There is no field to force it false, since a restriction may only
	// narrow what the project configured, never loosen it.
	ForceIsolated bool
}

// reasoningRank orders the three conventional reasoning-effort labels so a
// restriction can act as a ceiling: low(0) < medium(1) < high(2). An
// unrecognized label ranks as low, the most conservative, so a corrupt or
// unfamiliar value fails closed rather than being treated as unlimited.
func reasoningRank(r string) int {
	switch r {
	case "high":
		return 2
	case "medium":
		return 1
	default:
		return 0
	}
}

// Effective intersects pp with an optional workflow restriction, following
// the same "a workflow may only narrow, never widen" rule
// internal/roleconfig.Effective enforces for role permissions: Reasoning is
// clamped to the lower of pp's own effort and the restriction's ceiling,
// ForceIsolated can only turn isolation on, and Model/Strategy/Type, when
// the restriction sets them, pin the call to that specific choice in place
// of whatever the project configured - replacing a project's open-ended
// setting (e.g. "inherit") with one specific value is itself a narrowing,
// not a grant of new freedom.
func Effective(pp PurposePolicy, restriction *Restriction) PurposePolicy {
	eff := pp
	if restriction == nil {
		return eff
	}
	if restriction.Model != nil {
		eff.Model = *restriction.Model
	}
	if restriction.Reasoning != nil && reasoningRank(*restriction.Reasoning) < reasoningRank(eff.Reasoning) {
		eff.Reasoning = *restriction.Reasoning
	}
	if restriction.Strategy != nil {
		eff.Strategy = *restriction.Strategy
	}
	if restriction.Type != nil {
		eff.Type = *restriction.Type
	}
	if restriction.ForceIsolated {
		eff.Isolated = true
	}
	return eff
}

// Resolver implements the same Load-a-project's-config-then-narrow-it
// pattern as internal/roleconfig.Resolver, but for agent execution policy
// instead of role permissions: Load resolves a project id to its persisted
// Config, and Restrictions optionally resolves a workflow definition's
// per-purpose restriction on top of it. Keeping both as funcs keeps this
// package free of any dependency on the panel registry or the workflow
// definition store, exactly like roleconfig.Resolver.
type Resolver struct {
	// Load returns the persisted agent execution policy for a project id.
	Load func(projectID string) (*Config, error)
	// Restrictions returns the per-purpose restriction a workflow imposes,
	// or nil. It may be nil itself, in which case no workflow ever
	// restricts.
	Restrictions func(projectID, workflowID, purpose string) (*Restriction, error)
}

// EffectivePolicy returns purpose's configured policy for project after
// applying the given workflow's restriction. workflowID may be empty for
// "no workflow".
func (r Resolver) EffectivePolicy(projectID, workflowID, purpose string) (PurposePolicy, error) {
	cfg, err := r.Load(projectID)
	if err != nil {
		return PurposePolicy{}, err
	}
	pp, err := cfg.PurposePolicy(purpose)
	if err != nil {
		return PurposePolicy{}, err
	}
	var restriction *Restriction
	if workflowID != "" && r.Restrictions != nil {
		restriction, err = r.Restrictions(projectID, workflowID, purpose)
		if err != nil {
			return PurposePolicy{}, err
		}
	}
	return Effective(pp, restriction), nil
}
