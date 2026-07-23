package recipe

import (
	"strings"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestInstanceFingerprintStringNeverContainsASecretShape(t *testing.T) {
	fp := InstanceFingerprint{Host: "example.atlassian.net", CloudID: "abc-123-def"}
	s := fp.String()
	if s == "" {
		t.Fatal("String() = \"\", want a non-empty fingerprint")
	}
	if !strings.Contains(s, "example.atlassian.net") || !strings.Contains(s, "abc-123-def") {
		t.Fatalf("String() = %q, want it to contain host and cloud id", s)
	}
	// Task q9r.7 #4's own bar: never anything credential-shaped in what
	// gets persisted. Bearer tokens/API keys don't look like hostnames or
	// UUIDs, so this is a smoke check that String() only ever echoes back
	// what was given, not a general secret-shape detector.
	for _, secretish := range []string{"Bearer ", "Basic ", "ATATT3"} {
		if strings.Contains(s, secretish) {
			t.Fatalf("String() = %q unexpectedly contains secret-shaped substring %q", s, secretish)
		}
	}
}

func TestInstanceFingerprintHashIsStableAndNonReversible(t *testing.T) {
	fp := InstanceFingerprint{Host: "example.atlassian.net", CloudID: "abc-123"}
	h1 := fp.Hash()
	h2 := fp.Hash()
	if h1 != h2 {
		t.Fatalf("Hash() not stable: %q != %q", h1, h2)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Fatalf("Hash() = %q, want sha256: prefix matching the schema's compiled_query_hash pattern", h1)
	}
	if strings.Contains(h1, "example.atlassian.net") {
		t.Fatal("Hash() leaked the raw host into its output")
	}
}

func TestInstanceFingerprintEmptyValueRendersEmptyString(t *testing.T) {
	if got := (InstanceFingerprint{}).String(); got != "" {
		t.Fatalf("String() = %q, want empty for a zero-value fingerprint", got)
	}
}

func recipeWithFingerprint(id, fingerprint string) protocol.KnowledgeRecord {
	rec := verifiedRecipeFixture(id)
	rec.RetrievalRecipe.Validation = &protocol.KnowledgeRecordRetrievalRecipeValidation{
		ProviderInstanceFingerprint: &fingerprint,
	}
	return rec
}

func TestCheckInstanceFingerprintPassesOnMatch(t *testing.T) {
	fp := InstanceFingerprint{Host: "example.atlassian.net", CloudID: "abc-123"}
	rec := recipeWithFingerprint("pkw:recipe/a/fp-match", fp.String())

	if err := CheckInstanceFingerprint(rec, fp); err != nil {
		t.Fatalf("CheckInstanceFingerprint: %v, want nil for a matching instance", err)
	}
}

func TestCheckInstanceFingerprintFailsOnMismatch(t *testing.T) {
	stored := InstanceFingerprint{Host: "company-a.atlassian.net", CloudID: "aaa"}
	current := InstanceFingerprint{Host: "company-b.atlassian.net", CloudID: "bbb"}
	rec := recipeWithFingerprint("pkw:recipe/a/fp-mismatch", stored.String())

	err := CheckInstanceFingerprint(rec, current)
	var mismatch *FingerprintMismatchError
	if err == nil {
		t.Fatal("CheckInstanceFingerprint: want an error for a mismatched instance, got nil")
	}
	if !asFingerprintMismatch(err, &mismatch) {
		t.Fatalf("err = %v, want *FingerprintMismatchError", err)
	}
	if mismatch.Stored != stored.String() || mismatch.Current != current.String() {
		t.Fatalf("mismatch = %+v, want Stored=%q Current=%q", mismatch, stored.String(), current.String())
	}
}

func TestCheckInstanceFingerprintSkipsWhenNoStoredFingerprint(t *testing.T) {
	rec := verifiedRecipeFixture("pkw:recipe/a/fp-none")
	current := InstanceFingerprint{Host: "example.atlassian.net", CloudID: "abc"}

	if err := CheckInstanceFingerprint(rec, current); err != nil {
		t.Fatalf("CheckInstanceFingerprint: %v, want nil when the recipe predates fingerprinting", err)
	}
}

func TestCheckInstanceFingerprintSkipsWhenCallerHasNoCurrentFingerprint(t *testing.T) {
	fp := InstanceFingerprint{Host: "example.atlassian.net", CloudID: "abc"}
	rec := recipeWithFingerprint("pkw:recipe/a/fp-caller-blind", fp.String())

	if err := CheckInstanceFingerprint(rec, InstanceFingerprint{}); err != nil {
		t.Fatalf("CheckInstanceFingerprint: %v, want nil when the caller supplies no current fingerprint to compare", err)
	}
}

// asFingerprintMismatch is a tiny errors.As helper kept local to this file
// so the test above reads linearly.
func asFingerprintMismatch(err error, target **FingerprintMismatchError) bool {
	m, ok := err.(*FingerprintMismatchError)
	if !ok {
		return false
	}
	*target = m
	return true
}
