// Command gateway serves the intent classifier over HTTP and gRPC.
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/consultantstanleymn/inference-gateway/internal/batcher"
	"github.com/consultantstanleymn/inference-gateway/internal/config"
	"github.com/consultantstanleymn/inference-gateway/internal/model"
	"github.com/consultantstanleymn/inference-gateway/internal/server"
	"github.com/consultantstanleymn/inference-gateway/internal/telemetry"
)

func main() {
	if err := run(); err != nil {
		slog.Error("gateway failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	flush, err := telemetry.SetupTracing(ctx, "inference-gateway")
	if err != nil {
		return err
	}
	m, err := model.Load(cfg.ModelDir, cfg.ORTLib, cfg.ORTThreads)
	if err != nil {
		return err
	}
	defer func() { _ = m.Close() }()
	slog.Info("model loaded", "version", m.Meta.Version, "dir", cfg.ModelDir)

	metrics := telemetry.NewMetrics(cfg.MaxBatch)
	metrics.ModelInfo.WithLabelValues(m.Meta.Version).Set(1)
	b := batcher.New(m, batcher.Config{MaxBatch: cfg.MaxBatch, MaxWait: cfg.MaxWait, QueueSize: cfg.QueueSize, Workers: cfg.Workers},
		batcher.Observer{OnBatch: metrics.ObserveBatch, OnShed: metrics.Shed.Inc, OnDrop: metrics.Dropped.Inc})
	svc := &server.Service{Batcher: b, Labels: m.Meta.Labels, Version: m.Meta.Version, Timeout: cfg.RequestTimeout}
	srv := server.New(svc, metrics)

	hl, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return err
	}
	gl, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}
	errc := make(chan error, 3)
	go func() { errc <- srv.ServeHTTP(hl) }()
	go func() { errc <- srv.ServeGRPC(gl) }()
	admin, err := startAdmin(cfg.AdminAddr, errc)
	if err != nil {
		return err
	}
	slog.Info("listening", "http", hl.Addr().String(), "grpc", gl.Addr().String())

	// Warm-up: run a few batches so the first real request does not pay start-up cost.
	for _, n := range []int{1, cfg.MaxBatch} {
		for range 3 {
			ch := make(chan struct{}, n)
			for range n {
				go func() {
					_, _ = svc.Classify(ctx, "warm up request about my card")
					ch <- struct{}{}
				}()
			}
			for range n {
				<-ch
			}
		}
	}
	time.Sleep(cfg.WarmupHold)
	srv.SetReady(true)
	slog.Info("ready")

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutdown started")
	srv.SetReady(false)
	time.Sleep(cfg.ShutdownDelay) // lets the load balancer see not-ready first
	sctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		slog.Error("shutdown", "err", err)
	}
	if admin != nil {
		_ = admin.Shutdown(sctx)
	}
	b.Close()
	_ = flush(sctx)
	slog.Info("shutdown done")
	return nil
}
