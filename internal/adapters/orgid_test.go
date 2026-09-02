package adapters

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOrgQualifiedIDGetsItsOwnProcessAndCredentials(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer r.Close(ctx)

	var asked []string
	r.SetOrgEnvResolver(func(program, org string) ([]string, error) {
		asked = append(asked, program+"/"+org)
		return []string{"PROTOTYPE_ORG=" + org}, nil
	})

	first, err := r.Gate(ctx, "prototype:acme")
	if err != nil {
		t.Fatalf("Gate(prototype:acme): %v", err)
	}
	second, err := r.Gate(ctx, "prototype:globex")
	if err != nil {
		t.Fatalf("Gate(prototype:globex): %v", err)
	}
	if first == second {
		t.Fatal("two organisations must not share one adapter process; a process carries one credential")
	}
	// The manifest declares itself "prototype", not "prototype:acme" - the
	// organisation lives in the id, not in the adapter program.
	if first.manifest.Id != "prototype" {
		t.Fatalf("manifest.Id = %q, want prototype", first.manifest.Id)
	}
	if len(asked) != 2 || asked[0] != "prototype/acme" || asked[1] != "prototype/globex" {
		t.Fatalf("resolver asked %v, want the program and each organisation", asked)
	}

	again, err := r.Gate(ctx, "prototype:acme")
	if err != nil {
		t.Fatalf("Gate(prototype:acme) again: %v", err)
	}
	if again != first {
		t.Error("an organisation's gate should be memoized like any other")
	}
}

func TestUnknownOrganisationIsRefusedRatherThanRunOnAmbientCredentials(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	wantErr := errors.New("organisation not configured")
	r.SetOrgEnvResolver(func(string, string) ([]string, error) { return nil, wantErr })

	if _, err := r.Gate(ctx, "prototype:acme"); !errors.Is(err, wantErr) {
		t.Fatalf("Gate = %v, want it to refuse rather than fall back to whatever credential is ambient", err)
	}
}

func TestOrgQualifiedIDRunsOnTheProgramSpecWhenNoOrganisationsExist(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer r.Close(ctx)

	// A host still on the flat environment values has no organisations to
	// choose between, so its one spec carries the only credentials there
	// are and must keep working unchanged.
	r.SetOrgEnvResolver(func(string, string) ([]string, error) { return nil, nil })
	if _, err := r.Gate(ctx, "prototype:acme"); err != nil {
		t.Fatalf("Gate: %v", err)
	}
}

func TestAdapterIDRoundTrip(t *testing.T) {
	for _, tc := range []struct{ base, org, id string }{
		{"atlassian", "gdncomm", "atlassian:gdncomm"},
		{"atlassian", "", "atlassian"},
		{"github", " acme ", "github:acme"},
	} {
		if got := QualifyAdapterID(tc.base, tc.org); got != tc.id {
			t.Errorf("QualifyAdapterID(%q, %q) = %q, want %q", tc.base, tc.org, got, tc.id)
		}
	}
	base, org := SplitAdapterID("atlassian:gdncomm")
	if base != "atlassian" || org != "gdncomm" {
		t.Errorf("SplitAdapterID = (%q, %q), want (atlassian, gdncomm)", base, org)
	}
	if base, org := SplitAdapterID("atlassian"); base != "atlassian" || org != "" {
		t.Errorf("SplitAdapterID(atlassian) = (%q, %q), want (atlassian, \"\")", base, org)
	}
}
