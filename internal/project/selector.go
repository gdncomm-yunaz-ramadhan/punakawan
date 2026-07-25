package project

import "strings"

// DefaultSelectionLimit bounds how many metadata entries a single selection
// yields, per §4.4's "general project context with a strict limit":
// metadata must never be dumped wholesale into a prompt.
const DefaultSelectionLimit = 8

// MetadataSelector chooses the subset of a project's metadata relevant to one
// agent step, per §4.4. It exists so metadata stays generic (plain
// key/description/value) yet is never handed to an agent in bulk.
type MetadataSelector interface {
	Select(p Project, capability, intent string, requestedKeys []string) []MetadataEntry
}

// PrioritySelector is the default MetadataSelector. It applies §4.4's
// priority order, deterministically and without duplicates:
//
//  1. explicitly requested keys (in the order requested);
//  2. entries in the capability's key namespace (key == capability or
//     key has prefix "<capability>.");
//  3. entries whose key exactly matches the intent;
//  4. remaining entries in their stored order, as general context,
//
// truncated to Limit (DefaultSelectionLimit when zero).
type PrioritySelector struct {
	Limit int
}

// Select implements MetadataSelector.
func (s PrioritySelector) Select(p Project, capability, intent string, requestedKeys []string) []MetadataEntry {
	limit := s.Limit
	if limit <= 0 {
		limit = DefaultSelectionLimit
	}

	// Index by lowercase key for case-insensitive lookup, preserving the
	// stored entry (and its original casing) as the value.
	byKey := make(map[string]MetadataEntry, len(p.Metadata))
	for _, e := range p.Metadata {
		byKey[strings.ToLower(strings.TrimSpace(e.Key))] = e
	}

	var out []MetadataEntry
	seen := make(map[string]bool, len(p.Metadata))
	add := func(e MetadataEntry) bool {
		lk := strings.ToLower(strings.TrimSpace(e.Key))
		if seen[lk] {
			return true
		}
		seen[lk] = true
		out = append(out, e)
		return len(out) < limit
	}

	// 1. Explicitly requested keys, in request order.
	for _, k := range requestedKeys {
		if e, ok := byKey[strings.ToLower(strings.TrimSpace(k))]; ok {
			if !add(e) {
				return out
			}
		}
	}

	// 2. Capability namespace prefix match (stored order).
	if capNS := strings.ToLower(strings.TrimSpace(capability)); capNS != "" {
		prefix := capNS + "."
		for _, e := range p.Metadata {
			lk := strings.ToLower(strings.TrimSpace(e.Key))
			if lk == capNS || strings.HasPrefix(lk, prefix) {
				if !add(e) {
					return out
				}
			}
		}
	}

	// 3. Exact intent match.
	if in := strings.ToLower(strings.TrimSpace(intent)); in != "" {
		if e, ok := byKey[in]; ok {
			if !add(e) {
				return out
			}
		}
	}

	// 4. General fill, stored order, until the limit.
	for _, e := range p.Metadata {
		if !add(e) {
			return out
		}
	}
	return out
}
