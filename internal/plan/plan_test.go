package plan

import "testing"

func TestIsExecutable(t *testing.T) {
	complete := PlanStep{
		Objective:          "wire the checkout API",
		TargetProjectID:    "proj-1",
		ExpectedOutcome:    "checkout calls payments v2",
		AcceptanceCriteria: []string{"checkout tests pass"},
		VerificationMethod: "go test ./checkout/...",
	}
	if !IsExecutable(complete) {
		t.Fatalf("complete step: want executable")
	}

	cases := []struct {
		name string
		step PlanStep
	}{
		{"missing objective", func() PlanStep { s := complete; s.Objective = ""; return s }()},
		{"missing expected outcome", func() PlanStep { s := complete; s.ExpectedOutcome = ""; return s }()},
		{"missing target", func() PlanStep { s := complete; s.TargetProjectID = ""; return s }()},
		{"missing acceptance criteria", func() PlanStep { s := complete; s.AcceptanceCriteria = nil; return s }()},
		{"missing verification method", func() PlanStep { s := complete; s.VerificationMethod = ""; return s }()},
		{"unresolved blocking question", func() PlanStep {
			s := complete
			s.UnresolvedBlockingQuestion = "which auth flow?"
			return s
		}()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if IsExecutable(c.step) {
				t.Fatalf("%s: want not executable", c.name)
			}
		})
	}

	t.Run("target repo id alone is enough", func(t *testing.T) {
		s := complete
		s.TargetProjectID = ""
		s.TargetRepoID = "repo-1"
		if !IsExecutable(s) {
			t.Fatalf("target repo id alone: want executable")
		}
	})
}
