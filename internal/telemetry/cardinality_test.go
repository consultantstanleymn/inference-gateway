package telemetry_test

import (
	"context"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/consultantstanleymn/inference-gateway/gen/inferencev1"
	"github.com/consultantstanleymn/inference-gateway/internal/batcher"
	"github.com/consultantstanleymn/inference-gateway/internal/server"
	"github.com/consultantstanleymn/inference-gateway/internal/telemetry"
)

// seriesBudget is the max series per gateway_* family, per instance.
// A histogram series counts one series per bucket, plus _sum and _count.
var seriesBudget = map[string]int{
	"gateway_requests_total":           14, // 2 transports x 7 outcomes
	"gateway_request_duration_seconds": 28, // 2 transports x (11 buckets + Inf + sum + count)
	"gateway_queue_wait_seconds":       14,
	"gateway_batch_size":               10,
	"gateway_batch_run_seconds":        14,
	"gateway_shed_total":               1,
	"gateway_cancelled_total":          1,
	"gateway_queue_depth":              1,
	"gateway_model_info":               2, // at most 2 versions
}

// allowedLabels is the exact label name set for each family.
var allowedLabels = map[string][]string{
	"gateway_requests_total":           {"transport", "outcome"},
	"gateway_request_duration_seconds": {"transport"},
	"gateway_queue_wait_seconds":       {},
	"gateway_batch_size":               {},
	"gateway_batch_run_seconds":        {},
	"gateway_shed_total":               {},
	"gateway_cancelled_total":          {},
	"gateway_queue_depth":              {},
	"gateway_model_info":               {"version"},
}

var allowedTransport = map[string]bool{"http": true, "grpc": true}

var allowedOutcome = map[string]bool{
	"ok": true, "shed": true, "timeout": true, "cancelled": true,
	"invalid": true, "closed": true, "error": true,
}

// allowedVersion is the fixed set of model versions used in this test.
var allowedVersion = map[string]bool{"vtest": true}

type runner struct{}

func (runner) Run(rows [][]int64) ([][]float32, error) {
	out := make([][]float32, len(rows))
	for i := range rows {
		out[i] = []float32{0.1, 0.7, 0.2}
	}
	return out, nil
}

// randomText builds a request-like string: random length, unicode, slashes, query marks.
func randomText(r *rand.Rand) string {
	n := r.Intn(80)
	var b strings.Builder
	for range n {
		switch r.Intn(6) {
		case 0:
			b.WriteRune(rune('a' + r.Intn(26)))
		case 1:
			b.WriteRune(rune('0' + r.Intn(10)))
		case 2:
			b.WriteString("/")
		case 3:
			b.WriteString("?")
		case 4:
			b.WriteRune(rune(0x4e00 + r.Intn(0x100))) // CJK block
		default:
			b.WriteRune(rune(0x00e0 + r.Intn(0x40))) // Latin-1 letters
		}
	}
	return b.String()
}

func TestLabelSetsStayBounded(t *testing.T) {
	m := telemetry.NewMetrics(16)
	m.ModelInfo.WithLabelValues("vtest").Set(1) // main sets this when the model loads
	b := batcher.New(runner{}, batcher.Config{MaxBatch: 16, MaxWait: time.Millisecond, QueueSize: 64}, batcher.Observer{OnShed: m.Shed.Inc})
	t.Cleanup(b.Close)
	svc := &server.Service{Batcher: b, Labels: []string{"a", "b", "c"}, Version: "vtest", Timeout: 2 * time.Second}
	srv := server.New(svc, m)

	hl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.ServeHTTP(hl) }()
	go func() { _ = srv.ServeGRPC(gl) }()
	srv.SetReady(true)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	base := "http://" + hl.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second}

	// One ok request and one invalid request on the HTTP path.
	for _, body := range []string{`{"text":"hello there"}`, `{"text":""}`} {
		resp, err := client.Post(base+"/v1/classify", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}

	// 50 random request-like strings as text, as URL paths and as query strings.
	r := rand.New(rand.NewSource(42))
	for range 50 {
		s := randomText(r)
		body := `{"text":` + quoteJSON(s) + `}`
		resp, err := client.Post(base+"/v1/classify", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()

		// A 404 path must not create a series.
		resp, err = client.Get(base + "/" + url.PathEscape(s))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()

		// A query string must not create a series.
		resp, err = client.Get(base + "/healthz?q=" + url.QueryEscape(s))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}

	// One gRPC call.
	conn, err := grpc.NewClient(gl.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := inferencev1.NewGatewayClient(conn).Classify(context.Background(), &inferencev1.ClassifyRequest{Text: "hello there"}); err != nil {
		t.Fatal(err)
	}

	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for _, f := range families {
		name := f.GetName()
		if !strings.HasPrefix(name, "gateway_") {
			continue
		}
		seen[name] = true
		budget, ok := seriesBudget[name]
		if !ok {
			t.Errorf("family %q has no budget entry", name)
			continue
		}
		if got := len(f.GetMetric()); got > budget {
			t.Errorf("family %q has %d series, budget %d", name, got, budget)
		}
		want := allowedLabels[name]
		for _, mt := range f.GetMetric() {
			labels := mt.GetLabel()
			if len(labels) != len(want) {
				t.Errorf("family %q has %d labels, want %d", name, len(labels), len(want))
			}
			for _, lp := range labels {
				if !contains(want, lp.GetName()) {
					t.Errorf("family %q has label %q outside the allowed set", name, lp.GetName())
				}
				checkValue(t, name, lp.GetName(), lp.GetValue())
			}
		}
	}
	for name := range seriesBudget {
		if !seen[name] {
			t.Errorf("family %q was not gathered", name)
		}
	}
}

func checkValue(t *testing.T, family, label, value string) {
	t.Helper()
	switch label {
	case "transport":
		if !allowedTransport[value] {
			t.Errorf("family %q transport value %q is outside the allowed set", family, value)
		}
	case "outcome":
		if !allowedOutcome[value] {
			t.Errorf("family %q outcome value %q is outside the allowed set", family, value)
		}
	case "version":
		if !allowedVersion[value] {
			t.Errorf("family %q version value %q is outside the allowed set", family, value)
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// quoteJSON escapes a string for use inside a JSON body.
func quoteJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				const hex = "0123456789abcdef"
				b.WriteByte(hex[(r>>4)&0xf])
				b.WriteByte(hex[r&0xf])
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// keep the dto import used if the file changes later
var _ = dto.MetricType_COUNTER
var _ prometheus.Collector
