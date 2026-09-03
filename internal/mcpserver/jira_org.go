package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/deliveryservice"
	"github.com/ygrip/punakawan/internal/jirahooks"
	"github.com/ygrip/punakawan/internal/providercreds"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// jiraIssueProbe reports whether one organisation's site can see an
// issue. It is a parameter so the decision below can be tested without
// spawning an adapter.
type jiraIssueProbe func(ctx context.Context, org, issueKey string) (bool, error)

// jiraOrgResolver resolves a Jira source's organisation against the
// credentials this host actually holds, so the name that reaches delivery
// identity is one exact configured organisation rather than whatever an
// agent happened to type, and one that can actually see the issue. A host
// with no organisations configured resolves nothing and behaves exactly
// as it did before.
func jiraOrgResolver(a *app.App) deliveryservice.Option {
	if a.Credentials == nil {
		return func(*deliveryservice.Service) {}
	}
	probe := func(ctx context.Context, org, issueKey string) (bool, error) {
		return jirahooks.IssueVisible(ctx, a.AdapterRegistry, org, issueKey)
	}
	return deliveryservice.WithJiraOrgResolver(func(ctx context.Context, named, issueKey string) (string, *protocol.NeedUserInput, error) {
		orgs, err := a.Credentials.Candidates(providercreds.ProviderJira)
		if err != nil {
			return "", nil, err
		}
		return locateJiraOrg(ctx, orgs, named, issueKey, probe)
	})
}

// locateJiraOrg picks the organisation a Jira issue belongs to, asking
// the caller only when the default organisation cannot see it.
//
// Delivery identity is written from this answer and is what every later
// comment, worklog and transition is addressed by, so getting it wrong is
// not a retryable mistake: the delivery exists, at a site that has never
// heard of the issue. Hydration would have noticed - two steps too late,
// and it records the 404 as a skipped step rather than a wrong site.
//
// The default organisation is probed and the others are not. A host with
// one organisation, or a caller that named one, makes no extra call at
// all; and on a miss the remaining organisations are offered rather than
// tried, because whose credential a delivery writes through is a decision
// with consequences a human owns, not a lookup to keep retrying until
// something answers.
func locateJiraOrg(ctx context.Context, orgs []providercreds.Org, named, issueKey string, probe jiraIssueProbe) (string, *protocol.NeedUserInput, error) {
	key := strings.ToUpper(strings.TrimSpace(issueKey))
	named = strings.TrimSpace(named)

	if named != "" || len(orgs) <= 1 || key == "" {
		// A named organisation is a stated answer - including the answer
		// the user just gave to the question below - so it is resolved,
		// not second-guessed. That is also what makes the ask-and-retry
		// loop terminate.
		org, err := resolveJiraOrgName(orgs, named)
		return org, nil, err
	}

	def := orgs[0]
	if !def.Default {
		// Several organisations and none marked default: there is no
		// first place to look, so there is nothing to probe and the
		// question is the whole answer.
		return "", jiraOrgChoice(orgs, key, ""), nil
	}
	visible, err := probe(ctx, def.ID, key)
	if err != nil {
		return "", nil, fmt.Errorf("mcpserver: look for %s at %s: %w", key, def.ID, err)
	}
	if visible {
		return def.ID, nil, nil
	}
	return "", jiraOrgChoice(orgs[1:], key, def.ID), nil
}

// resolveJiraOrgName is the canonicalization this resolver has always
// done: a name becomes the exact configured organisation it refers to,
// and a blank one becomes the default.
func resolveJiraOrgName(orgs []providercreds.Org, named string) (string, error) {
	if len(orgs) == 0 {
		return named, nil
	}
	wanted := providercreds.NormalizeOrgID(named)
	if wanted == "" {
		for _, org := range orgs {
			if org.Default {
				return org.ID, nil
			}
		}
		if len(orgs) == 1 {
			return orgs[0].ID, nil
		}
		return "", errors.New("name which Jira organisation this work belongs to: " + strings.Join(jiraOrgIDs(orgs), ", "))
	}
	for _, org := range orgs {
		if org.ID == wanted {
			return org.ID, nil
		}
	}
	return "", fmt.Errorf("no Jira credentials are configured for organisation %q; configured: %s", named, strings.Join(jiraOrgIDs(orgs), ", "))
}

func jiraOrgChoice(orgs []providercreds.Org, issueKey, searched string) *protocol.NeedUserInput {
	options := make([]protocol.NeedUserInputOptionsElem, 0, len(orgs))
	for _, org := range orgs {
		options = append(options, protocol.NeedUserInputOptionsElem{
			Id:     org.ID,
			Label:  org.ID + " (" + org.Host() + ")",
			Impact: fmt.Sprintf("Starts this delivery against %s at %s, and logs its work and transitions there", issueKey, org.Host()),
		})
	}
	question := fmt.Sprintf("Which Jira organisation holds %s?", issueKey)
	if searched != "" {
		question = fmt.Sprintf("%s is not visible at %s, this host's default Jira organisation. Which organisation holds it?", issueKey, searched)
	}
	return &protocol.NeedUserInput{
		Kind:          protocol.NeedUserInputKindDecisionRequired,
		Question:      question,
		MissingFields: []string{"source.tenant"},
		Options:       options,
	}
}

func jiraOrgIDs(orgs []providercreds.Org) []string {
	out := make([]string, 0, len(orgs))
	for _, org := range orgs {
		out = append(out, org.ID)
	}
	return out
}
