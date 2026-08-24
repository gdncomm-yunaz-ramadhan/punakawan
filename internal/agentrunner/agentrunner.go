// Package agentrunner defines the AgentRunner abstraction Punakawan uses to
// talk about invoking an agent for a piece of work (orchestration,
// implementation, or review), and provides the one honest implementation of
// it. Punakawan never calls a model or spawns a subagent itself - every
// review, plan, and implementation is actually produced by whatever agent
// (a Claude Code session or similar) is connected as an MCP client, which
// calls back into Punakawan's own tools to report what it found. AgentRunner
// exists so the rest of the codebase has one place to reason about "run this
// purpose of work," but CapabilityRunner, the only concrete implementation,
// never pretends Punakawan can do that in-process: Run always refuses,
// naming the missing capability when the request needs more than the
// project's own execution policy (internal/agentpolicy) declares its
// connected harness supports, and explaining plainly that there is no
// in-process execution to perform otherwise.
package agentrunner

import (
	"context"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/agentpolicy"
)

// Capabilities is what a connected agent harness can actually do, as
// declared by the project operator in their execution policy - Punakawan
// has no way to detect any of this itself.
type Capabilities struct {
	// Fork reports whether the harness can dispatch forked/parallel workers.
	Fork bool
	// ModelSelection reports whether the harness can honor a request to use
	// a specific model rather than whatever it defaults to.
	ModelSelection bool
	// ReasoningControl reports whether the harness can honor a requested
	// reasoning effort level.
	ReasoningControl bool
	// IsolatedContext reports whether the harness can run a request in a
	// context isolated from the conversation that triggered it.
	IsolatedContext bool
}

// Purpose names one of the three kinds of work a caller may ask an
// AgentRunner to carry out. The values match internal/agentpolicy's Purpose
// constants exactly, so a Request's Purpose and a project's per-purpose
// policy never need translating between two different vocabularies.
type Purpose string

const (
	PurposeOrchestrate Purpose = "orchestrate"
	PurposeImplement   Purpose = "implement"
	PurposeReview      Purpose = "review"
)

// Request describes one piece of work a caller wants carried out. Model,
// Reasoning, and Isolated are the already-resolved values the caller wants
// applied (typically internal/agentpolicy's Effective policy for this
// purpose) - Run's job is only to check those resolved values against what
// the project's harness has declared it can actually do, not to resolve
// them itself.
type Request struct {
	Purpose      Purpose
	ProjectID    string
	RepoID       string
	WorktreePath string
	PlanID       string
	PlanRevision int
	Model        string
	Reasoning    string
	Isolated     bool
}

// Result is what a caller gets back from a successful Run. It stays
// minimal because the one concrete AgentRunner in this codebase never
// actually reaches a success path - see CapabilityRunner.Run - so there is
// nothing yet that needs reporting beyond a short note.
type Result struct {
	// Note is a short, human-readable explanation of what happened. Any
	// non-nil error from Run is authoritative; Note is not a substitute for
	// checking it.
	Note string
}

// AgentRunner is how the rest of Punakawan asks for a purpose of work to be
// carried out. Capabilities reports what the underlying harness can do
// without needing a full Request; Run attempts the work itself.
type AgentRunner interface {
	// Capabilities reports the capabilities this runner's connected harness
	// has declared it supports.
	Capabilities(ctx context.Context) Capabilities
	// Run attempts req. The only concrete implementation in this codebase,
	// CapabilityRunner, always returns a non-nil error: either naming a
	// declared-unsupported capability the request needs, or explaining that
	// Punakawan has no in-process way to run an agent at all.
	Run(ctx context.Context, req Request) (Result, error)
}

// CapabilityRunner is the one honest AgentRunner implementation this
// codebase has. It is bound to a single project at construction (matching
// Capabilities' signature, which - unlike Run - carries no per-request
// project id of its own), and validates every Run request against that
// project's own declared capability set before ever getting to the point
// of admitting there is nothing here to actually execute the work.
type CapabilityRunner struct {
	// ProjectID is the project this runner reports capabilities for and
	// validates every Run request against.
	ProjectID string
	// Load returns the persisted execution policy for a project id. It is
	// required - a nil Load makes every call fail with a clear error rather
	// than silently treating the project as having declared nothing.
	Load func(projectID string) (*agentpolicy.Config, error)
}

func (r CapabilityRunner) policy() (*agentpolicy.Config, error) {
	if r.Load == nil {
		return nil, fmt.Errorf("agentrunner: no execution policy loader configured for project %q", r.ProjectID)
	}
	cfg, err := r.Load(r.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("agentrunner: load execution policy for project %q: %w", r.ProjectID, err)
	}
	return cfg, nil
}

func capabilitiesFromPolicy(cfg *agentpolicy.Config) Capabilities {
	return Capabilities{
		Fork:             cfg.Capabilities.Fork,
		ModelSelection:   cfg.Capabilities.ModelSelection,
		ReasoningControl: cfg.Capabilities.ReasoningControl,
		IsolatedContext:  cfg.Capabilities.IsolatedContext,
	}
}

// Capabilities reports the capability set r.ProjectID's execution policy
// declares. A load failure reports every capability as unsupported rather
// than returning an error - the interface gives Capabilities no way to
// surface one - since a project this runner cannot even read its policy for
// has, as far as anything here can tell, declared nothing; Run reloads the
// same policy and surfaces the real error there instead.
func (r CapabilityRunner) Capabilities(ctx context.Context) Capabilities {
	cfg, err := r.policy()
	if err != nil {
		return Capabilities{}
	}
	return capabilitiesFromPolicy(cfg)
}

// Run validates req against r.ProjectID's declared capabilities and then
// always refuses to execute: Punakawan has no way to invoke a model
// directly, so there is nothing here that could carry req out even once
// every capability check passes. A capability-mismatch error is returned
// in preference to the generic "no execution engine" error whenever req
// asks for something the project's own policy says its harness cannot do,
// since that is the more specific and more actionable problem for the
// caller to fix.
func (r CapabilityRunner) Run(ctx context.Context, req Request) (Result, error) {
	if req.ProjectID != "" && req.ProjectID != r.ProjectID {
		return Result{}, fmt.Errorf("agentrunner: request project %q does not match this runner's bound project %q", req.ProjectID, r.ProjectID)
	}

	cfg, err := r.policy()
	if err != nil {
		return Result{}, err
	}
	caps := capabilitiesFromPolicy(cfg)

	purposePolicy, err := cfg.PurposePolicy(string(req.Purpose))
	if err != nil {
		return Result{}, fmt.Errorf("agentrunner: %w", err)
	}

	var missing []string
	if req.Isolated && !caps.IsolatedContext {
		missing = append(missing, "isolated_context")
	}
	if req.Model != "" && req.Model != "inherit" && !caps.ModelSelection {
		missing = append(missing, "model_selection")
	}
	if req.Reasoning != "" && !caps.ReasoningControl {
		missing = append(missing, "reasoning_control")
	}
	if purposePolicy.Strategy == "fork" && !caps.Fork {
		missing = append(missing, "fork")
	}
	if len(missing) > 0 {
		return Result{}, fmt.Errorf(
			"agentrunner: project %q declared it does not support %s, but this %s request needs it: refusing rather than pretending the request was honored",
			r.ProjectID, strings.Join(missing, ", "), req.Purpose,
		)
	}

	return Result{}, fmt.Errorf(
		"agentrunner: cannot run this %s request in-process: Punakawan has no way to invoke a model or spawn a subagent itself - the MCP client already connected to this session is the one actually running the agent, and must perform this work itself and report the outcome back through the normal review/plan submission tools rather than asking Punakawan to run it",
		req.Purpose,
	)
}
