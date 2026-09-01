package server

import (
	"context"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestInitializeAdvertisesNegotiatedOptionalCapabilities(t *testing.T) {
	value := true
	root := uri.File(t.TempDir())
	s := New(nil, nil, nil)
	t.Cleanup(s.stopAnalysis)
	result, err := s.Initialize(context.Background(), &protocol.InitializeParams{
		RootURI:               &root,
		InitializationOptions: protocol.LSPAny([]byte(`{"targetVersion":"9.0","runtimepath":1,"unresolvedSeverity":"bad"}`)),
		Capabilities: protocol.ClientCapabilities{
			Workspace: &protocol.WorkspaceClientCapabilities{Configuration: &value, DidChangeWatchedFiles: &protocol.DidChangeWatchedFilesClientCapabilities{DynamicRegistration: &value, RelativePatternSupport: &value}},
			TextDocument: &protocol.TextDocumentClientCapabilities{
				Rename:     &protocol.RenameClientCapabilities{PrepareSupport: &value},
				CodeAction: &protocol.CodeActionClientCapabilities{CodeActionLiteralSupport: protocol.ClientCodeActionLiteralOptions{CodeActionKind: protocol.ClientCodeActionKindOptions{ValueSet: []protocol.CodeActionKind{protocol.CodeActionKindQuickFix}}}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Capabilities.RenameProvider.(*protocol.RenameOptions); !ok {
		t.Fatalf("rename provider = %#v", result.Capabilities.RenameProvider)
	}
	if _, ok := result.Capabilities.CodeActionProvider.(*protocol.CodeActionOptions); !ok {
		t.Fatalf("action provider = %#v", result.Capabilities.CodeActionProvider)
	}
	if result.Capabilities.PositionEncoding == "" || result.Capabilities.Workspace == nil {
		t.Fatalf("capabilities = %#v", result.Capabilities)
	}
	s.mu.Lock()
	if !s.watchDynamicRegistration || !s.watchRelativePatterns || !s.workspaceConfiguration || s.pendingWarning == "" {
		t.Fatalf("server state = %#v", s)
	}
	s.mu.Unlock()
}
