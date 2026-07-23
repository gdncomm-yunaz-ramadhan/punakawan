// Package validation implements §11's plan proposal validator - the
// mechanically checkable subset of it. §11 groups checks into
// "structural", "consistency", and "review-compliance". Structural
// checks (this file) are deterministic and checkable from the two
// content strings alone. Consistency checks ("goals and non-goals do not
// directly contradict," "acceptance criteria cover new behavior," etc.)
// are inherently a reading-comprehension judgment call, not a mechanical
// property of the text - they are Punakawan's own revising agent's
// responsibility to reason about while drafting a proposal, not
// something this package fabricates a heuristic for. Review-compliance
// checks (comment_resolutions.go) are also deterministic, checked
// against the review's actual comments.
package validation

import (
	"regexp"
	"strconv"
	"strings"
)

// Issue is one failed (or, for informational findings, non-blocking)
// check.
type Issue struct {
	Check   string `json:"check"`
	Message string `json:"message"`
}

// StructuralReport is §11's "Structural checks" section's result.
type StructuralReport struct {
	Passed bool    `json:"passed"`
	Issues []Issue `json:"issues"`
}

var blockMarkerRe = regexp.MustCompile(`<!--\s*pk:block:(\S+?)\s*-->`)
var headingRe = regexp.MustCompile(`(?m)^(#{1,6})\s+\S`)
var fenceRe = regexp.MustCompile("(?m)^```")

// ValidateStructure checks proposed (the complete proposed artifact
// content) against base (the artifact's current canonical content) and
// the version numbers involved, per §11's structural checklist. Not
// every bullet in §11 is implemented (backlog-id-uniqueness and
// internal-reference-resolution are plan-content-specific conventions
// with no fixed, schema-defined shape to check mechanically here) - what
// is implemented is real and blocking, not a placeholder.
func ValidateStructure(base, proposed string, baseVersion, proposedVersion int) StructuralReport {
	var issues []Issue

	if proposedVersion != baseVersion+1 {
		issues = append(issues, Issue{Check: "version_increment", Message: "proposed version must be exactly base version + 1"})
	}

	if dupes := duplicateBlockIDs(proposed); len(dupes) > 0 {
		issues = append(issues, Issue{Check: "unique_block_ids", Message: "duplicate block id(s): " + strings.Join(dupes, ", ")})
	}

	if n := len(fenceRe.FindAllString(proposed, -1)); n%2 != 0 {
		issues = append(issues, Issue{Check: "balanced_fences", Message: "markdown code fences are unbalanced (odd number of ``` lines)"})
	}

	if err := validHeadingHierarchy(proposed); err != "" {
		issues = append(issues, Issue{Check: "heading_hierarchy", Message: err})
	}

	return StructuralReport{Passed: len(issues) == 0, Issues: issues}
}

func duplicateBlockIDs(content string) []string {
	seen := make(map[string]int)
	var order []string
	for _, m := range blockMarkerRe.FindAllStringSubmatch(content, -1) {
		id := m[1]
		if seen[id] == 0 {
			order = append(order, id)
		}
		seen[id]++
	}
	var dupes []string
	for _, id := range order {
		if seen[id] > 1 {
			dupes = append(dupes, id)
		}
	}
	return dupes
}

// validHeadingHierarchy returns a non-empty message if any heading level
// jumps forward by more than one step from the previous heading (e.g. an
// H1 followed directly by an H3, skipping H2) - dropping back down any
// number of levels is always valid (a new H2 legitimately follows a
// deeply nested H4).
func validHeadingHierarchy(content string) string {
	prevLevel := 0
	for _, m := range headingRe.FindAllStringSubmatch(content, -1) {
		level := len(m[1])
		if prevLevel != 0 && level > prevLevel+1 {
			return "heading level jumps from H" + strconv.Itoa(prevLevel) + " to H" + strconv.Itoa(level) + " without an intermediate heading"
		}
		prevLevel = level
	}
	return ""
}
