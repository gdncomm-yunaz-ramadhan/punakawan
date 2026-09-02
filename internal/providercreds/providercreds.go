// Package providercreds stores one set of provider credentials per
// organisation, so a machine can work against several Jira sites or
// GitHub organisations at once.
//
// It replaces a single flat host+token pair, which forced every delivery
// through one organisation and left the delivery source's tenant field as
// free text that selected nothing - the reason the same Jira issue could
// be started once under "gdn" and once under "gdncomm" without either
// spelling being wrong or right.
//
// Secrets are held in a 0600 file rather than a keychain: the daemon, the
// adapters, and any CI runner all need them without a login session, and
// a keychain that has to fall back to a file anyway is the weaker of the
// two designs. The file is the same posture as the credential .env it
// supersedes, and never lives in a project's workspace.
package providercreds

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"

	"gopkg.in/yaml.v3"
)

// SupportedVersion is the credentials file's schema version.
const SupportedVersion = "punakawan.credentials/v1"

// ErrNotFound is returned for an organisation that is not configured.
var ErrNotFound = errors.New("providercreds: organisation not configured")

// Provider names the system an organisation's credentials are for.
type Provider string

const (
	ProviderJira   Provider = "jira"
	ProviderGitHub Provider = "github"
)

// Providers lists every provider setup can configure, in the order a
// human-facing list should show them.
var Providers = []Provider{ProviderJira, ProviderGitHub}

// ParseProvider accepts the names a person types on the command line.
func ParseProvider(s string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "jira", "atlassian":
		return ProviderJira, nil
	case "github", "gh":
		return ProviderGitHub, nil
	default:
		return "", fmt.Errorf("providercreds: unknown provider %q, want jira or github", s)
	}
}

// AdapterID is the adapter process an organisation's work runs through.
// Every organisation gets its own, because an adapter process carries its
// credentials in its environment and so can only ever speak for one.
func (p Provider) AdapterID(org string) string {
	return adapters.QualifyAdapterID(p.AdapterProgram(), org)
}

// AdapterProgram is the adapter a provider's work runs through, with no
// organisation attached. The Jira provider's adapter is named for
// Atlassian, which is the vendor rather than the product.
func (p Provider) AdapterProgram() string {
	if p == ProviderGitHub {
		return "github"
	}
	return "atlassian"
}

// ProviderForAdapterProgram reverses AdapterProgram.
func ProviderForAdapterProgram(program string) (Provider, error) {
	return ParseProvider(program)
}

// Env returns the NAME=value entries that point one adapter process at
// this organisation. These are handed to the adapter directly rather than
// forwarded from the ambient environment, so two organisations' processes
// hold two different credentials at the same time.
func (o Org) Env() []string {
	switch o.Provider {
	case ProviderGitHub:
		env := []string{"GITHUB_TOKEN=" + o.Token}
		if api := o.gitHubAPIBaseURL(); api != "" {
			env = append(env, "GITHUB_API_URL="+api)
		}
		return env
	default:
		env := []string{
			"ATLASSIAN_HOST=" + o.Host(),
			"ATLASSIAN_API_TOKEN=" + o.Token,
		}
		if o.Email != "" {
			env = append(env, "ATLASSIAN_EMAIL="+o.Email)
		}
		if o.TokenScoped {
			env = append(env, "ATLASSIAN_API_TOKEN_SCOPED=true")
		}
		return env
	}
}

// gitHubAPIBaseURL is api.github.com for github.com and /api/v3 on an
// enterprise install, which is where those deployments put it. It returns
// "" for github.com itself so the adapter keeps its own default.
func (o Org) gitHubAPIBaseURL() string {
	host := o.Host()
	if host == "" || host == "github.com" || host == "www.github.com" {
		return ""
	}
	return "https://" + host + "/api/v3"
}

// SplitAdapterID reverses AdapterID. An id with no organisation suffix is
// reported with an empty org, which is what every adapter id looked like
// before this package existed.
func SplitAdapterID(id string) (base, org string) {
	return adapters.SplitAdapterID(id)
}

// Org is one organisation's credentials for one provider.
type Org struct {
	ID       string   `yaml:"id"`
	Provider Provider `yaml:"provider"`
	// BaseURL is the site the organisation lives at, exactly as it is
	// reached: https://team.atlassian.net, or https://github.com.
	BaseURL string `yaml:"base_url"`
	// Email is required by Jira Cloud's Basic auth and unused elsewhere.
	Email string `yaml:"email,omitempty"`
	Token string `yaml:"token"`
	// TokenScoped selects Atlassian's API gateway over the site URL.
	TokenScoped bool `yaml:"token_scoped,omitempty"`
	// Default marks the organisation used when a caller names none. The
	// first one configured for a provider becomes its default.
	Default        bool      `yaml:"default,omitempty"`
	AddedAt        time.Time `yaml:"added_at,omitempty"`
	LastVerifiedAt time.Time `yaml:"last_verified_at,omitempty"`
}

// Host is BaseURL's hostname, which is what the adapters actually take.
func (o Org) Host() string {
	if u, err := url.Parse(o.BaseURL); err == nil && u.Host != "" {
		return u.Host
	}
	return o.BaseURL
}

type file struct {
	Version string `yaml:"version"`
	Orgs    []Org  `yaml:"orgs"`
}

// Store is the credentials file on disk. Every call reads and writes it
// afresh: setup runs rarely, several processes share the file, and a
// cached copy would let one of them persist a view that another already
// changed.
type Store struct{ path string }

// Open returns the Store at path. It creates nothing; a missing file
// simply reads as no organisations.
func Open(path string) *Store { return &Store{path: path} }

// Path is where this Store reads and writes, for messages that have to
// tell a person which file to look at.
func (s *Store) Path() string { return s.path }

func (s *Store) load() (*file, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return &file{Version: SupportedVersion}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("providercreds: read %s: %w", s.path, err)
	}
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("providercreds: parse %s: %w", s.path, err)
	}
	if f.Version != "" && f.Version != SupportedVersion {
		return nil, fmt.Errorf("providercreds: %s has unsupported version %q, want %s", s.path, f.Version, SupportedVersion)
	}
	return &f, nil
}

func (s *Store) save(f *file) error {
	f.Version = SupportedVersion
	sort.SliceStable(f.Orgs, func(i, j int) bool {
		if f.Orgs[i].Provider != f.Orgs[j].Provider {
			return f.Orgs[i].Provider < f.Orgs[j].Provider
		}
		return f.Orgs[i].ID < f.Orgs[j].ID
	})
	data, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("providercreds: encode %s: %w", s.path, err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("providercreds: create %s: %w", filepath.Dir(s.path), err)
	}
	// Written through a temp file in the same directory so a crash mid-write
	// cannot leave a machine with a half-parsed credentials file and no way
	// to reach any provider.
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".credentials-*.yaml")
	if err != nil {
		return fmt.Errorf("providercreds: create temp file beside %s: %w", s.path, err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("providercreds: secure temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("providercreds: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("providercreds: close temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("providercreds: replace %s: %w", s.path, err)
	}
	return nil
}

// List returns every configured organisation, ordered by provider then id.
func (s *Store) List() ([]Org, error) {
	f, err := s.load()
	if err != nil {
		return nil, err
	}
	return f.Orgs, nil
}

// ListFor returns the organisations configured for one provider.
func (s *Store) ListFor(provider Provider) ([]Org, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []Org
	for _, org := range all {
		if org.Provider == provider {
			out = append(out, org)
		}
	}
	return out, nil
}

// Get returns one organisation's credentials. An empty id asks for the
// provider's default, which is the only organisation when there is one
// and the one marked default when there are several.
func (s *Store) Get(provider Provider, id string) (Org, error) {
	orgs, err := s.ListFor(provider)
	if err != nil {
		return Org{}, err
	}
	id = NormalizeOrgID(id)
	if id == "" {
		for _, org := range orgs {
			if org.Default {
				return org, nil
			}
		}
		if len(orgs) == 1 {
			return orgs[0], nil
		}
		if len(orgs) == 0 {
			return Org{}, fmt.Errorf("%w: no %s organisation configured; run `punakawan setup %s`", ErrNotFound, provider, provider)
		}
		return Org{}, fmt.Errorf("providercreds: several %s organisations are configured and none is the default; name one of %s", provider, strings.Join(orgIDs(orgs), ", "))
	}
	for _, org := range orgs {
		if org.ID == id {
			return org, nil
		}
	}
	return Org{}, fmt.Errorf("%w: %s organisation %q; configured: %s", ErrNotFound, provider, id, strings.Join(orgIDs(orgs), ", "))
}

// Put stores one organisation, replacing any entry with the same provider
// and id. The first organisation configured for a provider becomes its
// default, so a machine with exactly one never has to name it.
func (s *Store) Put(org Org) error {
	org.ID = NormalizeOrgID(org.ID)
	if org.ID == "" {
		return fmt.Errorf("providercreds: organisation id is required")
	}
	if org.Provider == "" {
		return fmt.Errorf("providercreds: provider is required for organisation %q", org.ID)
	}
	f, err := s.load()
	if err != nil {
		return err
	}
	if org.AddedAt.IsZero() {
		org.AddedAt = time.Now().UTC()
	}

	replaced := false
	hasDefault := false
	for i, existing := range f.Orgs {
		if existing.Provider != org.Provider {
			continue
		}
		if existing.ID == org.ID {
			// A re-run keeps whatever this organisation already was in the
			// two respects setup does not ask about again.
			if org.AddedAt.IsZero() {
				org.AddedAt = existing.AddedAt
			}
			org.Default = org.Default || existing.Default
			f.Orgs[i] = org
			replaced = true
			continue
		}
		hasDefault = hasDefault || existing.Default
	}
	if !replaced {
		if !hasDefault {
			org.Default = true
		}
		f.Orgs = append(f.Orgs, org)
	}
	if org.Default {
		for i := range f.Orgs {
			if f.Orgs[i].Provider == org.Provider && f.Orgs[i].ID != org.ID {
				f.Orgs[i].Default = false
			}
		}
	}
	return s.save(f)
}

// Remove deletes one organisation. Removing the default promotes whatever
// is left, so a provider never ends up with organisations but no default.
func (s *Store) Remove(provider Provider, id string) error {
	id = NormalizeOrgID(id)
	f, err := s.load()
	if err != nil {
		return err
	}
	kept := make([]Org, 0, len(f.Orgs))
	removedDefault := false
	for _, org := range f.Orgs {
		if org.Provider == provider && org.ID == id {
			removedDefault = org.Default
			continue
		}
		kept = append(kept, org)
	}
	if len(kept) == len(f.Orgs) {
		return fmt.Errorf("%w: %s organisation %q", ErrNotFound, provider, id)
	}
	if removedDefault {
		for i := range kept {
			if kept[i].Provider == provider {
				kept[i].Default = true
				break
			}
		}
	}
	f.Orgs = kept
	return s.save(f)
}

// MarkVerified records that this organisation's credentials were last
// accepted by the provider, so `punakawan setup --list` can say how stale
// a credential is before it fails somewhere less convenient.
func (s *Store) MarkVerified(provider Provider, id string, at time.Time) error {
	f, err := s.load()
	if err != nil {
		return err
	}
	id = NormalizeOrgID(id)
	for i := range f.Orgs {
		if f.Orgs[i].Provider == provider && f.Orgs[i].ID == id {
			f.Orgs[i].LastVerifiedAt = at.UTC()
			return s.save(f)
		}
	}
	return fmt.Errorf("%w: %s organisation %q", ErrNotFound, provider, id)
}

func orgIDs(orgs []Org) []string {
	out := make([]string, 0, len(orgs))
	for _, org := range orgs {
		out = append(out, org.ID)
	}
	if len(out) == 0 {
		return []string{"none"}
	}
	return out
}

// NormalizeOrgID lowercases and trims an organisation id so the same
// organisation typed two ways is the same organisation.
func NormalizeOrgID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

// AdapterOrgEnv resolves an org-qualified adapter id to that
// organisation's credentials. Wiring it into an adapters.Registry is what
// makes "atlassian:gdncomm" spawn a process holding gdncomm's token and
// nothing else.
func (s *Store) AdapterOrgEnv() adapters.OrgEnvResolver {
	return func(program, org string) ([]string, error) {
		provider, err := ParseProvider(program)
		if err != nil {
			return nil, err
		}
		configured, err := s.Get(provider, org)
		if err == nil {
			return configured.Env(), nil
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		// A host that has configured no organisations at all for this
		// provider is a single-site host still running on the flat
		// environment values, and its deliveries name an organisation only
		// because their Jira source always did. Returning no override
		// leaves that host working exactly as it did. Once even one
		// organisation is configured, an unknown one is a real
		// misconfiguration and must not fall back to whichever credential
		// happens to be ambient - that would send one organisation's
		// writes through another's token.
		existing, listErr := s.ListFor(provider)
		if listErr != nil {
			return nil, listErr
		}
		if len(existing) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %s organisation %q; run `punakawan setup %s` to configure it", ErrNotFound, provider, org, provider)
	}
}

// ResolveOrgID turns a caller-supplied organisation name into the exact
// one this host holds credentials for. A blank name resolves to the
// provider's default; a name that matches nothing configured is an error
// naming what is.
//
// A host with no organisations configured for the provider is still
// running on the flat environment values and distinguishes none, so the
// name is returned untouched.
func (s *Store) ResolveOrgID(provider Provider, name string) (string, error) {
	configured, err := s.ListFor(provider)
	if err != nil {
		return "", err
	}
	if len(configured) == 0 {
		return strings.TrimSpace(name), nil
	}

	wanted := NormalizeOrgID(name)
	if wanted == "" {
		for _, org := range configured {
			if org.Default {
				return org.ID, nil
			}
		}
		return "", fmt.Errorf("name which %s organisation this work belongs to: %s", provider, strings.Join(orgIDs(configured), ", "))
	}
	for _, org := range configured {
		if org.ID == wanted {
			return org.ID, nil
		}
	}
	return "", fmt.Errorf("no %s credentials are configured for organisation %q; configured: %s (add one with `punakawan setup %s`)",
		provider, wanted, strings.Join(orgIDs(configured), ", "), provider)
}
