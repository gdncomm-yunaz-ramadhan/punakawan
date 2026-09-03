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
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
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

// GitHubOrgForRepository returns the organisation whose credential was
// last proven to reach repository ("owner/repo"), as remembered on the
// registered project for it.
//
// A repository owner is not always an organisation id - a credential
// holds an account of whatever name its token belongs to, so a personal
// or sub-organisation repository is reached by an organisation named
// after somewhere else entirely. Rather than re-derive that every time,
// the answer is remembered where the repository already is. A repository
// no project is registered for, or one two projects claim, remembers
// nothing: this reports what is recorded, it never guesses.
func (s *Store) GitHubOrgForRepository(ctx context.Context, repository string) (string, bool, error) {
	project, err := s.projectForGitHubRepository(ctx, repository)
	if err != nil || project == nil {
		return "", false, err
	}
	if project.Metadata == nil || project.Metadata.GithubOrg == nil {
		return "", false, nil
	}
	org := strings.TrimSpace(*project.Metadata.GithubOrg)
	return org, org != "", nil
}

// RememberGitHubOrg records that org's credential reaches repository, on
// the project registered for it. It merges one metadata field and never
// writes the host's credentials file: which organisation speaks for one
// repository is a local routing fact, not a change to what this machine
// is configured with.
func (s *Store) RememberGitHubOrg(ctx context.Context, repository, org string) error {
	org = strings.TrimSpace(org)
	if org == "" {
		return nil
	}
	project, err := s.projectForGitHubRepository(ctx, repository)
	if err != nil || project == nil {
		return err
	}
	if project.Metadata != nil && project.Metadata.GithubOrg != nil && strings.TrimSpace(*project.Metadata.GithubOrg) == org {
		return nil
	}
	_, err = s.MergeProjectMetadata(ctx, "github-org:"+project.Id+":"+org, project.Id,
		protocol.DeliveryProjectMetadata{GithubOrg: &org})
	return err
}

// projectForGitHubRepository resolves "owner/repo" to the single active
// project registered for it, or nil when zero or several claim it.
func (s *Store) projectForGitHubRepository(ctx context.Context, repository string) (*protocol.DeliveryProject, error) {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if repository == "" {
		return nil, nil
	}
	projects, err := s.FindProjectsByRepositoryURL(ctx, "https://github.com/"+repository)
	if err != nil {
		return nil, err
	}
	if len(projects) != 1 {
		return nil, nil
	}
	return &projects[0], nil
}
