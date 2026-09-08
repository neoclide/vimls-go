package server

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

func TestAnalysisInputBackoffExtendsAndHonorsCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var priority completionPriority
		priority.userActivity()
		done := make(chan error, 1)
		go func() { done <- priority.wait(context.Background(), nil) }()
		synctest.Wait()
		time.Sleep(100 * time.Millisecond)
		priority.userActivity()
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("analysis ignored the latest edit")
		default:
		}
		finish := priority.begin()
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("idle deadline bypassed active completion")
		default:
		}
		finish()
		synctest.Wait()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatal("analysis did not resume")
		}
	})
}

func TestAnalysisInputBackoffCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var priority completionPriority
		priority.userActivity()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- priority.wait(ctx, nil) }()
		synctest.Wait()
		cancel()
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel = %v", err)
		}
	})
}

func TestDidChangeBackoffCoalescesBeforeParsingWithoutBlockingCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		instance, documentURI := openNavigationDocument(t, text.UTF16, "let local = 1\necho loc\n")
		defer instance.stopAnalysis()
		var parsed atomic.Int32
		instance.testHooks.beforeParseSnapshotCacheMiss = func(*text.Snapshot) { parsed.Add(1) }
		for version := int32(2); version <= 4; version++ {
			if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
				TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: version},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: strings.Repeat("\n", int(version)) + "let local = 1\necho loc\n"}},
			}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(50 * time.Millisecond)
			synctest.Wait()
		}
		if count := parsed.Load(); count != 0 {
			t.Fatalf("parsed during typing %d times", count)
		}
		items := completionListRequest(t, instance, documentURI, 5, 8).Items
		if !hasCompletionLabel(items, "local") {
			t.Fatal("completion did not bypass idle wait")
		}
		if count := parsed.Load(); count != 1 {
			t.Fatalf("completion parse count = %d", count)
		}
		time.Sleep(analysisInputBackoff)
		synctest.Wait()
		if count := parsed.Load(); count != 1 {
			t.Fatalf("background reparsed the current snapshot: %d", count)
		}
		snapshot, _ := instance.documents.Snapshot(documentURI.String())
		instance.publishMu.Lock()
		cached := instance.parsed[snapshot.URI()]
		instance.publishMu.Unlock()
		if cached.analysis == nil || cached.file.Source != snapshot.Text() {
			t.Fatal("background did not analyze the final edit")
		}
	})
}

func TestCompletionDoesNotWaitForFullAnalysis(t *testing.T) {
	for _, test := range []struct {
		name, source, label string
	}{
		{"option", "set nu§\n", "number"},
		{"command", "echo '😀' | endf§\n", "endfunction"},
		{"legacy local", "function! Read(arg)\n  let local = 1\n  echo loc§\nendfunction\n", "local"},
		{"vim9 local", "vim9script\ndef Read(arg: number)\n  var local = arg\n  echo loc§\nenddef\n", "local"},
		{"member", "vim9script\nclass Box\n  var value: number\nendclass\nvar box = Box.new()\necho box.§\n", "value"},
		{"forward return type", "vim9script\nclass Box\n  var value: number\nendclass\nvar box = Make()\necho box.§\ndef Make()\n  return Box.new()\nenddef\n", "value"},
		{"comment", "vim9script\n# text §\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			offset := strings.Index(test.source, "§")
			source := strings.Replace(test.source, "§", "", 1)
			instance, documentURI := openNavigationDocument(t, text.UTF16, source)
			t.Cleanup(instance.stopAnalysis)
			snapshot, _ := instance.documents.Snapshot(documentURI.String())
			position, err := snapshot.Position(offset, text.UTF16)
			if err != nil {
				t.Fatal(err)
			}
			blocked, release, analyzed := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			t.Cleanup(func() { once.Do(func() { close(release) }); <-analyzed })
			instance.testHooks.beforeAnalyze = func(*syntax.File) {
				close(blocked)
				<-release
			}
			go func() { instance.analyzeSnapshot(snapshot); close(analyzed) }()
			waitForServerRace(t, blocked, "full analysis to block")
			result := make(chan protocol.CompletionResult, 1)
			errCh := make(chan error, 1)
			go func() {
				items, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: uint32(position.Line), Character: uint32(position.Character)},
				}})
				if list, ok := items.(*protocol.CompletionList); err == nil && ok {
					for _, item := range list.Items {
						if target, ok := completionResolveTargetFromData(item.Data); ok && target.Kind == completionResolveLocal {
							_, err = instance.CompletionResolve(context.Background(), &item)
							if err != nil {
								break
							}
						}
					}
				}
				errCh <- err
				result <- items
			}()
			select {
			case items := <-result:
				if err := <-errCh; err != nil {
					t.Fatal(err)
				}
				if test.label != "" && !hasCompletionLabel(completionItems(t, items), test.label) {
					t.Fatalf("missing %q in %#v", test.label, items)
				}
				if test.label == "" && len(completionItems(t, items)) != 0 {
					t.Fatal("comment completion returned candidates")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("completion waited for full analysis")
			}
			instance.publishMu.Lock()
			cached := instance.parsed[documentURI.String()]
			instance.publishMu.Unlock()
			if cached.analysis != nil {
				t.Fatal("completion installed partial facts as full analysis")
			}
		})
	}
}

func TestCompletionPausesAndResumesFullAnalysis(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar count: number = 'wrong'\necho cou\n")
	t.Cleanup(instance.stopAnalysis)
	snapshot, _ := instance.documents.Snapshot(documentURI.String())
	checkpoint, resumeCheckpoint := make(chan struct{}), make(chan struct{})
	completionEntered, runCompletion := make(chan struct{}), make(chan struct{})
	paused, analyzed := make(chan struct{}), make(chan struct{})
	var checkpointOnce, completionOnce, pausedOnce sync.Once
	t.Cleanup(func() {
		checkpointOnce.Do(func() { close(resumeCheckpoint) })
		completionOnce.Do(func() { close(runCompletion) })
		<-analyzed
	})
	var checkpoints atomic.Int32
	instance.testHooks.beforeAnalysisCheckpoint = func() {
		if checkpoints.Add(1) == 8 {
			close(checkpoint)
			<-resumeCheckpoint
		}
	}
	instance.testHooks.analysisPaused = func() { pausedOnce.Do(func() { close(paused) }) }
	instance.testHooks.beforeCompletion = func() { close(completionEntered); <-runCompletion }
	var full *analysis.FileAnalysis
	go func() { _, full = instance.analyzeSnapshot(snapshot); close(analyzed) }()
	waitForServerRace(t, checkpoint, "analysis phase boundary")
	completionDone := make(chan error, 1)
	go func() {
		_, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 8},
		}})
		completionDone <- err
	}()
	waitForServerRace(t, completionEntered, "completion priority")
	checkpointOnce.Do(func() { close(resumeCheckpoint) })
	waitForServerRace(t, paused, "background analysis to yield")
	if got := checkpoints.Load(); got != 8 {
		t.Fatalf("analysis advanced while completion was active: %d", got)
	}
	completionOnce.Do(func() { close(runCompletion) })
	select {
	case err := <-completionDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("completion did not finish while analysis was paused")
	}
	waitForServerRace(t, analyzed, "analysis to resume")
	if full == nil || len(full.Diagnostics) == 0 || checkpoints.Load() <= 8 {
		t.Fatal("resumed analysis did not finish its diagnostics")
	}
}

func TestCompletionPriorityOverlappingRequestsAndCancellation(t *testing.T) {
	var priority completionPriority
	first, second := priority.begin(), priority.begin()
	first()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	paused, done := make(chan struct{}), make(chan error, 1)
	go func() { done <- priority.wait(ctx, func() { close(paused) }) }()
	waitForServerRace(t, paused, "remaining completion")
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled pause = %v", err)
	}
	second()
	if err := priority.wait(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledCompletionReleasesPriorityWhileSharedParseContinues(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "set nu\n")
	t.Cleanup(instance.stopAnalysis)
	snapshot, _ := instance.documents.Snapshot(documentURI.String())
	parsed, release, leaderDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }); <-leaderDone })
	instance.testHooks.beforeParseSnapshotCacheMiss = func(*text.Snapshot) { close(parsed); <-release }
	go func() { instance.analyzeSnapshot(snapshot); close(leaderDone) }()
	waitForServerRace(t, parsed, "shared parse")
	waiting := make(chan struct{})
	instance.testHooks.beforeInFlightWait = func(*text.Snapshot) { close(waiting) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := instance.Completion(ctx, &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 0, Character: 6},
		}})
		done <- err
	}()
	waitForServerRace(t, waiting, "completion waiting for syntax")
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, protocol.ErrRequestCancelled) {
			t.Fatalf("completion cancellation = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("completion did not cancel its syntax wait")
	}
	instance.completionPriority.mu.Lock()
	active := instance.completionPriority.active
	instance.completionPriority.mu.Unlock()
	if active != 0 {
		t.Fatalf("canceled completion retained priority: %d", active)
	}
	once.Do(func() { close(release) })
	waitForServerRace(t, leaderDone, "shared analysis after completion cancellation")
	instance.publishMu.Lock()
	full := instance.parsed[documentURI.String()].analysis
	instance.publishMu.Unlock()
	if full == nil {
		t.Fatal("completion cancellation abandoned shared analysis")
	}
}

func TestCompletionUsesEditedDeclarationsAndUTF16Ranges(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar Before = 1\necho Before\n")
	t.Cleanup(instance.stopAnalysis)
	before, _ := instance.documents.Snapshot(documentURI.String())
	instance.analyzeSnapshot(before)
	// Includes a BOM, CRLF and an astral identifier. Completion must derive
	// candidates and replacement ranges from the new syntax, not old offsets.
	source := "\ufeffvim9script\r\nvar 𐐀After = 2\r\necho 𐐀Af\r\n"
	snapshot, changed, err := instance.documents.Change(documentURI.String(), 2, text.UTF16, []text.Change{{Text: source}})
	if err != nil || !changed {
		t.Fatalf("change: %t, %v", changed, err)
	}
	blocked, release, analyzed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }); <-analyzed })
	instance.testHooks.beforeAnalyze = func(*syntax.File) { close(blocked); <-release }
	go func() { instance.analyzeSnapshot(snapshot); close(analyzed) }()
	waitForServerRace(t, blocked, "new full analysis")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := instance.Completion(ctx, &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 2, Character: 9},
	}})
	if err != nil {
		t.Fatal(err)
	}
	items := completionItems(t, result)
	if !hasCompletionLabel(items, "𐐀After") || hasCompletionLabel(items, "Before") {
		t.Fatalf("stale declaration completions: %#v", items)
	}
	item := completionItemWithLabel(items, "𐐀After")
	edit := completionMainEditFromItem(*item)
	if edit.replace != navigationRange(2, 5, 9) {
		t.Fatalf("replacement range = %#v", edit)
	}
}

func TestShutdownInterruptsAnalysisPausedForCompletion(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\nvar count = 1\n")
	t.Cleanup(instance.stopAnalysis)
	finish := instance.completionPriority.begin()
	defer finish()
	paused := make(chan struct{})
	instance.testHooks.analysisPaused = func() { close(paused) }
	instance.startAnalysis(documentURI.String())
	waitForServerRace(t, paused, "paused background worker")
	done := make(chan struct{})
	go func() { instance.stopAnalysis(); close(done) }()
	waitForServerRace(t, done, "shutdown without waiting for completion")
	instance.publishMu.Lock()
	defer instance.publishMu.Unlock()
	if len(instance.analysisInFlight) != 0 || instance.parsed[documentURI.String()].analysis != nil {
		t.Fatal("shutdown retained in-flight or partial analysis")
	}
}
