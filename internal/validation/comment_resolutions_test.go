package validation

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func strp(s string) *string { return &s }

func TestValidateReviewComplianceRequiresEveryCommentResolved(t *testing.T) {
	comments := []protocol.ArtifactComment{
		{Id: "c-1", Status: protocol.ArtifactCommentStatusOpen},
	}
	report := ValidateReviewCompliance(comments, nil)
	if report.Passed {
		t.Fatal("Passed = true, want a failure for an unresolved comment")
	}
	if len(report.UnresolvedCommentIDs) != 1 || report.UnresolvedCommentIDs[0] != "c-1" {
		t.Fatalf("UnresolvedCommentIDs = %v, want [c-1]", report.UnresolvedCommentIDs)
	}
}

func TestValidateReviewComplianceIgnoresObsoleteComments(t *testing.T) {
	comments := []protocol.ArtifactComment{
		{Id: "c-1", Status: protocol.ArtifactCommentStatusObsolete},
	}
	report := ValidateReviewCompliance(comments, nil)
	if !report.Passed {
		t.Fatalf("Passed = false, issues = %+v, want obsolete comments to need no resolution", report.Issues)
	}
}

func TestValidateReviewComplianceRequiresExplanationForRejected(t *testing.T) {
	comments := []protocol.ArtifactComment{{Id: "c-1", Status: protocol.ArtifactCommentStatusOpen}}
	resolutions := []protocol.ArtifactRevisionProposalResultsCommentResolutionsElem{
		{CommentId: "c-1", Status: protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusRejected},
	}
	report := ValidateReviewCompliance(comments, resolutions)
	if report.Passed {
		t.Fatal("Passed = true, want a failure for a rejected resolution with no explanation")
	}

	resolutions[0].Explanation = strp("Out of scope for this revision.")
	report = ValidateReviewCompliance(comments, resolutions)
	if !report.Passed {
		t.Fatalf("Passed = false, issues = %+v, want it to pass once explained", report.Issues)
	}
}

func TestValidateReviewComplianceRequiresChangedBlocksForAddressed(t *testing.T) {
	comments := []protocol.ArtifactComment{{Id: "c-1", Status: protocol.ArtifactCommentStatusOpen}}
	resolutions := []protocol.ArtifactRevisionProposalResultsCommentResolutionsElem{
		{CommentId: "c-1", Status: protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusAddressed},
	}
	report := ValidateReviewCompliance(comments, resolutions)
	if report.Passed {
		t.Fatal("Passed = true, want a failure for an addressed resolution naming no changed blocks")
	}

	resolutions[0].ChangedBlockIds = []string{"panel.security"}
	report = ValidateReviewCompliance(comments, resolutions)
	if !report.Passed {
		t.Fatalf("Passed = false, issues = %+v, want it to pass once changed blocks are named", report.Issues)
	}
}

func TestValidateReviewCompliancePassesWhenFullyResolved(t *testing.T) {
	comments := []protocol.ArtifactComment{
		{Id: "c-1", Status: protocol.ArtifactCommentStatusOpen},
		{Id: "c-2", Status: protocol.ArtifactCommentStatusOpen},
	}
	resolutions := []protocol.ArtifactRevisionProposalResultsCommentResolutionsElem{
		{CommentId: "c-1", Status: protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusAddressed, ChangedBlockIds: []string{"b-1"}},
		{CommentId: "c-2", Status: protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusNotApplicable},
	}
	report := ValidateReviewCompliance(comments, resolutions)
	if !report.Passed {
		t.Fatalf("Passed = false, issues = %+v", report.Issues)
	}
}
