package main

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/consultantstanleymn/inference-gateway/internal/batcher"
	"github.com/consultantstanleymn/inference-gateway/internal/config"
	"github.com/consultantstanleymn/inference-gateway/internal/server"
	"github.com/consultantstanleymn/inference-gateway/internal/telemetry"
)

type stubRunner struct{}

func (stubRunner) Run(rows [][]int64) ([][]float32, error) {
	out := make([][]float32, len(rows))
	for i := range rows {
		out[i] = []float32{1}
	}
	return out, nil
}

func status(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url) //nolint:gosec // test URL on a loopback listener
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestPprofOffByDefault(t *testing.T) {
	cfg, err := config.Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminAddr != "" {
		t.Fatalf("admin address is %q by default, want empty", cfg.AdminAddr)
	}
	srv, err := startAdmin(cfg.AdminAddr, make(chan error, 1))
	if err != nil || srv != nil {
		t.Fatalf("startAdmin with an empty address returned %v, %v", srv, err)
	}
}

func TestPprofOnlyOnAdminAddr(t *testing.T) {
	m := telemetry.NewMetrics(4)
	b := batcher.New(stubRunner{}, batcher.Config{MaxBatch: 4, MaxWait: time.Millisecond, QueueSize: 8}, batcher.Observer{})
	t.Cleanup(b.Close)
	pub := server.New(&server.Service{Batcher: b, Labels: []string{"a"}, Version: "vtest", Timeout: time.Second}, m)
	pl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = pub.ServeHTTP(pl) }()

	admin, err := startAdmin("127.0.0.1:0", make(chan error, 1))
	if err != nil || admin == nil {
		t.Fatalf("startAdmin: %v %v", admin, err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	// startAdmin hides the listener, so test the handler on its own listener as well.
	al, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	as := &http.Server{Handler: adminHandler(), ReadHeaderTimeout: time.Second}
	go func() { _ = as.Serve(al) }()
	t.Cleanup(func() { _ = as.Close() })

	if got := status(t, "http://"+al.Addr().String()+"/debug/pprof/"); got != http.StatusOK {
		t.Fatalf("admin /debug/pprof/ status %d, want 200", got)
	}
	if got := status(t, "http://"+pl.Addr().String()+"/debug/pprof/"); got != http.StatusNotFound {
		t.Fatalf("public /debug/pprof/ status %d, want 404", got)
	}
}
