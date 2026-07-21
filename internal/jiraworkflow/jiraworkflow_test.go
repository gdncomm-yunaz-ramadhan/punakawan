package jiraworkflow

import "testing"

const fixtureConfigPath = "../../test/fixtures/jira-workflow.yaml"

func TestLoadFromFixture(t *testing.T) {
	c, err := Load(fixtureConfigPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	wantSkip := []string{"Won't Do", "Duplicate"}
	if len(c.SkipStatuses) != len(wantSkip) {
		t.Fatalf("SkipStatuses: got %v, want %v", c.SkipStatuses, wantSkip)
	}
	for i, s := range wantSkip {
		if c.SkipStatuses[i] != s {
			t.Errorf("SkipStatuses[%d]: got %q, want %q", i, c.SkipStatuses[i], s)
		}
	}

	if c.ClarificationStatus != "Sent Back to Product Review" {
		t.Errorf("ClarificationStatus: got %q, want %q", c.ClarificationStatus, "Sent Back to Product Review")
	}

	wantScale := []float64{1, 2, 3, 5, 8, 13, 21}
	if len(c.Estimation.Scale) != len(wantScale) {
		t.Fatalf("Estimation.Scale: got %v, want %v", c.Estimation.Scale, wantScale)
	}
	for i, v := range wantScale {
		if c.Estimation.Scale[i] != v {
			t.Errorf("Estimation.Scale[%d]: got %v, want %v", i, c.Estimation.Scale[i], v)
		}
	}

	if c.Estimation.PointsToHours != 4 {
		t.Errorf("Estimation.PointsToHours: got %v, want 4", c.Estimation.PointsToHours)
	}
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	c, err := Load("/nonexistent/jira-workflow.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.SkipStatuses) != 0 {
		t.Errorf("expected default SkipStatuses to be empty, got %v", c.SkipStatuses)
	}
	if c.ClarificationStatus != "" {
		t.Errorf("expected default ClarificationStatus to be empty, got %q", c.ClarificationStatus)
	}
	if c.Estimation.PointsToHours != 0 {
		t.Errorf("expected default PointsToHours to be 0 (not configured), got %v", c.Estimation.PointsToHours)
	}
	if len(c.Estimation.Scale) == 0 {
		t.Errorf("expected default Estimation.Scale to be a non-empty sensible default")
	}
}

func TestShouldSkip(t *testing.T) {
	c, err := Load(fixtureConfigPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := []struct {
		name   string
		status string
		want   bool
	}{
		{"exact match", "Won't Do", true},
		{"exact match, other entry", "Duplicate", true},
		{"case-insensitive match (lower)", "won't do", true},
		{"case-insensitive match (upper)", "DUPLICATE", true},
		{"case-insensitive match (mixed)", "wOn'T dO", true},
		{"no match", "In Progress", false},
		{"no match, similar but distinct", "Won't Fix", false},
		{"empty status", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := c.ShouldSkip(tc.status)
			if got != tc.want {
				t.Errorf("ShouldSkip(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestShouldSkip_UnconfiguredEmpty(t *testing.T) {
	c := &Config{}
	if c.ShouldSkip("anything") {
		t.Errorf("ShouldSkip on empty config should always be false")
	}
}

func TestEstimateHours_Configured(t *testing.T) {
	c, err := Load(fixtureConfigPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	hours, ok := c.EstimateHours(5)
	if !ok {
		t.Fatalf("EstimateHours(5): ok = false, want true (PointsToHours is configured as 4)")
	}
	if hours != 20 {
		t.Errorf("EstimateHours(5) = %v, want 20", hours)
	}
}

func TestEstimateHours_Unconfigured(t *testing.T) {
	c := Default()

	hours, ok := c.EstimateHours(5)
	if ok {
		t.Fatalf("EstimateHours: ok = true, want false when PointsToHours is unconfigured")
	}
	if hours != 0 {
		t.Errorf("EstimateHours: hours = %v, want 0 when unconfigured", hours)
	}
}

func TestEstimateHours_ConfiguredAsZero(t *testing.T) {
	// PointsToHours == 0 is used as the "not configured" sentinel, so a
	// team that (oddly) configures a literal zero ratio is indistinguishable
	// from "unconfigured". Document and verify that explicitly: this is a
	// known, accepted tradeoff of using the zero value as the sentinel.
	c := &Config{Estimation: EstimationConfig{PointsToHours: 0}}
	hours, ok := c.EstimateHours(8)
	if ok {
		t.Fatalf("EstimateHours: ok = true, want false (zero ratio is treated as unconfigured)")
	}
	if hours != 0 {
		t.Errorf("EstimateHours: hours = %v, want 0", hours)
	}
}

func TestValidateStoryPoints_OnScale(t *testing.T) {
	c, err := Load(fixtureConfigPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, v := range []float64{1, 2, 3, 5, 8, 13, 21} {
		if err := c.ValidateStoryPoints(v); err != nil {
			t.Errorf("ValidateStoryPoints(%v): unexpected error: %v", v, err)
		}
	}
}

func TestValidateStoryPoints_OffScale(t *testing.T) {
	c, err := Load(fixtureConfigPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, v := range []float64{0, 4, 6, 100} {
		if err := c.ValidateStoryPoints(v); err == nil {
			t.Errorf("ValidateStoryPoints(%v): expected error, got nil", v)
		}
	}
}

func TestValidateStoryPoints_UnconfiguredScaleAllowsAny(t *testing.T) {
	c := &Config{} // no scale configured at all
	for _, v := range []float64{-1, 0, 4, 6, 100, 1000.5} {
		if err := c.ValidateStoryPoints(v); err != nil {
			t.Errorf("ValidateStoryPoints(%v) with unconfigured scale: unexpected error: %v", v, err)
		}
	}
}
