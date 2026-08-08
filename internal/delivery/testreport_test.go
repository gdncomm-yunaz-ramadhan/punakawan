package delivery

import (
	"strings"
	"testing"
)

// TestParseExtractsDeepestCausedBy covers AC4's Maven/Spring case: a
// multi-level "Caused by:" chain must surface the deepest (actual root)
// cause, not the outermost wrapper exception.
func TestParseExtractsDeepestCausedBy(t *testing.T) {
	log := strings.Join([]string{
		"org.springframework.beans.factory.BeanCreationException: Error creating bean",
		"Caused by: org.springframework.jdbc.CannotGetJdbcConnectionException: Could not get connection",
		"Caused by: java.net.ConnectException: Connection refused",
	}, "\n")

	summary := Parse(Invocation{Command: "mvn test", ExitCode: 1, Combined: []byte(log)}, "artifact-1")
	if summary.FirstCausalFailure == nil {
		t.Fatal("expected a causal failure to be extracted")
	}
	if !strings.Contains(*summary.FirstCausalFailure, "Connection refused") {
		t.Fatalf("expected deepest Caused by line, got %q", *summary.FirstCausalFailure)
	}
	if summary.ArtifactId != "artifact-1" || summary.Command != "mvn test" || summary.ExitCode != 1 {
		t.Fatalf("unexpected summary fields: %+v", summary)
	}
}

// TestParseFallsBackToKnownSignatures covers AC4's non-"Caused by:"
// frameworks explicitly required: GCP ADC, MongoDB, and ByteBuddy.
func TestParseFallsBackToKnownSignatures(t *testing.T) {
	cases := []struct {
		name string
		log  string
		want string
	}{
		{"gcp-adc", "INFO: starting\ncom.google.auth.oauth2.DefaultCredentialsProvider: Could not find Application Default Credentials\n", "Application Default Credentials"},
		{"mongo", "connecting...\ncom.mongodb.MongoException: no server available\n", "com.mongodb.MongoException"},
		{"bytebuddy", "generating proxy\nnet.bytebuddy.dynamic.scaffold.InstrumentedType$Default$MethodConfigurableInstrumentedType: cannot instrument\n", "net.bytebuddy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			summary := Parse(Invocation{Command: "test", ExitCode: 1, Combined: []byte(c.log)}, "artifact-1")
			if summary.FirstCausalFailure == nil || !strings.Contains(*summary.FirstCausalFailure, c.want) {
				t.Fatalf("expected failure signature containing %q, got %+v", c.want, summary.FirstCausalFailure)
			}
		})
	}
}

// TestParseDedupesRetryNoiseAndBoundsTail ensures repeated retry lines
// collapse (so the failure isn't pushed out of the bounded window) and
// that the tail never exceeds maxTailBytes.
func TestParseDedupesRetryNoiseAndBoundsTail(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("Retrying connection attempt...\n")
	}
	b.WriteString("panic: something broke\n")
	for i := 0; i < 5000; i++ {
		b.WriteString("padding line to force truncation\n")
	}

	summary := Parse(Invocation{Command: "go test", Combined: []byte(b.String())}, "artifact-2")
	if !summary.Truncated {
		t.Fatal("expected tail to be truncated for an oversized log")
	}
	if len(summary.Tail) > maxTailBytes {
		t.Fatalf("tail exceeds bound: %d > %d", len(summary.Tail), maxTailBytes)
	}

	deduped := dedupeRetryNoise(b.String())
	noiseCount := strings.Count(deduped, "Retrying connection attempt...")
	if noiseCount != 1 {
		t.Fatalf("expected retry noise collapsed to 1 line, got %d", noiseCount)
	}
}

func TestParseJUnitXMLCountsAndFirstFailure(t *testing.T) {
	xml := `<testsuite tests="3" failures="1" errors="0" skipped="1">
		<testcase name="a"/>
		<testcase name="b"><failure message="expected true, got false">stack trace line 1
more trace</failure></testcase>
		<testcase name="c"/>
	</testsuite>`

	summary, err := ParseJUnitXML([]byte(xml))
	if err != nil {
		t.Fatalf("ParseJUnitXML: %v", err)
	}
	if summary.TotalTests == nil || *summary.TotalTests != 3 {
		t.Fatalf("expected total 3, got %+v", summary.TotalTests)
	}
	if summary.Failed == nil || *summary.Failed != 1 {
		t.Fatalf("expected failed 1, got %+v", summary.Failed)
	}
	if summary.Skipped == nil || *summary.Skipped != 1 {
		t.Fatalf("expected skipped 1, got %+v", summary.Skipped)
	}
	if summary.Passed == nil || *summary.Passed != 1 {
		t.Fatalf("expected passed 1, got %+v", summary.Passed)
	}
	if summary.FirstCausalFailure == nil || *summary.FirstCausalFailure != "expected true, got false" {
		t.Fatalf("unexpected first causal failure: %+v", summary.FirstCausalFailure)
	}
}

func TestParseGoTestJSONCountsAndFirstFailure(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"Action":"run","Test":"TestA"}`,
		`{"Action":"pass","Test":"TestA"}`,
		`{"Action":"run","Test":"TestB"}`,
		`{"Action":"output","Test":"TestB","Output":"    want 1, got 2\n"}`,
		`{"Action":"fail","Test":"TestB"}`,
		`{"Action":"run","Test":"TestC"}`,
		`{"Action":"skip","Test":"TestC"}`,
	}, "\n")

	summary := ParseGoTestJSON([]byte(ndjson))
	if summary.TotalTests == nil || *summary.TotalTests != 3 {
		t.Fatalf("expected total 3, got %+v", summary.TotalTests)
	}
	if summary.Passed == nil || *summary.Passed != 1 {
		t.Fatalf("expected passed 1, got %+v", summary.Passed)
	}
	if summary.Failed == nil || *summary.Failed != 1 {
		t.Fatalf("expected failed 1, got %+v", summary.Failed)
	}
	if summary.Skipped == nil || *summary.Skipped != 1 {
		t.Fatalf("expected skipped 1, got %+v", summary.Skipped)
	}
	if summary.FirstCausalFailure == nil || !strings.Contains(*summary.FirstCausalFailure, "want 1, got 2") {
		t.Fatalf("unexpected first causal failure: %+v", summary.FirstCausalFailure)
	}
}
