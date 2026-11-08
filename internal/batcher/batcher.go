// Package batcher groups single requests into micro-batches for one model call.
package batcher

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	// ErrQueueFull means the bounded queue has no room. Callers must shed the request.
	ErrQueueFull = errors.New("queue full")
	// ErrClosed means the batcher no longer accepts work.
	ErrClosed = errors.New("batcher closed")
)

// Runner runs one model call for a whole batch.
type Runner interface {
	Run(rows [][]int64) ([][]float32, error)
}

// Config sets batching and queue limits.
type Config struct {
	MaxBatch  int           // largest batch size
	MaxWait   time.Duration // longest a request waits for company
	QueueSize int           // bounded queue length
	Workers   int           // parallel batch loops
}

// Result is what one request gets back.
type Result struct {
	Probs      []float32
	BatchSize  int
	EnqueuedAt time.Time
	RunStart   time.Time
	RunEnd     time.Time
}

// Observer receives events for metrics. Every field is optional.
type Observer struct {
	OnBatch func(size int, queueWait []time.Duration, run time.Duration)
	OnShed  func()
	OnDrop  func()
}

type item struct {
	ctx      context.Context
	ids      []int64
	enqueued time.Time
	resp     chan outcome
}

type outcome struct {
	res Result
	err error
}

// Batcher is safe for concurrent use.
type Batcher struct {
	cfg    Config
	runner Runner
	obs    Observer
	queue  chan *item

	mu     sync.RWMutex
	closed bool
	wg     sync.WaitGroup
}

// New starts the worker loops.
func New(r Runner, cfg Config, obs Observer) *Batcher {
	if cfg.MaxBatch < 1 {
		cfg.MaxBatch = 1
	}
	if cfg.QueueSize < 1 {
		cfg.QueueSize = 1
	}
	if cfg.Workers < 1 {
		cfg.Workers = 1
	}
	b := &Batcher{cfg: cfg, runner: r, obs: obs, queue: make(chan *item, cfg.QueueSize)}
	for range cfg.Workers {
		b.wg.Add(1)
		go b.loop()
	}
	return b
}

// QueueDepth reports how many requests wait now.
func (b *Batcher) QueueDepth() int { return len(b.queue) }

// Submit queues one request and waits for its result.
// It returns ErrQueueFull at once when the queue is full.
func (b *Batcher) Submit(ctx context.Context, ids []int64) (Result, error) {
	it := &item{ctx: ctx, ids: ids, enqueued: time.Now(), resp: make(chan outcome, 1)}
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return Result{}, ErrClosed
	}
	select {
	case b.queue <- it:
		b.mu.RUnlock()
	default:
		b.mu.RUnlock()
		if b.obs.OnShed != nil {
			b.obs.OnShed()
		}
		return Result{}, ErrQueueFull
	}
	select {
	case o := <-it.resp:
		return o.res, o.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// Close stops intake, finishes every queued request, and waits for the workers.
func (b *Batcher) Close() {
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.queue)
	}
	b.mu.Unlock()
	b.wg.Wait()
}

func (b *Batcher) loop() {
	defer b.wg.Done()
	for {
		first, ok := <-b.queue
		if !ok {
			return
		}
		batch := b.collect(first)
		b.process(batch)
	}
}

// collect builds a batch: it stops at MaxBatch items or MaxWait after the first item.
func (b *Batcher) collect(first *item) []*item {
	batch := []*item{first}
	timer := time.NewTimer(b.cfg.MaxWait)
	defer timer.Stop()
	for len(batch) < b.cfg.MaxBatch {
		select {
		case it, ok := <-b.queue:
			if !ok {
				return batch
			}
			batch = append(batch, it)
		case <-timer.C:
			return batch
		}
	}
	return batch
}

func (b *Batcher) process(batch []*item) {
	live := batch[:0:0]
	for _, it := range batch {
		if it.ctx.Err() != nil { // cancelled while waiting: do not run it
			if b.obs.OnDrop != nil {
				b.obs.OnDrop()
			}
			it.resp <- outcome{err: it.ctx.Err()}
			continue
		}
		live = append(live, it)
	}
	if len(live) == 0 {
		return
	}
	rows := make([][]int64, len(live))
	for i, it := range live {
		rows[i] = it.ids
	}
	start := time.Now()
	probs, err := b.runner.Run(rows)
	end := time.Now()
	waits := make([]time.Duration, len(live))
	for i, it := range live {
		waits[i] = start.Sub(it.enqueued)
		if err != nil {
			it.resp <- outcome{err: err}
			continue
		}
		it.resp <- outcome{res: Result{Probs: probs[i], BatchSize: len(live), EnqueuedAt: it.enqueued, RunStart: start, RunEnd: end}}
	}
	if b.obs.OnBatch != nil {
		b.obs.OnBatch(len(live), waits, end.Sub(start))
	}
}
