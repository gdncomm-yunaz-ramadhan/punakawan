package validation

import "testing"

func TestValidateStructureAcceptsAWellFormedProposal(t *testing.T) {
	base := "# Plan\n\n## Section\n\nBody.\n"
	proposed := "# Plan\n\n## Section\n\nUpdated body.\n\n### Subsection\n\nMore.\n"
	report := ValidateStructure(base, proposed, 3, 4)
	if !report.Passed {
		t.Fatalf("Passed = false, issues = %+v", report.Issues)
	}
}

func TestValidateStructureRejectsAWrongVersionIncrement(t *testing.T) {
	report := ValidateStructure("# Plan\n", "# Plan\n", 3, 5)
	if report.Passed {
		t.Fatal("Passed = true, want a version_increment failure")
	}
	if report.Issues[0].Check != "version_increment" {
		t.Fatalf("Issues[0] = %+v, want version_increment", report.Issues[0])
	}
}

func TestValidateStructureRejectsDuplicateBlockIDs(t *testing.T) {
	proposed := "# Plan\n\n<!-- pk:block:panel.security -->\n## Security\n\nBody.\n\n<!-- pk:block:panel.security -->\n## Also Security\n\nBody.\n"
	report := ValidateStructure("# Plan\n", proposed, 1, 2)
	if report.Passed {
		t.Fatal("Passed = true, want a unique_block_ids failure")
	}
	found := false
	for _, issue := range report.Issues {
		if issue.Check == "unique_block_ids" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Issues = %+v, want a unique_block_ids issue", report.Issues)
	}
}

func TestValidateStructureRejectsUnbalancedFences(t *testing.T) {
	proposed := "# Plan\n\n```go\nfunc f() {}\n"
	report := ValidateStructure("# Plan\n", proposed, 1, 2)
	if report.Passed {
		t.Fatal("Passed = true, want a balanced_fences failure")
	}
}

func TestValidateStructureRejectsSkippedHeadingLevels(t *testing.T) {
	proposed := "# Plan\n\n### Skipped H2\n\nBody.\n"
	report := ValidateStructure("# Plan\n", proposed, 1, 2)
	if report.Passed {
		t.Fatal("Passed = true, want a heading_hierarchy failure")
	}
}

func TestValidateStructureAllowsDroppingBackDownHeadingLevels(t *testing.T) {
	proposed := "# Plan\n\n## A\n\n### A.1\n\n## B\n\nBody.\n"
	report := ValidateStructure("# Plan\n", proposed, 1, 2)
	if !report.Passed {
		t.Fatalf("Passed = false, issues = %+v, want dropping back to H2 after H3 to be valid", report.Issues)
	}
}
