package artifact

import (
	"fmt"
	"strings"
)

// DiffLineKind classifies one line of a DiffLines result.
type DiffLineKind string

const (
	DiffLineEqual   DiffLineKind = "equal"
	DiffLineAdded   DiffLineKind = "added"
	DiffLineRemoved DiffLineKind = "removed"
)

// DiffLine is one line of an aligned line-by-line diff between two
// documents.
type DiffLine struct {
	Kind DiffLineKind
	Text string
}

// DiffSummary is §13.10's "added, removed, and modified summaries" -
// modified is deliberately not tracked as its own count: a line-level
// diff sees a changed line as a removed-then-added pair, which is the
// same thing a "modified" count would report differently, not additional
// information.
type DiffSummary struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

// DiffLines computes a line-level diff between base and proposed via the
// standard longest-common-subsequence backtrack, so callers can render a
// unified or side-by-side view (§13.10's "side-by-side when sufficient
// width exists, unified diff on mobile" is a rendering choice over this
// same aligned line list, not two different diff computations).
func DiffLines(base, proposed string) ([]DiffLine, DiffSummary) {
	a := splitLines(base)
	b := splitLines(proposed)
	lcs := lcsTable(a, b)

	var lines []DiffLine
	var summary DiffSummary
	i, j := len(a), len(b)
	var reversed []DiffLine
	for i > 0 && j > 0 {
		switch {
		case a[i-1] == b[j-1]:
			reversed = append(reversed, DiffLine{Kind: DiffLineEqual, Text: a[i-1]})
			i--
			j--
		case lcs[i-1][j] >= lcs[i][j-1]:
			reversed = append(reversed, DiffLine{Kind: DiffLineRemoved, Text: a[i-1]})
			summary.Removed++
			i--
		default:
			reversed = append(reversed, DiffLine{Kind: DiffLineAdded, Text: b[j-1]})
			summary.Added++
			j--
		}
	}
	for i > 0 {
		reversed = append(reversed, DiffLine{Kind: DiffLineRemoved, Text: a[i-1]})
		summary.Removed++
		i--
	}
	for j > 0 {
		reversed = append(reversed, DiffLine{Kind: DiffLineAdded, Text: b[j-1]})
		summary.Added++
		j--
	}

	lines = make([]DiffLine, len(reversed))
	for k, l := range reversed {
		lines[len(reversed)-1-k] = l
	}
	return lines, summary
}

// UnifiedDiff renders lines (DiffLines' output) as a conventional
// +/-/space-prefixed unified-diff-style text block, for storage as
// ReviewStore.PutProposal's machine_patch.
func UnifiedDiff(lines []DiffLine) string {
	var b strings.Builder
	for _, l := range lines {
		switch l.Kind {
		case DiffLineAdded:
			fmt.Fprintf(&b, "+%s\n", l.Text)
		case DiffLineRemoved:
			fmt.Fprintf(&b, "-%s\n", l.Text)
		default:
			fmt.Fprintf(&b, " %s\n", l.Text)
		}
	}
	return b.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// lcsTable is the standard O(len(a)*len(b)) longest-common-subsequence
// dynamic-programming table, sized for backtracking in DiffLines.
func lcsTable(a, b []string) [][]int {
	table := make([][]int, len(a)+1)
	for i := range table {
		table[i] = make([]int, len(b)+1)
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				table[i][j] = table[i-1][j-1] + 1
			} else if table[i-1][j] >= table[i][j-1] {
				table[i][j] = table[i-1][j]
			} else {
				table[i][j] = table[i][j-1]
			}
		}
	}
	return table
}
