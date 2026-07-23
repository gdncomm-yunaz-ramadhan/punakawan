package artifact

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// precedenceMarkdown gives every resolution step in §6's fallback chain a
// distinct, independently-matchable target, so a test can construct an
// anchor that *could* be resolved by more than one step and confirm
// ResolveAnchor stops at the earliest one in order, per §6's "1. Exact
// block ID. 2. Exact anchored-block content hash. 3. Heading path plus
// quoted text. 4. Heading path plus limited fuzzy quoted-text match. 5.
// Mark as conflicted."
const precedenceMarkdown = `# Doc

## Alpha

<!-- pk:block:doc.alpha.target -->
The exact target sentence.

<!-- pk:block:doc.alpha.decoy -->
A decoy sentence that also mentions target loosely.
`

func TestResolveAnchorPrefersBlockIDOverContentHashWhenBothCouldMatch(t *testing.T) {
	// This anchor names the decoy block by ID, but also carries
	// QuotedText that exactly matches the *target* block's content. If
	// content-hash matching (step 2) ran first, it would resolve to the
	// target block instead of the decoy. Per §6's fixed order, the exact
	// block ID (step 1) must win.
	anchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BaseRevisionHash: Hash([]byte(precedenceMarkdown)),
		BlockId:          strp("doc.alpha.decoy"),
		QuotedText:       strp("The exact target sentence."),
	}
	block, method := ResolveAnchor(precedenceMarkdown, anchor)
	if method != AnchorResolvedByBlockID {
		t.Fatalf("method = %q, want block_id to win when both a block id and a matching quoted_text are present", method)
	}
	if block.ID != "doc.alpha.decoy" {
		t.Fatalf("block.ID = %q, want doc.alpha.decoy (the anchor's own block id, not the quoted-text match)", block.ID)
	}
}

func TestResolveAnchorFallsBackToContentHashWhenBlockIDIsUnresolvable(t *testing.T) {
	// BlockId names a block that no longer exists (e.g. renamed), but
	// QuotedText still exactly matches a live block's content - step 1
	// fails to resolve, so step 2 must be tried next rather than
	// immediately conflicting.
	anchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BaseRevisionHash: Hash([]byte(precedenceMarkdown)),
		BlockId:          strp("doc.alpha.renamed-away"),
		QuotedText:       strp("The exact target sentence."),
	}
	block, method := ResolveAnchor(precedenceMarkdown, anchor)
	if method != AnchorResolvedByContentHash {
		t.Fatalf("method = %q, want content_hash once the named block id no longer exists", method)
	}
	if block.ID != "doc.alpha.target" {
		t.Fatalf("block.ID = %q, want doc.alpha.target", block.ID)
	}
}

func TestResolveAnchorPrefersContentHashOverHeadingFuzzyMatchWhenBothCouldMatch(t *testing.T) {
	// QuotedText exactly matches the target block's full content (an
	// exact content-hash hit, step 2), while HeadingPath happens to also
	// be consistent with the decoy block, which fuzzy-matches a reworded
	// substring of the same words (step 3/4). Step 2 must win since it
	// runs first and it is an unambiguous exact match.
	anchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BaseRevisionHash: Hash([]byte(precedenceMarkdown)),
		QuotedText:       strp("The exact target sentence."),
		HeadingPath:      []string{"Doc", "Alpha"},
	}
	block, method := ResolveAnchor(precedenceMarkdown, anchor)
	if method != AnchorResolvedByContentHash {
		t.Fatalf("method = %q, want content_hash to win over a heading+quoted-text match when the quoted text is an exact whole-block match", method)
	}
	if block.ID != "doc.alpha.target" {
		t.Fatalf("block.ID = %q, want doc.alpha.target", block.ID)
	}
}

func TestResolveAnchorPrefersExactHeadingQuotedTextOverFuzzyMatch(t *testing.T) {
	// QuotedText is a verbatim substring of the target block (satisfies
	// step 3's strings.Contains exactly, no normalization needed) while
	// also being fuzzy-matchable (trivially, since exact containment
	// implies normalized containment too). This confirms step 3 is tried
	// - and succeeds - before step 4 is ever reached, by checking the
	// reported method is the exact step, not the fuzzy one.
	anchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BaseRevisionHash: Hash([]byte(precedenceMarkdown)),
		HeadingPath:      []string{"Doc", "Alpha"},
		QuotedText:       strp("exact target sentence"),
	}
	block, method := ResolveAnchor(precedenceMarkdown, anchor)
	if method != AnchorResolvedByHeadingText {
		t.Fatalf("method = %q, want heading_and_quoted_text for a verbatim substring match", method)
	}
	if block.ID != "doc.alpha.target" {
		t.Fatalf("block.ID = %q, want doc.alpha.target", block.ID)
	}
}

func TestResolveAnchorFallsBackThroughEveryStepToConflicted(t *testing.T) {
	// No block id, no content-hash match, no heading+quoted-text match
	// (exact or fuzzy) - every step must be attempted and exhausted
	// before conflicting, not short-circuited early with a false
	// negative.
	anchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BaseRevisionHash: Hash([]byte(precedenceMarkdown)),
		BlockId:          strp("doc.alpha.nonexistent"),
		HeadingPath:      []string{"Doc", "Alpha"},
		QuotedText:       strp("nothing resembling this text exists anywhere in the document"),
	}
	_, method := ResolveAnchor(precedenceMarkdown, anchor)
	if method != AnchorConflicted {
		t.Fatalf("method = %q, want conflicted once every step in the chain has been exhausted", method)
	}
}

func TestResolveAnchorWithNoAnchorHintsAtAllIsConflicted(t *testing.T) {
	// An anchor with neither a block id nor quoted text (e.g. a
	// corrupted or partially-migrated comment) must not resolve by
	// accident - it should immediately fall through to conflicted since
	// no step has anything to match against.
	anchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BaseRevisionHash: Hash([]byte(precedenceMarkdown)),
	}
	_, method := ResolveAnchor(precedenceMarkdown, anchor)
	if method != AnchorConflicted {
		t.Fatalf("method = %q, want conflicted when the anchor carries no block id or quoted text", method)
	}
}
