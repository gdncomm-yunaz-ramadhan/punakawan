package delivery

import (
	"context"
	"os/exec"
	"strings"

	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// RunPreflight computes every capability check this task defines for
// profile, using inspector for the git-specific checks (reusing
// internal/gitops rather than re-implementing remote/push detection).
// A check is only ever reported pass or fail if it was actually
// evaluated; anything this package cannot yet verify (PR permissions,
// CI visibility, external service reachability, subprocess
// restrictions, quality-tool configuration) is reported skipped with an
// explanation, never a fabricated pass.
func RunPreflight(ctx context.Context, profile *protocol.ProjectDeliveryProfile, inspector *gitops.Inspector) []protocol.PreflightCheck {
	var checks []protocol.PreflightCheck
	add := func(name string, classification protocol.PreflightCheckClassification, status protocol.PreflightCheckStatus, detail string) {
		c := protocol.PreflightCheck{Name: name, Classification: classification, Status: status}
		if detail != "" {
			c.Detail = &detail
		}
		checks = append(checks, c)
	}

	// git is required for every project's execution lifecycle regardless
	// of what the profile happens to list in RequiredExecutables, so it
	// is checked unconditionally here rather than depending on the
	// profile author having remembered to add it.
	if _, err := exec.LookPath("git"); err != nil {
		add("executable:git", protocol.PreflightCheckClassificationRequired, protocol.PreflightCheckStatusFail, err.Error())
	} else {
		add("executable:git", protocol.PreflightCheckClassificationRequired, protocol.PreflightCheckStatusPass, "")
	}

	for _, name := range profile.RequiredExecutables {
		if _, err := exec.LookPath(name); err != nil {
			add("executable:"+name, protocol.PreflightCheckClassificationRequired, protocol.PreflightCheckStatusFail, err.Error())
		} else {
			add("executable:"+name, protocol.PreflightCheckClassificationRequired, protocol.PreflightCheckStatusPass, "")
		}
	}
	for _, cmd := range []string{firstToken(stringOrEmpty(profile.BuildCommand)), firstToken(stringOrEmpty(profile.TestCommand))} {
		if cmd == "" {
			continue
		}
		if _, err := exec.LookPath(cmd); err != nil {
			add("executable:"+cmd, protocol.PreflightCheckClassificationRequired, protocol.PreflightCheckStatusFail, err.Error())
		} else {
			add("executable:"+cmd, protocol.PreflightCheckClassificationRequired, protocol.PreflightCheckStatusPass, "")
		}
	}

	localPath := ""
	if profile.LocalPath != nil {
		localPath = *profile.LocalPath
	}
	if localPath == "" || inspector == nil {
		add("repository-access", protocol.PreflightCheckClassificationRequired, protocol.PreflightCheckStatusSkipped, "no local checkout to inspect yet (worktree not created)")
	} else {
		root, err := inspector.RepositoryRoot(ctx, localPath)
		if err != nil {
			add("repository-access", protocol.PreflightCheckClassificationRequired, protocol.PreflightCheckStatusFail, err.Error())
		} else {
			add("repository-access", protocol.PreflightCheckClassificationRequired, protocol.PreflightCheckStatusPass, root)
		}

		remote := "origin"
		if profile.CanonicalRemote != nil && *profile.CanonicalRemote != "" {
			remote = *profile.CanonicalRemote
		}
		if ok, reason, err := inspector.CanPush(ctx, localPath, remote, profile.BaseBranch); err != nil {
			add("push-dry-run", protocol.PreflightCheckClassificationRequired, protocol.PreflightCheckStatusFail, err.Error())
		} else if !ok {
			add("push-dry-run", protocol.PreflightCheckClassificationRequired, protocol.PreflightCheckStatusFail, reason)
		} else {
			add("push-dry-run", protocol.PreflightCheckClassificationRequired, protocol.PreflightCheckStatusPass, "")
		}
	}

	// Not implemented anywhere in this codebase yet (internal/gitops's own
	// DetectCapabilities comments flag PR-permission checking the same
	// way): reported skipped rather than a fabricated pass, matching this
	// task's failure-behavior requirement to fail early and honestly.
	add("pr-permissions", protocol.PreflightCheckClassificationDelegatedToCi, protocol.PreflightCheckStatusSkipped, "no provider adapter implements PR-permission checking yet")
	add("private-repository-identity", protocol.PreflightCheckClassificationDelegatedToCi, protocol.PreflightCheckStatusSkipped, "no provider adapter implements identity checking yet")
	add("ci-visibility", protocol.PreflightCheckClassificationDelegatedToCi, protocol.PreflightCheckStatusSkipped, "CI adapter reachability is not implemented yet")

	for _, svc := range profile.RequiredServices {
		add("external-service:"+svc, protocol.PreflightCheckClassificationOptional, protocol.PreflightCheckStatusSkipped, "external service reachability is not implemented yet")
	}
	for _, rule := range profile.QualityRules {
		add("quality-rule:"+rule, protocol.PreflightCheckClassificationOptional, protocol.PreflightCheckStatusSkipped, "quality configuration validation is not implemented yet")
	}

	return checks
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func firstToken(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
