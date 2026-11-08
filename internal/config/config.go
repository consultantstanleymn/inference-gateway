// Package config reads settings from flags with environment variable defaults.
package config

import (
	"flag"
	"os"
	"strconv"
	"time"
)

// Config holds every setting. Each field has a flag and a GATEWAY_* variable.
type Config struct {
	ModelDir        string
	ORTLib          string
	HTTPAddr        string
	GRPCAddr        string
	MaxBatch        int
	MaxWait         time.Duration
	QueueSize       int
	Workers         int
	ORTThreads      int
	RequestTimeout  time.Duration
	ShutdownDelay   time.Duration
	ShutdownTimeout time.Duration
	WarmupHold      time.Duration
}

func env(name, def string) string {
	if v := os.Getenv("GATEWAY_" + name); v != "" {
		return v
	}
	return def
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(env(name, "")); err == nil {
		return v
	}
	return def
}

func envDur(name string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(env(name, "")); err == nil {
		return v
	}
	return def
}

// Parse reads args (without the program name).
func Parse(args []string) (Config, error) {
	var c Config
	fs := flag.NewFlagSet("gateway", flag.ContinueOnError)
	fs.StringVar(&c.ModelDir, "model-dir", env("MODEL_DIR", "models/v1"), "directory with model.onnx and meta.json")
	fs.StringVar(&c.ORTLib, "ort-lib", env("ORT_LIB", "third_party/libonnxruntime.so"), "path to libonnxruntime.so")
	fs.StringVar(&c.HTTPAddr, "http-addr", env("HTTP_ADDR", ":8080"), "HTTP listen address")
	fs.StringVar(&c.GRPCAddr, "grpc-addr", env("GRPC_ADDR", ":9090"), "gRPC listen address")
	fs.IntVar(&c.MaxBatch, "max-batch", envInt("MAX_BATCH", 16), "largest batch")
	fs.DurationVar(&c.MaxWait, "max-wait", envDur("MAX_WAIT", 5*time.Millisecond), "longest wait to fill a batch")
	fs.IntVar(&c.QueueSize, "queue-size", envInt("QUEUE_SIZE", 256), "bounded queue length")
	fs.IntVar(&c.Workers, "workers", envInt("WORKERS", 1), "parallel batch loops")
	fs.IntVar(&c.ORTThreads, "ort-threads", envInt("ORT_THREADS", 1), "ONNX Runtime threads per call")
	fs.DurationVar(&c.RequestTimeout, "request-timeout", envDur("REQUEST_TIMEOUT", 2*time.Second), "deadline for one request")
	fs.DurationVar(&c.ShutdownDelay, "shutdown-delay", envDur("SHUTDOWN_DELAY", 0), "wait after going not-ready, before closing listeners")
	fs.DurationVar(&c.ShutdownTimeout, "shutdown-timeout", envDur("SHUTDOWN_TIMEOUT", 20*time.Second), "longest drain time")
	fs.DurationVar(&c.WarmupHold, "warmup-hold", envDur("WARMUP_HOLD", 0), "extra time to stay not-ready after warm-up (used by tests)")
	return c, fs.Parse(args)
}
