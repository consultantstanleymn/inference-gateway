// Package telemetry holds Prometheus metrics and OpenTelemetry tracing setup.
package telemetry

import (
	"context"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Metrics is the set of Prometheus collectors for the gateway.
type Metrics struct {
	Registry   *prometheus.Registry
	Requests   *prometheus.CounterVec   // transport, outcome
	Latency    *prometheus.HistogramVec // transport, model_version
	QueueWait  prometheus.Histogram
	BatchSize  prometheus.Histogram
	BatchRun   prometheus.Histogram
	Shed       prometheus.Counter
	Dropped    prometheus.Counter
	QueueDepth prometheus.Gauge
	ModelInfo  *prometheus.GaugeVec
}

// NewMetrics builds a private registry so tests do not clash.
func NewMetrics(maxBatch int) *Metrics {
	m := &Metrics{Registry: prometheus.NewRegistry()}
	lat := []float64{.001, .002, .005, .01, .025, .05, .1, .25, .5, 1, 2.5}
	m.Requests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gateway_requests_total", Help: "Requests by transport and outcome."}, []string{"transport", "outcome"})
	m.Latency = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "gateway_request_duration_seconds", Help: "End to end request time.", Buckets: lat}, []string{"transport", "model_version"})
	m.QueueWait = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "gateway_queue_wait_seconds", Help: "Time a request waited before its batch ran.", Buckets: lat})
	sizes := []float64{1, 2, 4, 8, 16, 32, 64}
	m.BatchSize = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "gateway_batch_size", Help: "Requests per model call.", Buckets: sizes})
	m.BatchRun = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "gateway_batch_run_seconds", Help: "Model call time.", Buckets: lat})
	m.Shed = prometheus.NewCounter(prometheus.CounterOpts{Name: "gateway_shed_total", Help: "Requests refused because the queue was full."})
	m.Dropped = prometheus.NewCounter(prometheus.CounterOpts{Name: "gateway_cancelled_total", Help: "Requests dropped before inference because the caller left."})
	m.QueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{Name: "gateway_queue_depth", Help: "Requests waiting now."})
	m.ModelInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gateway_model_info", Help: "Loaded model version."}, []string{"version"})
	// Runtime metrics: process_cpu_seconds_total feeds the saturation panel.
	m.Registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m.Registry.MustRegister(m.Requests, m.Latency, m.QueueWait, m.BatchSize, m.BatchRun, m.Shed, m.Dropped, m.QueueDepth, m.ModelInfo)
	return m
}

// ObserveBatch is the batcher hook.
func (m *Metrics) ObserveBatch(size int, waits []time.Duration, run time.Duration) {
	m.BatchSize.Observe(float64(size))
	m.BatchRun.Observe(run.Seconds())
	for _, w := range waits {
		m.QueueWait.Observe(w.Seconds())
	}
}

// SetupTracing installs a tracer provider. It exports with OTLP/gRPC only when
// OTEL_EXPORTER_OTLP_ENDPOINT (or the traces variant) is set. The returned func flushes spans.
func SetupTracing(ctx context.Context, service string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}
	exp, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, err
	}
	res := resource.NewSchemaless(semconv.ServiceName(service))
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(time.Second)), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}
