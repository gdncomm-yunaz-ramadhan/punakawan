package validation

import "github.com/ygrip/punakawan/pkg/protocol"

// ReviewComplianceReport is §11's "Review-compliance checks" section's
// result: every open comment must have a resolution, a rejected
// resolution must explain why, and an unresolved comment blocks
// acceptance.
type ReviewComplianceReport struct {
	Passed               bool     `json:"passed"`
	Issues               []Issue  `json:"issues"`
	UnresolvedCommentIDs []string `json:"unresolved_comment_ids,omitempty"`
}

// ValidateReviewCompliance checks resolutions (the proposal's
// results.comment_resolutions) against comments (the review's actual,
// latest comment set, excluding obsolete/withdrawn ones - a withdrawn
// comment needs no resolution). It never sees or reasons about comment
// bodies or explanations' actual content - it can only check that a
// resolution exists and, where required, that an explanation string is
// non-empty; whether that explanation is a *good* one is Punakawan's
// revising agent's responsibility, not this package's.
func ValidateReviewCompliance(comments []protocol.ArtifactComment, resolutions []protocol.ArtifactRevisionProposalResultsCommentResolutionsElem) ReviewComplianceReport {
	byID := make(map[string]protocol.ArtifactRevisionProposalResultsCommentResolutionsElem, len(resolutions))
	for _, r := range resolutions {
		byID[r.CommentId] = r
	}

	var issues []Issue
	var unresolved []string
	for _, c := range comments {
		if c.Status == protocol.ArtifactCommentStatusObsolete {
			continue
		}
		res, ok := byID[c.Id]
		if !ok {
			issues = append(issues, Issue{Check: "every_comment_resolved", Message: "comment " + c.Id + " has no resolution"})
			unresolved = append(unresolved, c.Id)
			continue
		}
		if res.Status == protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusRejected && derefOrEmpty(res.Explanation) == "" {
			issues = append(issues, Issue{Check: "rejected_comments_explained", Message: "comment " + c.Id + " is rejected but has no explanation"})
		}
		if res.Status == protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusAddressed && len(res.ChangedBlockIds) == 0 {
			issues = append(issues, Issue{Check: "addressed_comments_identify_changes", Message: "comment " + c.Id + " is addressed but names no changed_block_ids"})
		}
	}

	return ReviewComplianceReport{Passed: len(issues) == 0, Issues: issues, UnresolvedCommentIDs: unresolved}
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
