package mcpserver

import (
	"reflect"
	"testing"
)

func TestProjectResultTopLevelAndDotted(t *testing.T) {
	result := map[string]any{
		"key":     "PAY-123",
		"summary": "top-level summary",
		"fields": map[string]any{
			"summary":     "nested summary",
			"description": "big ADF payload",
			"status":      "In Progress",
		},
		"raw": map[string]any{"huge": "envelope"},
	}

	got := projectResult(result, []string{"key", "fields.summary", "fields.status", "missing", "raw.nope"})
	want := map[string]any{
		"key": "PAY-123",
		"fields": map[string]any{
			"summary": "nested summary",
			"status":  "In Progress",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projection mismatch:\n got=%#v\nwant=%#v", got, want)
	}
	// original not mutated
	if _, ok := result["raw"]; !ok {
		t.Fatal("projectResult mutated the source map")
	}
}

func TestProjectResultEmptyFieldsIgnored(t *testing.T) {
	got := projectResult(map[string]any{"a": 1}, []string{"", "a"})
	if !reflect.DeepEqual(got, map[string]any{"a": 1}) {
		t.Fatalf("unexpected projection: %#v", got)
	}
}
