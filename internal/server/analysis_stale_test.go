package server

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

func TestPausedAnalysisAbandonsChangedContentButKeepsIdenticalContent(t *testing.T) {
	for _, change := range []string{"edit", "close", "identical", "aba"} {
		t.Run(change, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const source = "let local = 1\necho local\n"
				instance, documentURI := openNavigationDocument(t, text.UTF16, source)
				defer instance.stopAnalysis()
				// Keep notification requeues pending; this test drives the shared
				// computation directly and controls all of its consumers.
				instance.analysisWorkers = 1
				snapshot, _ := instance.documents.Snapshot(documentURI.String())
				finish := instance.completionPriority.begin()
				var once sync.Once
				paused := make(chan struct{})
				instance.testHooks.analysisPaused = func() { once.Do(func() { close(paused) }) }
				done := make(chan *analysis.FileAnalysis, 1)
				go func() { _, facts := instance.analyzeSnapshotContext(context.Background(), snapshot); done <- facts }()
				<-paused
				edit := func(version int32, source string) {
					t.Helper()
					if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
						TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: version},
						ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: source}},
					}); err != nil {
						t.Fatal(err)
					}
				}
				switch change {
				case "close":
					instance.DidClose(context.Background(), &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
				case "identical":
					edit(2, source)
				default:
					edit(2, "let local = 2\necho local\n")
				}
				if change == "aba" {
					edit(3, source)
				}
				synctest.Wait()
				if change == "identical" {
					select {
					case <-done:
						t.Fatal("same-content work exited while still paused")
					default:
					}
					finish()
					if facts := <-done; facts == nil {
						t.Fatal("same-content work did not continue")
					}
				} else {
					select {
					case facts := <-done:
						if facts != nil {
							t.Fatal("obsolete work returned partial facts")
						}
					default:
						t.Fatal("obsolete work waited for completion/idle deadline")
					}
					finish()
					if change == "aba" {
						current, _ := instance.documents.Snapshot(documentURI.String())
						_, facts := instance.analyzeSnapshotContext(context.Background(), current)
						if facts == nil || facts.File.Source != source {
							t.Fatal("new A joined canceled work")
						}
					}
				}
			})
		})
	}
}

func TestLateCanceledAnalysisCannotDeleteReplacement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const source = "let value = 1\necho value\n"
		instance, documentURI := openNavigationDocument(t, text.UTF16, source)
		defer instance.stopAnalysis()
		instance.analysisWorkers = 1
		oldSnapshot, _ := instance.documents.Snapshot(documentURI.String())
		entered := make(chan int, 2)
		oldRelease, newRelease := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		instance.testHooks.beforeAnalyze = func(*syntax.File) {
			call := calls.Add(1)
			entered <- int(call)
			if call == 1 {
				<-oldRelease
			} else {
				<-newRelease
			}
		}
		oldDone := make(chan *analysis.FileAnalysis, 1)
		go func() {
			_, facts := instance.analyzeSnapshotContext(context.Background(), oldSnapshot)
			oldDone <- facts
		}()
		<-entered
		for index, content := range []string{"let changed = 2\n", source} {
			if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
				TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: int32(index + 2)},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: content}},
			}); err != nil {
				t.Fatal(err)
			}
		}
		current, _ := instance.documents.Snapshot(documentURI.String())
		newDone := make(chan *analysis.FileAnalysis, 1)
		go func() { _, facts := instance.analyzeSnapshotContext(context.Background(), current); newDone <- facts }()
		if call := <-entered; call != 2 {
			t.Fatalf("replacement computation = %d", call)
		}
		key := analysisInFlightKey{parseInFlightKey: parseInFlightKey{uri: current.URI(), contentID: current.ContentID(), configFile: instance.configFileRoleForURI(current.URI())}}
		instance.publishMu.Lock()
		replacement := instance.analysisInFlight[key]
		instance.publishMu.Unlock()
		close(oldRelease)
		if facts := <-oldDone; facts != nil {
			t.Fatal("canceled owner returned facts")
		}
		instance.publishMu.Lock()
		retained := instance.analysisInFlight[key]
		instance.publishMu.Unlock()
		if replacement == nil || retained != replacement {
			t.Fatal("old owner deleted the replacement")
		}
		close(newRelease)
		facts := <-newDone
		instance.publishMu.Lock()
		cached := instance.parsed[current.URI()]
		remaining := len(instance.analysisInFlight)
		instance.publishMu.Unlock()
		if facts == nil || cached.analysis != facts || remaining != 0 {
			t.Fatal("replacement did not install or clean up")
		}
	})
}

func TestSameContentWaitersShareAnalysisDespiteRequestCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const source = "let value = 1\necho value\n"
		instance, documentURI := openNavigationDocument(t, text.UTF16, source)
		defer instance.stopAnalysis()
		instance.analysisWorkers = 1
		snapshot, _ := instance.documents.Snapshot(documentURI.String())
		finish := instance.completionPriority.begin()
		paused := make(chan struct{})
		var once sync.Once
		var computes atomic.Int32
		instance.testHooks.beforeAnalyze = func(*syntax.File) { computes.Add(1) }
		instance.testHooks.analysisPaused = func() { once.Do(func() { close(paused) }) }
		leaderDone := make(chan *analysis.FileAnalysis, 1)
		go func() {
			_, facts := instance.analyzeSnapshotContext(context.Background(), snapshot)
			leaderDone <- facts
		}()
		<-paused
		if err := instance.DidChange(context.Background(), &protocol.DidChangeTextDocumentParams{
			TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: documentURI}, Version: 2},
			ContentChanges: []protocol.TextDocumentContentChangeEvent{&protocol.TextDocumentContentChangeWholeDocument{Text: source}},
		}); err != nil {
			t.Fatal(err)
		}
		current, _ := instance.documents.Snapshot(documentURI.String())
		joined := make(chan struct{}, 2)
		instance.testHooks.beforeAnalysisInFlightWait = func(*text.Snapshot) { joined <- struct{}{} }
		ctx, cancel := context.WithCancel(context.Background())
		canceledDone := make(chan *analysis.FileAnalysis, 1)
		liveDone := make(chan *analysis.FileAnalysis, 1)
		go func() { _, facts := instance.analyzeSnapshotContext(ctx, current); canceledDone <- facts }()
		go func() { _, facts := instance.analyzeSnapshotContext(context.Background(), current); liveDone <- facts }()
		<-joined
		<-joined
		cancel()
		if facts := <-canceledDone; facts != nil {
			t.Fatal("canceled waiter returned facts")
		}
		synctest.Wait()
		select {
		case <-liveDone:
			t.Fatal("waiter cancellation interrupted live consumer")
		default:
		}
		finish()
		leader, live := <-leaderDone, <-liveDone
		if leader == nil || leader != live || computes.Load() != 1 {
			t.Fatal("same-content consumers did not share one computation")
		}
	})
}
