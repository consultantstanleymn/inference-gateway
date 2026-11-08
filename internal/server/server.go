package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/consultantstanleymn/inference-gateway/gen/inferencev1"
	"github.com/consultantstanleymn/inference-gateway/internal/batcher"
	"github.com/consultantstanleymn/inference-gateway/internal/telemetry"
)

// Server serves HTTP and gRPC on top of one Service.
type Server struct {
	svc     *Service
	metrics *telemetry.Metrics
	ready   atomic.Bool
	http    *http.Server
	grpc    *grpc.Server
}

type grpcHandler struct {
	inferencev1.UnimplementedGatewayServer
	s *Server
}

// New wires both front ends. Call SetReady(true) after warm-up.
func New(svc *Service, m *telemetry.Metrics) *Server {
	s := &Server{svc: svc, metrics: m}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/classify", s.handleClassify)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if s.ready.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{}))
	// While not ready (warm-up or draining) ask clients to drop the connection after each answer.
	// Without this a client can reuse an idle keep-alive connection at the moment the server
	// closes it, and see an EOF. That showed up as 4 failed requests in one rolling upgrade run.
	drain := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.ready.Load() {
			w.Header().Set("Connection", "close")
		}
		mux.ServeHTTP(w, r)
	})
	s.http = &http.Server{
		Handler:           otelhttp.NewHandler(drain, "request", otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path == "/v1/classify" })),
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.grpc = grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
	inferencev1.RegisterGatewayServer(s.grpc, &grpcHandler{s: s})
	return s
}

// SetReady flips the readiness answer.
func (s *Server) SetReady(v bool) { s.ready.Store(v) }

// ServeHTTP blocks until the listener closes.
func (s *Server) ServeHTTP(l net.Listener) error {
	if err := s.http.Serve(l); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// ServeGRPC blocks until the server stops.
func (s *Server) ServeGRPC(l net.Listener) error { return s.grpc.Serve(l) }

// Shutdown stops intake and waits for in-flight requests, up to the context deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	s.SetReady(false)
	done := make(chan struct{})
	go func() { s.grpc.GracefulStop(); close(done) }()
	err := s.http.Shutdown(ctx)
	select {
	case <-done:
	case <-ctx.Done():
		s.grpc.Stop()
	}
	return err
}

func outcome(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, batcher.ErrQueueFull):
		return "shed"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, ErrEmptyText):
		return "invalid"
	case errors.Is(err, batcher.ErrClosed):
		return "closed"
	}
	return "error"
}

func httpStatus(err error) int {
	switch outcome(err) {
	case "ok":
		return http.StatusOK
	case "shed":
		return http.StatusTooManyRequests
	case "timeout":
		return http.StatusGatewayTimeout
	case "invalid":
		return http.StatusBadRequest
	case "closed":
		return http.StatusServiceUnavailable
	case "cancelled":
		return 499
	}
	return http.StatusInternalServerError
}

func grpcCode(err error) codes.Code {
	switch outcome(err) {
	case "ok":
		return codes.OK
	case "shed":
		return codes.ResourceExhausted
	case "timeout":
		return codes.DeadlineExceeded
	case "invalid":
		return codes.InvalidArgument
	case "closed":
		return codes.Unavailable
	case "cancelled":
		return codes.Canceled
	}
	return codes.Internal
}

func (s *Server) record(transport string, start time.Time, err error) {
	s.metrics.Requests.WithLabelValues(transport, outcome(err)).Inc()
	s.metrics.Latency.WithLabelValues(transport).Observe(time.Since(start).Seconds())
}

func (s *Server) handleClassify(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req struct {
		Text string `json:"text"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.record("http", start, ErrEmptyText)
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	pred, err := s.svc.Classify(r.Context(), req.Text)
	s.record("http", start, err)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if outcome(err) == "shed" {
			w.Header().Set("Retry-After", "1")
		}
		w.WriteHeader(httpStatus(err))
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pred)
}

func (h *grpcHandler) Classify(ctx context.Context, in *inferencev1.ClassifyRequest) (*inferencev1.ClassifyResponse, error) {
	start := time.Now()
	pred, err := h.s.svc.Classify(ctx, in.GetText())
	h.s.record("grpc", start, err)
	if err != nil {
		return nil, status.Error(grpcCode(err), err.Error())
	}
	out := &inferencev1.ClassifyResponse{Intent: pred.Intent, Probability: pred.Probability, ModelVersion: pred.ModelVersion}
	for _, t := range pred.Top {
		out.Top = append(out.Top, &inferencev1.Score{Intent: t.Intent, Probability: t.Probability})
	}
	return out, nil
}
