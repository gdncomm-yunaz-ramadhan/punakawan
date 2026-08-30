// Package clienthooks maps a supported coding-agent client's own lifecycle
// hook events (Codex, Claude Code) onto telemetry.Store actions
// (Begin/IngestSnapshot/Finalize). Each client's event JSON is treated as
// versioned client input, not a stable shared protocol - see codex.go and
// claude.go's own doc comments for the exact event/field mapping each
// implements.
package clienthooks

import (
	"bufio"
	"encoding/json"
	"os"
	"time"

	"github.com/ygrip/punakawan/internal/telemetry"
)

// transcriptSummary is what summarizeTranscript sums out of one transcript
// JSONL file: per-model token usage, a count of distinct tool calls, and
// the wall-clock time spanned by every line in the file.
type transcriptSummary struct {
	Models         []telemetry.ModelUsage
	ToolCalls      int64
	ElapsedSeconds float64
}

// transcriptLine is the subset of one JSONL line this package reads. It
// tolerates both the Anthropic Messages API shape (message.content blocks
// of type "tool_use", each carrying an id) Claude Code transcripts use,
// and a looser tool_calls array some other clients may use instead -
// whichever is present is counted; neither being present is not an error,
// just zero tool calls contributed by that line.
type transcriptLine struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Message   *struct {
		Role  string `json:"role"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
		Content []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"content"`
		ToolCalls []struct {
			ID string `json:"id"`
		} `json:"tool_calls"`
	} `json:"message"`
}

// summarizeTranscript reads path (a main-session or subagent transcript
// JSONL file) and sums its per-model token usage, distinct tool-call
// count, and wall-clock elapsed time. A line that fails to parse, or has
// no timestamp/usage, is skipped rather than failing the whole read - a
// transcript is append-only and may have a partially-written trailing
// line if read racing a live write. Re-reading the exact same file
// content twice yields the exact same summary, which is what lets a
// caller derive a monotonic snapshot sequence straight from these totals:
// a replayed hook reading an unchanged transcript computes an identical
// sequence, and IngestSnapshot's monotonic upsert then treats it as a
// no-op automatically.
func summarizeTranscript(path string) (transcriptSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return transcriptSummary{}, err
	}
	defer f.Close()

	byModel := map[string]*telemetry.ModelUsage{}
	seenToolCalls := map[string]bool{}
	var started, ended time.Time

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var tl transcriptLine
		if err := json.Unmarshal(line, &tl); err != nil {
			continue
		}
		if !tl.Timestamp.IsZero() {
			if started.IsZero() || tl.Timestamp.Before(started) {
				started = tl.Timestamp
			}
			if ended.IsZero() || tl.Timestamp.After(ended) {
				ended = tl.Timestamp
			}
		}
		if tl.Type != "assistant" || tl.Message == nil {
			continue
		}
		for _, block := range tl.Message.Content {
			if block.Type == "tool_use" && block.ID != "" {
				seenToolCalls[block.ID] = true
			}
		}
		for _, call := range tl.Message.ToolCalls {
			if call.ID != "" {
				seenToolCalls[call.ID] = true
			}
		}
		if tl.Message.Usage == nil {
			continue
		}
		model := tl.Message.Model
		mu, ok := byModel[model]
		if !ok {
			mu = &telemetry.ModelUsage{Model: model}
			byModel[model] = mu
		}
		mu.InputTokens += tl.Message.Usage.InputTokens
		mu.OutputTokens += tl.Message.Usage.OutputTokens
		mu.CacheWriteTokens += tl.Message.Usage.CacheCreationInputTokens
		mu.CacheReadTokens += tl.Message.Usage.CacheReadInputTokens
	}
	if err := scanner.Err(); err != nil {
		return transcriptSummary{}, err
	}

	out := transcriptSummary{ToolCalls: int64(len(seenToolCalls))}
	if !started.IsZero() && !ended.IsZero() {
		out.ElapsedSeconds = ended.Sub(started).Seconds()
	}
	for _, mu := range byModel {
		out.Models = append(out.Models, *mu)
	}
	return out, nil
}

// snapshotSequence derives a monotonic sequence number straight from a
// transcript summary's own totals, per summarizeTranscript's doc comment:
// re-reading an unchanged transcript always yields the same sequence, and
// a transcript that only ever grows always yields a strictly larger one.
func snapshotSequence(summary transcriptSummary) int64 {
	var total int64
	for _, m := range summary.Models {
		total += m.InputTokens + m.OutputTokens + m.CacheWriteTokens + m.CacheReadTokens
	}
	return total + summary.ToolCalls
}

func snapshotFromTranscript(sessionID, sourceID string, summary transcriptSummary, observedAt time.Time) telemetry.SnapshotRequest {
	var input, output, cacheWrite, cacheRead int64
	for _, m := range summary.Models {
		input += m.InputTokens
		output += m.OutputTokens
		cacheWrite += m.CacheWriteTokens
		cacheRead += m.CacheReadTokens
	}
	return telemetry.SnapshotRequest{
		SessionID:        sessionID,
		SourceID:         sourceID,
		Sequence:         snapshotSequence(summary),
		InputTokens:      input,
		OutputTokens:     output,
		CacheWriteTokens: cacheWrite,
		CacheReadTokens:  cacheRead,
		ToolCalls:        summary.ToolCalls,
		ElapsedMS:        int64(summary.ElapsedSeconds * 1000),
		ModelUsage:       summary.Models,
		ObservedAt:       observedAt,
	}
}
