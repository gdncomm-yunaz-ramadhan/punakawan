// testreport.go implements a causal-first test report summarizer: a
// bounded, retry-noise-deduplicated projection of one
// invocation's combined stdout+stderr, returned alongside (never
// instead of) the full log stored as an EvidenceArtifact.
package delivery

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"regexp"
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// maxTailBytes bounds TestReportSummary.Tail so a continuous agent loop
// never has to page a multi-megabyte log through its own context just
// to see what failed.
const maxTailBytes = 8192

var causedByPattern = regexp.MustCompile(`(?m)^(?:Caused by|caused by):\s*(.+)$`)

// knownFailureSignatures catches frameworks that don't use a "Caused
// by:" chain but still print a single, recognizable root-cause line,
// including GCP Application Default Credentials, MongoDB driver, and
// ByteBuddy errors.
var knownFailureSignatures = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^.*(?:com\.google\.auth\.oauth2\.DefaultCredentialsProvider|Application Default Credentials).*$`),
	regexp.MustCompile(`(?m)^.*com\.mongodb\.MongoException.*$`),
	regexp.MustCompile(`(?m)^.*net\.bytebuddy\..*$`),
	regexp.MustCompile(`(?m)^.*panic:.*$`),
	regexp.MustCompile(`(?m)^(?:FAIL|Error:)\s.*$`),
}

// retryNoisePattern strips lines a retrying build tool repeats
// identically across attempts (progress spinners, "Retrying...",
// download progress), so the tail isn't dominated by noise instead of
// signal.
var retryNoisePattern = regexp.MustCompile(`(?i)^\s*(retrying|download(ing)?|\[\d+/\d+\]|progress:)\b`)

// Invocation is the raw material Parse turns into a TestReportSummary:
// the exact command run, its exit code and wall-clock duration, and its
// combined stdout+stderr exactly as produced - Parse never mutates or
// truncates this before it is stored as the full EvidenceArtifact.
type Invocation struct {
	Command    string
	ExitCode   int
	DurationMs int
	Combined   []byte
}

// Parse extracts a bounded TestReportSummary from inv, deduplicating
// consecutive retry-noise lines and bounding the tail to maxTailBytes.
// artifactID must already reference the untruncated Combined bytes
// stored via PutArtifact/RecordArtifact - Parse only computes the
// summary, it never touches storage.
func Parse(inv Invocation, artifactID string) protocol.TestReportSummary {
	text := string(inv.Combined)
	deduped := dedupeRetryNoise(text)

	summary := protocol.TestReportSummary{
		Command:    inv.Command,
		ExitCode:   inv.ExitCode,
		DurationMs: inv.DurationMs,
		ArtifactId: artifactID,
	}
	summary.FirstCausalFailure = firstCausalFailure(deduped)
	summary.Tail, summary.Truncated = boundedTail(deduped, maxTailBytes)
	return summary
}

// firstCausalFailure prefers the deepest "Caused by:" line (the actual
// root cause of a chained exception, not its wrapper); if none is
// present it falls back to the first recognized failure signature.
func firstCausalFailure(text string) *string {
	if matches := causedByPattern.FindAllStringSubmatch(text, -1); len(matches) > 0 {
		line := strings.TrimSpace(matches[len(matches)-1][1])
		return &line
	}
	for _, sig := range knownFailureSignatures {
		if m := sig.FindString(text); m != "" {
			line := strings.TrimSpace(m)
			return &line
		}
	}
	return nil
}

// dedupeRetryNoise collapses consecutive lines matching
// retryNoisePattern into a single line, so N identical retry attempts
// don't push the actual failure out of the bounded tail.
func dedupeRetryNoise(text string) string {
	var out []string
	lastWasNoise := false
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if retryNoisePattern.MatchString(line) {
			if lastWasNoise {
				continue
			}
			lastWasNoise = true
		} else {
			lastWasNoise = false
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// boundedTail returns the last maxBytes of text, cut at a line boundary
// so the tail never starts mid-line.
func boundedTail(text string, maxBytes int) (string, bool) {
	if len(text) <= maxBytes {
		return text, false
	}
	cut := text[len(text)-maxBytes:]
	if idx := strings.IndexByte(cut, '\n'); idx >= 0 {
		cut = cut[idx+1:]
	}
	return cut, true
}

// junitTestSuite mirrors the JUnit/Surefire/Gradle XML test report
// format closely enough to extract counts and per-failure messages;
// fields absent from a given tool's dialect are simply left zero.
type junitTestSuite struct {
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Errors   int             `xml:"errors,attr"`
	Skipped  int             `xml:"skipped,attr"`
	Cases    []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	Failure *junitFailure `xml:"failure"`
	Error   *junitFailure `xml:"error"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// ParseJUnitXML extracts pass/fail/skip counts and the first failure
// message from a JUnit/Surefire/Gradle-style XML report. It is a pure
// function operating on already-read bytes; it does not touch storage
// or discover report files on disk (report-file discovery is a separate
// concern, tracked elsewhere).
func ParseJUnitXML(data []byte) (protocol.TestReportSummary, error) {
	var suite junitTestSuite
	if err := xml.Unmarshal(data, &suite); err != nil {
		return protocol.TestReportSummary{}, err
	}
	failed := suite.Failures + suite.Errors
	passed := suite.Tests - failed - suite.Skipped
	summary := protocol.TestReportSummary{
		TotalTests: &suite.Tests,
		Passed:     &passed,
		Failed:     &failed,
		Skipped:    &suite.Skipped,
	}
	for _, tc := range suite.Cases {
		f := tc.Failure
		if f == nil {
			f = tc.Error
		}
		if f == nil {
			continue
		}
		msg := strings.TrimSpace(f.Message)
		if msg == "" {
			msg = strings.TrimSpace(firstLine(f.Body))
		}
		if msg != "" {
			summary.FirstCausalFailure = &msg
			break
		}
	}
	return summary, nil
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

// goTestEvent mirrors one line of `go test -json` output.
type goTestEvent struct {
	Action string `json:"Action"`
	Test   string `json:"Test"`
	Output string `json:"Output"`
}

// ParseGoTestJSON extracts pass/fail/skip counts and the first failing
// test's output from `go test -json` NDJSON output. Malformed lines are
// skipped rather than aborting the whole parse, since a truncated
// stream (killed process) should still yield a partial, useful summary.
func ParseGoTestJSON(data []byte) protocol.TestReportSummary {
	var passed, failed, skipped int
	var firstFailure *string
	failureOutput := map[string]*strings.Builder{}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev goTestEvent
		if err := json.Unmarshal(line, &ev); err != nil || ev.Test == "" {
			continue
		}
		switch ev.Action {
		case "pass":
			passed++
		case "fail":
			failed++
			if firstFailure == nil {
				if b, ok := failureOutput[ev.Test]; ok {
					msg := strings.TrimSpace(b.String())
					firstFailure = &msg
				}
			}
		case "skip":
			skipped++
		case "output":
			b, ok := failureOutput[ev.Test]
			if !ok {
				b = &strings.Builder{}
				failureOutput[ev.Test] = b
			}
			b.WriteString(ev.Output)
		}
	}
	total := passed + failed + skipped
	return protocol.TestReportSummary{
		TotalTests:         &total,
		Passed:             &passed,
		Failed:             &failed,
		Skipped:            &skipped,
		FirstCausalFailure: firstFailure,
	}
}
