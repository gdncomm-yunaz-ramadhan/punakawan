package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/providercreds"
)

func jiraOrgs(ids ...string) []providercreds.Org {
	out := make([]providercreds.Org, 0, len(ids))
	for i, id := range ids {
		out = append(out, providercreds.Org{
			ID: id, Provider: providercreds.ProviderJira,
			BaseURL: "https://" + id + ".atlassian.net", Default: i == 0,
		})
	}
	return out
}

// Delivery identity is written from this answer, so the wrong one is not
// a retryable mistake - it is a durable delivery at a site that has never
// heard of the issue.
func TestLocateJiraOrgProbesOnlyTheDefaultAndAsksWhenItMisses(t *testing.T) {
	ctx := context.Background()

	probed := []string{}
	seen := func(visible bool) jiraIssueProbe {
		return func(_ context.Context, org, _ string) (bool, error) {
			probed = append(probed, org)
			return visible, nil
		}
	}

	// One organisation, or a caller that named one: nothing to look for.
	if org, need, err := locateJiraOrg(ctx, jiraOrgs("acme"), "", "PAY-1", seen(false)); err != nil || need != nil || org != "acme" {
		t.Fatalf("single org = %q need:%v err:%v, want acme with no probe", org, need, err)
	}
	if org, need, err := locateJiraOrg(ctx, jiraOrgs("acme", "other"), "other", "PAY-1", seen(false)); err != nil || need != nil || org != "other" {
		t.Fatalf("named org = %q need:%v err:%v, want other with no probe", org, need, err)
	}
	if len(probed) != 0 {
		t.Fatalf("probed %v, want no lookup when there is nothing to choose between", probed)
	}

	// Several organisations, default holds it: no question, one lookup.
	org, need, err := locateJiraOrg(ctx, jiraOrgs("acme", "other"), "", "PAY-1", seen(true))
	if err != nil || need != nil || org != "acme" {
		t.Fatalf("default hit = %q need:%v err:%v, want acme", org, need, err)
	}
	if len(probed) != 1 || probed[0] != "acme" {
		t.Fatalf("probed %v, want only the default", probed)
	}

	// Default misses: the others are offered, never tried.
	probed = probed[:0]
	org, need, err = locateJiraOrg(ctx, jiraOrgs("acme", "other", "third"), "", "PAY-1", seen(false))
	if err != nil || org != "" || need == nil {
		t.Fatalf("default miss = %q need:%v err:%v, want a question", org, need, err)
	}
	if len(probed) != 1 {
		t.Fatalf("probed %v, want the remaining organisations offered rather than tried", probed)
	}
	if len(need.Options) != 2 || need.Options[0].Id != "other" || need.Options[1].Id != "third" {
		t.Fatalf("options = %+v, want one per remaining organisation", need.Options)
	}
	if !strings.Contains(need.Question, "PAY-1") || !strings.Contains(need.Question, "acme") {
		t.Fatalf("question = %q, want it to name the issue and where it was looked for", need.Question)
	}
}

// A refused credential says nothing about where an issue lives, so it
// must never read as "not here" and move a delivery to another site.
func TestLocateJiraOrgSurfacesARejectedCredentialRatherThanMovingOn(t *testing.T) {
	rejected := func(context.Context, string, string) (bool, error) {
		return false, errors.New("Atlassian credentials rejected: acme.atlassian.net answered HTTP 404")
	}
	org, need, err := locateJiraOrg(context.Background(), jiraOrgs("acme", "other"), "", "PAY-1", rejected)
	if err == nil || need != nil || org != "" {
		t.Fatalf("rejected credential = %q need:%v err:%v, want an error", org, need, err)
	}
}
