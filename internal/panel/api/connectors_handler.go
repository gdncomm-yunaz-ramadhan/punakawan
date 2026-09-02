package api

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/providercreds"
)

// Connectors answers "what is this install actually connected to?" - the
// adapters it can start, and for each one the organisations whose
// credentials it holds. It is a read of configuration only: no adapter is
// spawned and no provider is contacted, so opening the page cannot
// disturb work in flight.
type Connectors struct {
	// CredentialsPath is where the organisations below are stored, so a
	// person can go look at (or remove) them by hand.
	CredentialsPath string             `json:"credentials_path"`
	Adapters        []ConnectorAdapter `json:"adapters"`
}

// ConnectorAdapter is one configured adapter program.
type ConnectorAdapter struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Provider string `json:"provider,omitempty"`
	Command  string `json:"command"`
	// Entrypoint is the adapter's own script - the last argument, which is
	// what a person actually wants to see when checking which deployed
	// version this install points at.
	Entrypoint string `json:"entrypoint,omitempty"`
	// Installed reports whether that entrypoint exists on disk. A
	// configured adapter whose files were removed is the difference
	// between "not set up" and "set up and broken".
	Installed      bool                    `json:"installed"`
	EnvPassthrough []string                `json:"env_passthrough,omitempty"`
	Organizations  []ConnectorOrganization `json:"organizations"`
}

// ConnectorOrganization is one organisation's connection through an
// adapter. It never carries the token: this endpoint is readable by
// anything that can reach the panel.
type ConnectorOrganization struct {
	ID        string `json:"id"`
	AdapterID string `json:"adapter_id"`
	BaseURL   string `json:"base_url"`
	Host      string `json:"host"`
	Account   string `json:"account,omitempty"`
	// Default marks the organisation used when delivery work names none.
	Default        bool       `json:"default"`
	TokenScoped    bool       `json:"token_scoped"`
	AddedAt        *time.Time `json:"added_at,omitempty"`
	LastVerifiedAt *time.Time `json:"last_verified_at,omitempty"`
}

// adapterLabels name the adapters a person recognises by product rather
// than by the vendor the adapter is named for. An adapter with no entry
// here is shown under its own id.
var adapterLabels = map[string]string{
	"atlassian": "Jira",
	"github":    "GitHub",
	"docling":   "Docling",
}

// ConnectorsHandler serves GET /api/v1/connectors. specs is read at
// request time rather than captured once, so an adapter configured after
// the panel started still shows up on a refresh.
func ConnectorsHandler(specs func() map[string]adapters.AdapterSpec, creds *providercreds.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := Connectors{Adapters: []ConnectorAdapter{}}
		if creds != nil {
			out.CredentialsPath = creds.Path()
		}

		byAdapter := map[string][]ConnectorOrganization{}
		if creds != nil {
			orgs, err := creds.List()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			for _, org := range orgs {
				adapterID := org.Provider.AdapterID(org.ID)
				program, _ := adapters.SplitAdapterID(adapterID)
				byAdapter[program] = append(byAdapter[program], connectorOrganization(org, adapterID))
			}
		}

		configured := map[string]adapters.AdapterSpec{}
		if specs != nil {
			configured = specs()
		}
		// Every adapter this host can start, plus any program that has
		// credentials but no adapter configured - which is precisely the
		// case worth showing, since those credentials cannot be used until
		// the adapter is installed.
		seen := map[string]bool{}
		for id, spec := range configured {
			program, _ := adapters.SplitAdapterID(id)
			seen[program] = true
			out.Adapters = append(out.Adapters, connectorAdapter(program, spec, byAdapter[program]))
		}
		for program, orgs := range byAdapter {
			if !seen[program] {
				out.Adapters = append(out.Adapters, connectorAdapter(program, adapters.AdapterSpec{}, orgs))
			}
		}
		sort.Slice(out.Adapters, func(i, j int) bool { return out.Adapters[i].Label < out.Adapters[j].Label })

		writeJSON(w, http.StatusOK, out)
	}
}

func connectorAdapter(program string, spec adapters.AdapterSpec, orgs []ConnectorOrganization) ConnectorAdapter {
	label := adapterLabels[program]
	if label == "" {
		label = program
	}
	entrypoint := ""
	if len(spec.Args) > 0 {
		entrypoint = spec.Args[len(spec.Args)-1]
	}
	installed := false
	if entrypoint != "" {
		if info, err := os.Stat(entrypoint); err == nil && !info.IsDir() {
			installed = true
		}
	}
	if orgs == nil {
		orgs = []ConnectorOrganization{}
	}
	sort.Slice(orgs, func(i, j int) bool {
		if orgs[i].Default != orgs[j].Default {
			return orgs[i].Default
		}
		return orgs[i].ID < orgs[j].ID
	})
	provider := ""
	if p, err := providercreds.ParseProvider(program); err == nil {
		provider = string(p)
	}
	return ConnectorAdapter{
		ID: program, Label: label, Provider: provider,
		Command: spec.Command, Entrypoint: filepath.Clean(entrypoint), Installed: installed,
		EnvPassthrough: spec.EnvPassthrough, Organizations: orgs,
	}
}

func connectorOrganization(org providercreds.Org, adapterID string) ConnectorOrganization {
	// The account the provider itself named wins over the one typed
	// during setup: for GitHub only the former exists, and for Jira it is
	// the one the credential actually authenticates as.
	account := strings.TrimSpace(org.Account)
	if account == "" {
		account = strings.TrimSpace(org.Email)
	}
	out := ConnectorOrganization{
		ID: org.ID, AdapterID: adapterID, BaseURL: org.BaseURL, Host: org.Host(),
		Account: account, Default: org.Default, TokenScoped: org.TokenScoped,
	}
	if !org.AddedAt.IsZero() {
		added := org.AddedAt
		out.AddedAt = &added
	}
	if !org.LastVerifiedAt.IsZero() {
		verified := org.LastVerifiedAt
		out.LastVerifiedAt = &verified
	}
	return out
}
