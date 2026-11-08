// Package server holds the classify logic and the HTTP and gRPC front ends.
package server

import (
	"context"
	"errors"
	"sort"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/consultantstanleymn/inference-gateway/internal/batcher"
	"github.com/consultantstanleymn/inference-gateway/internal/tokenize"
)

// ErrEmptyText is returned for a request with no text.
var ErrEmptyText = errors.New("text is required")

// Score is one intent with its probability.
type Score struct {
	Intent      string  `json:"intent"`
	Probability float32 `json:"probability"`
}

// Prediction is the answer for one text.
type Prediction struct {
	Intent       string  `json:"intent"`
	Probability  float32 `json:"probability"`
	ModelVersion string  `json:"model_version"`
	Top          []Score `json:"top"`
}

// Service turns text into a prediction through the batcher.
type Service struct {
	Batcher *batcher.Batcher
	Labels  []string
	Version string
	Timeout time.Duration
}

var tracer = otel.Tracer("inference-gateway")

// Classify tokenizes, queues, waits and ranks. The context deadline is capped by Timeout.
func (s *Service) Classify(ctx context.Context, text string) (Prediction, error) {
	if text == "" {
		return Prediction{}, ErrEmptyText
	}
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	res, err := s.Batcher.Submit(ctx, tokenize.Features(text))
	if err != nil {
		return Prediction{}, err
	}
	s.recordSpans(ctx, res)
	return s.rank(res.Probs), nil
}

// recordSpans adds queue.wait and batch.run child spans with the real timestamps.
func (s *Service) recordSpans(ctx context.Context, res batcher.Result) {
	_, q := tracer.Start(ctx, "queue.wait", trace.WithTimestamp(res.EnqueuedAt))
	q.End(trace.WithTimestamp(res.RunStart))
	_, r := tracer.Start(ctx, "batch.run", trace.WithTimestamp(res.RunStart),
		trace.WithAttributes(attribute.Int("batch.size", res.BatchSize), attribute.String("model.version", s.Version)))
	r.End(trace.WithTimestamp(res.RunEnd))
}

func (s *Service) rank(p []float32) Prediction {
	idx := make([]int, len(p))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return p[idx[a]] > p[idx[b]] })
	n := min(3, len(idx))
	top := make([]Score, n)
	for i := range n {
		top[i] = Score{Intent: s.Labels[idx[i]], Probability: p[idx[i]]}
	}
	return Prediction{Intent: top[0].Intent, Probability: top[0].Probability, ModelVersion: s.Version, Top: top}
}
