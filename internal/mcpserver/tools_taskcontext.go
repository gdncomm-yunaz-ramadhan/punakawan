package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/taskcontext"
)

// BuildTaskContextInput is build_task_context's input, per §11.2.
type BuildTaskContextInput struct {
	TaskId                        string   `json:"task_id"`
	RequirementId                 string   `json:"requirement_id"`
	TaskScope                     string   `json:"task_scope,omitempty"`
	TaskAcceptanceCriteria        []string `json:"task_acceptance_criteria,omitempty"`
	TaskDefinitionOfDone          string   `json:"task_definition_of_done,omitempty"`
	TaskExpectedFilesOrComponents []string `json:"task_expected_files_or_components,omitempty"`
	AffectedSymbolsAndFiles       []string `json:"affected_symbols_and_files,omitempty"`
	RequiredTests                 []string `json:"required_tests,omitempty"`
	KnownConstraints              []string `json:"known_constraints,omitempty"`
	PreviousTaskOutputs           []string `json:"previous_task_outputs,omitempty"`
	RunId                         string   `json:"run_id" jsonschema:"the run this task belongs to, for the task.yaml evidence bundle"`

	// Capability, Intent, and RequestedMetadataKeys drive project-metadata
	// selection (§4.4): only the relevant subset of the project's metadata is
	// injected into the built context, never the whole set. All three are
	// optional. A workflow step declaring required_metadata should pass those
	// keys as requested_metadata_keys so they take top priority; capability
	// selects the metadata key namespace (key == capability or "<capability>."
	// prefix) and intent selects an exactly-matching key. When none are given,
	// the selector falls back to a strict-limited slice of general project
	// context. When the workspace has no project.yaml (or no metadata),
	// nothing is injected and behavior is unchanged.
	Capability            string   `json:"capability,omitempty" jsonschema:"project-metadata key namespace to prioritize (e.g. jira); selects keys equal to it or prefixed '<capability>.'"`
	Intent                string   `json:"intent,omitempty" jsonschema:"a project-metadata key to prioritize by exact match"`
	RequestedMetadataKeys []string `json:"requested_metadata_keys,omitempty" jsonschema:"explicit project-metadata keys to include first, in order; a workflow's required_metadata belongs here"`
}

// BuildTaskContextOutput is build_task_context's structured output: the
// fresh, bounded per-task execution Context (§11.2) plus any project metadata
// selected for this task (§4.4). The Context is embedded so every existing
// field is promoted unchanged; ProjectMetadata is additive and omitted
// entirely when the workspace declares no relevant project metadata, so a
// workspace without project.yaml sees exactly the prior output shape.
type BuildTaskContextOutput struct {
	taskcontext.Context
	ProjectMetadata []MetadataContextEntry `json:"project_metadata,omitempty"`
}

func buildTaskContextHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, BuildTaskContextInput) (*mcp.CallToolResult, BuildTaskContextOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in BuildTaskContextInput) (*mcp.CallToolResult, BuildTaskContextOutput, error) {
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, BuildTaskContextOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}

		// The evidence bundle path is deterministic per (run_id, task_id) and
		// NewBundle only ensures the directory exists, so opening it before
		// Build - to check for a prior task.yaml - is safe even on a task's
		// very first call (nothing to find yet).
		bundle, err := newEvidenceBundle(a, in.RunId, in.TaskId)
		if err != nil {
			return nil, BuildTaskContextOutput{}, err
		}
		previous, found, err := taskcontext.ReadFromBundle(bundle)
		if err != nil {
			return nil, BuildTaskContextOutput{}, fmt.Errorf("mcpserver: read prior task context: %w", err)
		}

		buildInput := taskcontext.BuildInput{
			TaskID:                        in.TaskId,
			RequirementID:                 in.RequirementId,
			TaskScope:                     in.TaskScope,
			TaskAcceptanceCriteria:        in.TaskAcceptanceCriteria,
			TaskDefinitionOfDone:          in.TaskDefinitionOfDone,
			TaskExpectedFilesOrComponents: in.TaskExpectedFilesOrComponents,
			AffectedSymbolsAndFiles:       in.AffectedSymbolsAndFiles,
			RequiredTests:                 in.RequiredTests,
			KnownConstraints:              in.KnownConstraints,
			PreviousTaskOutputs:           in.PreviousTaskOutputs,
		}
		if found {
			buildInput.Previous = &previous
		}

		built, err := taskcontext.Build(ctx, store, buildInput)
		if err != nil {
			return nil, BuildTaskContextOutput{}, fmt.Errorf("mcpserver: build task context: %w", err)
		}

		// Persist only the plain per-task Context as task.yaml evidence (§17.2):
		// the bundle format is unchanged. Project metadata (§4.4) is selected
		// fresh from the live project.yaml on every call and injected into the
		// agent-facing result only, not baked into the evidence bundle.
		if err := taskcontext.WriteToBundle(built, bundle); err != nil {
			return nil, BuildTaskContextOutput{}, fmt.Errorf("mcpserver: write task.yaml: %w", err)
		}

		// Inject the bounded, relevant subset of the project's metadata (§4.4).
		// The task's own capability/intent/requested keys (a workflow's
		// required_metadata belongs in requested_metadata_keys) drive selection;
		// with none supplied the selector returns general project context under
		// a strict limit. Safe and additive: a workspace without project.yaml
		// yields no entries and the field is omitted.
		metadata, err := selectProjectMetadata(a.Workspace.Root, in.Capability, in.Intent, in.RequestedMetadataKeys)
		if err != nil {
			return nil, BuildTaskContextOutput{}, err
		}

		return nil, BuildTaskContextOutput{Context: built, ProjectMetadata: metadata}, nil
	}
}
