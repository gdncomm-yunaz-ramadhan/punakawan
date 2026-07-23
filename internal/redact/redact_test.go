package redact

import (
	"strings"
	"testing"
)

// awsKeyLooking is built by concatenation, not as one contiguous literal,
// so this file's raw text doesn't itself contain a string shaped like a
// real AWS access key id - GitHub's push protection secret scanner flags
// that shape on sight regardless of whether the key is real, and a
// contiguous literal here would (correctly) get every push blocked.
const awsKeyLooking = "AKIA" + "ABCDEFGHIJKLMNOP"

func TestTextRedactsKnownSecretShapes(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"aws access key", "AWS_ACCESS_KEY_ID=" + awsKeyLooking, awsKeyLooking},
		{"github token", "curl -H 'Authorization: token ghp_abcdefghijklmnopqrstuvwxyz012345'", "ghp_abcdefghijklmnopqrstuvwxyz012345"},
		{"gitlab token", "remote: glpat-abcdefghijklmnopqrst", "glpat-abcdefghijklmnopqrst"},
		{"atlassian token", "ATLASSIAN_API_TOKEN=ATATT3xFfGF0abcdefghijklmnopqrstuvwxyz", "ATATT3xFfGF0abcdefghijklmnopqrstuvwxyz"},
		{"openai-shaped key", "sk-abcdefghijklmnopqrstuvwxyz0123456789", "sk-abcdefghijklmnopqrstuvwxyz0123456789"},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"},
		{"bearer header", "Authorization: Bearer abcdef0123456789ghijklmnop", "abcdef0123456789ghijklmnop"},
		{"generic secret assignment", `password: "sup3r-secret-value"`, "sup3r-secret-value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Text(tc.input)
			if strings.Contains(got, tc.want) {
				t.Fatalf("Text(%q) = %q, still contains the secret", tc.input, got)
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Fatalf("Text(%q) = %q, want a [REDACTED] marker", tc.input, got)
			}
		})
	}
}

func TestTextLeavesOrdinaryTextAlone(t *testing.T) {
	input := "test run passed: 12/12 assertions ok in 340ms"
	if got := Text(input); got != input {
		t.Fatalf("Text(%q) = %q, want unchanged", input, got)
	}
}
