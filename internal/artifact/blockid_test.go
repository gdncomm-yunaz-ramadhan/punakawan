package artifact

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

const sampleMarkdown = `# Punakawan Panel

## Security Model

<!-- pk:block:panel.security.network-boundary -->
## Network Boundary

Default binding: 127.0.0.1 only

<!-- pk:block:panel.security.loopback-default -->
The panel binds to loopback by default.

## Another Section

Unrelated content here.
`

func strp(s string) *string { return &s }

func TestExtractBlocksFindsEachMarker(t *testing.T) {
	blocks := ExtractBlocks(sampleMarkdown)
	if len(blocks) != 2 {
		t.Fatalf("ExtractBlocks returned %d blocks, want 2: %+v", len(blocks), blocks)
	}
	nb, ok := blocks["panel.security.network-boundary"]
	if !ok {
		t.Fatal("missing panel.security.network-boundary block")
	}
	if len(nb.HeadingPath) != 2 || nb.HeadingPath[0] != "Punakawan Panel" || nb.HeadingPath[1] != "Security Model" {
		t.Fatalf("HeadingPath = %v, want [Punakawan Panel Security Model] (heading path as of the marker, before its own heading)", nb.HeadingPath)
	}

	ld, ok := blocks["panel.security.loopback-default"]
	if !ok {
		t.Fatal("missing panel.security.loopback-default block")
	}
	// "Security Model" and "Network Boundary" are sibling level-2
	// headings, so by the time this marker appears (after the "Network
	// Boundary" heading line), the stack holds Network Boundary in
	// Security Model's place, not both - a heading path reflects the
	// currently active nesting, not every heading seen so far.
	if len(ld.HeadingPath) != 2 || ld.HeadingPath[1] != "Network Boundary" {
		t.Fatalf("HeadingPath = %v, want it nested under Network Boundary", ld.HeadingPath)
	}
	if ld.Content != "The panel binds to loopback by default." {
		t.Fatalf("Content = %q, want the loopback sentence", ld.Content)
	}
}

func TestResolveAnchorByExactBlockID(t *testing.T) {
	anchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BlockId:          strp("panel.security.loopback-default"),
		BaseRevisionHash: Hash([]byte(sampleMarkdown)),
	}
	block, method := ResolveAnchor(sampleMarkdown, anchor)
	if method != AnchorResolvedByBlockID {
		t.Fatalf("method = %q, want block_id", method)
	}
	if block.ID != "panel.security.loopback-default" {
		t.Fatalf("block.ID = %q, want panel.security.loopback-default", block.ID)
	}
}

func TestResolveAnchorByContentHashWhenBlockIDMissing(t *testing.T) {
	anchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BaseRevisionHash: Hash([]byte(sampleMarkdown)),
		QuotedText:       strp("The panel binds to loopback by default."),
	}
	block, method := ResolveAnchor(sampleMarkdown, anchor)
	if method != AnchorResolvedByContentHash {
		t.Fatalf("method = %q, want content_hash", method)
	}
	if block.ID != "panel.security.loopback-default" {
		t.Fatalf("block.ID = %q, want panel.security.loopback-default", block.ID)
	}
}

func TestResolveAnchorByHeadingPathAndQuotedText(t *testing.T) {
	anchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BaseRevisionHash: Hash([]byte(sampleMarkdown)),
		HeadingPath:      []string{"Punakawan Panel", "Network Boundary"},
		QuotedText:       strp("loopback by default"),
	}
	block, method := ResolveAnchor(sampleMarkdown, anchor)
	if method != AnchorResolvedByHeadingText {
		t.Fatalf("method = %q, want heading_and_quoted_text", method)
	}
	if block.ID != "panel.security.loopback-default" {
		t.Fatalf("block.ID = %q, want panel.security.loopback-default", block.ID)
	}
}

func TestResolveAnchorByFuzzyTextWhenReworded(t *testing.T) {
	anchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BaseRevisionHash: Hash([]byte(sampleMarkdown)),
		HeadingPath:      []string{"Punakawan Panel", "Network Boundary"},
		QuotedText:       strp("  LOOPBACK   by    default  "),
	}
	block, method := ResolveAnchor(sampleMarkdown, anchor)
	if method != AnchorResolvedByFuzzyText {
		t.Fatalf("method = %q, want heading_and_fuzzy_text", method)
	}
	if block.ID != "panel.security.loopback-default" {
		t.Fatalf("block.ID = %q, want panel.security.loopback-default", block.ID)
	}
}

func TestResolveAnchorConflictedWhenNothingMatches(t *testing.T) {
	anchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BaseRevisionHash: Hash([]byte(sampleMarkdown)),
		HeadingPath:      []string{"Nonexistent"},
		QuotedText:       strp("nothing like this exists anywhere"),
	}
	_, method := ResolveAnchor(sampleMarkdown, anchor)
	if method != AnchorConflicted {
		t.Fatalf("method = %q, want conflicted", method)
	}
}
