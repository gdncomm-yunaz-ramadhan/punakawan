package project

import (
	"errors"
	"testing"
)

func TestValidateEntry(t *testing.T) {
	cases := []struct {
		name  string
		entry MetadataEntry
		want  error
	}{
		{"ok string", MetadataEntry{Key: "a.b", Description: "d", Value: "v"}, nil},
		{"ok number", MetadataEntry{Key: "a.b", Description: "d", Value: 127}, nil},
		{"ok float", MetadataEntry{Key: "a.b", Description: "d", Value: 1.5}, nil},
		{"ok bool", MetadataEntry{Key: "a.b", Description: "d", Value: true}, nil},
		{"ok string list", MetadataEntry{Key: "a.b", Description: "d", Value: []string{"docs/", "README.md"}}, nil},
		{"ok map", MetadataEntry{Key: "a.b", Description: "d", Value: map[string]any{"k": "v"}}, nil},
		{"missing key", MetadataEntry{Key: "  ", Description: "d", Value: "v"}, ErrMissingField},
		{"missing description", MetadataEntry{Key: "a.b", Description: "", Value: "v"}, ErrMissingField},
		{"missing value", MetadataEntry{Key: "a.b", Description: "d", Value: nil}, ErrMissingField},
		{"secret token", MetadataEntry{Key: "atlassian.api_token", Description: "d", Value: "x"}, ErrSecretRejected},
		{"secret password", MetadataEntry{Key: "db.password", Description: "d", Value: "x"}, ErrSecretRejected},
		{"secret apikey", MetadataEntry{Key: "service.apikey", Description: "d", Value: "x"}, ErrSecretRejected},
		{"secret credential", MetadataEntry{Key: "aws.credential", Description: "d", Value: "x"}, ErrSecretRejected},
		{"invalid func value", MetadataEntry{Key: "a.b", Description: "d", Value: func() {}}, ErrInvalidValue},
		{"invalid chan value", MetadataEntry{Key: "a.b", Description: "d", Value: make(chan int)}, ErrInvalidValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEntry(tc.entry)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("validateEntry = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("validateEntry = %v, want %v", err, tc.want)
			}
		})
	}
}
