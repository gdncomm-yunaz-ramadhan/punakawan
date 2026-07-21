package testrun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/tools"
)

func TestRunAllCommandsSucceed(t *testing.T) {
	dir := t.TempDir()
	sup := tools.New(dir)

	commands := []Command{
		{Name: "true", Dir: dir},
		{Name: "printf", Args: []string{"hello"}, Dir: dir},
	}

	report, err := Run(context.Background(), sup, commands)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.AllPassed {
		t.Fatal("expected AllPassed to be true")
	}
	if len(report.Results) != 2 {
		t.Fatalf("results: got %d, want 2", len(report.Results))
	}
	for _, res := range report.Results {
		if res.ExitCode != 0 {
			t.Fatalf("command %s: exit code %d, want 0", res.Command.Name, res.ExitCode)
		}
	}
	if report.Results[1].Stdout != "hello" {
		t.Fatalf("stdout: got %q, want %q", report.Results[1].Stdout, "hello")
	}
}

func TestRunOneCommandFailsButOthersStillRun(t *testing.T) {
	dir := t.TempDir()
	sup := tools.New(dir)

	commands := []Command{
		{Name: "true", Dir: dir},
		{Name: "false", Dir: dir},
		{Name: "printf", Args: []string{"still-ran"}, Dir: dir},
	}

	report, err := Run(context.Background(), sup, commands)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.AllPassed {
		t.Fatal("expected AllPassed to be false")
	}
	if len(report.Results) != 3 {
		t.Fatalf("results: got %d, want 3 (all commands should run despite the failure)", len(report.Results))
	}
	if report.Results[0].ExitCode != 0 {
		t.Fatalf("command 0 (true): exit code %d, want 0", report.Results[0].ExitCode)
	}
	if report.Results[1].ExitCode == 0 {
		t.Fatal("command 1 (false): expected non-zero exit code")
	}
	if report.Results[2].ExitCode != 0 {
		t.Fatalf("command 2 (printf): exit code %d, want 0", report.Results[2].ExitCode)
	}
	if report.Results[2].Stdout != "still-ran" {
		t.Fatalf("command 2 stdout: got %q, want %q (should have run after command 1 failed)", report.Results[2].Stdout, "still-ran")
	}
}

func TestSpecForWrapsWithRTKWhenEnabled(t *testing.T) {
	cmd := Command{Name: "go", Args: []string{"test", "./..."}, Dir: "/repo"}

	spec := specFor(cmd, true)
	if spec.Name != "rtk" {
		t.Fatalf("Name: got %q, want %q", spec.Name, "rtk")
	}
	want := []string{"go", "test", "./..."}
	if !reflect.DeepEqual(spec.Args, want) {
		t.Fatalf("Args: got %v, want %v", spec.Args, want)
	}
	if spec.Dir != cmd.Dir {
		t.Fatalf("Dir: got %q, want %q", spec.Dir, cmd.Dir)
	}
}

func TestSpecForRunsDirectlyWhenRTKDisabled(t *testing.T) {
	cmd := Command{Name: "go", Args: []string{"test", "./..."}, Dir: "/repo"}

	spec := specFor(cmd, false)
	if spec.Name != "go" {
		t.Fatalf("Name: got %q, want %q", spec.Name, "go")
	}
	if !reflect.DeepEqual(spec.Args, cmd.Args) {
		t.Fatalf("Args: got %v, want %v", spec.Args, cmd.Args)
	}
}

func TestRunDisallowedDirStopsAndReturnsError(t *testing.T) {
	allowed := t.TempDir()
	other := t.TempDir()
	sup := tools.New(allowed)

	commands := []Command{
		{Name: "true", Dir: other},
	}

	report, err := Run(context.Background(), sup, commands)
	if err == nil {
		t.Fatal("expected error for a working directory outside the allowlist")
	}
	if len(report.Results) != 0 {
		t.Fatalf("results: got %d, want 0", len(report.Results))
	}
}

func TestWriteBundleWritesTestsJSON(t *testing.T) {
	dir := t.TempDir()
	sup := tools.New(dir)

	commands := []Command{
		{Name: "true", Dir: dir},
		{Name: "false", Dir: dir},
	}

	report, err := Run(context.Background(), sup, commands)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	workspaceRoot := t.TempDir()
	bundle, err := evidence.NewBundle(workspaceRoot, "run-test", "task-test")
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}

	if err := WriteBundle(report, bundle); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	wantPath := filepath.Join(workspaceRoot, ".punakawan", "evidence", "run-test", "task-test", "tests.json")
	gotPath := bundle.Path("tests.json")
	if gotPath != wantPath {
		t.Fatalf("bundle.Path: got %q, want %q", gotPath, wantPath)
	}

	data, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var decoded struct {
		Results []struct {
			Command struct {
				Name string   `json:"name"`
				Args []string `json:"args"`
				Dir  string   `json:"dir"`
			} `json:"command"`
			ExitCode   int    `json:"exit_code"`
			Stdout     string `json:"stdout"`
			Stderr     string `json:"stderr"`
			DurationMs int64  `json:"duration_ms"`
		} `json:"results"`
		AllPassed bool `json:"all_passed"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal tests.json: %v", err)
	}

	if decoded.AllPassed {
		t.Fatal("expected all_passed to be false in tests.json")
	}
	if len(decoded.Results) != 2 {
		t.Fatalf("results in tests.json: got %d, want 2", len(decoded.Results))
	}
	if decoded.Results[0].Command.Name != "true" {
		t.Fatalf("results[0].command.name: got %q, want %q", decoded.Results[0].Command.Name, "true")
	}
	if decoded.Results[1].Command.Name != "false" {
		t.Fatalf("results[1].command.name: got %q, want %q", decoded.Results[1].Command.Name, "false")
	}
	if decoded.Results[1].ExitCode == 0 {
		t.Fatal("results[1].exit_code: expected non-zero")
	}
}
