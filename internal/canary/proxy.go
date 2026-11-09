package canary

import (
	"context"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	upstreamStable = "stable"
	upstreamCanary = "canary"
)

// UpstreamStats holds the counters and latency for one upstream over a window.
type UpstreamStats struct {
	Requests int
	Errors   int
	P95      time.Duration
}

// Window holds the stats for both upstreams since the last TakeWindow call.
type Window struct {
	Stable UpstreamStats
	Canary UpstreamStats
}

type upstreamCollector struct {
	requests  int
	errors    int
	durations []time.Duration
}

func (c *upstreamCollector) record(d time.Duration, isErr bool) {
	c.requests++
	if isErr {
		c.errors++
	}
	c.durations = append(c.durations, d)
}

func (c *upstreamCollector) snapshot() UpstreamStats {
	s := UpstreamStats{Requests: c.requests, Errors: c.errors}
	if len(c.durations) > 0 {
		sorted := make([]time.Duration, len(c.durations))
		copy(sorted, c.durations)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		idx := (95*len(sorted) + 99) / 100
		if idx < 1 {
			idx = 1
		}
		if idx > len(sorted) {
			idx = len(sorted)
		}
		s.P95 = sorted[idx-1]
	}
	c.requests = 0
	c.errors = 0
	c.durations = c.durations[:0]
	return s
}

// Proxy sends traffic to a stable and a canary upstream by weight.
type Proxy struct {
	stable *httputil.ReverseProxy
	canary *httputil.ReverseProxy

	weight atomic.Int32

	mu  sync.Mutex
	rng *rand.Rand

	statsMu sync.Mutex
	stableC upstreamCollector
	canaryC upstreamCollector

	registry *prometheus.Registry

	reqStable, reqCanary prometheus.Counter
	errStable, errCanary prometheus.Counter
	durStable, durCanary prometheus.Observer
}

// NewProxy builds a Proxy for the given upstream URLs with a seeded RNG.
func NewProxy(stable, canary *url.URL, seed int64) *Proxy {
	p := &Proxy{
		stable: httputil.NewSingleHostReverseProxy(stable),
		canary: httputil.NewSingleHostReverseProxy(canary),
		rng:    rand.New(rand.NewSource(seed)), //nolint:gosec // deterministic split for canary routing, not security
	}

	p.stable.ErrorHandler = markTransportError
	p.canary.ErrorHandler = markTransportError

	reqVec := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "canary_proxy_requests_total",
		Help: "Total requests forwarded per upstream.",
	}, []string{"upstream"})
	errVec := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "canary_proxy_errors_total",
		Help: "Total error responses per upstream.",
	}, []string{"upstream"})
	durVec := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "canary_proxy_request_duration_seconds",
		Help:    "Request duration per upstream.",
		Buckets: prometheus.DefBuckets,
	}, []string{"upstream"})

	p.registry = prometheus.NewRegistry()
	p.registry.MustRegister(reqVec, errVec, durVec)

	p.reqStable = reqVec.WithLabelValues(upstreamStable)
	p.reqCanary = reqVec.WithLabelValues(upstreamCanary)
	p.errStable = errVec.WithLabelValues(upstreamStable)
	p.errCanary = errVec.WithLabelValues(upstreamCanary)
	p.durStable = durVec.WithLabelValues(upstreamStable)
	p.durCanary = durVec.WithLabelValues(upstreamCanary)

	return p
}

// SetWeight sets the canary weight in percent, clamped to 0..100.
func (p *Proxy) SetWeight(n int) {
	if n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}
	p.weight.Store(int32(n))
}

// Weight returns the current canary weight in percent.
func (p *Proxy) Weight() int {
	return int(p.weight.Load())
}

// Registry returns the proxy's own Prometheus registry.
func (p *Proxy) Registry() *prometheus.Registry {
	return p.registry
}

// TakeWindow returns the stats since the last call and resets the window.
func (p *Proxy) TakeWindow() Window {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	return Window{
		Stable: p.stableC.snapshot(),
		Canary: p.canaryC.snapshot(),
	}
}

type transportKey struct{}

// transportState tells ServeHTTP that the upstream call failed before it got a status.
type transportState struct{ failed atomic.Bool }

// markTransportError is the shared error handler of both reverse proxies.
func markTransportError(rw http.ResponseWriter, r *http.Request, _ error) {
	if ts, ok := r.Context().Value(transportKey{}).(*transportState); ok {
		ts.failed.Store(true)
	}
	rw.WriteHeader(http.StatusBadGateway)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (p *Proxy) pickCanary() bool {
	w := p.Weight()
	if w <= 0 {
		return false
	}
	if w >= 100 {
		return true
	}
	p.mu.Lock()
	n := p.rng.Intn(100)
	p.mu.Unlock()
	return n < w
}

// ServeHTTP forwards the request to the chosen upstream and records stats.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	toCanary := p.pickCanary()

	var target *httputil.ReverseProxy
	var reqC, errC prometheus.Counter
	var durO prometheus.Observer
	var coll *upstreamCollector
	if toCanary {
		target = p.canary
		reqC, errC, durO = p.reqCanary, p.errCanary, p.durCanary
		coll = &p.canaryC
	} else {
		target = p.stable
		reqC, errC, durO = p.reqStable, p.errStable, p.durStable
		coll = &p.stableC
	}

	rec := &statusRecorder{ResponseWriter: w}
	start := time.Now()

	// The error handler is set once at construction. It marks this request through the context,
	// so many requests can share one reverse proxy without a race.
	ts := &transportState{}
	r = r.WithContext(context.WithValue(r.Context(), transportKey{}, ts))

	target.ServeHTTP(rec, r)
	transportErr := ts.failed.Load()

	elapsed := time.Since(start)
	status := rec.status
	if status == 0 {
		status = http.StatusOK
	}
	isErr := transportErr || status >= 500

	reqC.Inc()
	if isErr {
		errC.Inc()
	}
	durO.Observe(elapsed.Seconds())

	p.statsMu.Lock()
	coll.record(elapsed, isErr)
	p.statsMu.Unlock()
}
