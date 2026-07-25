// Package impact is the stateless store and query engine for a Punakawan
// project's Cross-Repository Impact Graph, per
// punakawan-role-config-distinguished-improvements-plan.md Part III §23-31. The
// graph answers "if I change X, what else is affected?" across repository
// boundaries - which tests, deployments, owners, and downstream symbols a
// change reaches, and where coverage is missing.
//
// Like internal/project and internal/roleconfig this package is stateless:
// every function is keyed by a workspace `root` string and reads/writes files
// under <root>/.punakawan/impact/. Nodes and edges are persisted append-only as
// JSONL (nodes.jsonl, edges.jsonl) and folded to the latest value per identity
// on read, exactly like internal/approvals folds its append-only history by id.
// Append-only means a builder re-run never has to find-and-mutate prior lines;
// it just appends the current truth and the fold makes the newest line win. A
// missing file is a normal empty graph, never an error.
package impact

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ygrip/punakawan/pkg/protocol"
)

const (
	dirName   = ".punakawan"
	subDir    = "impact"
	nodesFile = "nodes.jsonl"
	edgesFile = "edges.jsonl"
)

// impactDir returns <root>/.punakawan/impact.
func impactDir(root string) string {
	return filepath.Join(root, dirName, subDir)
}

// edgeKey is the identity an edge is deduplicated by on read: the same
// (from, to, type) triple upserted twice folds to the latest line, so a builder
// can re-emit an edge idempotently without ever mutating a prior line.
func edgeKey(e protocol.ImpactEdge) string {
	return e.From + "\x00" + e.To + "\x00" + string(e.Type)
}

// UpsertNode appends n to nodes.jsonl. It is idempotent by node id: appending a
// node whose id already exists is not an error - Nodes folds to the latest line
// per id, so the newest UpsertNode wins. Append-only means concurrent builders
// never corrupt each other's lines.
func UpsertNode(root string, n protocol.ImpactNode) error {
	if n.Id == "" {
		return fmt.Errorf("impact: upsert node with empty id")
	}
	return appendJSONL(filepath.Join(impactDir(root), nodesFile), n)
}

// UpsertEdge appends e to edges.jsonl. It is idempotent by (from, to, type):
// Edges folds to the latest line per that triple, so re-emitting an edge with,
// say, an upgraded confidence simply supersedes the prior line.
func UpsertEdge(root string, e protocol.ImpactEdge) error {
	if e.From == "" || e.To == "" {
		return fmt.Errorf("impact: upsert edge with empty from/to")
	}
	return appendJSONL(filepath.Join(impactDir(root), edgesFile), e)
}

// Nodes returns all nodes folded to the latest line per id, in first-seen
// order (so results are deterministic regardless of how many times a node was
// re-upserted). A missing file is a normal empty graph.
func Nodes(root string) ([]protocol.ImpactNode, error) {
	path := filepath.Join(impactDir(root), nodesFile)
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}
	latest := make(map[string]protocol.ImpactNode)
	order := make([]string, 0, len(lines))
	for _, line := range lines {
		var n protocol.ImpactNode
		if uerr := json.Unmarshal(line, &n); uerr != nil {
			return nil, fmt.Errorf("impact: decode node in %s: %w", path, uerr)
		}
		if _, seen := latest[n.Id]; !seen {
			order = append(order, n.Id)
		}
		latest[n.Id] = n
	}
	out := make([]protocol.ImpactNode, 0, len(order))
	for _, id := range order {
		out = append(out, latest[id])
	}
	return out, nil
}

// Edges returns all edges folded to the latest line per (from, to, type), in
// first-seen order.
func Edges(root string) ([]protocol.ImpactEdge, error) {
	path := filepath.Join(impactDir(root), edgesFile)
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}
	latest := make(map[string]protocol.ImpactEdge)
	order := make([]string, 0, len(lines))
	for _, line := range lines {
		var e protocol.ImpactEdge
		if uerr := json.Unmarshal(line, &e); uerr != nil {
			return nil, fmt.Errorf("impact: decode edge in %s: %w", path, uerr)
		}
		k := edgeKey(e)
		if _, seen := latest[k]; !seen {
			order = append(order, k)
		}
		latest[k] = e
	}
	out := make([]protocol.ImpactEdge, 0, len(order))
	for _, k := range order {
		out = append(out, latest[k])
	}
	return out, nil
}

// GetNode returns the folded node for id and whether it exists.
func GetNode(root, id string) (protocol.ImpactNode, bool, error) {
	nodes, err := Nodes(root)
	if err != nil {
		return protocol.ImpactNode{}, false, err
	}
	for _, n := range nodes {
		if n.Id == id {
			return n, true, nil
		}
	}
	return protocol.ImpactNode{}, false, nil
}

// appendJSONL appends one JSON-encoded record as a line to path, creating the
// directory and file as needed.
func appendJSONL(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("impact: mkdir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("impact: open %s: %w", path, err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(v); err != nil {
		return fmt.Errorf("impact: encode record to %s: %w", path, err)
	}
	return nil
}

// readLines returns every non-empty line of path, or nil if the file does not
// exist (a normal empty graph, never an error - mirrors internal/approvals).
func readLines(path string) ([][]byte, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("impact: open %s: %w", path, err)
	}
	defer f.Close()

	var lines [][]byte
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		b := scanner.Bytes()
		if len(b) == 0 {
			continue
		}
		// scanner.Bytes reuses its buffer between iterations, so copy before
		// retaining the line for the caller.
		cp := make([]byte, len(b))
		copy(cp, b)
		lines = append(lines, cp)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("impact: scan %s: %w", path, err)
	}
	return lines, nil
}
