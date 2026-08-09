// githubidentity.go wires RunPreflight's pr-permissions and
// private-repository-identity checks to a real answer for GitHub
// repositories, by calling the GitHub adapter's read-only
// github.getRepository operation through an adapters.Gate. It imports
// internal/adapters directly for the Gate type - never internal/mcpserver,
// which does not exist as a dependency of this package (go list -deps
// confirms internal/delivery has no path to internal/mcpserver, and never
// should: preflight checks are core delivery-control-plane logic, not an
// MCP-tool-facing concern).
package delivery

import (
	"context"
	"encoding/json"
	"fmt"
)

// adapterGate is the minimal surface CheckGitHubRepositoryAccess needs
// from *adapters.Gate, mirroring internal/mcpserver's own narrow
// adapterGateProvider pattern (tools_createpr.go) so a test can substitute
// a fake caller instead of a real spawned GitHub adapter subprocess.
type adapterGate interface {
	Call(ctx context.Context, runID, op string, params map[string]any) (json.RawMessage, error)
}

// CheckGitHubRepositoryAccess calls github.getRepository for repoSlug
// ("owner/repo") and reports what it learned. accessible is false exactly
// when the repository 404s for the configured credential - a private repo
// the credential cannot see also 404s, so that is diagnostic information
// for a preflight check to report, not a failed call. canPush and
// canCreatePR are the same value here: this codebase's RunInLane
// worktree-per-lane design only ever creates same-repo branch PRs, so push
// access is exactly what a PR needs - there is no fork-PR case to
// distinguish.
func CheckGitHubRepositoryAccess(ctx context.Context, gate adapterGate, repoSlug string) (accessible bool, canPush bool, canCreatePR bool, detail string, err error) {
	raw, err := gate.Call(ctx, "preflight:"+repoSlug, "github.getRepository", map[string]any{"repository": repoSlug})
	if err != nil {
		return false, false, false, "", fmt.Errorf("delivery: check github repository access for %s: %w", repoSlug, err)
	}

	var result struct {
		Normalized struct {
			Accessible  bool  `json:"accessible"`
			Private     *bool `json:"private"`
			Permissions *struct {
				Admin    bool `json:"admin"`
				Maintain bool `json:"maintain"`
				Push     bool `json:"push"`
				Pull     bool `json:"pull"`
				Triage   bool `json:"triage"`
			} `json:"permissions"`
			DefaultBranch *string `json:"defaultBranch"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return false, false, false, "", fmt.Errorf("delivery: decode github.getRepository response for %s: %w", repoSlug, err)
	}

	if !result.Normalized.Accessible {
		return false, false, false, fmt.Sprintf("repository %s is not accessible to the configured GitHub credential (not found or not visible)", repoSlug), nil
	}

	push := result.Normalized.Permissions != nil && result.Normalized.Permissions.Push
	detail = fmt.Sprintf("repository %s is accessible; push access: %v", repoSlug, push)
	return true, push, push, detail, nil
}
