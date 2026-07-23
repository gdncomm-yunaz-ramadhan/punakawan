package artifact

import (
	"regexp"
	"testing"
)

var sha256Shape = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func TestHashIsDeterministicAndShaped(t *testing.T) {
	a := Hash([]byte("hello"))
	b := Hash([]byte("hello"))
	if a != b {
		t.Fatalf("Hash is not deterministic: %q != %q", a, b)
	}
	if !sha256Shape.MatchString(a) {
		t.Fatalf("Hash = %q, want sha256:<64 hex chars>", a)
	}
}

func TestHashDiffersForDifferentContent(t *testing.T) {
	if Hash([]byte("a")) == Hash([]byte("b")) {
		t.Fatal("Hash produced the same digest for different content")
	}
}
