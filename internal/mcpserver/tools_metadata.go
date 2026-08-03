package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/project"
)

// SetProjectMetadataInput drives set_project_metadata: an agent-writable
// counterpart to the panel's metadata editor (internal/panel/api/metadata_handler.go),
// so a workflow's required_metadata gap (surfaced by prepare_work_context as
// a "metadata" MissingItem) can be closed by the agent itself instead of
// stalling for a human to edit project.yaml by hand.
type SetProjectMetadataInput struct {
	Key         string `json:"key" jsonschema:"metadata key to set, e.g. test.command"`
	Value       any    `json:"value,omitempty" jsonschema:"explicit value to store; omit to attempt auto-detection for known keys (currently: test.command)"`
	Description string `json:"description,omitempty" jsonschema:"human-readable description; required when the key does not already exist and could not be auto-detected"`
}

type SetProjectMetadataOutput struct {
	Key          string `json:"key"`
	Value        any    `json:"value"`
	Description  string `json:"description"`
	Revision     int    `json:"revision"`
	Action       string `json:"action"` // "created" | "updated"
	AutoDetected bool   `json:"auto_detected"`
}

func setProjectMetadataHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SetProjectMetadataInput) (*mcp.CallToolResult, SetProjectMetadataOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SetProjectMetadataInput) (*mcp.CallToolResult, SetProjectMetadataOutput, error) {
		key := strings.TrimSpace(in.Key)
		if key == "" {
			return nil, SetProjectMetadataOutput{}, fmt.Errorf("mcpserver: set_project_metadata: key is required")
		}

		root := a.Workspace.Root
		value := in.Value
		autoDetected := false
		detectedDescription := ""

		if value == nil {
			detectedValue, desc, ok := detectMetadataValue(root, key)
			if !ok {
				return nil, SetProjectMetadataOutput{}, fmt.Errorf("mcpserver: set_project_metadata: no explicit value given and %q could not be auto-detected in %s; supply value explicitly", key, root)
			}
			value = detectedValue
			detectedDescription = desc
			autoDetected = true
		}

		p, err := project.Load(root)
		if err != nil {
			return nil, SetProjectMetadataOutput{}, fmt.Errorf("mcpserver: set_project_metadata: load project: %w", err)
		}

		description := strings.TrimSpace(in.Description)
		action := "updated"
		if _, exists := p.MetadataFor(key); exists {
			var descPtr *string
			if description != "" {
				descPtr = &description
			}
			if err := p.UpdateMetadata(key, descPtr, value, p.Revision); err != nil {
				return nil, SetProjectMetadataOutput{}, fmt.Errorf("mcpserver: set_project_metadata: update: %w", err)
			}
		} else {
			action = "created"
			if description == "" {
				description = detectedDescription
			}
			if description == "" {
				return nil, SetProjectMetadataOutput{}, fmt.Errorf("mcpserver: set_project_metadata: %q is a new key and needs a description: %w", key, project.ErrMissingField)
			}
			entry := project.MetadataEntry{Key: key, Description: description, Value: value}
			if err := p.AddMetadata(entry, p.Revision); err != nil {
				return nil, SetProjectMetadataOutput{}, fmt.Errorf("mcpserver: set_project_metadata: add: %w", err)
			}
		}

		if err := project.Save(root, p, project.SaveOptions{Action: action, Key: key, Actor: "agent"}); err != nil {
			return nil, SetProjectMetadataOutput{}, fmt.Errorf("mcpserver: set_project_metadata: save: %w", err)
		}

		saved, _ := p.MetadataFor(key)
		return nil, SetProjectMetadataOutput{
			Key:          saved.Key,
			Value:        saved.Value,
			Description:  saved.Description,
			Revision:     p.Revision,
			Action:       action,
			AutoDetected: autoDetected,
		}, nil
	}
}

// metadataDetectors maps a well-known metadata key to a best-effort
// auto-detection heuristic. Detection only ever runs when the caller omits
// an explicit value; it is scoped to exactly the keys listed here - an
// unrecognized key always requires an explicit value, never a guess.
var metadataDetectors = map[string]func(root string) (value, description string, ok bool){
	"test.command": detectTestCommand,
}

func detectMetadataValue(root, key string) (value, description string, ok bool) {
	detect, known := metadataDetectors[key]
	if !known {
		return "", "", false
	}
	return detect(root)
}

var makefileTestTarget = regexp.MustCompile(`(?m)^test:`)

// detectTestCommand tries, in order, the project conventions most likely to
// name an unambiguous test command: a Makefile "test" target, a package.json
// "test" script (via the lockfile already in use to pick the package
// manager), a Go module, or a JVM build file. It stops at the first match -
// this is a conservative, fast heuristic, not a general build-system prober.
func detectTestCommand(root string) (value, description string, ok bool) {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}

	if exists("Makefile") {
		if data, err := os.ReadFile(filepath.Join(root, "Makefile")); err == nil && makefileTestTarget.Match(data) {
			return "make test", "Command used to run this project's test suite (auto-detected: Makefile test target).", true
		}
	}
	if exists("package.json") {
		if pm, ok := detectNodeTestScript(root); ok {
			return pm + " test", "Command used to run this project's test suite (auto-detected: package.json test script).", true
		}
	}
	if exists("pom.xml") {
		return "mvn test", "Command used to run this project's test suite (auto-detected: pom.xml).", true
	}
	if exists("gradlew") {
		return "./gradlew test", "Command used to run this project's test suite (auto-detected: gradlew).", true
	}
	if exists("go.mod") {
		return "go test ./...", "Command used to run this project's test suite (auto-detected: go.mod).", true
	}
	return "", "", false
}

// detectNodeTestScript reports whether package.json declares a real "test"
// script (npm's default placeholder - `echo "Error: no test specified" &&
// exit 1` - does not count) and which package manager to invoke it with,
// picked from whichever lockfile is present.
func detectNodeTestScript(root string) (packageManager string, ok bool) {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return "", false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", false
	}
	script, hasTest := pkg.Scripts["test"]
	script = strings.TrimSpace(script)
	if !hasTest || script == "" || strings.Contains(script, "no test specified") {
		return "", false
	}

	fileExists := func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}
	switch {
	case fileExists("pnpm-lock.yaml"):
		return "pnpm", true
	case fileExists("yarn.lock"):
		return "yarn", true
	default:
		return "npm", true
	}
}
