// Command canary runs a weighted canary proxy in front of a stable and a
// canary backend. It steps the canary weight up, checks the gate at each
// step, and rolls back to weight 0 when the gate fails.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/consultantstanleymn/inference-gateway/internal/canary"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("canary", flag.ContinueOnError)
	fs.SetOutput(stderr)

	listen := fs.String("listen", "127.0.0.1:25481", "address for the canary proxy")
	stable := fs.String("stable", "", "stable backend URL (required)")
	canaryURL := fs.String("canary", "", "canary backend URL (required)")
	steps := fs.String("steps", "10,25,50,100", "comma separated canary weight steps in percent")
	stepDuration := fs.Duration("step-duration", 30*time.Second, "time to hold each weight step")
	maxErrorRate := fs.Float64("max-error-rate", 0.02, "gate: maximum canary error rate")
	maxP95 := fs.Duration("max-p95", 50*time.Millisecond, "gate: maximum canary p95 latency")
	minRequests := fs.Int("min-requests", 100, "gate: minimum canary requests per step")
	margin := fs.Float64("margin", 0.01, "gate: allowed margin over the stable error rate")
	seed := fs.Int64("seed", 1, "seed for the weighted picker")
	metricsAddr := fs.String("metrics-addr", "", "address to serve the proxy registry at /metrics (empty = off)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *stable == "" || *canaryURL == "" {
		_, _ = fmt.Fprintln(stderr, "canary: -stable and -canary are required")
		return 1
	}

	stableURL, err := url.Parse(*stable)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "canary: bad -stable URL: %v\n", err)
		return 1
	}
	canaryParsed, err := url.Parse(*canaryURL)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "canary: bad -canary URL: %v\n", err)
		return 1
	}

	stepList, err := parseSteps(*steps)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "canary: %v\n", err)
		return 1
	}

	proxy := canary.NewProxy(stableURL, canaryParsed, *seed)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              *listen,
		Handler:           proxy,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	var metricsSrv *http.Server
	if *metricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(proxy.Registry(), promhttp.HandlerOpts{}))
		metricsSrv = &http.Server{
			Addr:              *metricsAddr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				_, _ = fmt.Fprintf(stderr, "canary: metrics server: %v\n", err)
			}
		}()
	}

	gate := canary.Gate{
		MaxErrorRate: *maxErrorRate,
		MaxP95:       *maxP95,
		MinRequests:  *minRequests,
		Margin:       *margin,
	}

	runErr := canary.Run(ctx, proxy, stepList, *stepDuration, gate, stdout)

	exitCode := 0
	switch {
	case runErr == nil:
		writeJSON(stdout, map[string]any{"result": "promoted", "weight": proxy.Weight()})
	case errors.Is(runErr, canary.ErrRollback):
		writeJSON(stdout, map[string]any{"result": "rolled_back", "reason": runErr.Error()})
		exitCode = 3
	default:
		_, _ = fmt.Fprintf(stderr, "canary: run: %v\n", runErr)
		exitCode = 1
	}

	// Keep serving so the load test can finish. Wait for a signal.
	if exitCode != 1 {
		select {
		case <-ctx.Done():
		case err := <-serveErr:
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "canary: serve: %v\n", err)
				exitCode = 1
			}
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_, _ = fmt.Fprintf(stderr, "canary: shutdown: %v\n", err)
	}
	if metricsSrv != nil {
		if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
			_, _ = fmt.Fprintf(stderr, "canary: metrics shutdown: %v\n", err)
		}
	}

	return exitCode
}

func parseSteps(s string) ([]int, error) {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("bad step %q: %w", p, err)
		}
		if n < 0 || n > 100 {
			return nil, fmt.Errorf("step %d out of range 0..100", n)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, errors.New("no steps given")
	}
	return out, nil
}

func writeJSON(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		_, _ = fmt.Fprintf(w, "{\"result\":\"error\",\"reason\":%q}\n", err.Error())
		return
	}
	_, _ = fmt.Fprintf(w, "%s\n", b)
}
