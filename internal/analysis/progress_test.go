package analysis

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestTypeTraversalPausesContinuesAndCancels(t *testing.T) {
	file := syntax.Parse("vim9script\nvar value = 1\n" + strings.Repeat("echo value + 1\n", 512))
	for _, cancel := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			facts := CollectCompletionFacts(file)
			state := newTypeState(facts)
			state.collectFacts()
			paused, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			calls := 0
			facts.progress = &analysisProgress{yield: func() error {
				calls++
				if calls == 1 {
					close(paused)
					<-resume
				}
				if cancel {
					return context.Canceled
				}
				return nil
			}}
			go func() { state.walkCommands(); close(done) }()
			<-paused
			synctest.Wait()
			select {
			case <-done:
				t.Fatal("type traversal did not pause inside its loop")
			default:
			}
			close(resume)
			<-done
			if cancel {
				if calls != 1 || !errors.Is(facts.progress.err, context.Canceled) {
					t.Fatalf("cancellation: calls=%d err=%v", calls, facts.progress.err)
				}
			} else if calls < 2 || len(facts.expressionTypes) < 512 {
				t.Fatal("traversal did not continue after resume")
			}
		})
	}
	got, err := AnalyzeWithYield(file, false, func() error { return nil })
	if err != nil || got == nil || got.progress != nil {
		t.Fatal("yielding changed complete analysis or retained callback")
	}
	want := Analyze(file)
	if !reflect.DeepEqual(got.Declarations, want.Declarations) ||
		!reflect.DeepEqual(got.References, want.References) ||
		!reflect.DeepEqual(got.Diagnostics, want.Diagnostics) ||
		!reflect.DeepEqual(got.expressionTypes, want.expressionTypes) ||
		!reflect.DeepEqual(got.Scopes, want.Scopes) {
		t.Fatal("yielding changed analysis facts")
	}
}

func TestReferenceTraversalCancelsWithinOneExpression(t *testing.T) {
	file := syntax.Parse("echo [" + strings.Repeat("missing,", 512) + "]\n")
	facts := CollectCompletionFacts(file)
	calls := 0
	facts.progress = &analysisProgress{yield: func() error { calls++; return context.Canceled }}
	walkCommand(facts, file, &file.Commands[0], facts.Root)
	if calls != 1 || !errors.Is(facts.progress.err, context.Canceled) || len(facts.References) >= 512 {
		t.Fatalf("reference traversal ignored cancellation: calls=%d refs=%d", calls, len(facts.References))
	}
}

func TestAnalysisCancellationDiscardsPartialResultsAcrossTraversal(t *testing.T) {
	file := syntax.Parse("vim9script\nvar value: number = 'wrong'\ndef Read(): number\n" + strings.Repeat("  echo value + 1\n", 512) + "  return value\nenddef\n")
	checkpoints := 0
	complete, err := AnalyzeWithYield(file, false, func() error { checkpoints++; return nil })
	if err != nil || complete == nil || checkpoints < 100 {
		t.Fatalf("large fixture: result=%p err=%v checkpoints=%d", complete, err, checkpoints)
	}
	for _, stop := range []int{1, checkpoints / 4, checkpoints / 2, checkpoints - 1, checkpoints} {
		calls := 0
		partial, err := AnalyzeWithYield(file, false, func() error {
			calls++
			if calls == stop {
				return context.Canceled
			}
			return nil
		})
		if partial != nil || !errors.Is(err, context.Canceled) || calls != stop {
			t.Fatalf("stop=%d: partial=%p err=%v calls=%d", stop, partial, err, calls)
		}
	}
}
