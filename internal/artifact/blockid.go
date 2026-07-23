package artifact

import (
	"regexp"
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Block is one stable, anchorable unit of a Markdown plan: the content
// immediately following a `<!-- pk:block:<id> -->` marker (§6), up to
// the next block marker or heading of equal or higher level.
type Block struct {
	ID          string
	HeadingPath []string
	Content     string
	ContentHash string
}

var blockMarkerRe = regexp.MustCompile(`^<!--\s*pk:block:([A-Za-z0-9._-]+)\s*-->\s*$`)
var headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*\S)\s*$`)

// ExtractBlocks parses markdown for every `pk:block:` marker and returns
// each one's content (through the next marker or a heading at or above
// the marker's own nesting level) plus its heading path at that point,
// keyed by block ID (§6's "exact block ID" resolution step, and the
// input step 3/4 - heading path plus quoted text - falls back to).
func ExtractBlocks(markdown string) map[string]Block {
	lines := strings.Split(markdown, "\n")
	blocks := make(map[string]Block)

	var headingStack []headingEntry
	var current *Block
	var currentLevel int

	flush := func() {
		if current != nil {
			current.Content = strings.TrimRight(current.Content, "\n")
			current.ContentHash = Hash([]byte(current.Content))
			blocks[current.ID] = *current
			current = nil
		}
	}

	for _, line := range lines {
		if m := blockMarkerRe.FindStringSubmatch(line); m != nil {
			flush()
			current = &Block{ID: m[1], HeadingPath: headingPathOf(headingStack)}
			currentLevel = 99 // a marker with no heading yet closes on any heading
			continue
		}
		if h := headingRe.FindStringSubmatch(line); h != nil {
			level := len(h[1])
			if current != nil && level <= currentLevel {
				flush()
			}
			for len(headingStack) > 0 && headingStack[len(headingStack)-1].level >= level {
				headingStack = headingStack[:len(headingStack)-1]
			}
			headingStack = append(headingStack, headingEntry{level: level, text: h[2]})
			if current != nil {
				currentLevel = level
			}
			continue
		}
		if current != nil {
			current.Content += line + "\n"
		}
	}
	flush()
	return blocks
}

type headingEntry struct {
	level int
	text  string
}

func headingPathOf(stack []headingEntry) []string {
	path := make([]string, len(stack))
	for i, h := range stack {
		path[i] = h.text
	}
	return path
}

// AnchorResolution reports which of §6's 5 resolution steps located the
// comment's target block, or that none did.
type AnchorResolution string

const (
	AnchorResolvedByBlockID     AnchorResolution = "block_id"
	AnchorResolvedByContentHash AnchorResolution = "content_hash"
	AnchorResolvedByHeadingText AnchorResolution = "heading_and_quoted_text"
	AnchorResolvedByFuzzyText   AnchorResolution = "heading_and_fuzzy_text"
	AnchorConflicted            AnchorResolution = "conflicted"
)

// ResolveAnchor implements §6's anchor resolution order against
// markdown's current blocks. It never guesses past AnchorConflicted -
// a conflicted result means the comment needs a human to re-anchor it,
// not a best-effort placement.
func ResolveAnchor(markdown string, anchor protocol.ArtifactCommentAnchor) (Block, AnchorResolution) {
	blocks := ExtractBlocks(markdown)

	if anchor.BlockId != nil {
		if b, ok := blocks[*anchor.BlockId]; ok {
			return b, AnchorResolvedByBlockID
		}
	}

	if anchor.QuotedText != nil {
		quoted := strings.TrimSpace(*anchor.QuotedText)
		for _, b := range blocks {
			if Hash([]byte(b.Content)) == Hash([]byte(quoted)) {
				return b, AnchorResolvedByContentHash
			}
		}
	}

	if len(anchor.HeadingPath) > 0 && anchor.QuotedText != nil {
		quoted := strings.TrimSpace(*anchor.QuotedText)
		for _, b := range blocks {
			if headingPathEqual(b.HeadingPath, anchor.HeadingPath) && strings.Contains(b.Content, quoted) {
				return b, AnchorResolvedByHeadingText
			}
		}
	}

	if len(anchor.HeadingPath) > 0 && anchor.QuotedText != nil {
		quoted := normalizeForFuzzyMatch(*anchor.QuotedText)
		for _, b := range blocks {
			if headingPathEqual(b.HeadingPath, anchor.HeadingPath) &&
				strings.Contains(normalizeForFuzzyMatch(b.Content), quoted) {
				return b, AnchorResolvedByFuzzyText
			}
		}
	}

	return Block{}, AnchorConflicted
}

func headingPathEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// normalizeForFuzzyMatch collapses whitespace and case so a comment's
// quoted text survives minor rewording/reflowing (§6 step 4), without
// pretending to be a real fuzzy-matching algorithm - this is
// deliberately a narrow, explainable normalization, not a scored
// similarity search.
func normalizeForFuzzyMatch(s string) string {
	fields := strings.Fields(strings.ToLower(s))
	return strings.Join(fields, " ")
}
