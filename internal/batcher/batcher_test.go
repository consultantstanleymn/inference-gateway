package batcher

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRunner struct {
	mu     sync.Mutex
	sizes  []int
	rows   int
	delay  time.Duration
	block  chan struct{}
	called atomic.Int32
}

func (f *fakeRunner) Run(rows [][]int64) ([][]float32, error) {
	f.called.Add(1)
	if f.block != nil {
		<-f.block
	}
	time.Sleep(f.delay)
	f.mu.Lock()
	f.sizes = append(f.sizes, len(rows))
	f.rows += len(rows)
	f.mu.Unlock()
	out := make([][]float32, len(rows))
	for i, r := range rows {
		out[i] = []float32{float32(len(r))}
	}
	return out, nil
}

func TestBatchNeverExceedsMax(t *testing.T) {
	f := &fakeRunner{delay: time.Millisecond}
	b := New(f, Config{MaxBatch: 4, MaxWait: 20 * time.Millisecond, QueueSize: 200}, Observer{})
	defer b.Close()
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := b.Submit(context.Background(), make([]int64, i%5))
			if err != nil || r.Probs[0] != float32(i%5) {
				t.Errorf("request %d: %v %v", i, r.Probs, err)
			}
		}()
	}
	wg.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows != 100 {
		t.Fatalf("ran %d rows, want 100", f.rows)
	}
	big := false
	for _, s := range f.sizes {
		if s > 4 {
			t.Fatalf("batch of %d exceeds max 4", s)
		}
		big = big || s > 1
	}
	if !big {
		t.Fatal("no batching happened under load")
	}
}

func TestLoneRequestWaitsAtMostMaxWait(t *testing.T) {
	f := &fakeRunner{}
	maxWait := 30 * time.Millisecond
	b := New(f, Config{MaxBatch: 16, MaxWait: maxWait, QueueSize: 8}, Observer{})
	defer b.Close()
	start := time.Now()
	r, err := b.Submit(context.Background(), []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	if r.BatchSize != 1 {
		t.Fatalf("batch size %d", r.BatchSize)
	}
	if took < maxWait-5*time.Millisecond || took > maxWait+100*time.Millisecond {
		t.Fatalf("lone request took %v, want about %v", took, maxWait)
	}
}

func TestFullQueueSheds(t *testing.T) {
	f := &fakeRunner{block: make(chan struct{})}
	shed := 0
	b := New(f, Config{MaxBatch: 1, MaxWait: time.Millisecond, QueueSize: 2}, Observer{OnShed: func() { shed++ }})
	// One request is stuck in the runner, so the queue (size 2) fills up.
	go func() { _, _ = b.Submit(context.Background(), []int64{1}) }()
	for f.called.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	for range 2 {
		go func() { _, _ = b.Submit(context.Background(), []int64{1}) }()
	}
	for b.QueueDepth() < 2 {
		time.Sleep(time.Millisecond)
	}
	if _, err := b.Submit(context.Background(), []int64{1}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("got %v, want ErrQueueFull", err)
	}
	if shed != 1 {
		t.Fatalf("shed count %d", shed)
	}
	close(f.block)
	b.Close()
}

func TestCancelledRequestsAreNotRun(t *testing.T) {
	f := &fakeRunner{block: make(chan struct{})}
	dropped := 0
	var mu sync.Mutex
	b := New(f, Config{MaxBatch: 1, MaxWait: time.Millisecond, QueueSize: 8}, Observer{OnDrop: func() { mu.Lock(); dropped++; mu.Unlock() }})
	go func() { _, _ = b.Submit(context.Background(), []int64{1}) }()
	for f.called.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := b.Submit(ctx, []int64{2}); done <- err }()
	for b.QueueDepth() < 1 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	close(f.block)
	b.Close()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows != 1 {
		t.Fatalf("model ran %d rows, want 1 (the cancelled one must be skipped)", f.rows)
	}
	mu.Lock()
	defer mu.Unlock()
	if dropped != 1 {
		t.Fatalf("dropped %d", dropped)
	}
}

func TestCloseDrainsQueueAndRejectsNew(t *testing.T) {
	f := &fakeRunner{delay: 5 * time.Millisecond}
	b := New(f, Config{MaxBatch: 2, MaxWait: time.Millisecond, QueueSize: 50}, Observer{})
	var wg sync.WaitGroup
	var ok atomic.Int32
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := b.Submit(context.Background(), []int64{1}); err == nil {
				ok.Add(1)
			}
		}()
	}
	time.Sleep(2 * time.Millisecond)
	b.Close()
	wg.Wait()
	if _, err := b.Submit(context.Background(), []int64{1}); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
	if ok.Load() == 0 {
		t.Fatal("no request finished")
	}
}
