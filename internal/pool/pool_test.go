package pool

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRun_AllJobsProcessed(t *testing.T) {
	jobs := make([]int, 100)
	for i := range jobs {
		jobs[i] = i
	}

	var processed int64
	results := Run(context.Background(), jobs, 8, func(ctx context.Context, j int) int {
		atomic.AddInt64(&processed, 1)
		return j * 2
	}, nil)

	if processed != 100 {
		t.Fatalf("expected 100 jobs processed, got %d", processed)
	}
	if len(results) != 100 {
		t.Fatalf("expected 100 results, got %d", len(results))
	}
	sum := 0
	for _, r := range results {
		sum += r
	}
	if sum != 100*99 { // sum(2*i for i in 0..99) = 2 * (99*100/2) = 9900
		t.Fatalf("unexpected sum: %d", sum)
	}
}

func TestRun_OnResultCallback(t *testing.T) {
	jobs := []int{1, 2, 3}
	var seen int64
	Run(context.Background(), jobs, 2, func(ctx context.Context, j int) int { return j }, func(r int) {
		atomic.AddInt64(&seen, 1)
	})
	if seen != 3 {
		t.Fatalf("expected onResult called 3 times, got %d", seen)
	}
}

func TestRun_ContextCancellationStopsFeeding(t *testing.T) {
	jobs := make([]int, 1000)
	ctx, cancel := context.WithCancel(context.Background())

	var processed int64
	results := Run(ctx, jobs, 4, func(ctx context.Context, j int) int {
		n := atomic.AddInt64(&processed, 1)
		if n == 5 {
			cancel()
		}
		time.Sleep(time.Millisecond)
		return j
	}, nil)

	if len(results) >= 1000 {
		t.Fatalf("expected cancellation to stop feeding well before all 1000 jobs, got %d results", len(results))
	}
}

func TestRun_ZeroWorkersDefaultsToOne(t *testing.T) {
	results := Run(context.Background(), []int{1, 2}, 0, func(ctx context.Context, j int) int { return j }, nil)
	if len(results) != 2 {
		t.Fatalf("expected 2 results even with workers=0, got %d", len(results))
	}
}
