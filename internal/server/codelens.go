package server

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const maxCodeLensData = 1024

type codeLensKind uint8

const (
	codeLensReferences codeLensKind = iota + 1
	codeLensImplementations
)

type codeLensData struct {
	Version   uint8               `json:"v"`
	URI       string              `json:"u"`
	ContentID string              `json:"c"`
	Kind      analysis.SymbolKind `json:"k"`
	Start     int                 `json:"s"`
	End       int                 `json:"e"`
	Lens      codeLensKind        `json:"l"`
}

// CodeLens returns cheap, unresolved callable declaration lenses. Counts are
// deliberately deferred to resolve because they can require a workspace index.
func (s *Server) CodeLens(ctx context.Context, params *protocol.CodeLensParams) ([]protocol.CodeLens, error) {
	snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil || file == nil {
		return []protocol.CodeLens{}, nil
	}
	path, ok := workspaceURIPath(params.TextDocument.URI)
	if !ok {
		return []protocol.CodeLens{}, s.structureCurrent(ctx, snapshot)
	}
	facts := workspace.CollectSymbolFacts(path, file)
	result := make([]protocol.CodeLens, 0, len(facts)*2)
	for _, fact := range facts {
		if err := ctx.Err(); err != nil {
			return nil, protocol.ErrRequestCancelled
		}
		if !codeLensCallable(fact.Kind) {
			continue
		}
		if lens, ok := newCodeLens(snapshot, encoding, fact, codeLensReferences); ok {
			result = append(result, lens)
		}
		if codeLensImplementation(fact, facts) {
			if lens, ok := newCodeLens(snapshot, encoding, fact, codeLensImplementations); ok {
				result = append(result, lens)
			}
		}
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return result, nil
}

func newCodeLens(snapshot *text.Snapshot, encoding text.Encoding, fact workspace.SymbolFact, lens codeLensKind) (protocol.CodeLens, bool) {
	rangeValue, ok := protocolRange(snapshot, encoding, fact.SelectionRange)
	if !ok || rangeValue.Start.Line != rangeValue.End.Line {
		return protocol.CodeLens{}, false
	}
	contentID := snapshot.ContentID()
	data, err := protocol.Marshal(codeLensData{
		Version: 1, URI: snapshot.URI(), ContentID: hex.EncodeToString(contentID[:]), Kind: fact.Kind,
		Start: fact.SelectionRange.Start, End: fact.SelectionRange.End, Lens: lens,
	})
	if err != nil {
		return protocol.CodeLens{}, false
	}
	return protocol.CodeLens{Range: rangeValue, Data: protocol.LSPAny(data)}, true
}

// CodeLensResolve validates the immutable declaration identity before routing
// through the established reference and implementation query paths.
func (s *Server) CodeLensResolve(ctx context.Context, lens *protocol.CodeLens) (*protocol.CodeLens, error) {
	if err := ctx.Err(); err != nil {
		return nil, protocol.ErrRequestCancelled
	}
	data, documentURI, malformed := decodeCodeLensData(lens.Data)
	if malformed {
		return lens, nil
	}
	s.publishMu.Lock()
	snapshot, ok := s.documents.Snapshot(data.URI)
	s.publishMu.Unlock()
	if !ok {
		return nil, protocol.ErrContentModified
	}
	contentID := snapshot.ContentID()
	if hex.EncodeToString(contentID[:]) != data.ContentID {
		return nil, protocol.ErrContentModified
	}
	s.mu.Lock()
	encoding := s.encoding
	s.mu.Unlock()
	file := s.parseSnapshot(snapshot)
	if err := ctx.Err(); err != nil {
		return nil, protocol.ErrRequestCancelled
	}
	path, ok := workspaceURIPath(documentURI)
	if !ok || file == nil {
		return lens, nil
	}
	facts := workspace.CollectSymbolFacts(path, file)
	fact, ok := codeLensFact(data, facts)
	if !ok {
		return lens, nil
	}
	rangeValue, rangeOK := protocolRange(snapshot, encoding, fact.SelectionRange)
	if !rangeOK || lens.Range != rangeValue {
		return lens, nil
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	position := rangeValue.Start
	var locations []protocol.Location
	var err error
	switch data.Lens {
	case codeLensReferences:
		locations, err = s.References(ctx, &protocol.ReferenceParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: position,
		}, Context: protocol.ReferenceContext{IncludeDeclaration: false}})
	case codeLensImplementations:
		if !codeLensImplementation(fact, facts) {
			return lens, nil
		}
		var implementations protocol.DefinitionResult
		implementations, err = s.Implementation(ctx, &protocol.ImplementationParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI}, Position: position,
		}})
		if implementations != nil {
			locations = implementations.(protocol.LocationSlice)
		}
	default:
		return lens, nil
	}
	if err != nil {
		return nil, err
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	command, ok := codeLensCommand(data.Lens, documentURI, position, locations)
	if !ok {
		return lens, nil
	}
	resolved := *lens
	resolved.Command = command
	return &resolved, nil
}

func decodeCodeLensData(raw protocol.LSPAny) (codeLensData, uri.URI, bool) {
	if len(raw) == 0 || len(raw) > maxCodeLensData {
		return codeLensData{}, "", true
	}
	var data codeLensData
	if err := protocol.Unmarshal(raw, &data); err != nil || data.Version != 1 || data.Start < 0 || data.Start >= data.End ||
		!codeLensCallable(data.Kind) || (data.Lens != codeLensReferences && data.Lens != codeLensImplementations) {
		return codeLensData{}, "", true
	}
	decoded, err := hex.DecodeString(data.ContentID)
	if err != nil || len(decoded) != len(text.ContentID{}) {
		return codeLensData{}, "", true
	}
	documentURI := uri.URI(data.URI)
	if _, ok := workspaceURIPath(documentURI); !ok {
		return codeLensData{}, "", true
	}
	return data, documentURI, false
}

func codeLensFact(data codeLensData, facts []workspace.SymbolFact) (workspace.SymbolFact, bool) {
	for _, fact := range facts {
		if fact.Kind == data.Kind && fact.SelectionRange == (syntax.Span{Start: data.Start, End: data.End}) {
			return fact, true
		}
	}
	return workspace.SymbolFact{}, false
}

func codeLensCallable(kind analysis.SymbolKind) bool {
	return kind == analysis.SymbolKindFunction || kind == analysis.SymbolKindMethod || kind == analysis.SymbolKindConstructor
}

func codeLensImplementation(fact workspace.SymbolFact, facts []workspace.SymbolFact) bool {
	if fact.Kind != analysis.SymbolKindMethod {
		return false
	}
	for _, owner := range facts {
		if owner.SelectionRange != fact.OwnerSelectionRange {
			continue
		}
		return owner.Kind == analysis.SymbolKindInterface || owner.Kind == analysis.SymbolKindClass && fact.Abstract
	}
	return false
}

func codeLensCommand(kind codeLensKind, documentURI uri.URI, position protocol.Position, locations []protocol.Location) (protocol.Command, bool) {
	name := "reference"
	tooltip := "Show references"
	if kind == codeLensImplementations {
		name = "implementation"
		tooltip = "Show implementations"
	}
	if len(locations) != 1 {
		name += "s"
	}
	arguments := make([]protocol.LSPAny, 0, 3)
	for _, value := range []any{documentURI.String(), position, locations} {
		encoded, err := protocol.Marshal(value)
		if err != nil {
			return protocol.Command{}, false
		}
		arguments = append(arguments, protocol.LSPAny(encoded))
	}
	return protocol.Command{Title: fmt.Sprintf("%d %s", len(locations), name), Command: "editor.action.showReferences", Arguments: arguments, Tooltip: &tooltip}, true
}
