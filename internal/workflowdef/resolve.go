package workflowdef

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// Resolution and input errors. Callers branch on these with errors.Is.
var (
	// ErrNoSelectorMatch is returned by Resolve when no enabled definition has
	// a selector exactly matching the query (plan §4.2 rule 3: no workflow).
	ErrNoSelectorMatch = errors.New("workflowdef: no workflow matches the selector")
	// ErrAmbiguousSelector is returned by Resolve when more than one enabled
	// definition matches (plan §4.2 rule 4: return candidates, do not guess).
	ErrAmbiguousSelector = errors.New("workflowdef: selector matches more than one workflow")
	// ErrRevisionMismatch is returned by Resolve when an explicit id is found
	// but its revision differs from the caller-pinned revision.
	ErrRevisionMismatch = errors.New("workflowdef: definition revision does not match")
	// ErrMissingInput is returned by ResolveInputs when a required input has no
	// provided value and no default.
	ErrMissingInput = errors.New("workflowdef: required input missing")
)

// ContentHash returns a deterministic content fingerprint of the definition,
// as "sha256:<hex>". It is stored on a run's definition_ref so a historical
// run records the exact definition content it was created from, independent of
// the mutable revision counter — if the on-disk definition is later edited,
// the recorded hash still identifies what actually ran (plan §4.1).
//
// The hash is taken over the canonical JSON encoding of the definition. Go's
// json.Marshal emits struct fields in declaration order and map keys sorted,
// so the encoding is stable across runs for identical content.
func (d Definition) ContentHash() string {
	b, err := json.Marshal(d)
	if err != nil {
		// Definition is plain data with no unmarshalable fields; a marshal
		// error is not reachable in practice, but fall back to a value that is
		// obviously not a real hash rather than panicking.
		return "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Query selects a workflow definition either explicitly by id (optionally
// pinned to a revision) or implicitly by an exact capability/intent selector
// (plan §4.2). ID takes precedence; when ID is empty, Capability drives an
// implicit lookup.
type Query struct {
	ID         string
	Revision   *int
	Capability string
	Intent     string
}

// Resolve applies the plan §4.2 resolution order to defs:
//
//  1. an explicit id (with optional pinned revision) supplied by the caller;
//  2. otherwise, one enabled definition whose selector exactly matches the
//     query's capability and intent;
//  3. no definition if nothing matches (ErrNoSelectorMatch);
//  4. the matching candidates, unselected, if more than one matches
//     (ErrAmbiguousSelector) — the caller must disambiguate, never guess.
//
// On the ambiguous case Resolve returns the candidate slice alongside the
// error so the caller can present the choices.
func Resolve(defs []Definition, q Query) (Definition, []Definition, error) {
	if q.ID != "" {
		for _, d := range defs {
			if d.ID != q.ID {
				continue
			}
			if q.Revision != nil && d.Revision != *q.Revision {
				return Definition{}, nil, fmt.Errorf("%w: %q revision %d (have %d)", ErrRevisionMismatch, q.ID, *q.Revision, d.Revision)
			}
			return d, nil, nil
		}
		return Definition{}, nil, fmt.Errorf("%w: %q", ErrNoSelectorMatch, q.ID)
	}

	var matches []Definition
	for _, d := range defs {
		if !d.Enabled {
			continue
		}
		for _, sel := range d.Selectors {
			if sel.Capability == q.Capability && sel.Intent == q.Intent {
				matches = append(matches, d)
				break
			}
		}
	}
	switch len(matches) {
	case 0:
		return Definition{}, nil, fmt.Errorf("%w: capability %q intent %q", ErrNoSelectorMatch, q.Capability, q.Intent)
	case 1:
		return matches[0], nil, nil
	default:
		return Definition{}, matches, fmt.Errorf("%w: capability %q intent %q (%d candidates)", ErrAmbiguousSelector, q.Capability, q.Intent, len(matches))
	}
}

// ResolveInputs validates and defaults a definition's declared inputs against
// the values the caller provided (plan §4.4 step 2). It returns the resolved
// input map: each declared input takes the provided value, else its declared
// default. A required input with neither is an ErrMissingInput naming every
// such input. Values the caller supplied for undeclared inputs are preserved
// so callers can pass extra context without it being silently dropped.
func ResolveInputs(def Definition, provided map[string]any) (map[string]any, error) {
	resolved := make(map[string]any, len(provided)+len(def.Inputs))
	for k, v := range provided {
		resolved[k] = v
	}
	var missing []string
	for _, in := range def.Inputs {
		if _, ok := resolved[in.Name]; ok {
			continue
		}
		if in.Default != nil {
			resolved[in.Name] = in.Default
			continue
		}
		if in.Required {
			missing = append(missing, in.Name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %v", ErrMissingInput, missing)
	}
	return resolved, nil
}
