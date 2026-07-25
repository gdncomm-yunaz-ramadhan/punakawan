package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/contradiction"
	"github.com/ygrip/punakawan/internal/dossier"
	"github.com/ygrip/punakawan/internal/handoff"
	"github.com/ygrip/punakawan/internal/handoffprobe"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// handoffValidationDeps wires handoff.Validate's injected lookups from the app's
// project-scoped stores (§42). Every func is optional and a nil func is treated
// as "cannot check, so passing", so we wire the subsystems we have a store for
// and leave the rest nil rather than fabricate a verdict.
func handoffValidationDeps(a *app.App) handoff.ValidationDeps {
	root := a.Workspace.Root
	return handoff.ValidationDeps{
		// The role-config revision the capsule was created under vs. now.
		RoleConfigRevision: func() (int, error) {
			cfg, err := roleconfig.Load(root)
			if err != nil {
				return 0, err
			}
			return cfg.Revision, nil
		},
		// A capsule's open contradictions have "materially changed" once they
		// leave the open lifecycle set (resolved/accepted/superseded) or vanish
		// entirely - either way the resumer's contradiction summary is stale.
		ContradictionsChanged: func(ids []string) ([]string, error) {
			var changed []string
			for _, id := range ids {
				c, err := contradiction.Get(root, id)
				if errors.Is(err, contradiction.ErrNotFound) {
					changed = append(changed, id)
					continue
				}
				if err != nil {
					return nil, err
				}
				if !openContradictionStatuses[c.Status] {
					changed = append(changed, id)
				}
			}
			return changed, nil
		},
		// Whether the capsule's dossier was superseded (a superseded dossier
		// must never resume silently, §43).
		DossierSuperseded: func(id string) (bool, error) {
			loaded, err := dossier.Get(root, id)
			if err != nil {
				return false, err
			}
			return loaded.Dossier.Status == protocol.ChangeDossierStatusSuperseded, nil
		},
		// Plans live in the durable knowledge store, so we treat the pinned plan
		// as existing when its knowledge record is present. Opened lazily so a
		// capsule that pins no plan never pays the Dolt startup cost.
		// TODO: knowledge records carry no integer plan version yet, so the
		// version argument is not yet verified here; a dedicated versioned plan
		// store would let this check the exact revision.
		PlanVersionExists: func(planID string, version int) (bool, error) {
			store, err := a.OpenKnowledge()
			if err != nil {
				return false, err
			}
			if _, err := store.Get(planID); errors.Is(err, knowledge.ErrNotFound) {
				return false, nil
			} else if err != nil {
				return false, err
			}
			return true, nil
		},
		// Repository/evidence/task probes come from internal/handoffprobe
		// (git HEAD resolvability, the evidence ledger, and a conservative
		// task-currency default). See that package for each probe's contract.
		RepositoryStateMatches: handoffprobe.RepositoryStateMatches(root),
		EvidenceExists:         handoffprobe.EvidenceExists(root),
		TaskIsCurrent:          handoffprobe.TaskIsCurrent(root),
	}
}

// CreateHandoffCapsuleInput is create_handoff_capsule's input. It references
// existing objects (plan, task, dossier, contradictions, evidence) by id rather
// than copying them, so the capsule stays small (§40).
type CreateHandoffCapsuleInput struct {
	Id                  string                               `json:"id,omitempty" jsonschema:"optional stable id; the server mints one when omitted"`
	RunId               string                               `json:"run_id" jsonschema:"the workflow run this capsule snapshots"`
	Objective           protocol.HandoffCapsuleObjective     `json:"objective" jsonschema:"the objective statement and its source refs"`
	CurrentPhase        string                               `json:"current_phase" jsonschema:"e.g. implementation, verification"`
	AcceptedPlan        *protocol.HandoffCapsuleAcceptedPlan `json:"accepted_plan,omitempty" jsonschema:"the pinned plan id and version, if a plan was accepted"`
	CurrentTask         *protocol.HandoffCapsuleCurrentTask  `json:"current_task,omitempty" jsonschema:"the in-flight task id and its next action"`
	ChangedRepositories []string                             `json:"changed_repositories,omitempty"`
	CompletedTasks      []string                             `json:"completed_tasks,omitempty"`
	OpenContradictions  []string                             `json:"open_contradictions,omitempty" jsonschema:"ids of contradictions still open at handoff time"`
	Evidence            []string                             `json:"evidence,omitempty" jsonschema:"evidence record ids"`
	UnresolvedRisks     []string                             `json:"unresolved_risks,omitempty"`
	Dossier             *protocol.HandoffCapsuleDossier      `json:"dossier,omitempty" jsonschema:"the change dossier id and status, if any"`
	CreatedBy           *protocol.HandoffCapsuleCreatedBy    `json:"created_by,omitempty" jsonschema:"the agent client and role that created the capsule"`
}

// HandoffCapsuleOutput carries a full capsule.
type HandoffCapsuleOutput struct {
	Capsule protocol.HandoffCapsule `json:"capsule"`
}

func createHandoffCapsuleHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CreateHandoffCapsuleInput) (*mcp.CallToolResult, HandoffCapsuleOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CreateHandoffCapsuleInput) (*mcp.CallToolResult, HandoffCapsuleOutput, error) {
		if err := authorizeRoleSubmit(a, roleconfig.Semar, "handoff_capsule"); err != nil {
			return nil, HandoffCapsuleOutput{}, err
		}
		id := in.Id
		if id == "" {
			id = randomLocalID("handoff")
		}
		capsule := protocol.HandoffCapsule{
			Id:                  id,
			ProjectId:           a.Workspace.ID,
			RunId:               in.RunId,
			Objective:           in.Objective,
			CurrentPhase:        in.CurrentPhase,
			AcceptedPlan:        in.AcceptedPlan,
			CurrentTask:         in.CurrentTask,
			ChangedRepositories: in.ChangedRepositories,
			CompletedTasks:      in.CompletedTasks,
			OpenContradictions:  in.OpenContradictions,
			Evidence:            in.Evidence,
			UnresolvedRisks:     in.UnresolvedRisks,
			Dossier:             in.Dossier,
			CreatedBy:           in.CreatedBy,
		}
		// Stamp the role-config revision so resume-time validation can detect a
		// role-configuration change since the capsule was created (§43).
		// Best-effort: a read failure leaves the field unset.
		if cfg, err := roleconfig.Load(a.Workspace.Root); err == nil {
			rev := cfg.Revision
			capsule.RoleConfigurationRevision = &rev
		}
		stored, err := handoff.Create(a.Workspace.Root, capsule)
		if err != nil {
			return nil, HandoffCapsuleOutput{}, fmt.Errorf("mcpserver: create handoff capsule: %w", err)
		}
		return nil, HandoffCapsuleOutput{Capsule: stored}, nil
	}
}

// HandoffCapsuleIDInput identifies a capsule by id.
type HandoffCapsuleIDInput struct {
	Id string `json:"id" jsonschema:"the handoff capsule id"`
}

func getHandoffCapsuleHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, HandoffCapsuleIDInput) (*mcp.CallToolResult, HandoffCapsuleOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in HandoffCapsuleIDInput) (*mcp.CallToolResult, HandoffCapsuleOutput, error) {
		capsule, err := handoff.Get(a.Workspace.Root, in.Id)
		if err != nil {
			return nil, HandoffCapsuleOutput{}, fmt.Errorf("mcpserver: get handoff capsule: %w", err)
		}
		return nil, HandoffCapsuleOutput{Capsule: capsule}, nil
	}
}

// ValidateHandoffCapsuleOutput carries the §42 resume verdict.
type ValidateHandoffCapsuleOutput struct {
	Status              string   `json:"status"`
	ChangesSinceHandoff []string `json:"changes_since_handoff,omitempty"`
	RequiredRefresh     []string `json:"required_refresh,omitempty"`
}

func validateHandoffCapsuleHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, HandoffCapsuleIDInput) (*mcp.CallToolResult, ValidateHandoffCapsuleOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in HandoffCapsuleIDInput) (*mcp.CallToolResult, ValidateHandoffCapsuleOutput, error) {
		res, err := handoff.Validate(a.Workspace.Root, in.Id, handoffValidationDeps(a))
		if err != nil {
			return nil, ValidateHandoffCapsuleOutput{}, fmt.Errorf("mcpserver: validate handoff capsule: %w", err)
		}
		return nil, ValidateHandoffCapsuleOutput{
			Status:              string(res.Status),
			ChangesSinceHandoff: res.ChangesSinceHandoff,
			RequiredRefresh:     res.RequiredRefresh,
		}, nil
	}
}

// ResumeFromHandoffOutput is resume_from_handoff's output: the smallest
// necessary verified context (§43) plus any refresh steps the resumer must
// take before continuing.
type ResumeFromHandoffOutput struct {
	Status          string         `json:"status"`
	Context         map[string]any `json:"context"`
	RequiredRefresh []string       `json:"required_refresh,omitempty"`
}

func resumeFromHandoffHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, HandoffCapsuleIDInput) (*mcp.CallToolResult, ResumeFromHandoffOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in HandoffCapsuleIDInput) (*mcp.CallToolResult, ResumeFromHandoffOutput, error) {
		root := a.Workspace.Root
		res, err := handoff.Validate(root, in.Id, handoffValidationDeps(a))
		if err != nil {
			return nil, ResumeFromHandoffOutput{}, fmt.Errorf("mcpserver: resume from handoff: %w", err)
		}
		switch res.Status {
		case handoff.StatusSuperseded, handoff.StatusBlocked, handoff.StatusInvalid:
			reason := "; "
			if len(res.ChangesSinceHandoff) > 0 {
				reason = ": " + fmt.Sprint(res.ChangesSinceHandoff)
			} else {
				reason = ""
			}
			return nil, ResumeFromHandoffOutput{}, fmt.Errorf("mcpserver: handoff capsule %q is not resumable (status %s)%s", in.Id, res.Status, reason)
		}
		// resumable or refresh_required: return the smallest necessary verified
		// context, plus any refresh steps for refresh_required.
		ctxMap, err := handoff.ResumeContext(root, in.Id)
		if err != nil {
			return nil, ResumeFromHandoffOutput{}, fmt.Errorf("mcpserver: build resume context: %w", err)
		}
		return nil, ResumeFromHandoffOutput{
			Status:          string(res.Status),
			Context:         ctxMap,
			RequiredRefresh: res.RequiredRefresh,
		}, nil
	}
}
