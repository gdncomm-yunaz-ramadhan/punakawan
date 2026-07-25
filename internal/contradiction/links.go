package contradiction

import "github.com/ygrip/punakawan/pkg/protocol"

// unionIDs returns the union of a and b: a's ids in their original order,
// followed by any id in b not already present. Empty strings are dropped. An
// all-empty input yields a nil slice so an empty section stays omitted from the
// serialized record rather than materializing as an empty list.
func unionIDs(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	var out []string
	for _, group := range [][]string{a, b} {
		for _, id := range group {
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

// hasLinks reports whether any Links section carries at least one id, used to
// avoid attaching an empty Links block to a record that references nothing.
func hasLinks(l protocol.ContradictionLinks) bool {
	return len(l.Dossiers) > 0 || len(l.Handoffs) > 0 || len(l.Plans) > 0 ||
		len(l.Repositories) > 0 || len(l.Tasks) > 0
}

// MergeLinks unions the id slices of existing and add field-by-field without
// duplicates, so repeated detection of the same contradiction enriches its
// Links rather than clobbering ones an earlier caller recorded (§22 detail
// "affected ..."). Order is stable: existing ids keep their position and newly
// seen ids from add follow, which keeps the serialized record diff-friendly.
func MergeLinks(existing, add protocol.ContradictionLinks) protocol.ContradictionLinks {
	return protocol.ContradictionLinks{
		Dossiers:     unionIDs(existing.Dossiers, add.Dossiers),
		Handoffs:     unionIDs(existing.Handoffs, add.Handoffs),
		Plans:        unionIDs(existing.Plans, add.Plans),
		Repositories: unionIDs(existing.Repositories, add.Repositories),
		Tasks:        unionIDs(existing.Tasks, add.Tasks),
	}
}

// SetLinks merges links into the stored record's Links section (merge, not
// replace - see MergeLinks) and versions the result through the normal Put
// path, returning the persisted record. It fails with ErrNotFound if id has no
// record: you cannot attach affected-entity links to a contradiction that was
// never detected. This is the primary API for callers that hold real entity
// ids (impact nodes, task/plan ids, repositories, dossiers, handoffs); it takes
// plain data so this package depends on no other subsystem.
func SetLinks(root, id string, links protocol.ContradictionLinks, opts PutOptions) (protocol.Contradiction, error) {
	c, err := Get(root, id)
	if err != nil {
		return protocol.Contradiction{}, err
	}
	var existing protocol.ContradictionLinks
	if c.Links != nil {
		existing = *c.Links
	}
	merged := MergeLinks(existing, links)
	if hasLinks(merged) {
		c.Links = &merged
	}
	if err := Put(root, *c, opts); err != nil {
		return protocol.Contradiction{}, err
	}
	stored, err := Get(root, id)
	if err != nil {
		return protocol.Contradiction{}, err
	}
	return *stored, nil
}

// deriveSelfLinks returns the Links implied by a contradiction's own claim
// sources. It is deliberately conservative: a claim contributes a link only
// when its source type maps unambiguously onto a Links taxonomy slice AND the
// source carries a Ref to use as the entity id. Today that is:
//
//   - source type "plan"       -> Plans        (Ref is a plan id)
//   - source type "repository" -> Repositories (Ref is a repository id)
//
// Other source types (jira, confluence, knowledge, test, openapi, metadata,
// other) are intentionally excluded: their Ref is an issue key, page id, or
// file path - not an id in the Links taxonomy - so mapping them would invent
// links that do not exist. Callers holding real entity ids attach them
// explicitly via SetLinks. This function reads only c's own claims, so it adds
// no dependency on any other subsystem and cannot cycle.
func deriveSelfLinks(c protocol.Contradiction) protocol.ContradictionLinks {
	var links protocol.ContradictionLinks
	for _, claim := range c.Claims {
		if claim.Source.Ref == nil || *claim.Source.Ref == "" {
			continue
		}
		ref := *claim.Source.Ref
		switch claim.Source.Type {
		case protocol.ContradictionClaimsElemSourceTypePlan:
			links.Plans = append(links.Plans, ref)
		case protocol.ContradictionClaimsElemSourceTypeRepository:
			links.Repositories = append(links.Repositories, ref)
		}
	}
	return links
}
