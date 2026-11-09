package canary

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mustURL(t *testing.T, s *httptest.Server) *url.URL {
	t.Helper()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return u
}

func okServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestWeightZeroSendsAllToStable(t *testing.T) {
	var stableHits, canaryHits atomic.Int64
	stable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		stableHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(stable.Close)
	canary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		canaryHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(canary.Close)

	p := NewProxy(mustURL(t, stable), mustURL(t, canary), 1)
	p.SetWeight(0)

	for i := 0; i < 100; i++ {
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	}

	if got := canaryHits.Load(); got != 0 {
		t.Fatalf("canary hits = %d, want 0", got)
	}
	if got := stableHits.Load(); got != 100 {
		t.Fatalf("stable hits = %d, want 100", got)
	}
}

func TestWeightSplitWithinTolerance(t *testing.T) {
	stable := okServer(t)
	canary := okServer(t)

	p := NewProxy(mustURL(t, stable), mustURL(t, canary), 42)
	p.SetWeight(10)

	const total = 10000
	for i := 0; i < total; i++ {
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	}

	w := p.TakeWindow()
	ratio := float64(w.Canary.Requests) / float64(total)
	if ratio < 0.08 || ratio > 0.12 {
		t.Fatalf("canary ratio = %.4f, want 0.08..0.12 (canary=%d stable=%d)", ratio, w.Canary.Requests, w.Stable.Requests)
	}
}

func TestGateRollsBackOnErrors(t *testing.T) {
	g := Gate{MaxErrorRate: 0.05, MinRequests: 10}
	w := Window{
		Stable: UpstreamStats{Requests: 100, Errors: 0},
		Canary: UpstreamStats{Requests: 100, Errors: 20},
	}
	d := g.Evaluate(w)
	if d.Action != Rollback {
		t.Fatalf("action = %s, want rollback (reason %q)", d.Action, d.Reason)
	}
	if !strings.Contains(d.Reason, "0.2000") || !strings.Contains(d.Reason, "0.0500") {
		t.Fatalf("reason %q should include both numbers", d.Reason)
	}
}

func TestGateRollsBackOnLatency(t *testing.T) {
	g := Gate{MaxErrorRate: 0.05, MaxP95: 100 * time.Millisecond, MinRequests: 10}
	w := Window{
		Stable: UpstreamStats{Requests: 100, Errors: 0, P95: 10 * time.Millisecond},
		Canary: UpstreamStats{Requests: 100, Errors: 0, P95: 500 * time.Millisecond},
	}
	d := g.Evaluate(w)
	if d.Action != Rollback {
		t.Fatalf("action = %s, want rollback (reason %q)", d.Action, d.Reason)
	}
}

func TestGateHoldsUnderMinRequests(t *testing.T) {
	g := Gate{MaxErrorRate: 0.05, MinRequests: 50}
	w := Window{
		Stable: UpstreamStats{Requests: 100, Errors: 0},
		Canary: UpstreamStats{Requests: 10, Errors: 0},
	}
	d := g.Evaluate(w)
	if d.Action != Hold {
		t.Fatalf("action = %s, want hold (reason %q)", d.Action, d.Reason)
	}
}

// driveTraffic sends requests through the proxy until stop is closed. Six of these run together.
func driveTraffic(p *Proxy, stop <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-stop:
			return
		default:
		}
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	}
}

func TestControllerPromotesHealthyCanary(t *testing.T) {
	stable := okServer(t)
	canary := okServer(t)

	p := NewProxy(mustURL(t, stable), mustURL(t, canary), 7)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go driveTraffic(p, stop, &wg)
	}

	gate := Gate{MaxErrorRate: 0.05, MinRequests: 20}
	var log bytes.Buffer
	err := Run(context.Background(), p, []int{10, 25, 50, 100}, 150*time.Millisecond, gate, &log)
	close(stop)
	wg.Wait()

	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if p.Weight() != 100 {
		t.Fatalf("final weight = %d, want 100", p.Weight())
	}
	if !strings.Contains(log.String(), `"action":"promote"`) {
		t.Fatalf("log should contain a promote line, got:\n%s", log.String())
	}
}

func TestControllerRollsBackFaultyCanary(t *testing.T) {
	stable := okServer(t)

	var rngMu sync.Mutex
	rng := rand.New(rand.NewSource(99)) //nolint:gosec // deterministic fault injection for test
	canary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rngMu.Lock()
		n := rng.Intn(100)
		rngMu.Unlock()
		if n < 30 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(canary.Close)

	p := NewProxy(mustURL(t, stable), mustURL(t, canary), 3)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go driveTraffic(p, stop, &wg)
	}

	gate := Gate{MaxErrorRate: 0.05, MinRequests: 20}
	var log bytes.Buffer
	err := Run(context.Background(), p, []int{10, 25, 50, 100}, 150*time.Millisecond, gate, &log)
	close(stop)
	wg.Wait()

	if !errors.Is(err, ErrRollback) {
		t.Fatalf("err = %v, want ErrRollback", err)
	}
	if p.Weight() != 0 {
		t.Fatalf("final weight = %d, want 0", p.Weight())
	}
	if !strings.Contains(log.String(), `"action":"rollback"`) {
		t.Fatalf("log should contain a rollback line, got:\n%s", log.String())
	}
}
