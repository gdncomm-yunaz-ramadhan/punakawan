// Package convention detects a repository's existing conventions —
// .editorconfig, linter/formatter configuration, directory layout, and
// package-manager/build tooling — and normalizes them into a
// protocol.KnowledgeRecord of type convention-profile, per
// punakawan-go-typescript-detailed-plan.md §2.7, §13.5, and §27.
//
// This package only extracts and normalizes; it does not apply a profile
// during implementation (§27.5) or drive baseline scaffolding (§27.6). Those
// are Petruk/M3-era capabilities layered on top of the record this package
// produces.
package convention

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// extractorVersion identifies this heuristic extractor's own version, so a
// ConventionProfile records which version of the detection logic produced
// it (§7.3's extraction.extractor_version).
const extractorVersion = "0.1.0"

// confidenceObserved and confidenceInferred are the two confidence bands
// used by this extractor, per §27.4: explicit configuration files are
// recorded as observed with high confidence, while patterns inferred purely
// from directory structure (no explicit config backing them) get a lower
// confidence score. These are heuristic constants, not derived from any
// formula in the plan; 0.9/0.6 are picked to sit clearly on either side of
// the 0.5 midpoint while leaving room above and below for future signals.
const (
	confidenceObserved = 0.9
	confidenceInferred = 0.6
)

// skipDirs are directory names ignored both when sampling for naming
// convention and when looking for monorepo package directories, since they
// are either VCS/tooling internals or vendored/third-party code that says
// nothing about this repository's own conventions.
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	".git":         true,
}

// Extract inspects the repository at repoPath and produces a
// protocol.KnowledgeRecord of type convention-profile summarizing its
// detected formatting, linting, and structural conventions, per §27.3. It
// is a pure, read-only, dependency-free function: it only reads the local
// filesystem (and, best-effort, invokes `git rev-parse HEAD` to populate
// Source.Version) and never mutates the repository or talks to any
// external service.
//
// workspaceID and repoID are used to build the record's id
// (pkw:convention/<workspaceID>/<repoID>, per §6.2 and the provenance id
// pattern enforced by internal/knowledge.Validate) and its Source.Uri.
//
// Extract does not itself call internal/knowledge.Validate or Store.Put;
// callers are expected to do so (see internal/knowledge/service.go's Put).
func Extract(repoPath, workspaceID, repoID string) (protocol.KnowledgeRecord, error) {
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return protocol.KnowledgeRecord{}, err
	}

	names := make(map[string]bool, len(entries))
	dirNames := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
		if e.IsDir() {
			dirNames[e.Name()] = true
		}
	}
	hasFile := func(name string) bool { return names[name] }
	hasDir := func(name string) bool { return dirNames[name] }
	hasGlob := func(pattern string) bool {
		matches, _ := filepath.Glob(filepath.Join(repoPath, pattern))
		return len(matches) > 0
	}

	// allObserved tracks whether every signal contributing to this profile
	// came from an explicit config file (observed) or whether at least one
	// came from inference over the directory tree. Per §27.4, a record
	// must not claim validity.state: observed if any of its fields are
	// only inferred, so the overall state is the more conservative of the
	// two once every detector below has run.
	allObserved := true

	formatting := &protocol.KnowledgeRecordFormatting{}

	if hasFile(".editorconfig") {
		t := true
		formatting.Editorconfig = &t
	}

	var linters, formatters []string

	// ESLint: .eslintrc* (any extension) or the flat-config eslint.config.*.
	if hasGlob(".eslintrc*") || hasGlob("eslint.config.*") {
		linters = append(linters, "eslint")
	}
	// Prettier: .prettierrc* or prettier.config.*.
	if hasGlob(".prettierrc*") || hasGlob("prettier.config.*") {
		formatters = append(formatters, "prettier")
	}
	// golangci-lint: .golangci.yml/.yaml/.toml.
	if hasFile(".golangci.yml") || hasFile(".golangci.yaml") || hasFile(".golangci.toml") {
		linters = append(linters, "golangci-lint")
	}
	// gofmt: inferred from a go.mod plus at least one *.go file — gofmt has
	// no separate config file of its own, so its presence is really a
	// statement "this is a Go module with Go source", which is as close to
	// an explicit signal as gofmt gets (go.mod is itself an explicit,
	// version-controlled config file), so this still counts as observed.
	if hasFile("go.mod") && hasGlob("*.go") {
		formatters = append(formatters, "gofmt")
	}
	// stylelint: .stylelintrc*.
	if hasGlob(".stylelintrc*") {
		linters = append(linters, "stylelint")
	}
	// rustfmt: rustfmt.toml, or a Cargo.toml (rustfmt's defaults apply to
	// any cargo project even without a dedicated rustfmt.toml).
	if hasFile("rustfmt.toml") || hasFile("Cargo.toml") {
		formatters = append(formatters, "rustfmt")
	}

	formatting.Linters = linters
	formatting.Formatters = formatters

	structure := &protocol.KnowledgeRecordStructure{}

	// Package manager: check lockfiles first since they unambiguously name
	// a single package manager; go.mod/Cargo.toml are checked too but only
	// matter when no JS lockfile is present, since a repo could vendor a Go
	// module inside a JS-managed monorepo (JS lockfile still wins as the
	// top-level package manager in that case).
	switch {
	case hasFile("pnpm-lock.yaml"):
		structure.PackageManager = strPtr("pnpm")
	case hasFile("package-lock.json"):
		structure.PackageManager = strPtr("npm")
	case hasFile("yarn.lock"):
		structure.PackageManager = strPtr("yarn")
	case hasFile("go.mod"):
		structure.PackageManager = strPtr("go modules")
	case hasFile("Cargo.toml"):
		structure.PackageManager = strPtr("cargo")
	}

	// Layout: a monorepo needs both a container for multiple
	// packages/apps (packages/ or apps/ directory) and an explicit
	// multi-package declaration (root package.json "workspaces" field, or
	// pnpm-workspace.yaml) — the directory alone could just be app source
	// code named "packages". Absent both signals, default to "single"
	// rather than guessing further; this is the smallest reasonable call
	// per the task's own guidance to avoid wild guessing when signals are
	// absent.
	hasPackagesOrApps := hasDir("packages") || hasDir("apps")
	hasWorkspaceDeclaration := hasFile("pnpm-workspace.yaml") || packageJSONHasWorkspaces(repoPath)
	if hasPackagesOrApps && hasWorkspaceDeclaration {
		structure.Layout = strPtr("monorepo")
	} else {
		structure.Layout = strPtr("single")
	}

	// Naming convention: sampled from top-level directory names. This is
	// inference over the tree, not an explicit config file, so per §27.4
	// it always contributes to the inferred side of the overall record
	// even when a clear majority is found.
	if nc, ok := detectNamingConvention(entries); ok {
		structure.NamingConvention = strPtr(nc)
		allObserved = false
	}

	source := protocol.KnowledgeRecordSource{
		Provider:    "git",
		Uri:         strPtr("repo://" + repoID),
		RetrievedAt: time.Now().UTC(),
	}
	if sha, ok := gitHeadSHA(repoPath); ok {
		source.Version = sha
	}

	confidence := confidenceObserved
	state := protocol.KnowledgeRecordValidityStateObserved
	if !allObserved {
		confidence = confidenceInferred
		state = protocol.KnowledgeRecordValidityStateInferred
	}

	rec := protocol.KnowledgeRecord{
		Id:     "pkw:convention/" + workspaceID + "/" + repoID,
		Type:   protocol.KnowledgeRecordTypeConventionProfile,
		Status: "active",
		Title:  repoID + " convention profile",
		Source: source,
		Extraction: protocol.KnowledgeRecordExtraction{
			// This record is produced by heuristic code inspecting the
			// filesystem, not authored by hand (manual) or pulled in from
			// another knowledge system (imported), so model-assisted is
			// the closest fit among the three enum values even though no
			// model call actually happens here — it is "assisted"
			// detection logic acting in place of a human or an external
			// importer.
			Method:           protocol.KnowledgeRecordExtractionMethodModelAssisted,
			ExtractorVersion: strPtr(extractorVersion),
			Confidence:       &confidence,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State: state,
		},
		Formatting: formatting,
		Structure:  structure,
	}
	return rec, nil
}

// packageJSONHasWorkspaces reports whether a root package.json declares a
// "workspaces" field. It is intentionally minimal (a substring scan on the
// raw bytes) rather than a full JSON parse, since the only thing that
// matters here is whether the key is present at all.
func packageJSONHasWorkspaces(repoPath string) bool {
	data, err := os.ReadFile(filepath.Join(repoPath, "package.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), `"workspaces"`)
}

var (
	kebabPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	snakePattern = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)
	camelPattern = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
)

// detectNamingConvention samples top-level directory names (skipping
// dotfiles and skipDirs) and reports the convention if a clear majority of
// the sampled directories agree, per §27.4's "inferred" example
// (kebab-case-dirs). "Clear majority" is defined as: every single-word
// directory name is compatible with more than one convention (e.g. "docs"
// is simultaneously valid kebab/snake/camelCase), so those are ignored when
// they don't discriminate; only multi-word names (containing a separator or
// a case transition) are used as evidence, and a convention wins only if it
// accounts for all such discriminating names with no conflicting signal.
// If there are no discriminating names, or if more than one convention
// matches, the result is inconclusive and NamingConvention is left unset
// rather than fabricated.
func detectNamingConvention(entries []os.DirEntry) (string, bool) {
	kebab, snake, camel := 0, 0, 0
	discriminating := 0

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || skipDirs[name] {
			continue
		}

		isKebab := kebabPattern.MatchString(name) && strings.Contains(name, "-")
		isSnake := snakePattern.MatchString(name) && strings.Contains(name, "_")
		isCamel := camelPattern.MatchString(name) && strings.ToLower(name) != name

		if !isKebab && !isSnake && !isCamel {
			continue
		}
		discriminating++
		if isKebab {
			kebab++
		}
		if isSnake {
			snake++
		}
		if isCamel {
			camel++
		}
	}

	if discriminating == 0 {
		return "", false
	}

	switch {
	case kebab == discriminating && snake == 0 && camel == 0:
		return "kebab-case-dirs", true
	case snake == discriminating && kebab == 0 && camel == 0:
		return "snake-case-dirs", true
	case camel == discriminating && kebab == 0 && snake == 0:
		return "camel-case-dirs", true
	default:
		return "", false
	}
}

// gitHeadSHA best-effort resolves the repository's current commit SHA via
// `git rev-parse HEAD`. This is the one place Extract shells out, and only
// for this optional, read-only, side-effect-free lookup: on any error
// (git not installed, not a git repo, detached weirdness, etc.) it just
// reports ok=false so the caller leaves Source.Version unset rather than
// failing the whole extraction.
func gitHeadSHA(repoPath string) (string, bool) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return "", false
	}
	return sha, true
}

func strPtr(s string) *string { return &s }
