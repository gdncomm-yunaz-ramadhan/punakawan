// Package testrun executes caller-supplied compile/test commands through the
// existing internal/tools.Supervisor and records the results as a
// tests.json-shaped report in an evidence bundle, per
// punakawan-go-typescript-detailed-plan.md §11.3 ("compile" / "run targeted
// tests" execution-loop steps) and §17.1/§17.2 (the "test report" evidence
// type and the evidence bundle's tests.json file).
//
// This package is a thin layer on top of tools.Supervisor: it does not
// reimplement any of the Supervisor's safety controls (working-directory
// allowlist, environment allowlist, timeouts, output truncation). It is not
// a test framework — Go has no general way to know a project's test
// commands, so the caller (Petruk or another role) supplies the exact
// commands to run.
package testrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/tools"
)

// Command is one command to run, e.g. {"go", []string{"test", "./..."}, repoPath}.
type Command struct {
	Name string
	Args []string
	Dir  string
}

// CommandResult captures the outcome of running a single Command.
type CommandResult struct {
	Command    Command
	ExitCode   int
	Stdout     string
	Stderr     string
	DurationMs int64
}

// Report is the result of running a batch of Commands.
type Report struct {
	Results   []CommandResult
	AllPassed bool
}

// Run executes each command in commands via sup.Run, in order.
//
// A command that fails (starts but exits non-zero) does not stop the batch:
// Run continues on to subsequent commands so the Report reflects every
// command's outcome, not just the first failure. This mirrors how a test
// runner behaves — a caller wants to see all failing test suites in one
// pass, not just the first one alphabetically. Failures are reflected in
// each CommandResult.ExitCode and in Report.AllPassed, not by aborting early.
//
// Run only returns a non-nil error for conditions the Supervisor itself
// treats as unsupervisable: a disallowed working directory, a failure to
// start the process, or a timeout/cancellation (see tools.Supervisor.Run).
// In that case Run stops immediately and returns the error along with the
// partial Report gathered so far; the caller can inspect Report.Results for
// commands that already completed.
func Run(ctx context.Context, sup *tools.Supervisor, commands []Command) (Report, error) {
	report := Report{
		Results:   make([]CommandResult, 0, len(commands)),
		AllPassed: true,
	}

	for _, cmd := range commands {
		spec := tools.Spec{
			Name: cmd.Name,
			Args: cmd.Args,
			Dir:  cmd.Dir,
		}

		start := time.Now()
		res, err := sup.Run(ctx, spec)
		duration := time.Since(start)

		if err != nil {
			return report, fmt.Errorf("testrun: run %s %v: %w", cmd.Name, cmd.Args, err)
		}

		cr := CommandResult{
			Command:    cmd,
			ExitCode:   res.ExitCode,
			Stdout:     string(res.Stdout),
			Stderr:     string(res.Stderr),
			DurationMs: duration.Milliseconds(),
		}
		report.Results = append(report.Results, cr)
		if cr.ExitCode != 0 {
			report.AllPassed = false
		}
	}

	return report, nil
}

// MarshalJSON renders the report as tests.json-shaped JSON, per
// punakawan-go-typescript-detailed-plan.md §17.2.
func (r Report) MarshalJSON() ([]byte, error) {
	type resultJSON struct {
		Command struct {
			Name string   `json:"name"`
			Args []string `json:"args"`
			Dir  string   `json:"dir"`
		} `json:"command"`
		ExitCode   int    `json:"exit_code"`
		Stdout     string `json:"stdout"`
		Stderr     string `json:"stderr"`
		DurationMs int64  `json:"duration_ms"`
	}
	type reportJSON struct {
		Results   []resultJSON `json:"results"`
		AllPassed bool         `json:"all_passed"`
	}

	out := reportJSON{
		Results:   make([]resultJSON, len(r.Results)),
		AllPassed: r.AllPassed,
	}
	for i, res := range r.Results {
		out.Results[i].Command.Name = res.Command.Name
		out.Results[i].Command.Args = res.Command.Args
		out.Results[i].Command.Dir = res.Command.Dir
		out.Results[i].ExitCode = res.ExitCode
		out.Results[i].Stdout = res.Stdout
		out.Results[i].Stderr = res.Stderr
		out.Results[i].DurationMs = res.DurationMs
	}
	return json.MarshalIndent(out, "", "  ")
}

// WriteBundle marshals report as tests.json-shaped JSON and writes it to
// bundle's "tests.json" path.
func WriteBundle(report Report, bundle *evidence.Bundle) error {
	data, err := report.MarshalJSON()
	if err != nil {
		return fmt.Errorf("testrun: marshal report: %w", err)
	}
	path := bundle.Path("tests.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("testrun: write %s: %w", path, err)
	}
	return nil
}
