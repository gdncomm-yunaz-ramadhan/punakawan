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
