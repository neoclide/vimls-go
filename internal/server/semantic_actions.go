package server

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/vimdata"
	"go.lsp.dev/protocol"
)

var semanticTokenTypes = []string{
	"comment", "keyword", "modifier", "variable", "function", "method", "class", "interface", "enum", "enumMember", "type", "property", "namespace", "parameter", "special",
}

var semanticTokenModifiers = []string{"declaration", "readonly", "deprecated", "static", "defaultLibrary"}

const (
	semanticComment uint32 = iota
	semanticKeyword
	semanticModifier
	semanticVariable
	semanticFunction
	semanticMethod
	semanticClass
	semanticInterface
	semanticEnum
	semanticEnumMember
	semanticTypeName
	semanticProperty
	semanticNamespace
	semanticParameter
	semanticSpecial
)

const (
	semanticDeclaration uint32 = 1 << iota
	semanticReadonly
	semanticDeprecated
	semanticStatic
	semanticDefaultLibrary
)

type semanticFact struct {
	span      syntax.Span
	tokenType uint32
	modifiers uint32
	priority  uint8
}

type syntaxQuickFix struct {
	diagnostic       syntax.Diagnostic
	clientDiagnostic protocol.Diagnostic
	span             syntax.Span
	newText          string
	title            string
	preferred        bool
}

func (s *Server) SemanticTokensFull(ctx context.Context, params *protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	snapshot, data, err := s.semanticTokensData(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}
	result, err := s.installSemanticTokenResult(ctx, snapshot, data)
	if err != nil {
		return nil, err
	}
	return semanticTokensFullResult(result), nil
}

// SemanticTokensRange returns the tokens that overlap with the requested
// range.  Range responses carry no result ID and never touch the
// full/delta result registry, so a range request cannot corrupt a client's
// delta base.
func (s *Server) SemanticTokensRange(ctx context.Context, params *protocol.SemanticTokensRangeParams) (*protocol.SemanticTokens, error) {
	snapshot, file, fileAnalysis, encoding, err := s.structureDocumentWithAnalysis(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil || file == nil {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}
	start, startErr := snapshot.Offset(fromProtocolPosition(params.Range.Start), encoding)
	end, endErr := snapshot.Offset(fromProtocolPosition(params.Range.End), encoding)
	if startErr != nil || endErr != nil || end <= start {
		return &protocol.SemanticTokens{Data: []uint32{}}, s.structureCurrent(ctx, snapshot)
	}
	data, err := encodeSemanticTokens(ctx, snapshot, encoding, collectSemanticFacts(file, fileAnalysis), &semanticTokenRange{start: start, end: end})
	if err != nil {
		return nil, err
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return &protocol.SemanticTokens{Data: data}, nil
}

// SemanticTokensFullDelta returns an edit only when the supplied result is
// still this URI's latest result. In every other case a full result safely
// resets the client base.
func (s *Server) SemanticTokensFullDelta(ctx context.Context, params *protocol.SemanticTokensDeltaParams) (protocol.SemanticTokensDeltaResult, error) {
	snapshot, data, err := s.semanticTokensData(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, protocol.ErrRequestCancelled
	}
	current, ok := s.documents.Snapshot(snapshot.URI())
	if !ok || current != snapshot {
		return nil, protocol.ErrContentModified
	}
	previous, delta := s.semanticTokenResults[snapshot.URI()]
	result := s.installSemanticTokenResultLocked(snapshot, data)
	if !delta || previous.resultID != params.PreviousResultID {
		return semanticTokensFullResult(result), nil
	}
	return &protocol.SemanticTokensDelta{
		ResultID: new(result.resultID),
		Edits:    semanticTokenEdits(previous.data, result.data),
	}, nil
}

func (s *Server) semanticTokensData(ctx context.Context, documentURI string) (*text.Snapshot, []uint32, error) {
	snapshot, file, fileAnalysis, encoding, err := s.structureDocumentWithAnalysis(ctx, documentURI)
	if err != nil {
		return nil, nil, err
	}
	if snapshot == nil || file == nil {
		return nil, nil, nil
	}
	data, err := encodeSemanticTokens(ctx, snapshot, encoding, collectSemanticFacts(file, fileAnalysis), nil)
	if err != nil {
		return nil, nil, err
	}
	return snapshot, data, nil
}

type semanticTokenRange struct {
	start int
	end   int
}

// encodeSemanticTokens delta-encodes facts. If tokenRange is non-nil, output is
// restricted to facts overlapping with [tokenRange.start, tokenRange.end), as
// required by LSP 3.18 for textDocument/semanticTokens/range.
func encodeSemanticTokens(ctx context.Context, snapshot *text.Snapshot, encoding text.Encoding, facts []semanticFact, tokenRange *semanticTokenRange) ([]uint32, error) {
	data := make([]uint32, 0, len(facts)*5)
	if tokenRange != nil && tokenRange.end <= tokenRange.start {
		return data, nil
	}
	var previousLine, previousCharacter uint32
	for index, fact := range facts {
		if index%64 == 0 && ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		if tokenRange != nil && (fact.span.End <= tokenRange.start || fact.span.Start >= tokenRange.end) {
			continue
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
	return data, nil
}

func (s *Server) installSemanticTokenResult(ctx context.Context, snapshot *text.Snapshot, data []uint32) (semanticTokenResult, error) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if err := ctx.Err(); err != nil {
		return semanticTokenResult{}, protocol.ErrRequestCancelled
	}
	current, ok := s.documents.Snapshot(snapshot.URI())
	if !ok || current != snapshot {
		return semanticTokenResult{}, protocol.ErrContentModified
	}
	return s.installSemanticTokenResultLocked(snapshot, data), nil
}

func (s *Server) installSemanticTokenResultLocked(snapshot *text.Snapshot, data []uint32) semanticTokenResult {
	s.nextSemanticTokenResultID++
	result := semanticTokenResult{
		data:     data,
		resultID: strconv.FormatUint(s.nextSemanticTokenResultID, 10),
	}
	s.semanticTokenResults[snapshot.URI()] = result
	return result
}

func semanticTokensFullResult(result semanticTokenResult) *protocol.SemanticTokens {
	return &protocol.SemanticTokens{
		ResultID: new(result.resultID),
		Data:     append([]uint32(nil), result.data...),
	}
}

// semanticTokenEdits returns the single linear replacement between shared
// prefix and suffixes. It operates on flattened semantic-token data, as the
// protocol requires, rather than token groups.
func semanticTokenEdits(previous, current []uint32) []protocol.SemanticTokensEdit {
	prefix := 0
	for prefix < len(previous) && prefix < len(current) && previous[prefix] == current[prefix] {
		prefix++
	}
	previousEnd, currentEnd := len(previous), len(current)
	for previousEnd > prefix && currentEnd > prefix && previous[previousEnd-1] == current[currentEnd-1] {
		previousEnd--
		currentEnd--
	}
	if prefix == previousEnd && prefix == currentEnd {
		return []protocol.SemanticTokensEdit{}
	}
	return []protocol.SemanticTokensEdit{{
		Start:       uint32(prefix),
		DeleteCount: uint32(previousEnd - prefix),
		Data:        append([]uint32(nil), current[prefix:currentEnd]...),
	}}
}

func collectSemanticFacts(file *syntax.File, fileAnalysis *analysis.FileAnalysis) []semanticFact {
	facts := make([]semanticFact, 0, len(file.Tokens))
	commandKinds := make(map[syntax.Span]syntax.CommandKind)
	commandTokens := make(map[syntax.Span]bool)
	for _, token := range file.Tokens {
		if token.Kind == syntax.TokenCommand {
			commandTokens[token.Span] = true
		}
	}
	staticDeclarations := make(map[syntax.Span]bool)
	walkCommands(file.Commands, func(command *syntax.Command) {
		commandKinds[command.Name] = command.Kind
		if command.Name.Start < command.Name.End && !commandTokens[command.Name] {
			facts = append(facts, semanticCommandFact(command.Name, command.Kind))
		}
		isStatic := false
		for _, modifier := range command.Modifiers {
			if modifier.Name == "static" {
				isStatic = true
				break
			}
		}
		if isStatic {
			if command.Function != nil {
				staticDeclarations[command.Function.Name] = true
			}
			if command.Declaration != nil {
				for _, binding := range command.Declaration.Bindings {
					staticDeclarations[binding.Name] = true
				}
			}
		}
		if command.UserCommand != nil && command.UserCommand.Name.End > command.UserCommand.Name.Start {
			facts = append(facts, semanticFact{span: command.UserCommand.Name, tokenType: semanticKeyword, modifiers: semanticDeclaration, priority: 3})
		}
		if command.Function != nil {
			for _, parameter := range command.Function.TypeParameters {
				facts = append(facts, semanticFact{span: parameter.Span, tokenType: semanticTypeName, modifiers: semanticDeclaration, priority: 2})
			}
			for _, parameter := range command.Function.Parameters {
				collectTypeSemanticFacts(parameter.Type, &facts)
			}
			for _, attribute := range command.Function.Attributes {
				facts = append(facts, semanticFact{span: attribute, tokenType: semanticModifier, priority: 1})
			}
			collectTypeSemanticFacts(command.Function.ReturnType, &facts)
		}
		if command.Declaration != nil {
			for _, binding := range command.Declaration.Bindings {
				collectTypeSemanticFacts(binding.ParsedType, &facts)
			}
		}
		if command.For != nil {
			if command.For.In.Start < command.For.In.End {
				facts = append(facts, semanticFact{span: command.For.In, tokenType: semanticKeyword, modifiers: semanticDefaultLibrary, priority: 1})
			}
			for _, binding := range command.For.Bindings {
				collectTypeSemanticFacts(binding.ParsedType, &facts)
			}
		}
		if command.TypeAlias != nil {
			collectTypeSemanticFacts(command.TypeAlias.Type, &facts)
		}
		if command.Aggregate != nil {
			for _, span := range command.Aggregate.Extends {
				facts = append(facts, semanticFact{span: span, tokenType: semanticTypeName, priority: 1})
			}
			for _, span := range command.Aggregate.Implements {
				facts = append(facts, semanticFact{span: span, tokenType: semanticTypeName, priority: 1})
			}
		}
		if heredoc := command.Heredoc; heredoc != nil {
			// Header spans come from the parser; never scan the opaque payload.
			for _, region := range []syntax.Span{heredoc.Header, heredoc.EndMarker} {
				source := file.Text(region)
				offset := region.Start
				for _, word := range strings.Fields(source) {
					index := strings.Index(source, word)
					start := offset + index
					if word == "trim" || word == "eval" || word == heredoc.Marker {
						facts = append(facts, semanticFact{span: syntax.Span{Start: start, End: start + len(word)}, tokenType: semanticSpecial, priority: 2})
					}
					offset = start + len(word)
					source = source[index+len(word):]
				}
			}
		}
		if command.Set != nil {
			for _, option := range command.Set.Options {
				if _, ok := vimdata.LookupOption(file.Text(option.Name)); ok {
					span := option.Name
					if option.Prefix.Start < option.Prefix.End {
						span.Start = option.Prefix.Start
					}
					facts = append(facts, semanticFact{span: span, tokenType: semanticVariable, modifiers: semanticDefaultLibrary, priority: 2})
				}
			}
		}
		walkCommandExpressions(command, func(expression *syntax.Expression) {
			collectExpressionSemanticFacts(file, expression, &facts)
		})
	})
	for _, token := range file.Tokens {
		var tokenType uint32
		modifiers := uint32(0)
		switch token.Kind {
		case syntax.TokenComment:
			tokenType = semanticComment
		case syntax.TokenCommand:
			kind, known := commandKinds[token.Span]
			fact := semanticCommandFact(token.Span, kind)
			tokenType, modifiers = fact.tokenType, fact.modifiers
			if !known {
				modifiers = 0
			}
		case syntax.TokenModifier:
			tokenType = semanticModifier
		default:
			continue
		}
		facts = append(facts, semanticFact{span: token.Span, tokenType: tokenType, modifiers: modifiers, priority: 1})
	}
	result := fileAnalysis
	if result == nil {
		result = analysis.Analyze(file)
	}
	for _, declaration := range result.Declarations {
		modifiers := semanticDeclaration
		if !declaration.Mutable {
			modifiers |= semanticReadonly
		}
		if declaration.Deprecated {
			modifiers |= semanticDeprecated
		}
		if staticDeclarations[declaration.Span] {
			modifiers |= semanticStatic
		}
		appendSymbolSemanticFacts(file, &facts, declaration.Span, semanticType(declaration), modifiers, 3)
	}
	for _, reference := range result.References {
		if reference.Declaration == nil {
			continue
		}
		modifiers := uint32(0)
		if !reference.Declaration.Mutable {
			modifiers |= semanticReadonly
		}
		if reference.Declaration.Deprecated {
			modifiers |= semanticDeprecated
		}
		if staticDeclarations[reference.Declaration.Span] {
			modifiers |= semanticStatic
		}
		appendSymbolSemanticFacts(file, &facts, reference.Span, semanticType(reference.Declaration), modifiers, 2)
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

func semanticCommandFact(span syntax.Span, kind syntax.CommandKind) semanticFact {
	modifiers := uint32(0)
	if kind == syntax.CommandBuiltin || kind == syntax.CommandBlockStart || kind == syntax.CommandBlockEnd {
		modifiers = semanticDefaultLibrary
	}
	return semanticFact{span: span, tokenType: semanticKeyword, modifiers: modifiers, priority: 1}
}

func semanticType(declaration *analysis.Declaration) uint32 {
	if declaration.Parameter {
		return semanticParameter
	}
	if declaration.Scope != nil && (declaration.Scope.Kind == syntax.BlockClass || declaration.Scope.Kind == syntax.BlockInterface) &&
		(declaration.Kind == analysis.SymbolKindVariable || declaration.Kind == analysis.SymbolKindConstant) {
		return semanticProperty
	}
	switch declaration.Kind {
	case analysis.SymbolKindImport:
		return semanticNamespace
	case analysis.SymbolKindFunction:
		return semanticFunction
	case analysis.SymbolKindMethod, analysis.SymbolKindConstructor:
		return semanticMethod
	case analysis.SymbolKindClass:
		return semanticClass
	case analysis.SymbolKindInterface:
		return semanticInterface
	case analysis.SymbolKindEnum:
		return semanticEnum
	case analysis.SymbolKindEnumMember:
		return semanticEnumMember
	case analysis.SymbolKindTypeAlias:
		return semanticTypeName
	default:
		return semanticVariable
	}
}

func appendSymbolSemanticFacts(file *syntax.File, facts *[]semanticFact, span syntax.Span, tokenType, modifiers uint32, priority uint8) {
	name := file.Text(span)
	prefixLength := 0
	if len(name) >= 2 && name[1] == ':' && strings.ContainsRune("gbwtslav", rune(name[0])) {
		prefixLength = 2
	} else if len(name) >= len("<SID>") && strings.EqualFold(name[:len("<SID>")], "<SID>") {
		prefixLength = len("<SID>")
	}
	if prefixLength == 0 {
		*facts = append(*facts, semanticFact{span: span, tokenType: tokenType, modifiers: modifiers, priority: priority})
		return
	}
	*facts = append(*facts, semanticFact{span: syntax.Span{Start: span.Start, End: span.Start + prefixLength}, tokenType: semanticNamespace, priority: priority})
	if span.Start+prefixLength < span.End {
		*facts = append(*facts, semanticFact{span: syntax.Span{Start: span.Start + prefixLength, End: span.End}, tokenType: tokenType, modifiers: modifiers, priority: priority})
	}
}

func collectExpressionSemanticFacts(file *syntax.File, expression *syntax.Expression, facts *[]semanticFact) {
	for _, typeNode := range expression.TypeArguments {
		collectTypeSemanticFacts(typeNode, facts)
	}
	collectTypeSemanticFacts(expression.CastType, facts)
	collectTypeSemanticFacts(expression.ReturnType, facts)
	for _, parameter := range expression.Parameters {
		collectTypeSemanticFacts(parameter.Type, facts)
	}
	if expression.Kind == syntax.ExpressionCall && len(expression.Children) > 0 {
		callee := expression.Children[0]
		switch callee.Kind {
		case syntax.ExpressionIdentifier:
			modifiers := uint32(0)
			if _, ok := vimdata.LookupFunction(callee.Value); ok {
				modifiers = semanticDefaultLibrary
			}
			appendSymbolSemanticFacts(file, facts, callee.Span, semanticFunction, modifiers, 1)
		case syntax.ExpressionMember:
			// Arrow callees are classified when the member expression itself is
			// visited below. Avoid emitting the same function fact twice while
			// retaining the call-specific Method classification for dot members.
			if file.Text(callee.Operator) == "->" {
				break
			}
			if member, ok := expressionMemberSpan(callee); ok {
				*facts = append(*facts, semanticFact{span: member, tokenType: semanticMethod, priority: 1})
			}
		}
	}
	if expression.Kind == syntax.ExpressionMember {
		if member, ok := expressionMemberSpan(expression); ok {
			tokenType := uint32(semanticProperty)
			modifiers := uint32(0)
			if file.Text(expression.Operator) == "->" {
				tokenType = semanticFunction
				if function, ok := vimdata.LookupFunction(expression.Value); ok && function.MethodArgument > 0 {
					modifiers = semanticDefaultLibrary
				}
			}
			*facts = append(*facts, semanticFact{span: member, tokenType: tokenType, modifiers: modifiers, priority: 1})
		}
	}
	if expression.Kind != syntax.ExpressionIdentifier {
		return
	}
	modifiers := uint32(0)
	tokenType := semanticVariable
	classify := false
	switch {
	case strings.HasPrefix(expression.Value, "&"):
		classify = true
		if _, ok := vimdata.LookupOption(expression.Value); ok {
			modifiers |= semanticDefaultLibrary
		}
	case strings.HasPrefix(expression.Value, "@"), strings.HasPrefix(expression.Value, "$"):
		classify = true
	case strings.HasPrefix(expression.Value, "v:"):
		classify = true
		if variable, ok := vimdata.LookupVariable(expression.Value); ok {
			modifiers |= semanticDefaultLibrary
			if variable.Flags&(vimdata.VariableReadOnly|vimdata.VariableSandboxReadOnly) != 0 {
				modifiers |= semanticReadonly
			}
		}
	case len(expression.Value) >= 2 && expression.Value[1] == ':' && strings.ContainsRune("gbwtsla", rune(expression.Value[0])):
		classify = true
		if expression.Value[0] == 'a' {
			tokenType = semanticParameter
		}
	}
	if classify {
		appendSymbolSemanticFacts(file, facts, expression.Span, tokenType, modifiers, 1)
	}
}

func expressionMemberSpan(expression *syntax.Expression) (syntax.Span, bool) {
	if expression == nil || expression.Kind != syntax.ExpressionMember || expression.Value == "" || len(expression.Value) > expression.Span.End-expression.Span.Start {
		return syntax.Span{}, false
	}
	return syntax.Span{Start: expression.Span.End - len(expression.Value), End: expression.Span.End}, true
}

func collectTypeSemanticFacts(typeNode *syntax.Type, facts *[]semanticFact) {
	if typeNode == nil {
		return
	}
	if typeNode.Name != "" && typeNode.Name != "?" && typeNode.Name != "..." {
		name := typeNode.Name
		start := typeNode.Span.Start
		if dot := strings.LastIndexByte(name, '.'); dot > 0 {
			*facts = append(*facts, semanticFact{span: syntax.Span{Start: start, End: start + dot}, tokenType: semanticNamespace, priority: 1})
			start += dot + 1
			name = name[dot+1:]
		}
		end := start + len(name)
		if end <= typeNode.Span.End {
			modifiers := uint32(0)
			switch name {
			case "any", "blob", "bool", "channel", "dict", "float", "func", "job", "list", "number", "object", "string", "tuple", "void":
				modifiers = semanticDefaultLibrary
			}
			*facts = append(*facts, semanticFact{span: syntax.Span{Start: start, End: end}, tokenType: semanticTypeName, modifiers: modifiers, priority: 1})
		}
	}
	for _, argument := range typeNode.Arguments {
		collectTypeSemanticFacts(argument, facts)
	}
	collectTypeSemanticFacts(typeNode.ReturnType, facts)
}

func (s *Server) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CommandOrCodeAction, error) {
	snapshot, file, fileAnalysis, encoding, err := s.structureDocumentWithAnalysis(ctx, params.TextDocument.URI.String())
	if err != nil {
		return nil, err
	}
	if snapshot == nil || file == nil {
		return []protocol.CommandOrCodeAction{}, nil
	}
	if !allowsQuickFix(params.Context.Only) {
		return []protocol.CommandOrCodeAction{}, s.structureCurrent(ctx, snapshot)
	}
	if fileAnalysis == nil {
		fileAnalysis = analysis.Analyze(file)
	}
	diagnostics := append([]syntax.Diagnostic(nil), file.Diagnostics...)
	diagnostics = append(diagnostics, fileAnalysis.Diagnostics...)
	for _, clientDiagnostic := range params.Context.Diagnostics {
		code, ok := clientDiagnosticCode(clientDiagnostic)
		if !ok || !quickFixDiagnosticCode(code) || !rangesOverlapOrTouch(params.Range, clientDiagnostic.Range) {
			continue
		}
		start, startErr := snapshot.Offset(fromProtocolPosition(clientDiagnostic.Range.Start), encoding)
		end, endErr := snapshot.Offset(fromProtocolPosition(clientDiagnostic.Range.End), encoding)
		if startErr != nil || endErr != nil || end < start || !hasDiagnostic(diagnostics, code, syntax.Span{Start: start, End: end}) {
			return nil, protocol.ErrContentModified
		}
	}
	fixes := make([]syntaxQuickFix, 0, 2)
	for _, diagnostic := range diagnostics {
		diagnosticRange, validDiagnosticRange := protocolRange(snapshot, encoding, diagnostic.Span)
		clientDiagnostic, matched := matchingClientDiagnostic(params.Context.Diagnostics, diagnostic.Code, diagnosticRange)
		if !validDiagnosticRange || !matched || !rangesOverlapOrTouch(params.Range, diagnosticRange) {
			continue
		}
		if fix, ok := syntaxQuickFixFor(file, diagnostic); ok && syntaxQuickFixReparses(file, fix, true) {
			fix.clientDiagnostic = clientDiagnostic
			fixes = append(fixes, fix)
		}
		for _, fix := range styleQuickFixesFor(file, diagnostic) {
			if !syntaxQuickFixReparses(file, fix, false) {
				continue
			}
			fix.clientDiagnostic = clientDiagnostic
			fixes = append(fixes, fix)
		}
	}
	if len(fixes) == 0 {
		return []protocol.CommandOrCodeAction{}, s.structureCurrent(ctx, snapshot)
	}
	kind := protocol.CodeActionKindQuickFix
	version, versioned := snapshot.Version()
	var versionPointer *int32
	if versioned {
		versionPointer = &version
	}
	actions := make([]protocol.CommandOrCodeAction, 0, len(fixes))
	for _, fix := range fixes {
		editRange, validEditRange := protocolRange(snapshot, encoding, fix.span)
		if !validEditRange {
			continue
		}
		preferred := fix.preferred
		edit := &protocol.TextEdit{Range: editRange, NewText: fix.newText}
		workspaceEdit, err := s.clientWorkspaceEdit([]protocol.DocumentChange{&protocol.TextDocumentEdit{
			TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{TextDocumentIdentifier: params.TextDocument, Version: versionPointer},
			Edits:        []protocol.TextDocumentEditElement{edit},
		}})
		if err != nil {
			return nil, err
		}
		action := &protocol.CodeAction{
			Title:       fix.title,
			Kind:        &kind,
			Diagnostics: []protocol.Diagnostic{fix.clientDiagnostic},
			IsPreferred: &preferred,
			Edit:        workspaceEdit,
		}
		actions = append(actions, action)
	}
	if err := s.structureCurrent(ctx, snapshot); err != nil {
		return nil, err
	}
	return actions, nil
}

func syntaxQuickFixFor(file *syntax.File, diagnostic syntax.Diagnostic) (syntaxQuickFix, bool) {
	if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(file.Source) {
		return syntaxQuickFix{}, false
	}
	fix := syntaxQuickFix{diagnostic: diagnostic, preferred: true}
	switch diagnostic.Code {
	case "vim/E170", "vim/E171", "vim/E600":
		block, endCommand, ok := missingEndBlock(file, diagnostic)
		if !ok {
			return syntaxQuickFix{}, false
		}
		indent := lineIndent(file.Source, file.Commands[block.Header].Span.Start)
		fix.span = syntax.Span{Start: len(file.Source), End: len(file.Source)}
		fix.newText = indent + endCommand + "\n"
		if file.Source != "" && !strings.HasSuffix(file.Source, "\n") {
			fix.newText = "\n" + fix.newText
		}
		fix.title = "Insert :" + endCommand
	case "vim/E475":
		if !missingFunctionParameterEnd(file, diagnostic) {
			return syntaxQuickFix{}, false
		}
		fix.span = syntax.Span{Start: diagnostic.Span.End, End: diagnostic.Span.End}
		fix.newText = ")"
		fix.title = "Insert missing )"
	case "vimls/missing-method-call":
		if diagnostic.Span.Start != diagnostic.Span.End {
			return syntaxQuickFix{}, false
		}
		fix.span = diagnostic.Span
		fix.newText = "()"
		fix.title = "Insert missing ()"
	case "vim/E1123":
		if diagnostic.Message != "Missing comma before argument" {
			return syntaxQuickFix{}, false
		}
		start := diagnostic.Span.Start
		for start > 0 && (file.Source[start-1] == ' ' || file.Source[start-1] == '\t') {
			start--
		}
		if start == diagnostic.Span.Start || start == 0 || file.Source[start-1] == '\n' || file.Source[start-1] == '\r' {
			return syntaxQuickFix{}, false
		}
		fix.span = syntax.Span{Start: start, End: diagnostic.Span.Start}
		fix.newText = ", "
		fix.title = "Insert missing comma"
	default:
		return syntaxQuickFix{}, false
	}
	return fix, true
}

func styleQuickFixesFor(file *syntax.File, diagnostic syntax.Diagnostic) []syntaxQuickFix {
	fix := syntaxQuickFix{diagnostic: diagnostic, preferred: true}
	switch diagnostic.Code {
	case "vimls/normal-without-bang":
		var fixes []syntaxQuickFix
		walkCommands(file.Commands, func(command *syntax.Command) {
			if len(fixes) > 0 {
				return
			}
			if command.Canonical == "normal" && command.Name == diagnostic.Span && command.Bang.Start == command.Bang.End {
				fix.span = syntax.Span{Start: command.Name.End, End: command.Name.End}
				fix.newText = "!"
				fix.title = "Use :normal!"
				fixes = append(fixes, fix)
			}
		})
		if len(fixes) > 0 {
			return fixes
		}
	case "vim/E174":
		var fixes []syntaxQuickFix
		walkCommands(file.Commands, func(command *syntax.Command) {
			if len(fixes) > 0 {
				return
			}
			if command.Canonical == "command" && command.Bang.Start == command.Bang.End {
				if _, span, _, ok := syntax.DefinedUserCommand(file, command); ok && span == diagnostic.Span {
					fix.span = syntax.Span{Start: command.Name.End, End: command.Name.End}
					fix.newText = "!"
					fix.title = "Use :command!"
					fixes = append(fixes, fix)
				}
			}
		})
		if len(fixes) > 0 {
			return fixes
		}
	case "vim/E122":
		var fixes []syntaxQuickFix
		walkCommands(file.Commands, func(command *syntax.Command) {
			if len(fixes) > 0 {
				return
			}
			if command.Canonical == "function" && command.Function != nil && command.Bang.Start == command.Bang.End {
				if command.Function.Name == diagnostic.Span {
					fix.span = syntax.Span{Start: command.Name.End, End: command.Name.End}
					fix.newText = "!"
					fix.title = "Use :function!"
					fixes = append(fixes, fix)
				}
			}
		})
		if len(fixes) > 0 {
			return fixes
		}
	case "vimls/function-without-abort":
		var fixes []syntaxQuickFix
		walkCommands(file.Commands, func(command *syntax.Command) {
			if len(fixes) > 0 {
				return
			}
			if command.Canonical == "function" && command.Function != nil && command.Name == diagnostic.Span && !strings.Contains(file.Text(command.Argument), "abort") {
				fix.span = syntax.Span{Start: command.Argument.End, End: command.Argument.End}
				fix.newText = " abort"
				fix.title = "Add abort"
				fixes = append(fixes, fix)
			}
		})
		if len(fixes) > 0 {
			return fixes
		}
	case "vimls/implicit-string-case", "vimls/implicit-pattern-case":
		operator := file.Text(diagnostic.Span)
		caseSensitive, caseInsensitive, ok := explicitCaseOperators(operator)
		if !ok {
			return nil
		}
		fix.span = diagnostic.Span
		fix.newText = caseSensitive
		fix.title = "Use case-sensitive comparison"
		fix.preferred = false
		alternative := fix
		alternative.newText = caseInsensitive
		alternative.title = "Use case-insensitive comparison"
		return []syntaxQuickFix{fix, alternative}
	}
	return nil
}

func explicitCaseOperators(operator string) (string, string, bool) {
	switch operator {
	case "==":
		return "==#", "==?", true
	case "!=":
		return "!=#", "!=?", true
	case "is":
		return "is#", "is?", true
	case "isnot":
		return "isnot#", "isnot?", true
	case "=~":
		return "=~#", "=~?", true
	case "!~":
		return "!~#", "!~?", true
	default:
		return "", "", false
	}
}

func missingFunctionParameterEnd(file *syntax.File, diagnostic syntax.Diagnostic) bool {
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Function == nil || diagnostic.Span.End != command.Argument.End || diagnostic.Span.Start < command.Function.Name.End {
			continue
		}
		return !strings.Contains(file.Text(command.Argument), ")")
	}
	return false
}

func syntaxQuickFixReparses(file *syntax.File, fix syntaxQuickFix, resolvesDiagnostic bool) bool {
	if fix.span.Start < 0 || fix.span.End < fix.span.Start || fix.span.End > len(file.Source) {
		return false
	}
	source := file.Source[:fix.span.Start] + fix.newText + file.Source[fix.span.End:]
	reparsed := syntax.Parse(source)
	if resolvesDiagnostic && len(reparsed.Diagnostics) >= len(file.Diagnostics) {
		return false
	}
	if !resolvesDiagnostic && len(reparsed.Diagnostics) != len(file.Diagnostics) {
		return false
	}
	if resolvesDiagnostic {
		for _, diagnostic := range reparsed.Diagnostics {
			if diagnostic.Code == fix.diagnostic.Code && diagnostic.Message == fix.diagnostic.Message {
				return false
			}
		}
	}
	return true
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

func matchingClientDiagnostic(diagnostics []protocol.Diagnostic, code string, rangeValue protocol.Range) (protocol.Diagnostic, bool) {
	for _, diagnostic := range diagnostics {
		if value, ok := clientDiagnosticCode(diagnostic); ok && value == code && diagnostic.Range == rangeValue {
			return diagnostic, true
		}
	}
	return protocol.Diagnostic{}, false
}

func clientDiagnosticCode(diagnostic protocol.Diagnostic) (string, bool) {
	value, ok := diagnostic.Code.(protocol.String)
	return string(value), ok
}

func quickFixDiagnosticCode(code string) bool {
	switch code {
	case "vim/E122", "vim/E170", "vim/E171", "vim/E174", "vim/E475", "vim/E600", "vim/E1123", "vimls/missing-method-call", "vimls/normal-without-bang", "vimls/function-without-abort", "vimls/implicit-string-case", "vimls/implicit-pattern-case":
		return true
	default:
		return false
	}
}

func hasDiagnostic(diagnostics []syntax.Diagnostic, code string, span syntax.Span) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && diagnostic.Span == span {
			return true
		}
	}
	return false
}

func rangesOverlapOrTouch(left, right protocol.Range) bool {
	if left.Start == left.End {
		return !positionLess(left.Start, right.Start) && !positionLess(right.End, left.Start)
	}
	if right.Start == right.End {
		return !positionLess(right.Start, left.Start) && !positionLess(left.End, right.Start)
	}
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
	snapshot, file, fileAnalysis, encoding, err := s.structureDocumentWithAnalysis(ctx, params.TextDocument.URI.String())
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
	if fileAnalysis == nil {
		fileAnalysis = analysis.Analyze(file)
	}
	explicit := explicitTypeDeclarations(file)
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
