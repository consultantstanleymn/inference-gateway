// test helper, not part of the service
//
// Command stubfail is a test helper backend. It returns 500 for a set share of
// requests and passes the rest to a real gateway with a reverse proxy.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("stubfail", flag.ContinueOnError)
	fs.SetOutput(stderr)

	listen := fs.String("listen", "127.0.0.1:25483", "address to listen on")
	upstream := fs.String("upstream", "", "upstream gateway URL (required)")
	failRate := fs.Float64("fail-rate", 0.2, "share of requests to fail with 500 (0..1)")
	seed := fs.Int64("seed", 1, "seed for the failure picker")

	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *upstream == "" {
		_, _ = fmt.Fprintln(stderr, "stubfail: -upstream is required")
		return 1
	}
	if *failRate < 0 || *failRate > 1 {
		_, _ = fmt.Fprintf(stderr, "stubfail: -fail-rate %v out of range 0..1\n", *failRate)
		return 1
	}

	target, err := url.Parse(*upstream)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "stubfail: bad -upstream URL: %v\n", err)
		return 1
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	var mu sync.Mutex
	rng := rand.New(rand.NewSource(*seed)) //nolint:gosec // seeded fault injection for a test helper, not security

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only classify calls fail. Health and readiness checks pass through.
		fail := false
		if r.URL.Path == "/v1/classify" {
			mu.Lock()
			fail = rng.Float64() < *failRate
			mu.Unlock()
		}
		if fail {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			if _, err := io.WriteString(w, `{"error":"injected failure"}`); err != nil {
				return
			}
			return
		}
		proxy.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:              *listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		_, _ = fmt.Fprintf(stderr, "stubfail: serve: %v\n", err)
		return 1
	}
	return 0
}
