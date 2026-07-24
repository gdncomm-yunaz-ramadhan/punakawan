package mcpserver

import (
	"context"
	"testing"
)

func TestValidateRequestedBy(t *testing.T) {
	for _, v := range []string{"semar", "gareng", "petruk", "bagong"} {
		if _, err := validateRequestedBy(v); err != nil {
			t.Errorf("validateRequestedBy(%q) = %v, want nil", v, err)
		}
	}
	for _, v := range []string{"", "nobody", "Semar", "boss"} {
		if _, err := validateRequestedBy(v); err == nil {
			t.Errorf("validateRequestedBy(%q) = nil, want an error", v)
		}
	}
}

func TestValidateWorkflowName(t *testing.T) {
	for _, v := range []string{"feature-delivery", "requirement-review", "browser-flow-capture", "implementation-only", "final-review"} {
		if _, err := validateWorkflowName(v); err != nil {
			t.Errorf("validateWorkflowName(%q) = %v, want nil", v, err)
		}
	}
	for _, v := range []string{"", "feature_delivery", "delivery", "random"} {
		if _, err := validateWorkflowName(v); err == nil {
			t.Errorf("validateWorkflowName(%q) = nil, want an error", v)
		}
	}
}

func TestValidateCapsuleRole(t *testing.T) {
	for _, v := range []string{"gareng", "petruk", "bagong"} {
		if _, err := validateCapsuleRole(v); err != nil {
			t.Errorf("validateCapsuleRole(%q) = %v, want nil", v, err)
		}
	}
	// "semar" is a valid requested_by but NOT a valid context-capsule role.
	for _, v := range []string{"", "semar", "Petruk", "reviewer"} {
		if _, err := validateCapsuleRole(v); err == nil {
			t.Errorf("validateCapsuleRole(%q) = nil, want an error", v)
		}
	}
}

func TestCheckOpenAPICompatibilityConfinesPathsToWorktree(t *testing.T) {
	// punokawan-doe: base_path/head_path are resolved within the task worktree
	// and cannot escape it via ".." or be absolute host paths.
	a := newTestApp(t)
	h := checkOpenAPICompatibilityHandler(a)

	cases := []CheckOpenAPICompatibilityInput{
		{RunId: "run-1", TaskId: "task-1", RepoId: "repo-a", BasePath: "../../../etc/passwd", HeadPath: "spec.yaml"},
		{RunId: "run-1", TaskId: "task-1", RepoId: "repo-a", BasePath: "spec.yaml", HeadPath: "/etc/hosts"},
	}
	for _, in := range cases {
		if _, _, err := h(context.Background(), nil, in); err == nil {
			t.Errorf("expected an error for path-escaping input %+v", in)
		}
	}
}
