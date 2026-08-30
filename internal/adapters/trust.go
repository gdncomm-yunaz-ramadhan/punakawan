package adapters

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TrustEntry is one repository-local adapter executable a host operator has
// agreed to run: its own normalized absolute path, and the SHA-256 digest
// that file must still hash to.
type TrustEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// TrustStore is the parsed contents of a host-owned adapter trust file.
// A nil *TrustStore trusts nothing, the same as an empty one.
type TrustStore struct {
	digestByPath map[string]string
}

// LoadTrustFile reads and parses the trust file at path: a JSON array of
// TrustEntry values. A missing file is not an error - it parses as an
// empty TrustStore, so a host that has never trusted anything simply
// rejects every repository-local adapter command until an operator adds
// one (see SeedTrustFile for the one supported way to add one today).
func LoadTrustFile(path string) (*TrustStore, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &TrustStore{digestByPath: map[string]string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("adapters: read trust file %s: %w", path, err)
	}

	var entries []TrustEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("adapters: parse trust file %s: %w", path, err)
	}

	digestByPath := make(map[string]string, len(entries))
	for _, e := range entries {
		digestByPath[filepath.Clean(e.Path)] = strings.ToLower(e.SHA256)
	}
	return &TrustStore{digestByPath: digestByPath}, nil
}

// SeedTrustFile writes entries to path in the trust file's documented
// format, overwriting anything already there. It is deliberately the only
// way to populate a trust file today: no interactive or install-time tool
// calls it yet, so an operator (or a test) seeds one by calling this
// directly, or by hand-writing the same JSON array of {"path","sha256"}
// objects this function itself produces.
func SeedTrustFile(path string, entries []TrustEntry) error {
	normalized := make([]TrustEntry, len(entries))
	for i, e := range entries {
		normalized[i] = TrustEntry{Path: filepath.Clean(e.Path), SHA256: strings.ToLower(e.SHA256)}
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("adapters: encode trust file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("adapters: create trust file directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("adapters: write trust file %s: %w", path, err)
	}
	return nil
}

// trusts reports whether path (any form - it is normalized here) is
// present with a digest matching sha256Hex (case-insensitive).
func (t *TrustStore) trusts(path, sha256Hex string) bool {
	if t == nil {
		return false
	}
	want, ok := t.digestByPath[filepath.Clean(path)]
	return ok && want == strings.ToLower(sha256Hex)
}

// RequireTrustedIfRepositoryLocal enforces the executable-identity half of
// the outbox's enqueue-time trust requirement: a command whose resolved
// executable lives inside repoRoot (the current workspace checkout) can be
// swapped out by anyone who can write into that checkout, so Punakawan
// refuses to start it unless its exact path and content digest were
// explicitly trusted by this host beforehand. A command that resolves
// outside repoRoot - a bare name found on PATH, or an absolute path under
// an installed location such as the host data directory - is the ordinary
// trusted case and needs no entry.
//
// Deferred and not enforced here: the declared-permissions half of
// enqueue-time validation (checking an adapter's declared operations,
// input schema, and host/filesystem permissions before enqueue). The
// manifest schema does not yet carry that metadata for any command to
// validate against.
func RequireTrustedIfRepositoryLocal(command, repoRoot string, trust *TrustStore) error {
	resolved, local, err := resolveRepositoryLocalPath(command, repoRoot)
	if err != nil {
		return fmt.Errorf("adapters: resolve adapter command %q: %w", command, err)
	}
	if !local {
		return nil
	}

	digest, err := sha256File(resolved)
	if err != nil {
		return fmt.Errorf("adapters: hash repository-local adapter command %s: %w", resolved, err)
	}
	if !trust.trusts(resolved, digest) {
		return fmt.Errorf("adapters: repository-local adapter command %s (sha256 %s) is not present in the host trust file; add it before this workspace's adapters can run", resolved, digest)
	}
	return nil
}

// resolveRepositoryLocalPath resolves command to the absolute path of the
// executable it would actually run, and reports whether that path lives
// inside repoRoot. A bare command name (no path separator) is resolved
// against PATH the same way exec.Command would resolve it; if that lookup
// fails, command cannot be repository-local (there is nothing under
// repoRoot it could resolve to), so resolution is left to whatever later
// attempts to actually start the adapter.
func resolveRepositoryLocalPath(command, repoRoot string) (resolved string, local bool, err error) {
	if repoRoot == "" {
		return "", false, nil
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", false, fmt.Errorf("resolve workspace root: %w", err)
	}
	// Resolve symlinks on both root and candidate before comparing them -
	// otherwise a symlinked temp/data directory (e.g. macOS's /tmp ->
	// /private/tmp) makes an in-repo path look like it escapes repoRoot.
	if resolvedRoot, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		root = resolvedRoot
	}

	var candidate string
	switch {
	case filepath.IsAbs(command):
		candidate = command
	case strings.ContainsAny(command, "/\\"):
		candidate = filepath.Join(root, command)
	default:
		found, lookErr := exec.LookPath(command)
		if lookErr != nil {
			return "", false, nil
		}
		candidate = found
	}

	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", false, fmt.Errorf("resolve adapter command path: %w", err)
	}
	if resolvedSymlink, evalErr := filepath.EvalSymlinks(candidate); evalErr == nil {
		candidate = resolvedSymlink
	}

	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return candidate, false, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return candidate, false, nil
	}
	return candidate, true, nil
}

// sha256File returns the lowercase hex SHA-256 digest of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
