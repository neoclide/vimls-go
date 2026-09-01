package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestAnalysisParallelismClampsRuntimeLimits(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
	if got := analysisParallelism(); got != 1 {
		t.Fatalf("single processor parallelism = %d", got)
	}
	runtime.GOMAXPROCS(maxParallelAnalysis + 1)
	if got := analysisParallelism(); got != maxParallelAnalysis {
		t.Fatalf("high processor parallelism = %d", got)
	}
}

func TestCompletionHighlightKeyFallbackBoundaries(t *testing.T) {
	file := syntax.Parse("highlight Demo guifg=red ctermfg=blue\n")
	if got := completionHighlightKey(file, "guifg=red", len("guifg=red"), len("guifg=red")); got != "guifg" {
		t.Fatalf("highlight fallback key = %q", got)
	}
	if got := completionHighlightKey(file, "ctermfg=blue", len("ctermfg=blue"), len("ctermfg=blue")); got != "ctermfg" {
		t.Fatalf("highlight fallback key = %q", got)
	}
	if got := completionHighlightKey(file, "GUIFG=red", len("GUIFG=red"), len("GUIFG=red")); got != "guifg" {
		t.Fatalf("case-normalized fallback key = %q", got)
	}
}

func TestDocumentStructureFeaturesRecoverParserCorpusSample(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "testdata", "official", "v9.2.1015-parser-corpus.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var corpus struct{ Cases []struct{ Source string } }
	if err := json.NewDecoder(reader).Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 3267 {
		t.Fatalf("unexpected corpus size: %d", len(corpus.Cases))
	}
	instance := New(nil, nil, nil)
	t.Cleanup(instance.stopAnalysis)
	documentURI := uri.File("/tmp/vimls-go-structure-corpus.vim")
	// The parser package exercises every corpus input. Sampling here keeps this
	// server-level recovery test bounded: each selected input makes multiple LSP
	// requests, several of which walk all parsed commands.
	const corpusSampleStride = 64
	for index, test := range corpus.Cases {
		if index%corpusSampleStride != 0 && index != len(corpus.Cases)-1 {
			continue
		}
		instance.documents.Open(documentURI.String(), int32(index+1), test.Source)
		document := protocol.TextDocumentIdentifier{URI: documentURI}
		if _, err := instance.DocumentSymbol(context.Background(), &protocol.DocumentSymbolParams{TextDocument: document}); err != nil {
			t.Fatalf("case %d symbols: %v", index, err)
		}
		if _, err := instance.FoldingRanges(context.Background(), &protocol.FoldingRangeParams{TextDocument: document}); err != nil {
			t.Fatalf("case %d folding: %v", index, err)
		}
		if _, err := instance.SemanticTokensFull(context.Background(), &protocol.SemanticTokensParams{TextDocument: document}); err != nil {
			t.Fatalf("case %d tokens: %v", index, err)
		}
		if _, err := instance.DocumentLink(context.Background(), &protocol.DocumentLinkParams{TextDocument: document}); err != nil {
			t.Fatalf("case %d links: %v", index, err)
		}
		if _, err := instance.Formatting(context.Background(), &protocol.DocumentFormattingParams{TextDocument: document, Options: protocol.FormattingOptions{TabSize: 2, InsertSpaces: true}}); err != nil {
			t.Fatalf("case %d formatting: %v", index, err)
		}
		if _, err := instance.SelectionRange(context.Background(), &protocol.SelectionRangeParams{TextDocument: document, Positions: []protocol.Position{{}}}); err != nil {
			t.Fatalf("case %d selection range: %v", index, err)
		}
		lastLine := uint32(strings.Count(test.Source, "\n"))
		fullRange := protocol.Range{End: protocol.Position{Line: lastLine}}
		if _, err := instance.InlayHint(context.Background(), &protocol.InlayHintParams{TextDocument: document, Range: fullRange}); err != nil {
			t.Fatalf("case %d inlay hints: %v", index, err)
		}
		if _, err := instance.CodeAction(context.Background(), &protocol.CodeActionParams{TextDocument: document, Range: fullRange}); err != nil {
			t.Fatalf("case %d code actions: %v", index, err)
		}
		if _, err := instance.RangeFormatting(context.Background(), &protocol.DocumentRangeFormattingParams{TextDocument: document, Range: fullRange, Options: protocol.FormattingOptions{TabSize: 2, InsertSpaces: true}}); err != nil {
			t.Fatalf("case %d range formatting: %v", index, err)
		}
		for _, cursor := range corpusIdentifierPositions(test.Source) {
			position := protocol.TextDocumentPositionParams{TextDocument: document, Position: cursor}
			if _, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("case %d completion: %v", index, err)
			}
			if _, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("case %d signature help: %v", index, err)
			}
			if _, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("case %d hover: %v", index, err)
			}
			if _, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("case %d definition: %v", index, err)
			}
			if _, err := instance.Declaration(context.Background(), &protocol.DeclarationParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("case %d declaration: %v", index, err)
			}
			if _, err := instance.DocumentHighlight(context.Background(), &protocol.DocumentHighlightParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("case %d highlights: %v", index, err)
			}
			if _, err := instance.References(context.Background(), &protocol.ReferenceParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("case %d references: %v", index, err)
			}
			if _, err := instance.PrepareTypeHierarchy(context.Background(), &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("case %d type hierarchy: %v", index, err)
			}
			if _, err := instance.PrepareCallHierarchy(context.Background(), &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("case %d call hierarchy: %v", index, err)
			}
			if _, err := instance.Implementation(context.Background(), &protocol.ImplementationParams{TextDocumentPositionParams: position}); err != nil {
				var rpcError *jsonrpc2.Error
				if !errors.As(err, &rpcError) || rpcError.Code != jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed) {
					t.Fatalf("case %d implementation: %v", index, err)
				}
			}
			if _, err := instance.PrepareRename(context.Background(), &protocol.PrepareRenameParams{TextDocumentPositionParams: position}); err != nil {
				t.Fatalf("case %d prepare rename: %v", index, err)
			}
			if _, err := instance.Rename(context.Background(), &protocol.RenameParams{TextDocumentPositionParams: position, NewName: "Renamed"}); err != nil {
				var rpcError *jsonrpc2.Error
				if !errors.As(err, &rpcError) || rpcError.Code != jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed) {
					t.Fatalf("case %d rename: %v", index, err)
				}
			}
		}
	}
}

func corpusIdentifierPositions(source string) []protocol.Position {
	snapshot := text.NewSnapshot("file:///corpus.vim", 0, nil, source)
	result := make([]protocol.Position, 0, 48)
	for offset := 0; offset < len(source) && len(result) < 48; offset++ {
		current := source[offset]
		identifierStart := ((current >= 'A' && current <= 'Z') || (current >= 'a' && current <= 'z') || current == '_') && (offset == 0 || !((source[offset-1] >= 'A' && source[offset-1] <= 'Z') || (source[offset-1] >= 'a' && source[offset-1] <= 'z') || source[offset-1] == '_'))
		if !identifierStart && !strings.ContainsRune(".(:$&", rune(current)) {
			continue
		}
		position, err := snapshot.Position(offset, text.UTF16)
		if err == nil {
			result = append(result, protocol.Position{Line: uint32(position.Line), Character: uint32(position.Character)})
		}
	}
	if len(result) == 0 {
		return []protocol.Position{{}}
	}
	return result
}

// This covers the LSP request path for static imports at the same time as it
// verifies that completion, signature help, hover, and links agree on the
// imported symbol's public surface.
func TestImportedLanguageFeaturesExposeOnlyExportedSymbols(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "library.vim", "vim9script\nexport def Public(first: number, second: string): bool\n  return true\nenddef\ndef Private()\nenddef\nexport class Service\n  static def Build(value: number): Service\n    return Service.new()\n  enddef\nendclass\n")
	writeWorkspaceFile(t, root, "nested.vim", "vim9script\n")
	source := "vim9script\nimport './library.vim' as library\necho library.\necho library.Public(1, '')\necho library.Service.Build(1)\nif true\n  source nested.vim\nendif\n"
	path := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	documentURI := canonicalTestURI(t, path)
	instance.documents.Open(documentURI.String(), 1, source)

	completion, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 2, Character: uint32(len("echo library."))},
	}})
	if err != nil {
		t.Fatal(err)
	}
	items := completionItems(t, completion)
	if !hasCompletion(items, "Public", protocol.CompletionItemKindFunction) || !hasCompletion(items, "Service", protocol.CompletionItemKindClass) {
		t.Fatalf("import member completions = %#v", items)
	}
	for _, item := range items {
		if item.Label == "Private" {
			t.Fatalf("private import member leaked through completion: %#v", items)
		}
	}

	signature, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 3, Character: uint32(len("echo library.Public(1, '"))},
	}})
	if err != nil || signature == nil || len(signature.Signatures) != 1 {
		t.Fatalf("imported function signature = %#v, %v", signature, err)
	}
	active, activeSet := signature.Signatures[0].ActiveParameter.Get()
	if signature.Signatures[0].Label != "Public(first: number, second: string): bool" || !activeSet || active != 1 {
		t.Fatalf("imported function signature details = %#v", signature)
	}

	methodSignature, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 4, Character: uint32(len("echo library.Service.Build("))},
	}})
	if err != nil || methodSignature == nil || len(methodSignature.Signatures) != 1 || methodSignature.Signatures[0].Label != "Build(value: number): Service" {
		t.Fatalf("imported static method signature = %#v, %v", methodSignature, err)
	}

	hover, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 3, Character: uint32(len("echo library."))},
	}})
	if err != nil || hover == nil {
		t.Fatalf("imported hover = %#v, %v", hover, err)
	}
	contents, ok := hover.Contents.(*protocol.MarkupContent)
	if !ok || contents.Value == "" {
		t.Fatalf("imported hover contents = %#v", hover.Contents)
	}

	links, err := instance.DocumentLink(context.Background(), &protocol.DocumentLinkParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}})
	if err != nil || len(links) != 2 || links[0].Target == nil || links[1].Target == nil {
		t.Fatalf("import links = %#v, %v", links, err)
	}
	if links[0].Range != navigationRange(1, 7, 22) || links[1].Range != navigationRange(6, 9, 19) {
		t.Fatalf("import link ranges = %#v", links)
	}
}

func TestNavigationAndWorkspaceHelperBoundaries(t *testing.T) {
	for _, test := range []struct {
		left, right analysis.SymbolKind
		want        bool
	}{
		{analysis.SymbolKindMethod, analysis.SymbolKindConstructor, true},
		{analysis.SymbolKindVariable, analysis.SymbolKindConstant, true},
		{analysis.SymbolKindClass, analysis.SymbolKindClass, true},
		{analysis.SymbolKindMethod, analysis.SymbolKindVariable, false},
	} {
		if got := sameMemberCategory(test.left, test.right); got != test.want {
			t.Errorf("member category %s/%s = %t", test.left, test.right, got)
		}
	}
	if aggregateMemberDeclaration(nil) || aggregateMemberDeclaration(&analysis.Declaration{}) {
		t.Fatal("incomplete declaration reported as aggregate member")
	}
	for _, kind := range []syntax.BlockKind{syntax.BlockClass, syntax.BlockInterface, syntax.BlockEnum} {
		if !aggregateMemberDeclaration(&analysis.Declaration{Scope: &analysis.Scope{Kind: kind}}) {
			t.Errorf("%s member declaration rejected", kind)
		}
	}
	if aggregateMemberDeclaration(&analysis.Declaration{Scope: &analysis.Scope{Kind: syntax.BlockDef}}) {
		t.Fatal("function declaration reported as aggregate member")
	}
	if !hasInterfaceImplementationDiagnostic(nil) || !hasInterfaceImplementationDiagnostic(&analysis.FileAnalysis{Diagnostics: []syntax.Diagnostic{{Code: "vim/E1383"}}}) || hasInterfaceImplementationDiagnostic(&analysis.FileAnalysis{}) {
		t.Fatal("interface implementation diagnostic classification changed")
	}
	server := New(nil, nil, nil)
	t.Cleanup(server.stopAnalysis)
	target := workspaceNavigationTarget{match: workspace.SymbolMatch{Fact: workspace.SymbolFact{Path: "/tmp/helper.vim", SelectionRange: syntax.Span{Start: 0, End: 3}}, Source: "abc"}}
	location, ok := server.workspaceTargetLocation(target, text.UTF16)
	if !ok || location.URI != uri.File("/tmp/helper.vim") || location.Range.End.Character != 3 {
		t.Fatalf("workspace location = %#v, %v", location, ok)
	}
	target.match.Fact.SelectionRange.End = 4
	if _, ok := server.workspaceTargetLocation(target, text.UTF16); ok {
		t.Fatal("out-of-source selection range converted to a location")
	}
}

// Requests whose cursor cannot be converted to a source offset must retain
// the handler's empty-result contract instead of treating an adjacent symbol
// as selected. This guards UTF-16 protocol boundaries used by editor clients.
func TestLanguageFeaturesRejectInvalidUTF16PositionsWithoutAdjacentResults(t *testing.T) {
	source := "vim9script\nvar emoji = '𐐀'\necho emoji\n"
	instance, documentURI := openNavigationDocument(t, text.UTF16, source)
	invalid := protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: 1, Character: 100},
	}

	if result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: invalid}); err != nil || len(completionItems(t, result)) != 0 {
		t.Errorf("completion at invalid position = %#v, %v", result, err)
	}
	if result, err := instance.SignatureHelp(context.Background(), &protocol.SignatureHelpParams{TextDocumentPositionParams: invalid}); err != nil || result != nil {
		t.Errorf("signature help at invalid position = %#v, %v", result, err)
	}
	if result, err := instance.Hover(context.Background(), &protocol.HoverParams{TextDocumentPositionParams: invalid}); err != nil || result != nil {
		t.Errorf("hover at invalid position = %#v, %v", result, err)
	}
	if result, err := instance.Definition(context.Background(), &protocol.DefinitionParams{TextDocumentPositionParams: invalid}); err != nil || len(result.(protocol.LocationSlice)) != 0 {
		t.Errorf("definition at invalid position = %#v, %v", result, err)
	}
}

func TestLanguageFeatureRequestsHonorCancellationBeforeDocumentLookup(t *testing.T) {
	instance, documentURI := openNavigationDocument(t, text.UTF16, "vim9script\necho strlen('value')\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	position := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: protocol.Position{Line: 1, Character: 12}}

	if _, err := instance.Completion(ctx, &protocol.CompletionParams{TextDocumentPositionParams: position}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Errorf("cancelled completion error = %v", err)
	}
	if _, err := instance.SignatureHelp(ctx, &protocol.SignatureHelpParams{TextDocumentPositionParams: position}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Errorf("cancelled signature help error = %v", err)
	}
	if _, err := instance.Hover(ctx, &protocol.HoverParams{TextDocumentPositionParams: position}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Errorf("cancelled hover error = %v", err)
	}
}

func TestDocumentHandlersHonorCancelledContextBeforeMissingDocumentWork(t *testing.T) {
	instance := New(nil, nil, nil)
	t.Cleanup(instance.stopAnalysis)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	missingURI := uri.File("/missing.vim")
	position := protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: missingURI}, Position: protocol.Position{}}
	if _, err := instance.Completion(ctx, &protocol.CompletionParams{TextDocumentPositionParams: position}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Errorf("Completion error = %v", err)
	}
	if _, err := instance.SignatureHelp(ctx, &protocol.SignatureHelpParams{TextDocumentPositionParams: position}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Errorf("SignatureHelp error = %v", err)
	}
	if _, err := instance.Hover(ctx, &protocol.HoverParams{TextDocumentPositionParams: position}); err != nil {
		t.Errorf("Hover error = %v", err)
	}
	if _, err := instance.Definition(ctx, &protocol.DefinitionParams{TextDocumentPositionParams: position}); err != nil {
		t.Errorf("Definition error = %v", err)
	}
	if _, err := instance.Declaration(ctx, &protocol.DeclarationParams{TextDocumentPositionParams: position}); err != nil {
		t.Errorf("Declaration error = %v", err)
	}
	if _, err := instance.References(ctx, &protocol.ReferenceParams{TextDocumentPositionParams: position}); err != nil {
		t.Errorf("References error = %v", err)
	}
	if _, err := instance.DocumentHighlight(ctx, &protocol.DocumentHighlightParams{TextDocumentPositionParams: position}); err != nil {
		t.Errorf("DocumentHighlight error = %v", err)
	}
	if _, err := instance.PrepareRename(ctx, &protocol.PrepareRenameParams{TextDocumentPositionParams: position}); err != nil {
		t.Errorf("PrepareRename error = %v", err)
	}
	if _, err := instance.Rename(ctx, &protocol.RenameParams{TextDocumentPositionParams: position, NewName: "renamed"}); err != nil {
		t.Errorf("Rename error = %v", err)
	}
	if _, err := instance.PrepareTypeHierarchy(ctx, &protocol.TypeHierarchyPrepareParams{TextDocumentPositionParams: position}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Errorf("PrepareTypeHierarchy error = %v", err)
	}
	if _, err := instance.Implementation(ctx, &protocol.ImplementationParams{TextDocumentPositionParams: position}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Errorf("Implementation error = %v", err)
	}
	if _, err := instance.PrepareCallHierarchy(ctx, &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: position}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Errorf("PrepareCallHierarchy error = %v", err)
	}
	if _, err := instance.FoldingRanges(ctx, &protocol.FoldingRangeParams{TextDocument: protocol.TextDocumentIdentifier{URI: missingURI}}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Errorf("FoldingRanges error = %v", err)
	}
	if _, err := instance.SelectionRange(ctx, &protocol.SelectionRangeParams{TextDocument: protocol.TextDocumentIdentifier{URI: missingURI}}); !errors.Is(err, protocol.ErrRequestCancelled) {
		t.Errorf("SelectionRange error = %v", err)
	}
	if _, err := instance.Formatting(ctx, &protocol.DocumentFormattingParams{TextDocument: protocol.TextDocumentIdentifier{URI: missingURI}}); err == nil {
		t.Errorf("Formatting error = %v", err)
	}
}

func TestLanguageFeatureCapabilityPreferenceBoundaries(t *testing.T) {
	if got := preferredMarkupKind([]protocol.MarkupKind{"unsupported", protocol.MarkupKindMarkdown}); got != protocol.MarkupKindMarkdown {
		t.Fatalf("preferred markdown = %q", got)
	}
	if got := preferredMarkupKind([]protocol.MarkupKind{"unsupported"}); got != protocol.MarkupKindPlainText {
		t.Fatalf("fallback markup = %q", got)
	}
	if capabilities := languageFeatureCapabilitiesFromClient(nil); capabilities.hoverMarkup != protocol.MarkupKindPlainText || capabilities.signatureMarkup != protocol.MarkupKindPlainText || capabilities.diagnosticRelatedInformation {
		t.Fatalf("nil client capabilities = %#v", capabilities)
	}
	markdown := protocol.MarkupKindMarkdown
	related := true
	capabilities := languageFeatureCapabilitiesFromClient(&protocol.TextDocumentClientCapabilities{
		Hover:              &protocol.HoverClientCapabilities{ContentFormat: []protocol.MarkupKind{markdown}},
		SignatureHelp:      &protocol.SignatureHelpClientCapabilities{SignatureInformation: &protocol.ClientSignatureInformationOptions{DocumentationFormat: []protocol.MarkupKind{markdown}}},
		PublishDiagnostics: &protocol.PublishDiagnosticsClientCapabilities{DiagnosticsCapabilities: protocol.DiagnosticsCapabilities{RelatedInformation: &related}},
	})
	if capabilities.hoverMarkup != markdown || capabilities.signatureMarkup != markdown || !capabilities.diagnosticRelatedInformation {
		t.Fatalf("client capabilities = %#v", capabilities)
	}
}

func TestCompletionMemberKindAndContainerBoundaries(t *testing.T) {
	for _, test := range []struct {
		kind analysis.SymbolKind
		want protocol.CompletionItemKind
	}{
		{analysis.SymbolKindMethod, protocol.CompletionItemKindMethod}, {analysis.SymbolKindFunction, protocol.CompletionItemKindMethod}, {analysis.SymbolKindConstructor, protocol.CompletionItemKindMethod}, {analysis.SymbolKindEnumMember, protocol.CompletionItemKindConstant}, {analysis.SymbolKindConstant, protocol.CompletionItemKindConstant}, {analysis.SymbolKindVariable, protocol.CompletionItemKindField},
	} {
		if got := completionMemberKind(test.kind); got != test.want {
			t.Errorf("completion kind %q = %v", test.kind, got)
		}
	}
	symbols := []*analysis.Symbol{{Name: "Outer", Kind: analysis.SymbolKindClass, Children: []*analysis.Symbol{{Name: "Inner", Kind: analysis.SymbolKindInterface}}}, {Name: "Value", Kind: analysis.SymbolKindVariable}}
	if got := completionContainer(symbols, "Inner"); got == nil || got.Name != "Inner" {
		t.Fatalf("nested completion container = %#v", got)
	}
	if completionContainer(symbols, "Value") != nil || completionContainer(nil, "Missing") != nil {
		t.Fatal("non-container completion symbol accepted")
	}
}

func TestCompletionStaticTypeBoundaries(t *testing.T) {
	for _, test := range []struct {
		name string
		want bool
	}{
		{"", false}, {analysis.ValueTypeAny, false}, {"dict", false}, {"list", false}, {"number", true}, {"Custom", true},
	} {
		if got := completionStaticType(analysis.ValueType{Name: test.name}); got != test.want {
			t.Errorf("static type %q = %t", test.name, got)
		}
	}
}
