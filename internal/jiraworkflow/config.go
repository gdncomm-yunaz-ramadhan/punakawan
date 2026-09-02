// Package jiraworkflow loads and evaluates org/project-specific Jira
// workflow configuration (.punakawan/jira-workflow.yaml).
//
// Jira has no universal, auto-discoverable way to know that a custom status
// like "Sent Back to Product Review" means "needs clarification" — status
// names are entirely org/project-configured. Likewise there is no universal
// way to know a team's story-point estimation scale (Fibonacci vs T-shirt
// vs linear is a board/team convention, not exposed by a generic API). This
// package makes that split explicit: generic Jira concepts (statusCategory,
// issue types, etc.) belong elsewhere; this package only holds the
// workspace-level, human-configured facts that no API can tell us.
package jiraworkflow

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// EstimationConfig describes a workspace's story-point estimation
// conventions: the set of valid point values (a board/team's scale, e.g.
// Fibonacci-like) and the ratio used to convert points into an original
// time estimate.
type EstimationConfig struct {
	// Scale is the set of valid story-point values for this workspace,
	// e.g. a Fibonacci-like [1,2,3,5,8,13,21]. Empty means "no scale
	// configured" — ValidateStoryPoints then accepts any value, since no
	// scale configured means no validation is possible, not that
	// everything is invalid.
	Scale []float64 `yaml:"scale"`

	// PointsToHours is the conversion ratio used by EstimateHours to turn
	// story points into an original-estimate duration in hours. Zero
	// means "no conversion configured" — this is intentionally distinct
	// from "configured as a zero-hour conversion", which is why
	// EstimateHours returns an explicit ok bool rather than silently
	// returning a made-up ratio.
	PointsToHours float64 `yaml:"points_to_hours"`
}

// Config is a workspace's loaded Jira workflow configuration.
type Config struct {
	// SkipStatuses lists status names that should be excluded from task
	// graph processing (e.g. "Won't Do", "Duplicate").
	SkipStatuses []string `yaml:"skip_statuses"`

	// ClarificationStatus is the status name that means "this issue is
	// blocked pending clarification" for this workspace (e.g. "Sent Back
	// to Product Review"). Empty means no clarification status is
	// configured.
	ClarificationStatus string `yaml:"clarification_status"`

	Estimation EstimationConfig `yaml:"estimation"`

	// AutoLog is the master switch for automatically posting delivery-event
	// updates to a workspace's linked Jira issues. False (the default, and
	// what an existing config file with none of these fields keeps
	// behaving as) means every automatic Jira update stays off, regardless
	// of what CommentEvents/TransitionOnComplete/LogWork say - a workspace
	// opts in explicitly rather than an upgrade silently starting to write
	// to Jira on its behalf.
	AutoLog bool `yaml:"auto_log"`

	// CommentEvents lists which delivery event type names (e.g.
	// "delivery.started", "review.changes_required") should post a Jira
	// comment when AutoLog is true. An event type not in this list simply
	// never posts a comment; the list is empty by default, so AutoLog=true
	// with no CommentEvents configured still posts nothing.
	CommentEvents []string `yaml:"comment_events"`

	// TransitionOnComplete additionally requests a Jira workflow-status
	// transition when a delivery completes. Kept as its own toggle,
	// separate from CommentEvents, because moving an issue's status is a
	// stronger, more visible action than leaving a comment and a workspace
	// may want one without the other.
	TransitionOnComplete bool `yaml:"transition_on_complete"`

	// LogWork additionally requests that time spent be logged to Jira as a
	// worklog entry. Kept as its own toggle for the same reason as
	// TransitionOnComplete: worklog entries affect a project's time
	// tracking and a workspace may not want that just because it wants
	// comments.
	LogWork bool `yaml:"log_work"`

	// Transitions maps a Jira project key (the prefix before the hyphen in
	// every one of its issue keys, e.g. "PAY" for "PAY-123") to that
	// project's start/complete transition policy. A project absent from
	// this map has no configured policy: TransitionPolicyFor reports
	// ok=false for it, and a caller keeps its own prior fallback behavior
	// rather than attempting an unconfigured transition. Different Jira
	// projects in the same workspace commonly use different workflow
	// status names for functionally the same states, so this policy is
	// necessarily per-project rather than one workspace-wide pair of names.
	Transitions map[string]TransitionPolicy `yaml:"transitions"`
}

// TransitionPolicy names the Jira workflow status a project's issues
// should be moved to when a delivery starts and when it completes. Either
// field may be empty, meaning "attempt no transition for that instant" -
// a workspace can configure just one of the two.
type TransitionPolicy struct {
	StartStatus    string `yaml:"start_status"`
	CompleteStatus string `yaml:"complete_status"`
}

// TransitionPolicyFor returns the configured TransitionPolicy for
// projectKey. ok is false when the project has no configured policy, in
// which case the returned TransitionPolicy is the zero value and must not
// be used to attempt a transition. Matching is exact and case-sensitive:
// Jira project keys are conventionally all-uppercase and a workspace
// config is expected to spell them exactly as Jira does.
func (c *Config) TransitionPolicyFor(projectKey string) (TransitionPolicy, bool) {
	if c.Transitions == nil {
		return TransitionPolicy{}, false
	}
	policy, ok := c.Transitions[projectKey]
	return policy, ok
}

// ProjectKeyFromIssueKey extracts the project key prefix from a Jira issue
// key, e.g. "PAY-123" -> "PAY". Returns "" if issueKey does not look like a
// standard PROJECT-123 key.
func ProjectKeyFromIssueKey(issueKey string) string {
	idx := strings.LastIndex(issueKey, "-")
	if idx <= 0 {
		return ""
	}
	return issueKey[:idx]
}

// ShouldComment reports whether eventName (a delivery event type name, e.g.
// "delivery.completed") is one of the configured CommentEvents. Matching is
// exact: unlike ShouldSkip's Jira status names, delivery event type names
// are a fixed, code-defined vocabulary rather than free-text values an admin
// might retype inconsistently, so there is no casing drift to guard against
// here.
func (c *Config) ShouldComment(eventName string) bool {
	for _, e := range c.CommentEvents {
		if e == eventName {
			return true
		}
	}
	return false
}

// Default returns a safe, empty configuration: no statuses are skipped, no
// clarification status is recognized, and PointsToHours is left at 0 ("not
// configured"). Estimation.Scale defaults to a sensible Fibonacci-ish
// sequence purely as a reasonable starting point for validation — it is
// NOT a discovered or universal value. Jira does not expose a team's
// estimation scale via any generic API, so this default should be
// overridden by an explicit workspace config wherever accurate validation
// matters.
func Default() *Config {
	return &Config{
		SkipStatuses:        nil,
		ClarificationStatus: "",
		Estimation: EstimationConfig{
			Scale:         []float64{1, 2, 3, 5, 8, 13, 21},
			PointsToHours: 0,
		},
	}
}

// Load reads a jira-workflow.yaml file. If path does not exist, Default()
// is returned so a workspace without an explicit config file still behaves
// safely (no skipping, no clarification detection, no invented hour
// conversion) rather than failing to start.
//
// Note that "safely" includes AutoLog=false, which switches off every
// automatic Jira update. `punakawan setup` therefore writes this file for
// a workspace that has none, so the absent-file case is a deliberate
// opt-out rather than the accident it used to be.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("jiraworkflow: read %s: %w", path, err)
	}

	c := Default()
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("jiraworkflow: parse %s: %w", path, err)
	}
	return c, nil
}

// ShouldSkip reports whether statusName is one of the configured
// SkipStatuses. The comparison is case-insensitive: Jira status names are
// typically consistent in casing within a single site, but they are
// free-text fields that admins can (and do) retype inconsistently across
// workflow edits or when copy-pasted into config by hand. Treating
// "Won't Do" and "won't do" as equivalent avoids silent, hard-to-diagnose
// mismatches for what is otherwise a purely cosmetic difference.
func (c *Config) ShouldSkip(statusName string) bool {
	for _, s := range c.SkipStatuses {
		if strings.EqualFold(s, statusName) {
			return true
		}
	}
	return false
}

// EstimateHours converts storyPoints into an hours estimate using the
// configured PointsToHours ratio. ok is false when PointsToHours is not
// configured (zero), so callers can distinguish "no conversion available"
// from "this legitimately converts to zero hours" — hours is always 0 in
// the not-configured case, but callers must check ok rather than relying
// on the zero value.
func (c *Config) EstimateHours(storyPoints float64) (hours float64, ok bool) {
	if c.Estimation.PointsToHours == 0 {
		return 0, false
	}
	return storyPoints * c.Estimation.PointsToHours, true
}

// ValidateStoryPoints reports an error if points is not a member of the
// configured Estimation.Scale. If Scale is empty (no scale configured), all
// values pass: no scale configured means no validation is possible, not
// that everything is invalid.
func (c *Config) ValidateStoryPoints(points float64) error {
	if len(c.Estimation.Scale) == 0 {
		return nil
	}
	for _, v := range c.Estimation.Scale {
		if v == points {
			return nil
		}
	}
	return fmt.Errorf("jiraworkflow: story points %v not in configured scale %v", points, c.Estimation.Scale)
}
