package server

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/consultantstanleymn/inference-gateway/internal/batcher"
)

var (
	traceOnce sync.Once
	traceExp  *tracetest.InMemoryExporter
)

// useTracer installs one in-memory tracer for the whole test binary and clears it for each test.
// The global tracer provider can be set only once for the package tracer variable, so we reuse it.
func useTracer(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	traceOnce.Do(func() {
		traceExp = tracetest.NewInMemoryExporter()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(traceExp), sdktrace.WithSampler(sdktrace.AlwaysSample())))
	})
	traceExp.Reset()
	return traceExp
}

func scrape(t *testing.T, base, accept string) string {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/metrics", nil)
	req.Header.Set("Accept", accept)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// requestSpan is the name that the HTTP instrumentation gives to the request span.
const requestSpan = "POST /v1/classify"

func TestLatencyHasModelVersionLabel(t *testing.T) {
	g := start(t, &slowRunner{}, batcher.Config{MaxBatch: 4, MaxWait: time.Millisecond, QueueSize: 8})
	resp, err := post(g.httpBase, `{"text":"hello there"}`)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("post: %v %v", resp, err)
	}
	_ = resp.Body.Close()
	body := scrape(t, g.httpBase, "text/plain")
	if !strings.Contains(body, `gateway_request_duration_seconds_count{model_version="vtest",transport="http"} 1`) {
		t.Fatalf("latency count with model_version label not found in:\n%s", body)
	}
}

func TestExemplarCarriesTraceID(t *testing.T) {
	exp := useTracer(t)
	g := start(t, &slowRunner{}, batcher.Config{MaxBatch: 4, MaxWait: time.Millisecond, QueueSize: 8})
	resp, err := post(g.httpBase, `{"text":"hello there"}`)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("post: %v %v", resp, err)
	}
	_ = resp.Body.Close()
	var traceID string
	for _, s := range exp.GetSpans() {
		if s.Name == "queue.wait" {
			traceID = s.SpanContext.TraceID().String()
		}
	}
	if traceID == "" {
		t.Fatal("no queue.wait span recorded")
	}
	body := scrape(t, g.httpBase, "application/openmetrics-text")
	re := regexp.MustCompile(`gateway_request_duration_seconds_bucket\{[^}]*\} \d+ # \{trace_id="([0-9a-f]{32})"\}`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no exemplar found in:\n%s", body)
	}
	if m[1] != traceID {
		t.Fatalf("exemplar trace id %s, span trace id %s", m[1], traceID)
	}
}

func TestQueueWaitSpanLinkedToRequest(t *testing.T) {
	exp := useTracer(t)
	g := start(t, &slowRunner{}, batcher.Config{MaxBatch: 4, MaxWait: time.Millisecond, QueueSize: 8})
	resp, err := post(g.httpBase, `{"text":"hello there"}`)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("post: %v %v", resp, err)
	}
	_ = resp.Body.Close()
	// The server ends the request span after it writes the answer, so wait for it.
	byName := map[string]tracetest.SpanStub{}
	for range 100 {
		byName = map[string]tracetest.SpanStub{}
		for _, s := range exp.GetSpans() {
			byName[s.Name] = s
		}
		if _, ok := byName[requestSpan]; ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	root, ok := byName[requestSpan]
	if !ok {
		names := []string{}
		for n := range byName {
			names = append(names, n)
		}
		t.Fatalf("no request span; got %v", names)
	}
	for _, name := range []string{"tokenize", "queue.wait", "batch.run"} {
		s, ok := byName[name]
		if !ok {
			t.Fatalf("no %s span", name)
		}
		if s.SpanContext.TraceID() != root.SpanContext.TraceID() {
			t.Fatalf("%s is in another trace", name)
		}
		if s.Parent.SpanID() != root.SpanContext.SpanID() {
			t.Fatalf("%s is not a child of the request span", name)
		}
	}
}
