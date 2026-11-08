package server

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/consultantstanleymn/inference-gateway/gen/inferencev1"
	"github.com/consultantstanleymn/inference-gateway/internal/batcher"
	"github.com/consultantstanleymn/inference-gateway/internal/telemetry"
)

type slowRunner struct {
	delay time.Duration
	block chan struct{}
}

func (r *slowRunner) Run(rows [][]int64) ([][]float32, error) {
	if r.block != nil {
		<-r.block
	}
	time.Sleep(r.delay)
	out := make([][]float32, len(rows))
	for i := range rows {
		out[i] = []float32{0.1, 0.7, 0.2}
	}
	return out, nil
}

type rig struct {
	srv      *Server
	httpBase string
	grpcAddr string
	b        *batcher.Batcher
}

func start(t *testing.T, r batcher.Runner, cfg batcher.Config) *rig {
	t.Helper()
	m := telemetry.NewMetrics(cfg.MaxBatch)
	b := batcher.New(r, cfg, batcher.Observer{OnShed: m.Shed.Inc})
	svc := &Service{Batcher: b, Labels: []string{"a", "b", "c"}, Version: "vtest", Timeout: 2 * time.Second}
	srv := New(svc, m)
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
	t.Cleanup(func() { b.Close() })
	return &rig{srv: srv, httpBase: "http://" + hl.Addr().String(), grpcAddr: gl.Addr().String(), b: b}
}

func post(base, body string) (*http.Response, error) {
	return http.Post(base+"/v1/classify", "application/json", strings.NewReader(body))
}

func TestHTTPAndGRPCAnswer(t *testing.T) {
	g := start(t, &slowRunner{}, batcher.Config{MaxBatch: 4, MaxWait: time.Millisecond, QueueSize: 8})
	resp, err := post(g.httpBase, `{"text":"hello there"}`)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("http: %v %v", resp, err)
	}
	_ = resp.Body.Close()
	if resp, _ = post(g.httpBase, `{"text":""}`); resp.StatusCode != 400 {
		t.Fatalf("empty text status %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	conn, err := grpc.NewClient(g.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	out, err := inferencev1.NewGatewayClient(conn).Classify(context.Background(), &inferencev1.ClassifyRequest{Text: "hello there"})
	if err != nil || out.Intent != "b" || out.ModelVersion != "vtest" || len(out.Top) != 3 {
		t.Fatalf("grpc: %v %v", out, err)
	}
	_, err = inferencev1.NewGatewayClient(conn).Classify(context.Background(), &inferencev1.ClassifyRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty text code %v", status.Code(err))
	}
}

func TestOverloadReturns429AndResourceExhausted(t *testing.T) {
	r := &slowRunner{block: make(chan struct{})}
	g := start(t, r, batcher.Config{MaxBatch: 1, MaxWait: time.Millisecond, QueueSize: 1})
	var wg sync.WaitGroup
	// One request blocks in the model, one fills the queue.
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := post(g.httpBase, `{"text":"x"}`)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		time.Sleep(50 * time.Millisecond)
	}
	resp, err := post(g.httpBase, `{"text":"x"}`)
	if err != nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %v %v", resp, err)
	}
	_ = resp.Body.Close()
	conn, _ := grpc.NewClient(g.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer func() { _ = conn.Close() }()
	_, err = inferencev1.NewGatewayClient(conn).Classify(context.Background(), &inferencev1.ClassifyRequest{Text: "x"})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("want ResourceExhausted, got %v", err)
	}
	close(r.block)
	wg.Wait()
}

func TestReadyz(t *testing.T) {
	g := start(t, &slowRunner{}, batcher.Config{MaxBatch: 1, MaxWait: time.Millisecond, QueueSize: 1})
	resp, _ := post(g.httpBase, `{"text":"hello"}`)
	if resp.Close {
		t.Fatal("a ready server must keep connections alive")
	}
	_ = resp.Body.Close()
	g.srv.SetReady(false)
	resp, _ = http.Get(g.httpBase + "/readyz")
	if resp.StatusCode != 503 {
		t.Fatalf("not ready should be 503, got %d", resp.StatusCode)
	}
	if !resp.Close {
		t.Fatal("a draining server must ask clients to close the connection")
	}
	_ = resp.Body.Close()
	resp, _ = http.Get(g.httpBase + "/healthz")
	if resp.StatusCode != 200 {
		t.Fatalf("healthz %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestShutdownFinishesInFlight: a request that is running when shutdown starts still gets its answer.
func TestShutdownFinishesInFlight(t *testing.T) {
	g := start(t, &slowRunner{delay: 300 * time.Millisecond}, batcher.Config{MaxBatch: 4, MaxWait: time.Millisecond, QueueSize: 8})
	type res struct {
		code int
		err  error
	}
	done := make(chan res, 1)
	go func() {
		resp, err := post(g.httpBase, `{"text":"slow one"}`)
		if err != nil {
			done <- res{err: err}
			return
		}
		_ = resp.Body.Close()
		done <- res{code: resp.StatusCode}
	}()
	time.Sleep(100 * time.Millisecond) // the request is now inside the model call
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if err := g.srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil || r.code != 200 {
		t.Fatalf("in-flight request failed: %+v", r)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("shutdown did not wait for the in-flight request")
	}
	if _, err := post(g.httpBase, `{"text":"late"}`); err == nil {
		t.Fatal("new requests must be refused after shutdown")
	}
}
