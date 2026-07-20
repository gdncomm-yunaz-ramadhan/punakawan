// Package openapicheck implements OpenAPI compatibility checking (§13.4
// "Compare base and head, Detect breaking changes, Generate compatibility
// evidence"): diffing a base and head OpenAPI spec with oasdiff and
// classifying the differences as breaking or non-breaking.
package openapicheck

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oasdiff/oasdiff/checker"
	"github.com/oasdiff/oasdiff/diff"

	"github.com/ygrip/punakawan/internal/evidence"
)

// Change is one compatibility finding from comparing two OpenAPI specs, a
// decoupled projection of oasdiff's checker.Change interface (mirroring how
// internal/reconcile and internal/adapters keep their own plain structs
// rather than leaking third-party SDK types into their public API).
type Change struct {
	// Id is the stable check identifier, e.g. "request-parameter-removed".
	Id string `json:"id"`
	// Text is the human-readable description of the change.
	Text string `json:"text"`
	// Level is the check's severity: "error", "warning", or "info".
	Level string `json:"level"`
	// Operation is the HTTP method of the affected operation (e.g. "GET"),
	// empty for changes not tied to a specific operation.
	Operation string `json:"operation,omitempty"`
	// Path is the affected OpenAPI path (e.g. "/widgets"), empty for
	// changes not tied to a specific path.
	Path string `json:"path,omitempty"`
	// Breaking reports whether this change can break existing clients
	// (oasdiff's ERR and WARN levels; INFO is non-breaking).
	Breaking bool `json:"breaking"`
}

// Result is the outcome of comparing a base and head OpenAPI spec.
type Result struct {
	// BreakingChanges holds the subset of Changes with Breaking == true.
	BreakingChanges []Change `json:"breakingChanges"`
	// Changes holds every detected change, breaking or not.
	Changes []Change `json:"changes"`
	// HasBreakingChanges reports whether BreakingChanges is non-empty.
	HasBreakingChanges bool `json:"hasBreakingChanges"`
}

// Check loads the OpenAPI specs at basePath and headPath via kin-openapi,
// diffs them with oasdiff, and classifies the differences as breaking or
// non-breaking per §13.4's "Compare base and head, Detect breaking
// changes". The aggregator that runs oasdiff's full backward-compatibility
// check registry in one call is checker.CheckBackwardCompatibility(config,
// diffReport, operationsSources); config comes from
// checker.NewConfig(checker.GetAllChecks()), which wires up every built-in
// check (the same registry the oasdiff CLI's `breaking` command runs at
// its default WARN threshold, per checker.Level.IsBreaking treating both
// ERR and WARN as breaking).
//
// operationsSources (used only to annotate findings with source file
// locations) is passed as an empty, non-nil map: Check loads specs as
// plain *openapi3.T values via openapi3.Loader.LoadFromFile rather than
// oasdiff's own load.SpecInfo wrapper, so no source-location map is
// available to pass. It must still be non-nil, though: several checks
// (e.g. ResponseOptionalPropertyUpdatedCheck) build an ApiChange via
// checker.NewApiChange, which unconditionally dereferences the
// *diff.OperationsSourcesMap pointer to look up a source path, and a nil
// pointer there panics.
func Check(basePath, headPath string) (Result, error) {
	loader := openapi3.NewLoader()

	base, err := loader.LoadFromFile(basePath)
	if err != nil {
		return Result{}, fmt.Errorf("openapicheck: load base spec %q: %w", basePath, err)
	}
	head, err := loader.LoadFromFile(headPath)
	if err != nil {
		return Result{}, fmt.Errorf("openapicheck: load head spec %q: %w", headPath, err)
	}

	diffReport, err := diff.Get(diff.NewConfig(), base, head)
	if err != nil {
		return Result{}, fmt.Errorf("openapicheck: diff %q and %q: %w", basePath, headPath, err)
	}

	config := checker.NewConfig(checker.GetAllChecks())
	operationsSources := diff.OperationsSourcesMap{}
	checkerChanges := checker.CheckBackwardCompatibility(config, diffReport, &operationsSources)

	localizer := checker.NewDefaultLocalizer()
	result := Result{
		Changes:         make([]Change, 0, len(checkerChanges)),
		BreakingChanges: make([]Change, 0),
	}
	for _, cc := range checkerChanges {
		level := cc.GetLevel()
		change := Change{
			Id:        cc.GetId(),
			Text:      cc.GetUncolorizedText(localizer),
			Level:     level.String(),
			Operation: cc.GetOperation(),
			Path:      cc.GetPath(),
			Breaking:  level.IsBreaking(),
		}
		result.Changes = append(result.Changes, change)
		if change.Breaking {
			result.BreakingChanges = append(result.BreakingChanges, change)
		}
	}
	result.HasBreakingChanges = len(result.BreakingChanges) > 0

	return result, nil
}

// WriteEvidence marshals result as api-diff.json-shaped JSON (§17.2's
// evidence bundle file of the same name) and writes it into bundle via
// Bundle.Path("api-diff.json").
func WriteEvidence(bundle *evidence.Bundle, result Result) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("openapicheck: marshal result: %w", err)
	}
	if err := os.WriteFile(bundle.Path("api-diff.json"), data, 0o644); err != nil {
		return fmt.Errorf("openapicheck: write api-diff.json: %w", err)
	}
	return nil
}
