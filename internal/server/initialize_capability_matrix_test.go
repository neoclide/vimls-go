package server

import (
	"bytes"
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
		InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":1,"workspace":{"rebuildDebounce":0}}`)),
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
	if result.Capabilities.CodeLensProvider == nil || result.Capabilities.CodeLensProvider.ResolveProvider == nil || !*result.Capabilities.CodeLensProvider.ResolveProvider {
		t.Fatalf("code lens provider = %#v", result.Capabilities.CodeLensProvider)
	}
	s.mu.Lock()
	if !s.watchDynamicRegistration || !s.watchRelativePatterns || !s.workspaceConfiguration || s.pendingWarning == "" {
		t.Fatalf("server state = %#v", s)
	}
	s.mu.Unlock()
	s.workspaceMu.Lock()
	if s.workspaceDelay != defaultWorkspaceRebuildDebounce {
		t.Fatalf("initial workspace delay = %s, want %s", s.workspaceDelay, defaultWorkspaceRebuildDebounce)
	}
	s.workspaceMu.Unlock()
}

func TestInitializeDocumentRangeFormattingCapabilityShapes(t *testing.T) {
	rangesSupport := true
	t.Run("no client ranges support keeps plain boolean", func(t *testing.T) {
		s := New(nil, nil, nil)
		t.Cleanup(s.stopAnalysis)
		result, err := s.Initialize(context.Background(), &protocol.InitializeParams{})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := result.Capabilities.DocumentRangeFormattingProvider.(protocol.Boolean); !ok {
			t.Fatalf("range formatting provider = %#v, want plain true", result.Capabilities.DocumentRangeFormattingProvider)
		}
		encoded, err := protocol.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(encoded, []byte(`"documentRangeFormattingProvider":true`)) {
			t.Fatalf("initialize result omitted plain provider: %s", encoded)
		}
	})
	t.Run("client ranges support advertises options", func(t *testing.T) {
		s := New(nil, nil, nil)
		t.Cleanup(s.stopAnalysis)
		result, err := s.Initialize(context.Background(), &protocol.InitializeParams{
			Capabilities: protocol.ClientCapabilities{
				TextDocument: &protocol.TextDocumentClientCapabilities{
					RangeFormatting: &protocol.DocumentRangeFormattingClientCapabilities{RangesSupport: &rangesSupport},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		options, ok := result.Capabilities.DocumentRangeFormattingProvider.(*protocol.DocumentRangeFormattingOptions)
		if !ok || options.RangesSupport == nil || !*options.RangesSupport {
			t.Fatalf("range formatting provider = %#v, want options with rangesSupport true", result.Capabilities.DocumentRangeFormattingProvider)
		}
		encoded, err := protocol.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(encoded, []byte(`"documentRangeFormattingProvider":{"rangesSupport":true}`)) {
			t.Fatalf("initialize result omitted options provider: %s", encoded)
		}
	})
}

func TestInitializeDiagnosticTransportCapability(t *testing.T) {
	for name, diagnostic := range map[string]*protocol.DiagnosticClientCapabilities{"legacy": nil, "pull": {}} {
		t.Run(name, func(t *testing.T) {
			s := New(nil, nil, nil)
			t.Cleanup(s.stopAnalysis)
			result, err := s.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: protocol.ClientCapabilities{TextDocument: &protocol.TextDocumentClientCapabilities{Diagnostic: diagnostic}}})
			if err != nil {
				t.Fatal(err)
			}
			options, ok := result.Capabilities.DiagnosticProvider.(*protocol.DiagnosticOptions)
			if diagnostic == nil && (ok || result.Capabilities.DiagnosticProvider != nil) {
				t.Fatalf("legacy diagnostic provider = %#v", result.Capabilities.DiagnosticProvider)
			}
			if diagnostic != nil && (!ok || !options.InterFileDependencies || !options.WorkspaceDiagnostics) {
				t.Fatalf("pull diagnostic provider = %#v", result.Capabilities.DiagnosticProvider)
			}
		})
	}
}
