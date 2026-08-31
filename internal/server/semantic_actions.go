package server

import (
	"context"
	"sort"
	"strings"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"go.lsp.dev/protocol"
)

var semanticTokenTypes = []string{
	"comment", "keyword", "modifier", "variable", "function", "method", "class", "interface", "enum", "enumMember", "type", "property",
}

var semanticTokenModifiers = []string{"declaration", "readonly", "deprecated"}

type semanticFact struct {
	span      syntax.Span
	tokenType uint32
	modifiers uint32
	priority  uint8
}

func (s *Server) SemanticTokensFull(ctx context.Context, params *protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil || file == nil {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}
	facts := collectSemanticFacts(file)
	data := make([]uint32, 0, len(facts)*5)
	var previousLine, previousCharacter uint32
	for index, fact := range facts {
		if index%64 == 0 && ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		start, startErr := snapshot.Position(fact.span.Start, encoding)
		end, endErr := snapshot.Position(fact.span.End, encoding)
		if startErr != nil || endErr != nil || start.Line != end.Line || end.Character <= start.Character {
			continue
		}
		line, character := uint32(start.Line), uint32(start.Character)
		deltaLine := line - previousLine
		deltaCharacter := character
		if deltaLine == 0 {
			deltaCharacter -= previousCharacter
		}
		data = append(data, deltaLine, deltaCharacter, uint32(end.Character-start.Character), fact.tokenType, fact.modifiers)
		previousLine, previousCharacter = line, character
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return &protocol.SemanticTokens{Data: data}, nil
}

func collectSemanticFacts(file *syntax.File) []semanticFact {
	facts := make([]semanticFact, 0, len(file.Tokens))
	for _, token := range file.Tokens {
		var tokenType uint32
		switch token.Kind {
		case syntax.TokenComment:
			tokenType = 0
		case syntax.TokenCommand:
			tokenType = 1
		case syntax.TokenModifier:
			tokenType = 2
		default:
			continue
		}
		facts = append(facts, semanticFact{span: token.Span, tokenType: tokenType, priority: 1})
	}
	result := analysis.Analyze(file)
	for _, declaration := range result.Declarations {
		modifiers := uint32(1)
		if !declaration.Mutable {
			modifiers |= 2
		}
		if declaration.Deprecated {
			modifiers |= 4
		}
		facts = append(facts, semanticFact{span: declaration.Span, tokenType: semanticType(declaration.Kind), modifiers: modifiers, priority: 3})
	}
	for _, reference := range result.References {
		if reference.Declaration == nil {
			continue
		}
		modifiers := uint32(0)
		if !reference.Declaration.Mutable {
			modifiers |= 2
		}
		if reference.Declaration.Deprecated {
			modifiers |= 4
		}
		facts = append(facts, semanticFact{span: reference.Span, tokenType: semanticType(reference.Declaration.Kind), modifiers: modifiers, priority: 2})
	}
	sort.SliceStable(facts, func(i, j int) bool {
		if facts[i].span.Start != facts[j].span.Start {
			return facts[i].span.Start < facts[j].span.Start
		}
		if facts[i].priority != facts[j].priority {
			return facts[i].priority > facts[j].priority
		}
		return facts[i].span.End < facts[j].span.End
	})
	filtered := facts[:0]
	for _, fact := range facts {
		if fact.span.End <= fact.span.Start || fact.span.Start < 0 || fact.span.End > len(file.Source) {
			continue
		}
		if len(filtered) > 0 && fact.span.Start < filtered[len(filtered)-1].span.End {
			continue
		}
		filtered = append(filtered, fact)
	}
	return filtered
}

func semanticType(kind analysis.SymbolKind) uint32 {
	switch kind {
	case analysis.SymbolKindFunction:
		return 4
	case analysis.SymbolKindMethod, analysis.SymbolKindConstructor:
		return 5
	case analysis.SymbolKindClass:
		return 6
	case analysis.SymbolKindInterface:
		return 7
	case analysis.SymbolKindEnum:
		return 8
	case analysis.SymbolKindEnumMember:
		return 9
	case analysis.SymbolKindTypeAlias:
		return 10
	default:
		return 3
	}
}

func (s *Server) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CommandOrCodeAction, error) {
	snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil || file == nil {
		return []protocol.CommandOrCodeAction{}, nil
	}
	if !allowsQuickFix(params.Context.Only) {
		return []protocol.CommandOrCodeAction{}, s.structureCurrent(ctx, snapshot)
	}
	missing := make([]syntax.Diagnostic, 0)
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Code == "vimls/missing-end" {
			missing = append(missing, diagnostic)
		}
	}
	if len(missing) != 1 {
		return []protocol.CommandOrCodeAction{}, s.structureCurrent(ctx, snapshot)
	}
	diagnosticRange, validDiagnosticRange := protocolRange(snapshot, encoding, missing[0].Span)
	if !validDiagnosticRange || !clientHasDiagnostic(params.Context.Diagnostics, "vimls/missing-end", diagnosticRange) || !rangesOverlap(params.Range, diagnosticRange) {
		return []protocol.CommandOrCodeAction{}, s.structureCurrent(ctx, snapshot)
	}
	block, endCommand, ok := missingEndBlock(file, missing[0])
	if !ok {
		return []protocol.CommandOrCodeAction{}, s.structureCurrent(ctx, snapshot)
	}
	endPosition, positionErr := snapshot.Position(len(file.Source), encoding)
	if positionErr != nil {
		return []protocol.CommandOrCodeAction{}, s.structureCurrent(ctx, snapshot)
	}
	indent := lineIndent(file.Source, file.Commands[block.Header].Span.Start)
	newText := indent + endCommand + "\n"
	if file.Source != "" && !strings.HasSuffix(file.Source, "\n") {
		newText = "\n" + newText
	}
	editRange := protocol.Range{
		Start: protocol.Position{Line: uint32(endPosition.Line), Character: uint32(endPosition.Character)},
		End:   protocol.Position{Line: uint32(endPosition.Line), Character: uint32(endPosition.Character)},
	}
	preferred := true
	kind := protocol.CodeActionKindQuickFix
	version, versioned := snapshot.Version()
	var versionPointer *int32
	if versioned {
		versionPointer = &version
	}
	edit := &protocol.TextEdit{Range: editRange, NewText: newText}
	action := &protocol.CodeAction{
		Title:       "Insert :" + endCommand,
		Kind:        &kind,
		Diagnostics: params.Context.Diagnostics,
		IsPreferred: &preferred,
		Edit: &protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{&protocol.TextDocumentEdit{
			TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
				TextDocumentIdentifier: params.TextDocument,
				Version:                versionPointer,
			},
			Edits: []protocol.TextDocumentEditElement{edit},
		}}},
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return []protocol.CommandOrCodeAction{action}, nil
}

func allowsQuickFix(only []protocol.CodeActionKind) bool {
	if len(only) == 0 {
		return true
	}
	for _, kind := range only {
		if kind == protocol.CodeActionKindQuickFix || strings.HasPrefix(string(protocol.CodeActionKindQuickFix), string(kind)+".") || strings.HasPrefix(string(kind), string(protocol.CodeActionKindQuickFix)+".") {
			return true
		}
	}
	return false
}

func clientHasDiagnostic(diagnostics []protocol.Diagnostic, code string, rangeValue protocol.Range) bool {
	for _, diagnostic := range diagnostics {
		if value, ok := diagnostic.Code.(protocol.String); ok && string(value) == code && diagnostic.Range == rangeValue {
			return true
		}
	}
	return false
}

func rangesOverlap(left, right protocol.Range) bool {
	return positionLess(left.Start, right.End) && positionLess(right.Start, left.End)
}

func positionLess(left, right protocol.Position) bool {
	return left.Line < right.Line || left.Line == right.Line && left.Character < right.Character
}

func missingEndBlock(file *syntax.File, diagnostic syntax.Diagnostic) (syntax.Block, string, bool) {
	for _, block := range file.Blocks {
		if block.End >= 0 || block.Header < 0 || block.Header >= len(file.Commands) || file.Commands[block.Header].Name != diagnostic.Span {
			continue
		}
		end := map[syntax.BlockKind]string{
			syntax.BlockIf: "endif", syntax.BlockFor: "endfor", syntax.BlockWhile: "endwhile", syntax.BlockTry: "endtry",
			syntax.BlockFunction: "endfunction", syntax.BlockDef: "enddef", syntax.BlockClass: "endclass",
			syntax.BlockInterface: "endinterface", syntax.BlockEnum: "endenum",
		}[block.Kind]
		if end != "" {
			return block, end, true
		}
	}
	return syntax.Block{}, "", false
}

func lineIndent(source string, offset int) string {
	if offset < 0 || offset > len(source) {
		return ""
	}
	start := strings.LastIndexByte(source[:offset], '\n') + 1
	end := start
	for end < offset && (source[end] == ' ' || source[end] == '\t') {
		end++
	}
	return source[start:end]
}

func (s *Server) InlayHint(ctx context.Context, params *protocol.InlayHintParams) ([]protocol.InlayHint, error) {
	snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil || file == nil {
		return []protocol.InlayHint{}, nil
	}
	start, startErr := snapshot.Offset(fromProtocolPosition(params.Range.Start), encoding)
	end, endErr := snapshot.Offset(fromProtocolPosition(params.Range.End), encoding)
	if startErr != nil || endErr != nil || end < start {
		return []protocol.InlayHint{}, s.structureCurrent(ctx, snapshot)
	}
	explicit := explicitTypeDeclarations(file)
	fileAnalysis := analysis.Analyze(file)
	hints := make([]protocol.InlayHint, 0)
	for _, declaration := range fileAnalysis.Declarations {
		if declaration.Span.Start < start || declaration.Span.End > end || explicit[declaration.Span] || declaration.Type.Name == "" || declaration.Type.Name == analysis.ValueTypeAny {
			continue
		}
		if declaration.Kind != analysis.SymbolKindVariable && declaration.Kind != analysis.SymbolKindConstant {
			continue
		}
		position, positionErr := snapshot.Position(declaration.Span.End, encoding)
		if positionErr != nil {
			continue
		}
		hints = append(hints, protocol.InlayHint{
			Position: protocol.Position{Line: uint32(position.Line), Character: uint32(position.Character)},
			Label:    protocol.String(": " + formatValueType(declaration.Type)),
			Kind:     protocol.InlayHintKindType,
		})
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return hints, nil
}

func explicitTypeDeclarations(file *syntax.File) map[syntax.Span]bool {
	result := make(map[syntax.Span]bool)
	walkCommands(file.Commands, func(command *syntax.Command) {
		if command.Declaration != nil {
			for _, binding := range command.Declaration.Bindings {
				if binding.ParsedType != nil {
					result[binding.Name] = true
				}
			}
		}
		if command.For != nil {
			for _, binding := range command.For.Bindings {
				if binding.ParsedType != nil {
					result[binding.Name] = true
				}
			}
		}
	})
	return result
}
