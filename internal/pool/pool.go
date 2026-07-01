// Package pool provides a small bounded worker pool. It intentionally has
// no notion of "job" or "result" types of its own — the actual concurrency
// throttling and politeness (rate limiting, backoff) live in the fetch
// package's Limiter, which each worker's do function calls into. This pool
// just bounds how many goroutines are running at once.
package pool

import (
	"context"
	"sync"
)

// Run executes jobs across up to `workers` concurrent goroutines, calling
// do for each job. Results are collected as they complete (not necessarily
// in input order). If onResult is non-nil, it is invoked once per result as
// it arrives — useful for live progress output or incremental cache saves.
// Cancelling ctx stops feeding new jobs to idle workers; already-dispatched
// jobs are expected to observe ctx themselves (as fetch.GetPage does).
func Run[J any, R any](ctx context.Context, jobs []J, workers int, do func(context.Context, J) R, onResult func(R)) []R {
	if workers < 1 {
		workers = 1
	}

	jobCh := make(chan J)
	resCh := make(chan R, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				resCh <- do(ctx, j)
			}
		}()
	}

	go func() {
		defer close(jobCh)
		for _, j := range jobs {
			select {
			case jobCh <- j:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(resCh)
	}()

	results := make([]R, 0, len(jobs))
	for r := range resCh {
		results = append(results, r)
		if onResult != nil {
			onResult(r)
		}
	}
	return results
}
