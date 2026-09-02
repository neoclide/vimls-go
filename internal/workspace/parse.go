package workspace

import (
	"context"
	"runtime"
	"sync"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
)

// AnalyzedSource pairs parser and file-local analysis results for one source.
// Analysis always belongs to File when both are non-nil.
type AnalyzedSource struct {
	File     *syntax.File
	Analysis *analysis.FileAnalysis
}

// ParseSources parses independent source snapshots with a bounded worker pool.
// Results use the same indexes as sources, even when workers finish out of
// order. A canceled context prevents further work from being scheduled; a
// parse already inside syntax.Parse is allowed to finish because the parser
// does not accept a cancellation hook.
func ParseSources(ctx context.Context, sources []string, workers int) []*syntax.File {
	analyzed := parseAndAnalyzeSources(ctx, sources, workers, false)
	results := make([]*syntax.File, len(analyzed))
	for index, result := range analyzed {
		results[index] = result.File
	}
	return results
}

// ParseAndAnalyzeSources parses and performs file-local semantic analysis of
// independent sources in a bounded worker pool. Results retain source order.
func ParseAndAnalyzeSources(ctx context.Context, sources []string, workers int) []AnalyzedSource {
	return parseAndAnalyzeSources(ctx, sources, workers, true)
}

func parseAndAnalyzeSources(ctx context.Context, sources []string, workers int, analyze bool) []AnalyzedSource {
	results := make([]AnalyzedSource, len(sources))
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
					file := syntax.Parse(sources[index])
					results[index].File = file
					if analyze && ctx.Err() == nil {
						results[index].Analysis = analysis.Analyze(file)
					}
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
	maximum := min(runtime.GOMAXPROCS(0), 6, sourceCount)
	if requested > 0 && requested < maximum {
		return requested
	}
	return maximum
}
