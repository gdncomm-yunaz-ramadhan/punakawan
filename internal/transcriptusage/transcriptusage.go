// Package transcriptusage sums token usage and wall-clock elapsed time out
// of a Claude Code subagent transcript JSONL file, so a SubagentStop hook
// can report what a subagent actually cost without having to trust a
// self-reported number.
//
// Confirmed empirically against a real subagent transcript
// (~/.claude/projects/<project>/<session>/subagents/agent-<id>.jsonl):
// every line of type "assistant" carries message.model and
// message.usage.{input_tokens,output_tokens,cache_creation_input_tokens,
// cache_read_input_tokens}; each such line is one API turn's usage, not a
// running total, so summing across every assistant line gives the
// subagent's real total. Every line carries a top-level RFC3339 timestamp;
// the earliest and latest across the whole file bound the subagent's
// wall-clock duration.
package transcriptusage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ModelUsage is one model's summed token usage across a transcript.
type ModelUsage struct {
	Model                    string
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
}

// Summary is one transcript file's usage summary.
type Summary struct {
	ByModel        []ModelUsage
	StartedAt      time.Time
	EndedAt        time.Time
	ElapsedSeconds float64
}

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
	} `json:"message"`
}

// Summarize reads path (a subagent transcript JSONL file) and returns its
// per-model token usage plus the elapsed wall-clock time spanned by every
// line in the file. A line that fails to parse as JSON, or has no
// timestamp/usage, is skipped rather than failing the whole read - a
// transcript is append-only and may have a partially-written trailing line
// if read racing a live write.
func Summarize(path string) (*Summary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("transcriptusage: open %s: %w", path, err)
	}
	defer f.Close()

	byModel := map[string]*ModelUsage{}
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
		if tl.Type != "assistant" || tl.Message == nil || tl.Message.Usage == nil {
			continue
		}
		model := tl.Message.Model
		mu, ok := byModel[model]
		if !ok {
			mu = &ModelUsage{Model: model}
			byModel[model] = mu
		}
		mu.InputTokens += tl.Message.Usage.InputTokens
		mu.OutputTokens += tl.Message.Usage.OutputTokens
		mu.CacheCreationInputTokens += tl.Message.Usage.CacheCreationInputTokens
		mu.CacheReadInputTokens += tl.Message.Usage.CacheReadInputTokens
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("transcriptusage: read %s: %w", path, err)
	}

	out := &Summary{StartedAt: started, EndedAt: ended}
	if !started.IsZero() && !ended.IsZero() {
		out.ElapsedSeconds = ended.Sub(started).Seconds()
	}
	for _, mu := range byModel {
		out.ByModel = append(out.ByModel, *mu)
	}
	return out, nil
}
