package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ygrip/punakawan/internal/providercreds"
	"github.com/ygrip/punakawan/internal/workspace"
)

// newSetupProviderCmd builds `punakawan setup jira` and `punakawan setup
// github`: one organisation's credentials, asked for in the order a
// person already has them.
//
// The organisation id is derived from the site URL rather than asked for,
// because an id someone invents is an id someone else spells differently
// - which is exactly how one Jira issue came to be started twice, once
// under "gdn" and once under "gdncomm", with neither spelling wrong.
//
// This is CLI-only on purpose. A credential belongs to the human at the
// terminal; exposing it as an MCP tool would let a connected agent read
// or replace one.
func newSetupProviderCmd(provider providercreds.Provider) *cobra.Command {
	var (
		siteURL string
		orgID   string
		token   string
		email   string
		scoped  bool
		list    bool
		remove  string
	)

	name := string(provider)
	cmd := &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("Configure %s credentials for one organisation", name),
		Long: fmt.Sprintf("Asks for the %s site URL, derives the organisation from it, asks for a token, "+
			"and verifies the pair with a live authenticated read before saving anything. Run it once per "+
			"organisation; several can be configured side by side, and delivery work names which one it is for.", name),
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := workspace.GlobalCredentialsPath()
			if err != nil {
				return err
			}
			store := providercreds.Open(path)

			switch {
			case list:
				return listProviderOrgs(cmd.OutOrStdout(), store, provider)
			case remove != "":
				if err := store.Remove(provider, remove); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s Removed %s organisation %q\n", okGlyph, name, providercreds.NormalizeOrgID(remove))
				return nil
			}
			return runProviderSetup(cmd, provider, store, providerAnswers{
				SiteURL: siteURL, OrgID: orgID, Token: token, Email: email, Scoped: scoped,
			})
		},
	}

	cmd.Flags().StringVar(&siteURL, "url", "", "site URL, e.g. https://team.atlassian.net or https://github.com/acme")
	cmd.Flags().StringVar(&orgID, "org", "", "organisation id; derived from --url when omitted")
	cmd.Flags().StringVar(&token, "token", "", "API token; prompted for when omitted")
	cmd.Flags().StringVar(&email, "email", "", "account email, required by Jira Cloud's Basic auth")
	cmd.Flags().BoolVar(&scoped, "scoped-token", false, "the token is an Atlassian scoped token, reached through the API gateway")
	cmd.Flags().BoolVar(&list, "list", false, "list the organisations already configured for this provider")
	cmd.Flags().StringVar(&remove, "remove", "", "remove one organisation's credentials")
	return cmd
}

// okGlyph marks a completed fact. Progress is reported as it happens
// rather than summarized at the end, so a person watching a step that
// reaches the network knows which one they are waiting on.
const okGlyph = "✓"

type providerAnswers struct {
	SiteURL string
	OrgID   string
	Token   string
	Email   string
	Scoped  bool
}

func runProviderSetup(cmd *cobra.Command, provider providercreds.Provider, store *providercreds.Store, in providerAnswers) error {
	out, stdin := cmd.OutOrStdout(), cmd.InOrStdin()

	siteURL := strings.TrimSpace(in.SiteURL)
	if siteURL == "" {
		siteURL = ask(stdin, out, siteURLPrompt(provider), defaultSiteURL(provider))
	}
	baseURL, err := providercreds.NormalizeBaseURL(siteURL)
	if err != nil {
		return err
	}

	orgID := providercreds.NormalizeOrgID(in.OrgID)
	if orgID == "" {
		if orgID, err = providercreds.DeriveOrgID(provider, siteURL); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "%s Organisation: %s (%s)\n", okGlyph, orgID, baseURL)

	email := strings.TrimSpace(in.Email)
	scoped := in.Scoped
	if provider == providercreds.ProviderJira && !scoped && email == "" {
		// Jira Cloud authenticates a personal token as Basic email:token,
		// so a token alone is not a credential there.
		email = ask(stdin, out, "Account email", "")
	}

	token := strings.TrimSpace(in.Token)
	if token == "" {
		token = askSecret(stdin, out, tokenPrompt(provider))
	}
	if token == "" {
		return fmt.Errorf("setup %s: a token is required; create one at %s", provider, tokenURL(provider, baseURL))
	}

	org := providercreds.Org{
		ID: orgID, Provider: provider, BaseURL: baseURL,
		Email: email, Token: token, TokenScoped: scoped,
	}

	// Verified before anything is written, so a wrong URL or a dead token
	// leaves the machine exactly as it was rather than half-configured.
	fmt.Fprintf(out, "  Verifying against %s ...\n", org.Host())
	ctx, cancel := context.WithTimeout(cmd.Context(), doctorCheckTimeout)
	defer cancel()
	if err := verifyProviderOrg(ctx, org); err != nil {
		return fmt.Errorf("setup %s: %w\n\nNothing was saved. Create a fresh token at %s and run this again.", provider, err, tokenURL(provider, baseURL))
	}
	fmt.Fprintf(out, "%s Credentials accepted\n", okGlyph)

	org.LastVerifiedAt = time.Now().UTC()
	if err := store.Put(org); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s Saved to %s\n", okGlyph, store.Path())

	saved, err := store.Get(provider, orgID)
	if err == nil && saved.Default {
		fmt.Fprintf(out, "%s %s work defaults to this organisation\n", okGlyph, provider)
	}
	if provider == providercreds.ProviderJira {
		fmt.Fprintf(out, "\nStart delivery work against it with source {\"kind\":\"jira\",\"tenant\":%q,\"key\":\"ABC-123\"}.\n", orgID)
	}
	return nil
}

// verifyProviderOrg performs the same authenticated read `punakawan
// doctor` uses, so "verify while configuring" and "check it later" can
// never disagree about whether a credential works.
func verifyProviderOrg(ctx context.Context, org providercreds.Org) error {
	switch org.Provider {
	case providercreds.ProviderJira:
		return checkAtlassianConnectivity(ctx, org.Host(), org.Token, org.Email, org.TokenScoped)
	case providercreds.ProviderGitHub:
		return checkGitHubConnectivity(ctx, org.Token, gitHubAPIBaseURL(org))
	default:
		return fmt.Errorf("setup: unknown provider %q", org.Provider)
	}
}

// gitHubAPIBaseURL is api.github.com for github.com and /api/v3 on an
// enterprise install, which is where those deployments put it.
func gitHubAPIBaseURL(org providercreds.Org) string {
	host := org.Host()
	if host == "" || host == "github.com" || host == "www.github.com" {
		return "https://api.github.com"
	}
	return "https://" + host + "/api/v3"
}

func listProviderOrgs(out io.Writer, store *providercreds.Store, provider providercreds.Provider) error {
	orgs, err := store.ListFor(provider)
	if err != nil {
		return err
	}
	if len(orgs) == 0 {
		fmt.Fprintf(out, "No %s organisations configured. Run `punakawan setup %s`.\n", provider, provider)
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ORGANISATION\tURL\tDEFAULT\tLAST VERIFIED")
	for _, org := range orgs {
		def := ""
		if org.Default {
			def = okGlyph
		}
		verified := "never"
		if !org.LastVerifiedAt.IsZero() {
			verified = org.LastVerifiedAt.Local().Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", org.ID, org.BaseURL, def, verified)
	}
	return w.Flush()
}

func siteURLPrompt(provider providercreds.Provider) string {
	if provider == providercreds.ProviderGitHub {
		return "GitHub organisation URL"
	}
	return "Jira site URL"
}

func defaultSiteURL(provider providercreds.Provider) string {
	if provider == providercreds.ProviderGitHub {
		return "https://github.com/"
	}
	return ""
}

func tokenPrompt(provider providercreds.Provider) string {
	if provider == providercreds.ProviderGitHub {
		return "GitHub personal access token"
	}
	return "Jira API token"
}

// tokenURL names where a token is created, because "the token is wrong"
// is only actionable alongside where to get a right one.
func tokenURL(provider providercreds.Provider, baseURL string) string {
	if provider == providercreds.ProviderGitHub {
		if strings.Contains(baseURL, "github.com") {
			return "https://github.com/settings/tokens"
		}
		return strings.TrimRight(baseURL, "/") + "/settings/tokens"
	}
	return "https://id.atlassian.com/manage-profile/security/api-tokens"
}

// ask reads one visible answer. A non-interactive caller gets the default
// rather than a read that will never return, so an agent or a CI job
// fails with a missing-flag message instead of hanging.
func ask(in io.Reader, out io.Writer, label, def string) string {
	if !isInteractiveInput(in) {
		return def
	}
	if def != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	line, _ := bufio.NewReader(in).ReadString('\n')
	if answer := strings.TrimSpace(line); answer != "" {
		return answer
	}
	return def
}

// askSecret reads one answer without echoing it.
func askSecret(in io.Reader, out io.Writer, label string) string {
	if !isInteractiveInput(in) {
		return ""
	}
	fmt.Fprintf(out, "%s: ", label)
	if f, ok := in.(*os.File); ok {
		value, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(out)
		if err == nil {
			return strings.TrimSpace(string(value))
		}
	}
	line, _ := bufio.NewReader(in).ReadString('\n')
	return strings.TrimSpace(line)
}
