package workspace

import (
	"context"
	"runtime"
	"sync"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// ParseSources parses independent source snapshots with a bounded worker pool.
// Results use the same indexes as sources, even when workers finish out of
// order. A canceled context prevents further work from being scheduled; a
// parse already inside syntax.Parse is allowed to finish because the parser
// does not accept a cancellation hook.
func ParseSources(ctx context.Context, sources []string, workers int) []*syntax.File {
	results := make([]*syntax.File, len(sources))
	if len(sources) == 0 || ctx.Err() != nil {
		return results
	}

	workerCount := parseWorkerCount(workers, len(sources))
	jobs := make(chan int, workerCount)
	var group sync.WaitGroup
	group.Add(workerCount)
	for range workerCount {
		go func() {
			defer group.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case index, ok := <-jobs:
					if !ok || ctx.Err() != nil {
						return
					}
					results[index] = syntax.Parse(sources[index])
				}
			}
		}()
	}

	// Dispatch from the calling goroutine so cancellation cannot leave a
	// producer blocked on a send after workers have stopped consuming jobs.
	for index := range sources {
		select {
		case <-ctx.Done():
			close(jobs)
			group.Wait()
			return results
		case jobs <- index:
		}
	}
	close(jobs)
	group.Wait()
	return results
}

func parseWorkerCount(requested, sourceCount int) int {
	if sourceCount <= 0 {
		return 0
	}
	maximum := min(runtime.GOMAXPROCS(0), 4, sourceCount)
	if requested > 0 && requested < maximum {
		return requested
	}
	return maximum
}
