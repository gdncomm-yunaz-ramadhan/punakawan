// Package worklogalloc derives a proposed development/test/review Jira
// worklog hour allocation from a run's verified command time and maps it
// onto a project's already-configured Jira subtasks, so the proposal is
// visible before a human approves project delivery (punokawan-14yn.9
// AC2's "proposed worklogs map to configured Jira subtasks and are
// visible before project approval"), without fabricating a stage-level
// breakdown the underlying data does not actually support.
//
// Honesty note: internal/testrun records only that a command ran for N
// milliseconds and exited 0 or non-zero (see that package's own doc: it
// is not a test framework and has no general way to know whether an
// arbitrary caller-supplied command was a "build" step, a "test" step,
// or a "review" step). Nothing else in this codebase classifies a
// command into such a stage either. So this package does not claim to
// know how much of a run's verified time was spent on development vs
// testing vs review - it computes one total "verified work" duration
// (deliverysummary.Summary's TotalDurationMs, converted to hours) and,
// only when a project's configured Jira subtasks give it somewhere
// honest to put a split, divides that total evenly across whichever of
// the dev/test/review buckets have a matching subtask by name. A bucket
// with no matching subtask gets no share (its portion is not
// redistributed to the buckets that do match), and if no subtask
// matches any bucket at all, the whole total is reported unmapped
// rather than guessed onto an arbitrary subtask.
package worklogalloc

import "strings"

// Bucket names the three conventional development-lifecycle categories
// this package recognizes in a subtask's name.
type Bucket string

const (
	BucketDev    Bucket = "dev"
	BucketTest   Bucket = "test"
	BucketReview Bucket = "review"
)

// orderedBuckets fixes iteration order so Allocate's output (and the
// even split's per-bucket rounding, were rounding ever introduced) is
// deterministic across calls given the same input.
var orderedBuckets = []Bucket{BucketDev, BucketTest, BucketReview}

// bucketKeywords are the case-insensitive substrings checked against a
// subtask's summary to classify it into a Bucket. This is a naming
// convention, not a schema: list_jira_subtasks/internal/jiraworkflow
// model a Jira subtask as a plain key/summary/status - there is no
// dedicated "type" field this codebase's Jira integration surfaces that
// would let it distinguish a dev subtask from a test or review one any
// other way.
var bucketKeywords = map[Bucket][]string{
	BucketDev:    {"dev"},
	BucketTest:   {"test", "qa"},
	BucketReview: {"review"},
}

// Subtask is the minimal shape Allocate needs from a configured Jira
// subtask. This is deliberately not internal/mcpserver's JiraSubtask
// type, so this package does not depend on the MCP tool layer; callers
// convert (Key/Summary map directly).
type Subtask struct {
	Key     string
	Summary string
}

// ProposedWorklog is one bucket's proposed hours against the specific
// configured subtask it was matched to - ready to show a human before
// project approval, or to hand to update_jira_task_progress's
// worklog_hours once a manifest carrying it has been approved.
type ProposedWorklog struct {
	Bucket      Bucket
	SubtaskKey  string
	SubtaskName string
	Hours       float64
}

// Allocation is Allocate's result.
type Allocation struct {
	// TotalHours is the verified-work hours this allocation was derived
	// from, unchanged - Worklogs' hours plus UnmappedHours always sum
	// back to this value.
	TotalHours float64
	// Worklogs holds one entry per dev/test/review bucket that could be
	// matched to a configured subtask, each carrying its even share of
	// TotalHours.
	Worklogs []ProposedWorklog
	// UnmappedHours is the portion of TotalHours that could not be
	// matched to any configured subtask (no subtask name contained a
	// recognized bucket keyword for that share). It is never silently
	// dropped or folded into a matched bucket.
	UnmappedHours float64
}

// classifySubtask returns the first Bucket whose keywords appear in
// summary (case-insensitive, substring match), or "" if none match.
func classifySubtask(summary string) Bucket {
	lower := strings.ToLower(summary)
	for _, b := range orderedBuckets {
		for _, kw := range bucketKeywords[b] {
			if strings.Contains(lower, kw) {
				return b
			}
		}
	}
	return ""
}

// Allocate splits totalHours evenly across whichever of the dev/test/
// review buckets have at least one matching subtask in subtasks, one
// ProposedWorklog per matched bucket against its first matching subtask
// (subtasks' own given order - list_jira_subtasks returns a parent
// issue's subtasks in Jira's own order, so ties resolve stably for a
// given issue). If totalHours is zero or negative, or no subtask
// matches any bucket, Worklogs is empty and UnmappedHours carries the
// whole total: this function never invents a subtask to post hours
// against when nothing in the ticket's own subtask list names a
// dev/test/review role.
func Allocate(totalHours float64, subtasks []Subtask) Allocation {
	out := Allocation{TotalHours: totalHours}
	if totalHours <= 0 {
		return out
	}

	matched := map[Bucket]Subtask{}
	for _, st := range subtasks {
		b := classifySubtask(st.Summary)
		if b == "" {
			continue
		}
		if _, already := matched[b]; already {
			continue
		}
		matched[b] = st
	}

	if len(matched) == 0 {
		out.UnmappedHours = totalHours
		return out
	}

	share := totalHours / float64(len(matched))
	for _, b := range orderedBuckets {
		st, ok := matched[b]
		if !ok {
			continue
		}
		out.Worklogs = append(out.Worklogs, ProposedWorklog{
			Bucket:      b,
			SubtaskKey:  st.Key,
			SubtaskName: st.Summary,
			Hours:       share,
		})
	}
	return out
}
