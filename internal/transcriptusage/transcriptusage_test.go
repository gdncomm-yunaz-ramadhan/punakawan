package transcriptusage

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent-test.jsonl")
	data := ""
	for _, l := range lines {
		data += l + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestSummarizeSumsUsagePerModelAcrossAssistantLines(t *testing.T) {
	path := writeTranscript(t,
		`{"type":"assistant","timestamp":"2026-08-27T07:00:00.000Z","message":{"role":"assistant","model":"claude-sonnet-5","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":5,"cache_read_input_tokens":100}}}`,
		`{"type":"user","timestamp":"2026-08-27T07:00:01.000Z","message":{"role":"user"}}`,
		`{"type":"assistant","timestamp":"2026-08-27T07:05:00.000Z","message":{"role":"assistant","model":"claude-sonnet-5","usage":{"input_tokens":15,"output_tokens":25,"cache_creation_input_tokens":0,"cache_read_input_tokens":200}}}`,
	)

	summary, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(summary.ByModel) != 1 {
		t.Fatalf("ByModel = %+v, want exactly one model", summary.ByModel)
	}
	got := summary.ByModel[0]
	if got.Model != "claude-sonnet-5" || got.InputTokens != 25 || got.OutputTokens != 45 || got.CacheCreationInputTokens != 5 || got.CacheReadInputTokens != 300 {
		t.Fatalf("ByModel[0] = %+v, want summed usage across both assistant lines", got)
	}
	if summary.ElapsedSeconds != 300 {
		t.Fatalf("ElapsedSeconds = %v, want 300 (07:00:00 to 07:05:00)", summary.ElapsedSeconds)
	}
}

func TestSummarizeHandlesMultipleModelsSeparately(t *testing.T) {
	path := writeTranscript(t,
		`{"type":"assistant","timestamp":"2026-08-27T07:00:00.000Z","message":{"role":"assistant","model":"model-a","usage":{"input_tokens":10,"output_tokens":10}}}`,
		`{"type":"assistant","timestamp":"2026-08-27T07:00:10.000Z","message":{"role":"assistant","model":"model-b","usage":{"input_tokens":1,"output_tokens":1}}}`,
	)

	summary, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(summary.ByModel) != 2 {
		t.Fatalf("ByModel = %+v, want two distinct models", summary.ByModel)
	}
}

func TestSummarizeSkipsMalformedLinesInsteadOfFailing(t *testing.T) {
	path := writeTranscript(t,
		`not json at all`,
		`{"type":"assistant","timestamp":"2026-08-27T07:00:00.000Z","message":{"role":"assistant","model":"claude-sonnet-5","usage":{"input_tokens":1,"output_tokens":1}}}`,
		``,
	)

	summary, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(summary.ByModel) != 1 || summary.ByModel[0].InputTokens != 1 {
		t.Fatalf("ByModel = %+v, want the one valid line still counted", summary.ByModel)
	}
}

func TestSummarizeEmptyFileReturnsZeroValueSummary(t *testing.T) {
	path := writeTranscript(t)

	summary, err := Summarize(path)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(summary.ByModel) != 0 || summary.ElapsedSeconds != 0 {
		t.Fatalf("summary = %+v, want zero-value for an empty transcript", summary)
	}
}

func TestSummarizeMissingFileErrors(t *testing.T) {
	if _, err := Summarize(filepath.Join(t.TempDir(), "does-not-exist.jsonl")); err == nil {
		t.Fatal("expected an error for a missing transcript file")
	}
}
