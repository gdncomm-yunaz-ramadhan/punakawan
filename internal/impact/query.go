package impact

import "github.com/ygrip/punakawan/pkg/protocol"

// ImpactResult is the answer to "if the subject changes, what is affected?",
// per §26. It is defined here (not in protocol/) because it is a derived query
// view over the persisted nodes/edges, not a stored entity. DirectImpact is the
// subject's immediate neighbors; TransitiveImpact is everything else reachable
// within the query depth. The remaining slices are the same affected set sliced
// by the questions a reviewer actually asks - which repositories, tests,
// deployments, and owners are touched, and where coverage is missing.
type ImpactResult struct {
	// DirectImpact is nodes exactly one hop from the subject.
	DirectImpact []protocol.ImpactNode
	// TransitiveImpact is nodes two or more hops from the subject, within
	// depth. When the query supplies includeTypes it is filtered to those
	// node types (DirectImpact is not filtered - immediate neighbors always
	// matter).
	TransitiveImpact []protocol.ImpactNode
	// AffectedRepositories is the unique set of repositories owning any
	// affected node (from each node's repository attribute).
	AffectedRepositories []string
	// AffectedTests is affected nodes of type test.
	AffectedTests []protocol.ImpactNode
	// DeploymentArtifacts is affected nodes of type deployment_artifact.
	DeploymentArtifacts []protocol.ImpactNode
	// Owners is affected nodes of type team_owner.
	Owners []protocol.ImpactNode
	// MissingCoverage is affected source_symbol/api_operation nodes with no
	// incoming `tests` edge - the change reaches them but nothing tests them.
	MissingCoverage []protocol.ImpactNode
	// RelatedContradictions surfaces the counterpart of any disputed-confidence
	// or `contradicts` edge touching the affected set, so an impact query also
	// warns that part of the graph is itself in dispute (§30).
	RelatedContradictions []string
}

// Query runs a cycle-safe breadth-first traversal from subjectID over both
// outgoing (from == cur) and incoming (to == cur) edges, up to depth hops, and
// summarizes what it reached (§26/§28). Traversing edges in both directions is
// deliberate: a change to a symbol affects both what it calls (outgoing) and
// what calls it (incoming). A visited set makes cycles (A->B->A) terminate. The
// subject itself is never reported as its own impact, but it does participate
// in the aggregate slices (e.g. a subject symbol with no test surfaces in
// MissingCoverage). depth <= 0 yields an empty impact set.
func Query(root, subjectID string, depth int, includeTypes []string) (ImpactResult, error) {
	nodes, err := Nodes(root)
	if err != nil {
		return ImpactResult{}, err
	}
	edges, err := Edges(root)
	if err != nil {
		return ImpactResult{}, err
	}

	// BFS over both edge directions with a visited set for cycle safety.
	levelOf := map[string]int{subjectID: 0}
	visited := map[string]bool{subjectID: true}
	queue := []string{subjectID}
	relatedSeen := map[string]bool{}
	var related []string

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		d := levelOf[cur]
		if d >= depth {
			// Reached the depth limit; do not expand further from cur.
			continue
		}
		for _, e := range edges {
			var nb string
			switch {
			case e.From == cur:
				nb = e.To
			case e.To == cur:
				nb = e.From
			default:
				continue
			}
			// A disputed or `contradicts` edge means this part of the graph is
			// itself in dispute; surface the counterpart node regardless of
			// whether we have already visited it.
			if e.Confidence == protocol.ImpactEdgeConfidenceDisputed || e.Type == protocol.ImpactEdgeTypeContradicts {
				if !relatedSeen[nb] {
					relatedSeen[nb] = true
					related = append(related, nb)
				}
			}
			if !visited[nb] {
				visited[nb] = true
				levelOf[nb] = d + 1
				queue = append(queue, nb)
			}
		}
	}

	// Build an incoming-tests index once, so MissingCoverage is O(nodes+edges)
	// rather than O(nodes*edges).
	hasIncomingTest := map[string]bool{}
	for _, e := range edges {
		if e.Type == protocol.ImpactEdgeTypeTests {
			hasIncomingTest[e.To] = true
		}
	}

	res := ImpactResult{RelatedContradictions: related}
	seenRepo := map[string]bool{}
	// Iterate nodes in their stable first-seen order so the result is
	// deterministic (map iteration order is not).
	for _, n := range nodes {
		if !visited[n.Id] {
			continue
		}
		// Aggregate slices include the subject; the DirectImpact/
		// TransitiveImpact split excludes it (a node is not its own impact).
		if n.Repository != nil && *n.Repository != "" && !seenRepo[*n.Repository] {
			seenRepo[*n.Repository] = true
			res.AffectedRepositories = append(res.AffectedRepositories, *n.Repository)
		}
		switch n.Type {
		case protocol.ImpactNodeTypeTest:
			res.AffectedTests = append(res.AffectedTests, n)
		case protocol.ImpactNodeTypeDeploymentArtifact:
			res.DeploymentArtifacts = append(res.DeploymentArtifacts, n)
		case protocol.ImpactNodeTypeTeamOwner:
			res.Owners = append(res.Owners, n)
		case protocol.ImpactNodeTypeSourceSymbol, protocol.ImpactNodeTypeApiOperation:
			if !hasIncomingTest[n.Id] {
				res.MissingCoverage = append(res.MissingCoverage, n)
			}
		}

		if n.Id == subjectID {
			continue
		}
		if levelOf[n.Id] == 1 {
			res.DirectImpact = append(res.DirectImpact, n)
		} else {
			res.TransitiveImpact = append(res.TransitiveImpact, n)
		}
	}

	if len(includeTypes) > 0 {
		res.TransitiveImpact = filterByTypes(res.TransitiveImpact, includeTypes)
	}
	return res, nil
}

// filterByTypes keeps only nodes whose type is in want.
func filterByTypes(nodes []protocol.ImpactNode, want []string) []protocol.ImpactNode {
	set := make(map[string]bool, len(want))
	for _, w := range want {
		set[w] = true
	}
	out := make([]protocol.ImpactNode, 0, len(nodes))
	for _, n := range nodes {
		if set[string(n.Type)] {
			out = append(out, n)
		}
	}
	return out
}
