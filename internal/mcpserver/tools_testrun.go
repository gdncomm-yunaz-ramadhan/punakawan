package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/testrun"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// RunTestsInputCommand is one command to run in a run_tests call.
type RunTestsInputCommand struct {
	Name string   `json:"name"`
	Args []string `json:"args,omitempty"`
	Dir  string   `json:"dir" jsonschema:"absolute path, must be within an allowed root (e.g. the task's worktree)"`
}

// RunTestsInput is run_tests's input.
type RunTestsInput struct {
	RunId    string                 `json:"run_id"`
	TaskId   string                 `json:"task_id"`
	Commands []RunTestsInputCommand `json:"commands"`
}

// RunTestsOutputResult is one command's result in run_tests's output.
type RunTestsOutputResult struct {
	Name       string   `json:"name"`
	Args       []string `json:"args,omitempty"`
	Dir        string   `json:"dir"`
	ExitCode   int      `json:"exit_code"`
	Stdout     string   `json:"stdout"`
	Stderr     string   `json:"stderr"`
	DurationMs int64    `json:"duration_ms"`
}

// RunTestsOutput is run_tests's output.
type RunTestsOutput struct {
	Results   []RunTestsOutputResult `json:"results"`
	AllPassed bool                   `json:"all_passed"`
}

func runTestsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, RunTestsInput) (*mcp.CallToolResult, RunTestsOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in RunTestsInput) (*mcp.CallToolResult, RunTestsOutput, error) {
		commands := make([]testrun.Command, len(in.Commands))
		for i, c := range in.Commands {
			commands[i] = testrun.Command{Name: c.Name, Args: c.Args, Dir: c.Dir}
		}

		report, err := testrun.Run(ctx, a.Supervisor, commands)
		if err != nil {
			return nil, RunTestsOutput{}, fmt.Errorf("mcpserver: run_tests: %w", err)
		}

		bundle, err := newEvidenceBundle(a, in.RunId, in.TaskId)
		if err != nil {
			return nil, RunTestsOutput{}, err
		}
		if err := testrun.WriteBundle(report, bundle); err != nil {
			return nil, RunTestsOutput{}, fmt.Errorf("mcpserver: write tests.json: %w", err)
		}

		ledger, err := newEvidenceLedger(a, in.RunId)
		if err != nil {
			return nil, RunTestsOutput{}, err
		}
		if _, err := evidence.RecordArtifact(ledger, in.RunId, in.TaskId, protocol.EvidenceRecordTypeTestReport, bundle, "tests.json", time.Now().UTC()); err != nil {
			return nil, RunTestsOutput{}, fmt.Errorf("mcpserver: record tests.json evidence: %w", err)
		}

		out := RunTestsOutput{Results: make([]RunTestsOutputResult, len(report.Results)), AllPassed: report.AllPassed}
		for i, r := range report.Results {
			out.Results[i] = RunTestsOutputResult{
				Name:       r.Command.Name,
				Args:       r.Command.Args,
				Dir:        r.Command.Dir,
				ExitCode:   r.ExitCode,
				Stdout:     r.Stdout,
				Stderr:     r.Stderr,
				DurationMs: r.DurationMs,
			}
		}
		return nil, out, nil
	}
}
