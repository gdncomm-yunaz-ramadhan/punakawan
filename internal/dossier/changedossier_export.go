package dossier

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// ExportMarkdown renders the human-readable §37/§38 export of a dossier: a
// header, the §38 "Summary indicators" block, then a section per dimension
// (objective, requirements, contradictions, impact, plan and tasks,
// implementation, verification, plan conformance, claims, evidence,
// unresolved risks, rollback). It is deterministic for a given persisted
// dossier because it reads, never regenerates, timestamps and content.
func ExportMarkdown(root, id string) (string, error) {
	loaded, err := Get(root, id)
	if err != nil {
		return "", err
	}
	d := loaded.Dossier

	var b strings.Builder
	title := d.Title
	if title == "" {
		title = d.Id
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "- Id: %s\n- Status: %s\n\n", d.Id, d.Status)

	// The §38 summary indicators are the at-a-glance signals a reviewer scans
	// first, so they lead the document before the detailed sections.
	b.WriteString("## Summary\n\n")
	for _, line := range summaryIndicators(loaded) {
		fmt.Fprintf(&b, "- %s\n", line)
	}
	b.WriteString("\n")

	b.WriteString("## Objective\n\n")
	fmt.Fprintf(&b, "%s\n", orDash(d.Objective.Statement))
	writeBullets(&b, "Source refs", d.Objective.SourceRefs)
	b.WriteString("\n")

	if d.Requirements != nil {
		b.WriteString("## Requirements\n\n")
		writeBullets(&b, "Covered", d.Requirements.Covered)
		writeBullets(&b, "Uncovered", d.Requirements.Uncovered)
		b.WriteString("\n")
	}

	if d.Contradictions != nil {
		b.WriteString("## Contradictions\n\n")
		fmt.Fprintf(&b, "- Counts: %d resolved, %d unresolved\n",
			len(d.Contradictions.Resolved), len(d.Contradictions.Unresolved))
		writeBullets(&b, "Resolved", d.Contradictions.Resolved)
		writeBullets(&b, "Unresolved", d.Contradictions.Unresolved)
		b.WriteString("\n")
	}

	if d.Impact != nil {
		b.WriteString("## Impact\n\n")
		writeBullets(&b, "Repositories", d.Impact.Repositories)
		writeBullets(&b, "Missing coverage", d.Impact.MissingCoverage)
		for _, ex := range d.Impact.ExcludedRepositories {
			fmt.Fprintf(&b, "- Excluded %s: %s\n", ex.Repository, ex.Reason)
		}
		b.WriteString("\n")
	}

	if d.Plan != nil || d.Tasks != nil {
		b.WriteString("## Plan and Tasks\n\n")
		if d.Plan != nil {
			fmt.Fprintf(&b, "- Plan: %s (version %s)\n", orDashPtr(d.Plan.Id), intPtr(d.Plan.Version))
		}
		if d.Tasks != nil {
			writeBullets(&b, "Completed tasks", d.Tasks.Completed)
		}
		b.WriteString("\n")
	}

	if d.Implementation != nil {
		b.WriteString("## Implementation\n\n")
		writeBullets(&b, "Changed repositories", d.Implementation.ChangedRepositories)
		b.WriteString("\n")
	}

	if len(d.Verification) > 0 {
		b.WriteString("## Verification\n\n")
		for _, dim := range sortedKeys(d.Verification) {
			fmt.Fprintf(&b, "- %s: %s\n", dim, d.Verification[dim])
		}
		b.WriteString("\n")
	}

	implemented, partial, missing := Conformance(d)
	b.WriteString("## Plan Conformance\n\n")
	fmt.Fprintf(&b, "- Implemented: %d\n- Partial: %d\n- Missing: %d\n", implemented, partial, missing)
	if d.PlanConformance != nil {
		for _, dev := range d.PlanConformance.DeliberateDeviations {
			fmt.Fprintf(&b, "- Deviation %q -> %q (approved: %t): %s\n",
				dev.Item, dev.Actual, dev.Approved != nil && *dev.Approved, dev.Rationale)
		}
	}
	b.WriteString("\n")

	if len(loaded.Claims) > 0 {
		b.WriteString("## Claims\n\n")
		for _, c := range loaded.Claims {
			fmt.Fprintf(&b, "- [%s] %s (%s by %s)\n", c.Status, c.Statement, c.Type, c.Producer.Role)
		}
		b.WriteString("\n")
	}

	if len(loaded.Evidence) > 0 {
		b.WriteString("## Evidence\n\n")
		for _, e := range loaded.Evidence {
			fmt.Fprintf(&b, "- %s (%s)\n", e.Id, e.Type)
		}
		b.WriteString("\n")
	}

	if len(d.UnresolvedRisks) > 0 {
		b.WriteString("## Unresolved Risks\n\n")
		writeBullets(&b, "", d.UnresolvedRisks)
		b.WriteString("\n")
	}

	if d.Rollback != nil {
		b.WriteString("## Rollback\n\n")
		fmt.Fprintf(&b, "- Verified: %t\n", d.Rollback.Verified != nil && *d.Rollback.Verified)
		if d.Rollback.Procedure != nil && *d.Rollback.Procedure != "" {
			fmt.Fprintf(&b, "- Procedure: %s\n", *d.Rollback.Procedure)
		}
		b.WriteString("\n")
	}

	return b.String(), nil
}

// summaryIndicators produces the exact §38 indicator lines. They are computed
// here (not stored) so they always reflect the current dossier and claims.
func summaryIndicators(loaded Loaded) []string {
	d := loaded.Dossier

	reqCovered, reqTotal := 0, 0
	if d.Requirements != nil {
		reqCovered = len(d.Requirements.Covered)
		reqTotal = reqCovered + len(d.Requirements.Uncovered)
	}

	implemented, partial, missing := Conformance(d)
	planTotal := implemented + partial + missing

	reposHandled, reposTotal := 0, 0
	if d.Impact != nil {
		reposHandled = len(d.Impact.Repositories)
		// Missing coverage counts as repositories that should have been
		// handled but were not, so the denominator is handled + missing.
		reposTotal = reposHandled + len(d.Impact.MissingCoverage)
	}

	openContradictions := 0
	if d.Contradictions != nil {
		openContradictions = len(d.Contradictions.Unresolved)
	}

	verifiedClaims := 0
	for _, c := range loaded.Claims {
		if c.Status == protocol.DossierClaimStatusVerified {
			verifiedClaims++
		}
	}

	return []string{
		fmt.Sprintf("Requirements covered: %d / %d", reqCovered, reqTotal),
		fmt.Sprintf("Plan conformance: %d / %d", implemented, planTotal),
		fmt.Sprintf("Repositories handled: %d / %d", reposHandled, reposTotal),
		fmt.Sprintf("Open contradictions: %d", openContradictions),
		fmt.Sprintf("Verified claims: %d / %d", verifiedClaims, len(loaded.Claims)),
		fmt.Sprintf("Blocking findings: %d", len(finalizeBlockers(loaded))),
	}
}

// ExportJSON renders a stable, deterministic JSON export of the dossier and its
// claims and evidence. It round-trips through an untyped map so every object's
// keys are emitted in sorted order (encoding/json sorts map keys), giving byte
// -for-byte identical output across runs for a given persisted dossier.
func ExportJSON(root, id string) ([]byte, error) {
	loaded, err := Get(root, id)
	if err != nil {
		return nil, err
	}
	combined := map[string]any{
		"dossier":  loaded.Dossier,
		"claims":   loaded.Claims,
		"evidence": loaded.Evidence,
	}
	// First marshal typed values, then decode into a generic tree so the final
	// marshal emits sorted keys throughout (structs alone would keep field
	// order, which is stable but not sorted).
	raw, err := json.Marshal(combined)
	if err != nil {
		return nil, fmt.Errorf("dossier: marshal export: %w", err)
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, fmt.Errorf("dossier: normalize export: %w", err)
	}
	out, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("dossier: render export: %w", err)
	}
	return out, nil
}

// PRSummary returns a short markdown PR description for the dossier
// (DOSSIER-018): title, objective, the key summary indicators, and the changed
// repositories. It is intentionally compact - a PR body, not the full export.
func PRSummary(root, id string) (string, error) {
	loaded, err := Get(root, id)
	if err != nil {
		return "", err
	}
	d := loaded.Dossier

	var b strings.Builder
	title := d.Title
	if title == "" {
		title = d.Id
	}
	fmt.Fprintf(&b, "## %s\n\n", title)
	if d.Objective.Statement != "" {
		fmt.Fprintf(&b, "%s\n\n", d.Objective.Statement)
	}
	b.WriteString("### Summary\n\n")
	for _, line := range summaryIndicators(loaded) {
		fmt.Fprintf(&b, "- %s\n", line)
	}
	if d.Contradictions != nil &&
		(len(d.Contradictions.Resolved) > 0 || len(d.Contradictions.Unresolved) > 0) {
		b.WriteString("\n### Contradictions\n\n")
		fmt.Fprintf(&b, "- Counts: %d resolved, %d unresolved\n",
			len(d.Contradictions.Resolved), len(d.Contradictions.Unresolved))
		writeBullets(&b, "Resolved", d.Contradictions.Resolved)
		writeBullets(&b, "Unresolved", d.Contradictions.Unresolved)
	}
	if d.Impact != nil &&
		(len(d.Impact.Repositories) > 0 || len(d.Impact.ExcludedRepositories) > 0 ||
			len(d.Impact.MissingCoverage) > 0) {
		b.WriteString("\n### Impact\n\n")
		writeBullets(&b, "Repositories", d.Impact.Repositories)
		writeBullets(&b, "Missing coverage", d.Impact.MissingCoverage)
		for _, ex := range d.Impact.ExcludedRepositories {
			fmt.Fprintf(&b, "- Excluded %s: %s\n", ex.Repository, ex.Reason)
		}
	}
	if d.Implementation != nil && len(d.Implementation.ChangedRepositories) > 0 {
		b.WriteString("\n### Changed repositories\n\n")
		for _, r := range d.Implementation.ChangedRepositories {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	return b.String(), nil
}

// writeBullets writes a labeled bullet list, skipping an empty list entirely.
// An empty label writes the items as plain top-level bullets.
func writeBullets(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		return
	}
	for _, it := range items {
		if label == "" {
			fmt.Fprintf(b, "- %s\n", it)
		} else {
			fmt.Fprintf(b, "- %s: %s\n", label, it)
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func orDashPtr(s *string) string {
	if s == nil || *s == "" {
		return "-"
	}
	return *s
}

func intPtr(n *int) string {
	if n == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *n)
}
