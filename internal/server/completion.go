package server

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/vimdata"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type completionSelection struct {
	start, cursor, end int
	prefix             string
}

type completionCandidate struct {
	item   protocol.CompletionItem
	score  int
	source completionSource
}

type completionSource uint8

const (
	completionSourceLocal completionSource = iota
	completionSourceImport
	completionSourceBuiltin
	completionSourceCommand
)

func completionPathPredicate(state workspaceNavigationSnapshot, excludeRuntimePath bool) func(string) bool {
	if !excludeRuntimePath {
		return nil
	}
	return func(path string) bool {
		return !workspacePathInRoots(path, state.runtimePaths) || workspacePathInRoots(path, state.workspaceRoots)
	}
}

func (s *Server) Completion(ctx context.Context, params *protocol.CompletionParams) (protocol.CompletionResult, error) {
	if ctx.Err() != nil {
		return nil, protocol.ErrRequestCancelled
	}
	s.mu.Lock()
	excludeRuntimePath := s.excludeRuntimePathCompletions
	s.mu.Unlock()
	for attempt := range 2 {
		snapshot, file, analysisResult, encoding, err := s.structureDocumentWithAnalysis(ctx, params.TextDocument.URI.String())
		if err != nil {
			return nil, err
		}
		if snapshot == nil || file == nil {
			return &protocol.CompletionList{Items: []protocol.CompletionItem{}}, nil
		}
		configFile := s.configFileRoleForURI(snapshot.URI())
		offset, err := snapshot.Offset(fromProtocolPosition(params.Position), encoding)
		if err != nil {
			return &protocol.CompletionList{Items: []protocol.CompletionItem{}}, s.structureCurrent(ctx, snapshot)
		}
		contextKind := completionContextAt(file, offset)
		selection := completionSelectionAt(snapshot.Text(), offset)
		switch contextKind {
		case completionContextMappingArgument:
			selection = completionMappingArgumentSelection(snapshot.Text(), offset)
		case completionContextSetOperator, completionContextSetValue, completionContextUserCommandAttribute, completionContextUserCommandAttributeValue:
			selection = completionCommandPartSelection(file, offset, contextKind)
		case completionContextAutocmdPattern:
			selection = completionAutocmdPatternSelection(snapshot.Text(), offset)
		case completionContextAutocmdModifier:
			selection = completionAutocmdPartSelection(snapshot.Text(), offset)
		case completionContextHighlightKey, completionContextHighlightValue:
			selection = completionHighlightSelection(snapshot.Text(), file, offset, contextKind == completionContextHighlightValue)
		case completionContextColorscheme:
			selection = completionColorschemeSelection(snapshot.Text(), offset)
		case completionContextHasFeature, completionContextExpandSpecial:
			selection = completionBuiltinStringSelection(file, offset, contextKind)
		case completionContextMethod:
			selection = completionMethodSelection(file, offset)
		}
		if contextKind == completionContextImportPath {
			selection = completionImportPathSelection(snapshot.Text(), offset)
			state := s.captureWorkspaceNavigationState()
			from, ok := workspaceURIPath(uri.URI(snapshot.URI()))
			if !ok {
				return s.completionList(snapshot, encoding, selection, nil), nil
			}
			var paths []workspace.PathCompletion
			var truncated bool
			acceptPath := completionPathPredicate(state, excludeRuntimePath)
			if workspace.RuntimeImportCompletionPrefix(selection.prefix) {
				if state.index == nil {
					return s.completionList(snapshot, encoding, selection, nil), nil
				}
				directory := "import"
				if importAutoloadAt(file, offset) {
					directory = "autoload"
				}
				paths, truncated = state.index.RuntimePathCompletions(directory, selection.prefix, maxCompletionItems, acceptPath)
			} else {
				if state.resolver == nil {
					return s.completionList(snapshot, encoding, selection, nil), nil
				}
				paths, truncated = state.resolver.ImportPathCompletions(from, selection.prefix, importAutoloadAt(file, offset), maxCompletionItems, acceptPath)
			}
			items := make(map[string]completionCandidate, len(paths))
			for _, path := range paths {
				items[path.Display] = completionCandidate{item: protocol.CompletionItem{Label: path.Display, Kind: importPathKind(path.IsDir)}, score: 8500, source: completionSourceImport}
			}
			document := navigationDocument{server: s, snapshot: snapshot}
			current, err := document.workspaceNavigationCurrent(ctx, state, workspaceNavigationTarget{})
			if err != nil {
				return nil, err
			}
			if !current {
				if attempt == 1 {
					return nil, protocol.ErrContentModified
				}
				continue
			}
			result := s.completionList(snapshot, encoding, selection, items)
			if ctx.Err() != nil {
				return nil, protocol.ErrRequestCancelled
			}
			result.IsIncomplete = result.IsIncomplete || truncated
			if len(result.Items) == 0 {
				result.IsIncomplete = true
			}
			return result, nil
		}
		if contextKind == completionContextColorscheme {
			state := s.captureWorkspaceNavigationState()
			if state.index == nil {
				return s.completionList(snapshot, encoding, selection, nil), nil
			}
			paths, truncated := state.index.ColorSchemeCompletions(selection.prefix, maxCompletionItems, completionPathPredicate(state, excludeRuntimePath))
			items := make(map[string]completionCandidate, len(paths))
			for _, path := range paths {
				items[path.Display] = completionCandidate{item: protocol.CompletionItem{Label: path.Display, Kind: protocol.CompletionItemKindValue}, score: 8500, source: completionSourceImport}
			}
			document := navigationDocument{server: s, snapshot: snapshot}
			current, err := document.workspaceNavigationCurrent(ctx, state, workspaceNavigationTarget{})
			if err != nil {
				return nil, err
			}
			if !current {
				if attempt == 1 {
					return nil, protocol.ErrContentModified
				}
				continue
			}
			result := s.completionList(snapshot, encoding, selection, items)
			result.IsIncomplete = result.IsIncomplete || truncated
			return result, nil
		}
		if alias, member := importMemberContext(snapshot.Text(), offset); member && importAlias(file, alias) {
			state := s.captureWorkspaceNavigationState()
			items, target := s.importMemberCompletionsInState(snapshot.URI(), file, alias, state)
			if acceptPath := completionPathPredicate(state, excludeRuntimePath); acceptPath != nil && !acceptPath(target.match.Fact.Path) {
				items = nil
			}
			document := navigationDocument{server: s, snapshot: snapshot}
			current, err := document.workspaceNavigationCurrent(ctx, state, target)
			if err != nil {
				return nil, err
			}
			if current {
				result := s.completionList(snapshot, encoding, selection, completionCandidates(items, 8500, completionSourceImport))
				if ctx.Err() != nil {
					return nil, protocol.ErrRequestCancelled
				}
				if len(result.Items) == 0 {
					result.IsIncomplete = true
				}
				return result, nil
			}
			if attempt == 1 {
				return nil, protocol.ErrContentModified
			}
			continue
		}
		if contextKind == completionContextMember {
			items, deep := completionObjectMembers(file, analysisResult, offset)
			result := s.completionList(snapshot, encoding, selection, completionCandidates(items, 9000, completionSourceLocal))
			if ctx.Err() != nil {
				return nil, protocol.ErrRequestCancelled
			}
			result.IsIncomplete = result.IsIncomplete || deep
			if len(result.Items) == 0 {
				result.IsIncomplete = true
			}
			return result, s.structureCurrent(ctx, snapshot)
		}
		if contextKind == completionContextNone {
			return &protocol.CompletionList{Items: []protocol.CompletionItem{}}, s.structureCurrent(ctx, snapshot)
		}
		s.mu.Lock()
		canSnippet := s.completion.snippet
		s.mu.Unlock()
		started := s.completionNow()
		budgetExpired := false
		workspaceIncomplete := false
		var completionWorkspaceState workspaceNavigationSnapshot
		completionWorkspaceStateUsed := false
		candidates := make(map[string]completionCandidate)
		add := func(item protocol.CompletionItem, score int, source completionSource) bool {
			if ctx.Err() != nil {
				return false
			}
			if s.completionNow().Sub(started) >= completionBudget {
				budgetExpired = true
				return false
			}
			if item.Label == "" || !completionTextMatches(selection.prefix, item.Label) {
				return true
			}
			if previous, ok := candidates[item.Label]; !ok || score > previous.score || score == previous.score && source < previous.source {
				candidates[item.Label] = completionCandidate{item: item, score: score, source: source}
			}
			return true
		}
		addCommandCandidates := func() {
			for _, command := range vimdata.Commands() {
				if !completionTextMatches(selection.prefix, command.Name) {
					continue
				}
				item := protocol.CompletionItem{Label: command.Name, Kind: protocol.CompletionItemKindKeyword, Detail: protocol.NewOptional("Ex command")}
				if snippet, ok := commandBlockSnippet(command.Name, file.Dialect, canSnippet, configFile); ok {
					item.InsertText = protocol.NewOptional(snippet)
					item.InsertTextFormat = protocol.InsertTextFormatSnippet
				}
				if !add(item, 8000, completionSourceCommand) {
					break
				}
			}
			for _, item := range completionSnippetItems(file.Dialect, canSnippet, configFile) {
				if !add(item, 7500, completionSourceCommand) {
					break
				}
			}
			userCommandState := s.captureWorkspaceNavigationState()
			s.workspaceMu.Lock()
			if s.workspaceIndexReadyLocked() && s.workspaceIndex.Complete() {
				for _, name := range s.workspaceIndex.UserCommandCompletionNames(completionPathPredicate(userCommandState, excludeRuntimePath)) {
					if !completionTextMatches(selection.prefix, name) {
						continue
					}
					if !add(protocol.CompletionItem{Label: name, Kind: protocol.CompletionItemKindFunction, Detail: protocol.NewOptional("user command")}, 7500, completionSourceCommand) {
						break
					}
				}
			} else {
				workspaceIncomplete = true
			}
			s.workspaceMu.Unlock()
		}
		if contextKind == completionContextHasFeature || contextKind == completionContextExpandSpecial {
			values := vimdata.HasFeatures()
			detail := "has() feature"
			if contextKind == completionContextExpandSpecial {
				values = vimdata.ExpandSpecials()
				detail = "expand() special token"
			}
			for _, value := range values {
				item := protocol.CompletionItem{
					Label:         value.Name,
					Kind:          protocol.CompletionItemKindEnumMember,
					Detail:        protocol.NewOptional(detail),
					Documentation: protocol.String(value.Documentation),
				}
				if !add(item, 8000, completionSourceBuiltin) {
					break
				}
			}
		} else if contextKind == completionContextExpression || contextKind == completionContextMethod || contextKind == completionContextVim9Statement {
			methodCall := contextKind == completionContextMethod
			scopePrefix := completionScopePrefixAt(snapshot.Text(), selection.start)
			insideCallable := completionInsideCallable(analysisResult, offset)
			if !methodCall && scopePrefix != "" {
				selection.start -= 2
				selection.prefix = snapshot.Text()[selection.start:selection.cursor]
			}
			if !methodCall && scopePrefix == "v:" {
				for _, variable := range vimdata.Variables() {
					if !add(protocol.CompletionItem{Label: variable.Name, Kind: protocol.CompletionItemKindConstant, Detail: protocol.NewOptional("variable: " + variable.Type)}, 8000, completionSourceBuiltin) {
						break
					}
				}
			} else if selection.start > 0 && snapshot.Text()[selection.start-1] == '&' {
				selection.start--
				selection.prefix = snapshot.Text()[selection.start:selection.cursor]
				for _, option := range vimdata.Options() {
					if !add(protocol.CompletionItem{Label: "&" + scopePrefix + option.Name, Kind: protocol.CompletionItemKindProperty, Detail: protocol.NewOptional(completionOptionDetail(option))}, 8000, completionSourceBuiltin) {
						break
					}
				}
			}
			for _, visible := range visibleCompletionDeclarations(analysisResult, offset) {
				declaration := visible.declaration
				if methodCall && declaration.Kind != analysis.SymbolKindFunction {
					continue
				}
				label := completionDeclarationLabel(declaration, analysisResult.Root, file.Dialect, scopePrefix)
				if label == "" {
					continue
				}
				item := protocol.CompletionItem{Label: label, Kind: completionSymbolKind(declaration.Kind)}
				var (
					snippet string
					ok      bool
				)
				if methodCall {
					parameters, found := completionUserFunctionParameters(file, label)
					if found && len(parameters) == 0 {
						continue
					}
					if found && len(parameters) > 0 {
						snippet, ok = completionFunctionSnippet(label, parameters[1:], canSnippet)
					}
				} else {
					snippet, ok = completionUserFunctionSnippet(file, label, canSnippet)
				}
				if ok {
					item.InsertText = protocol.NewOptional(snippet)
					item.InsertTextFormat = protocol.InsertTextFormatSnippet
					item.FilterText = protocol.NewOptional(label)
				}
				if declaration.Deprecated {
					item.Tags = []protocol.CompletionItemTag{protocol.CompletionItemTagDeprecated}
				}
				item.Detail = protocol.NewOptional(string(declaration.Kind))
				if declaration.Type.Name != "" && declaration.Type.Name != analysis.ValueTypeAny {
					item.Detail = protocol.NewOptional(string(declaration.Kind) + ": " + formatValueType(declaration.Type))
				}
				score := 10000
				for depth := 0; depth < visible.scopeDepth; depth++ {
					score = score * 99 / 100
				}
				// A g: value declared by a user configuration file is
				// commonly the setting being edited. Prefer it over otherwise
				// equal unscoped declarations without changing the candidate
				// set or overriding an explicit scope prefix.
				if configFile && scopePrefix == "" && !insideCallable && strings.HasPrefix(declaration.Name, "g:") {
					score++
				}
				if declaration.Deprecated {
					score /= 2
				}
				if !add(item, score, completionSourceLocal) {
					break
				}
			}
			completionWorkspaceState = s.captureWorkspaceNavigationState()
			if completionWorkspaceState.index == nil {
				workspaceIncomplete = true
			} else if scopePrefix == "" || scopePrefix == "g:" {
				completionWorkspaceStateUsed = true
				workspacePrefix := strings.TrimPrefix(selection.prefix, "g:")
				labelPrefix := ""
				if scopePrefix == "g:" {
					labelPrefix = "g:"
				}
				functions, incomplete := completionWorkspaceState.index.FunctionCompletionsMatching(func(name string) bool {
					return completionTextMatches(workspacePrefix, name)
				}, file.Dialect == syntax.Legacy || scopePrefix == "g:", maxCompletionItems, completionPathPredicate(completionWorkspaceState, excludeRuntimePath))
				workspaceIncomplete = workspaceIncomplete || incomplete
				for _, function := range functions {
					if methodCall && len(function.Parameters) == 0 {
						continue
					}
					label := labelPrefix + function.Name
					detail := function.Match.Fact.Signature
					if detail != "" && label != function.Match.Fact.Name && strings.HasPrefix(detail, function.Match.Fact.Name+"(") {
						detail = label + detail[len(function.Match.Fact.Name):]
					}
					item := protocol.CompletionItem{Label: label, Kind: protocol.CompletionItemKindFunction}
					parameters := function.Parameters
					if methodCall {
						parameters = parameters[1:]
					}
					if snippet, ok := completionFunctionSnippet(label, parameters, canSnippet); ok {
						item.InsertText = protocol.NewOptional(snippet)
						item.InsertTextFormat = protocol.InsertTextFormatSnippet
						item.FilterText = protocol.NewOptional(label)
					}
					if detail != "" {
						item.Detail = protocol.NewOptional(detail)
					}
					if function.Match.Fact.Documentation != "" {
						item.Documentation = protocol.String(function.Match.Fact.Documentation)
					}
					if function.Match.Fact.Deprecated {
						item.Tags = []protocol.CompletionItemTag{protocol.CompletionItemTagDeprecated}
					}
					if !add(item, 7500, completionSourceImport) {
						break
					}
				}
				if !methodCall && file.Dialect == syntax.Legacy && (scopePrefix == "g:" || !insideCallable) {
					documentPath, _ := workspaceURIPath(uri.URI(snapshot.URI()))
					variables, variablesIncomplete := completionWorkspaceState.index.GlobalVariableCompletionsMatching(func(name string) bool {
						return completionTextMatches(workspacePrefix, name)
					}, documentPath, maxCompletionItems, completionPathPredicate(completionWorkspaceState, excludeRuntimePath))
					workspaceIncomplete = workspaceIncomplete || variablesIncomplete
					for _, variable := range variables {
						if !add(protocol.CompletionItem{Label: labelPrefix + variable.Name, Kind: protocol.CompletionItemKindVariable, Detail: protocol.NewOptional("workspace global variable")}, 7500, completionSourceImport) {
							break
						}
					}
				}
			}
			for _, function := range vimdata.BuiltinFunctions() {
				if methodCall && function.MethodArgument == 0 {
					continue
				}
				if !completionTextMatches(selection.prefix, function.Name) {
					continue
				}
				item := protocol.CompletionItem{Label: function.Name, Kind: protocol.CompletionItemKindFunction}
				var (
					snippet string
					ok      bool
				)
				if methodCall {
					snippet, ok = completionBuiltinMethodSnippet(function, canSnippet)
				} else {
					snippet, ok = completionBuiltinFunctionSnippet(function, canSnippet)
				}
				if ok {
					item.InsertText = protocol.NewOptional(snippet)
					item.InsertTextFormat = protocol.InsertTextFormatSnippet
					item.FilterText = protocol.NewOptional(function.Name)
				}
				if !add(item, 8000, completionSourceBuiltin) {
					break
				}
			}
		} else if contextKind == completionContextFunctionAttribute {
			used := make(map[string]bool)
			walkCommands(file.Commands, func(command *syntax.Command) {
				current, ok := completionFunctionAttributeAt(file, command, offset)
				if !ok {
					return
				}
				for index := range command.Function.Attributes {
					attribute := &command.Function.Attributes[index]
					if current == attribute {
						continue
					}
					used[file.Text(*attribute)] = true
				}
			})
			for _, attribute := range []string{"range", "abort", "dict", "closure"} {
				if used[attribute] {
					continue
				}
				if !add(protocol.CompletionItem{Label: attribute, Kind: protocol.CompletionItemKindKeyword, Detail: protocol.NewOptional("function attribute")}, 8000, completionSourceCommand) {
					break
				}
			}
		} else if contextKind == completionContextModifier {
			for _, modifier := range vimdata.Modifiers() {
				if (!modifier.Vim9Member || completionAggregateAt(file, offset)) && len(selection.prefix) >= modifier.MinLen && completionTextMatches(selection.prefix, modifier.Name) {
					if !add(protocol.CompletionItem{Label: modifier.Name, Kind: protocol.CompletionItemKindKeyword}, 8000, completionSourceCommand) {
						break
					}
				}
			}
		} else if contextKind == completionContextSetOption {
			prefix := selection.prefix
			for _, option := range vimdata.Options() {
				if strings.HasPrefix(prefix, "no") || strings.HasPrefix(prefix, "inv") {
					if option.Type != vimdata.OptionBool {
						continue
					}
					for _, form := range []string{"no" + option.Name, "inv" + option.Name} {
						if !add(protocol.CompletionItem{Label: form, Kind: protocol.CompletionItemKindProperty, Detail: protocol.NewOptional(completionOptionDetail(option))}, 8000, completionSourceBuiltin) {
							break
						}
					}
					continue
				}
				if !add(protocol.CompletionItem{Label: option.Name, Kind: protocol.CompletionItemKindProperty, Detail: protocol.NewOptional(completionOptionDetail(option))}, 8000, completionSourceBuiltin) {
					break
				}
				if option.ShortName != "" {
					if !add(protocol.CompletionItem{Label: option.ShortName, Kind: protocol.CompletionItemKindProperty, Detail: protocol.NewOptional(completionOptionDetail(option))}, 8000, completionSourceBuiltin) {
						break
					}
				}
			}
		} else if contextKind == completionContextSetOperator {
			option := completionSetOptionAt(file, offset)
			if option != nil {
				metadata, known := vimdata.LookupOption(file.Text(option.Name))
				operators := []string{"?", "&", "&vi", "&vim"}
				if known && metadata.Type == vimdata.OptionBool {
					operators = append(operators, "!", "<")
				} else {
					operators = append(operators, "<", "=", ":", "+=", "^=", "-=")
				}
				for _, operator := range operators {
					if !add(protocol.CompletionItem{Label: operator, Kind: protocol.CompletionItemKindOperator}, 8000, completionSourceCommand) {
						break
					}
				}
			}
		} else if contextKind == completionContextSetValue {
			option := completionSetOptionAt(file, offset)
			if option != nil {
				for _, value := range vimdata.OptionValues(file.Text(option.Name)) {
					if !add(protocol.CompletionItem{Label: value, Kind: protocol.CompletionItemKindEnumMember}, 8000, completionSourceBuiltin) {
						break
					}
				}
			}
		} else if contextKind == completionContextAugroup {
			for _, name := range completionAugroups(file) {
				if name != "END" && !add(protocol.CompletionItem{Label: name, Kind: protocol.CompletionItemKindModule}, 10000, completionSourceLocal) {
					break
				}
			}
		} else if contextKind == completionContextUserCommandAttribute {
			command, current := completionUserCommandAttributeAt(file, offset)
			if command == nil {
				command = completionUserCommandAt(file, offset)
			}
			used := make(map[string]bool)
			if command != nil && command.UserCommand != nil {
				for index := range command.UserCommand.Attributes {
					attribute := &command.UserCommand.Attributes[index]
					if attribute != current {
						used[strings.ToLower(file.Text(attribute.Name))] = true
					}
				}
			}
			for _, attribute := range vimdata.UserCommandAttributes() {
				if used[strings.TrimSuffix(attribute.Name, "=")] {
					continue
				}
				item := protocol.CompletionItem{
					Label:         "-" + attribute.Name,
					Kind:          protocol.CompletionItemKindProperty,
					Detail:        protocol.NewOptional(attribute.Detail),
					Documentation: protocol.String(attribute.Documentation),
				}
				if !add(item, 8000, completionSourceCommand) {
					break
				}
			}
		} else if contextKind == completionContextUserCommandAttributeValue {
			_, attribute := completionUserCommandAttributeAt(file, offset)
			var values []vimdata.CompletionValue
			var detail string
			if attribute != nil {
				attrName := completionUserCommandAttributeName(file.Text(attribute.Name))
				switch attrName {
				case "addr":
					values = vimdata.UserCommandAddrValues()
					detail = "command address type"
				case "complete":
					if !strings.Contains(file.Text(attribute.Value), ",") {
						values = vimdata.UserCommandCompleteValues()
						detail = "command completion type"
					}
				case "completeopt":
					values = vimdata.UserCommandCompleteoptValues()
					detail = "command completion option"
				case "nargs":
					values = vimdata.UserCommandNargsValues()
					detail = "command argument count"
				}
			}
			for _, value := range values {
				item := protocol.CompletionItem{
					Label:         value.Name,
					Kind:          protocol.CompletionItemKindEnumMember,
					Detail:        protocol.NewOptional(detail),
					Documentation: protocol.String(value.Documentation),
				}
				if !add(item, 8000, completionSourceCommand) {
					break
				}
			}
		} else if contextKind == completionContextSyntaxSubcommand {
			for _, name := range []string{"keyword", "match", "region", "cluster", "case", "conceal", "spell", "include", "clear", "list", "sync", "iskeyword", "foldlevel", "enable", "manual", "on", "off", "reset"} {
				if !add(protocol.CompletionItem{Label: name, Kind: protocol.CompletionItemKindKeyword}, 8000, completionSourceCommand) {
					break
				}
			}
		} else if contextKind == completionContextSyntaxGroup || contextKind == completionContextHighlight {
			if contextKind == completionContextHighlight {
				for _, name := range []string{"default", "link", "clear", "NONE"} {
					if !add(protocol.CompletionItem{Label: name, Kind: protocol.CompletionItemKindKeyword}, 8000, completionSourceCommand) {
						break
					}
				}
			}
			for _, name := range completionGroups(file) {
				if !add(protocol.CompletionItem{Label: name, Kind: protocol.CompletionItemKindValue}, 10000, completionSourceLocal) {
					break
				}
			}
		} else if contextKind == completionContextHighlightKey {
			for _, name := range []string{"term=", "start=", "stop=", "cterm=", "ctermfg=", "ctermbg=", "ctermul=", "ctermfont=", "gui=", "guifg=", "guibg=", "guisp=", "font=", "NONE"} {
				if !add(protocol.CompletionItem{Label: name, Kind: protocol.CompletionItemKindProperty}, 8000, completionSourceCommand) {
					break
				}
			}
		} else if contextKind == completionContextHighlightValue {
			key := completionHighlightKey(file, snapshot.Text(), offset, selection.start)
			var values []string
			switch key {
			case "term", "cterm", "gui":
				values = []string{"bold", "standout", "underline", "undercurl", "underdouble", "underdotted", "underdashed", "italic", "reverse", "inverse", "nocombine", "strikethrough", "NONE"}
			case "ctermfg", "ctermbg", "ctermul":
				values = []string{"fg", "bg", "ul", "Black", "Blue", "Brown", "Cyan", "DarkBlue", "DarkCyan", "DarkGray", "DarkGreen", "DarkGrey", "DarkMagenta", "DarkRed", "DarkYellow", "Gray", "Green", "Grey", "LightBlue", "LightCyan", "LightGray", "LightGreen", "LightGrey", "LightMagenta", "LightRed", "LightYellow", "Magenta", "NONE", "Red", "White", "Yellow"}
			case "ctermfont":
				values = []string{"NONE"}
			case "guifg", "guibg", "guisp":
				values = []string{"NONE", "bg", "background", "fg", "foreground", "Red", "LightRed", "DarkRed", "Green", "LightGreen", "DarkGreen", "SeaGreen", "Blue", "LightBlue", "DarkBlue", "SlateBlue", "Cyan", "LightCyan", "DarkCyan", "Magenta", "LightMagenta", "DarkMagenta", "Yellow", "LightYellow", "Brown", "DarkYellow", "Gray", "LightGray", "DarkGray", "Black", "White", "Orange", "Purple", "Violet"}
			case "font":
				values = []string{"NONE"}
			}
			for _, value := range values {
				if !add(protocol.CompletionItem{Label: value, Kind: protocol.CompletionItemKindEnumMember}, 8000, completionSourceCommand) {
					break
				}
			}
		} else if contextKind == completionContextAutocmdHead || contextKind == completionContextAutocmdEvent {
			for _, event := range vimdata.AutocmdEvents() {
				if !add(protocol.CompletionItem{Label: event.Name, Kind: protocol.CompletionItemKindEvent}, 8000, completionSourceBuiltin) {
					break
				}
			}
			if contextKind == completionContextAutocmdHead {
				for _, name := range completionAugroups(file) {
					if !add(protocol.CompletionItem{Label: name, Kind: protocol.CompletionItemKindModule}, 10000, completionSourceLocal) {
						break
					}
				}
			}
		} else if contextKind == completionContextAutocmdPattern {
			add(protocol.CompletionItem{Label: "<buffer>", Kind: protocol.CompletionItemKindKeyword}, 8000, completionSourceCommand)
		} else if contextKind == completionContextAutocmdModifier {
			used := completionAutocmdModifiersAt(file, offset)
			for _, label := range []string{"++once", "++nested"} {
				if used[label] {
					continue
				}
				if !add(protocol.CompletionItem{Label: label, Kind: protocol.CompletionItemKindKeyword}, 8000, completionSourceCommand) {
					break
				}
			}
		} else if contextKind == completionContextMappingArgument {
			var mapping *syntax.Mapping
			walkCommands(file.Commands, func(command *syntax.Command) {
				if mapping == nil && command.Mapping != nil && command.Argument.Start <= offset && offset <= command.Argument.End {
					mapping = command.Mapping
				}
			})
			if mapping != nil {
				arguments := []struct {
					label string
					used  bool
				}{
					{label: "<buffer>", used: mapping.Buffer},
					{label: "<nowait>", used: mapping.Nowait},
					{label: "<silent>", used: mapping.Silent},
					{label: "<special>", used: mapping.Special},
					{label: "<script>", used: mapping.Script},
					{label: "<expr>", used: mapping.Expr},
					{label: "<unique>", used: mapping.Unique},
				}
				current := snapshot.Text()[selection.start:selection.end]
				for index, argument := range arguments {
					if argument.used && !strings.EqualFold(current, argument.label) || mapping.Kind == syntax.MappingClear && index != 0 {
						continue
					}
					if !add(protocol.CompletionItem{Label: argument.label, Kind: protocol.CompletionItemKindKeyword}, 8000, completionSourceCommand) {
						break
					}
				}
				// §7 P1: user configuration files get a <Leader> mapping
				// skeleton at the empty LHS position (no <unique>).
				if configFile && canSnippet && mapping.LHS.Start == mapping.LHS.End && mapping.RHS.Start == mapping.RHS.End {
					item := configMappingSkeleton()
					if completionTextMatches(selection.prefix, item.Label) && !add(item, 9000, completionSourceCommand) {
						break
					}
				}
			}
		} else {
			addCommandCandidates()
		}
		if contextKind == completionContextVim9Statement {
			addCommandCandidates()
		}
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		if err := s.structureCurrent(ctx, snapshot); err != nil {
			return nil, err
		}
		if completionWorkspaceStateUsed {
			document := navigationDocument{server: s, snapshot: snapshot}
			current, err := document.workspaceNavigationCurrent(ctx, completionWorkspaceState, workspaceNavigationTarget{})
			if err != nil {
				return nil, err
			}
			if !current {
				if attempt == 1 {
					return nil, protocol.ErrContentModified
				}
				continue
			}
		}
		result := s.completionList(snapshot, encoding, selection, candidates)
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		result.IsIncomplete = result.IsIncomplete || budgetExpired || workspaceIncomplete || len(result.Items) == 0
		return result, nil
	}
	return nil, protocol.ErrContentModified
}

func importAlias(file *syntax.File, alias string) bool {
	found := false
	walkCommands(file.Commands, func(command *syntax.Command) {
		if command.Import != nil && file.Text(command.Import.Alias) == alias {
			found = true
		}
	})
	return found
}

func completionGroups(file *syntax.File) []string {
	seen := map[string]bool{}
	walkCommands(file.Commands, func(command *syntax.Command) {
		if command.Syntax != nil {
			for _, span := range append([]syntax.Span{command.Syntax.Group}, command.Syntax.Keywords...) {
				if name := file.Text(span); name != "" {
					seen[name] = true
				}
			}
		}
		if command.Highlight != nil {
			for _, span := range []syntax.Span{command.Highlight.Group, command.Highlight.LinkTarget} {
				if name := file.Text(span); name != "" {
					seen[name] = true
				}
			}
		}
	})
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func completionAugroups(file *syntax.File) []string {
	seen := map[string]bool{}
	walkCommands(file.Commands, func(command *syntax.Command) {
		if command.Canonical == "augroup" {
			if name := strings.TrimSpace(file.Text(command.Argument)); name != "" {
				seen[strings.Fields(name)[0]] = true
			}
		}
	})
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func completionSelectionAt(source string, cursor int) completionSelection {
	if cursor < 0 {
		cursor = 0
	} else if cursor > len(source) {
		cursor = len(source)
	}
	selection := completionSelection{start: cursor, cursor: cursor, end: cursor}
	for selection.start > 0 {
		r, size := utf8.DecodeLastRuneInString(source[:selection.start])
		if r == utf8.RuneError && size == 1 || !isCompletionIdentifierRune(r) {
			break
		}
		selection.start -= size
	}
	for selection.end < len(source) {
		r, size := utf8.DecodeRuneInString(source[selection.end:])
		if r == utf8.RuneError && size == 1 || !isCompletionIdentifierRune(r) {
			break
		}
		selection.end += size
	}
	selection.prefix = source[selection.start:cursor]
	return selection
}

func completionMethodSelection(file *syntax.File, cursor int) completionSelection {
	selection := completionSelectionAt(file.Source, cursor)
	span, ok := completionMethodCallableSpanAt(file, cursor)
	if !ok || span.Start < 0 || span.Start > cursor || cursor > span.End || span.End > len(file.Source) {
		return selection
	}
	return completionSelection{start: span.Start, cursor: cursor, end: span.End, prefix: file.Source[span.Start:cursor]}
}

func completionCommandPartSelection(file *syntax.File, cursor int, contextKind completionContext) completionSelection {
	selection := completionSelectionAt(file.Source, cursor)
	switch contextKind {
	case completionContextSetOperator:
		option := completionSetOptionAt(file, cursor)
		if option != nil {
			end := max(option.Operator.End, cursor)
			return completionSelection{start: option.Name.End, cursor: cursor, end: end, prefix: file.Source[option.Name.End:cursor]}
		}
	case completionContextSetValue:
		option := completionSetOptionAt(file, cursor)
		if option != nil {
			start := option.Value.Start
			for index := option.Value.Start; index < cursor; index++ {
				if file.Source[index] == ',' {
					start = index + 1
				}
			}
			end := cursor
			for ; end < option.Value.End && file.Source[end] != ','; end++ {
			}
			return completionSelection{start: start, cursor: cursor, end: end, prefix: file.Source[start:cursor]}
		}
	case completionContextUserCommandAttribute, completionContextUserCommandAttributeValue:
		_, attribute := completionUserCommandAttributeAt(file, cursor)
		if attribute != nil {
			if contextKind == completionContextUserCommandAttribute {
				return completionSelection{start: attribute.Span.Start, cursor: cursor, end: attribute.Name.End, prefix: file.Source[attribute.Span.Start:cursor]}
			}
			start := attribute.Value.Start
			for index := start; index < cursor; index++ {
				if file.Source[index] == ',' {
					start = index + 1
				}
			}
			end := cursor
			for ; end < attribute.Value.End && file.Source[end] != ','; end++ {
			}
			return completionSelection{start: start, cursor: cursor, end: end, prefix: file.Source[start:cursor]}
		}
	}
	return selection
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func completionAutocmdPartSelection(source string, cursor int) completionSelection {
	if cursor < 0 {
		cursor = 0
	} else if cursor > len(source) {
		cursor = len(source)
	}
	start := cursor
	for start > 0 && !isSpace(source[start-1]) {
		start--
	}
	end := cursor
	for end < len(source) && !isSpace(source[end]) {
		end++
	}
	return completionSelection{start: start, cursor: cursor, end: end, prefix: source[start:cursor]}
}

func completionAutocmdPatternSelection(source string, cursor int) completionSelection {
	if cursor < 0 {
		cursor = 0
	} else if cursor > len(source) {
		cursor = len(source)
	}
	argStart := cursor
	for argStart > 0 && !isSpace(source[argStart-1]) {
		argStart--
	}
	argEnd := cursor
	for argEnd < len(source) && !isSpace(source[argEnd]) {
		argEnd++
	}

	isSep := func(i int) bool {
		if source[i] != ',' {
			return false
		}
		bsCount := 0
		for j := i - 1; j >= argStart && source[j] == '\\'; j-- {
			bsCount++
		}
		return bsCount%2 == 0
	}

	start := cursor
	for start > argStart {
		if isSep(start - 1) {
			break
		}
		start--
	}

	end := cursor
	for end < argEnd {
		if isSep(end) {
			break
		}
		end++
	}

	return completionSelection{start: start, cursor: cursor, end: end, prefix: source[start:cursor]}
}

func autocmdModifierPrefixAt(source string, command *syntax.AutocmdCommand, offset int) bool {
	if command == nil || command.Pattern.End >= offset || offset > len(source) {
		return false
	}
	start := offset
	for start > command.Pattern.End && !isSpace(source[start-1]) {
		start--
	}
	if start == offset || source[start] != '+' {
		return false
	}
	previous := command.Pattern.End
	for _, modifier := range command.Modifiers {
		if modifier.Span.End <= start {
			previous = modifier.Span.End
		}
	}
	return strings.TrimSpace(source[previous:start]) == ""
}

func completionAutocmdModifiersAt(file *syntax.File, offset int) map[string]bool {
	used := make(map[string]bool)
	walkCommands(file.Commands, func(command *syntax.Command) {
		if command.Autocmd == nil || !spanContains(command.Argument, offset) {
			return
		}
		for _, modifier := range command.Autocmd.Modifiers {
			if modifier.Span.End <= offset {
				used[file.Text(modifier.Span)] = true
			}
		}
	})
	return used
}

func completionSetOptionAt(file *syntax.File, offset int) *syntax.SetOption {
	var foundOption *syntax.SetOption
	walkCommands(file.Commands, func(command *syntax.Command) {
		if foundOption != nil || command.Set == nil {
			return
		}
		for index := range command.Set.Options {
			option := &command.Set.Options[index]
			if option.Span.Start <= offset && offset <= option.Span.End {
				foundOption = option
				return
			}
		}
	})
	return foundOption
}

func completionUserCommandAttributeAt(file *syntax.File, offset int) (*syntax.Command, *syntax.UserCommandAttribute) {
	var foundCommand *syntax.Command
	var foundAttribute *syntax.UserCommandAttribute
	walkCommands(file.Commands, func(command *syntax.Command) {
		if foundAttribute != nil || command.UserCommand == nil {
			return
		}
		for index := range command.UserCommand.Attributes {
			attribute := &command.UserCommand.Attributes[index]
			if attribute.Span.Start <= offset && offset <= attribute.Span.End {
				foundCommand, foundAttribute = command, attribute
				return
			}
		}
	})
	return foundCommand, foundAttribute
}

func completionUserCommandAt(file *syntax.File, offset int) *syntax.Command {
	var found *syntax.Command
	walkCommands(file.Commands, func(command *syntax.Command) {
		if found == nil && command.Canonical == "command" && command.UserCommand != nil && (spanContains(command.Span, offset) || offset == command.Span.End) {
			found = command
		}
	})
	return found
}

func completionUserCommandAttributeName(name string) string {
	name = strings.ToLower(name)
	for _, canonical := range []string{"complete", "completeopt", "addr", "nargs"} {
		if name != "" && strings.HasPrefix(canonical, name) {
			return canonical
		}
	}
	return name
}

func completionBuiltinStringSelection(file *syntax.File, cursor int, contextKind completionContext) completionSelection {
	selection := completionSelectionAt(file.Source, cursor)
	foundContext, argument := completionBuiltinStringAt(file, cursor)
	if foundContext != contextKind || argument == nil {
		return selection
	}
	content, ok := completionStringContent(file, argument)
	if !ok {
		return selection
	}
	end := content.End
	if contextKind == completionContextExpandSpecial {
		if colon := strings.IndexByte(file.Source[content.Start:content.End], ':'); colon >= 0 {
			end = content.Start + colon
		}
	}
	if cursor > end {
		return selection
	}
	return completionSelection{start: content.Start, cursor: cursor, end: end, prefix: file.Source[content.Start:cursor]}
}

func completionColorschemeSelection(source string, cursor int) completionSelection {
	if cursor < 0 {
		cursor = 0
	} else if cursor > len(source) {
		cursor = len(source)
	}
	start, end := cursor, cursor
	for start > 0 && !isCompletionSpace(source[start-1]) && source[start-1] != '|' {
		start--
	}
	for end < len(source) && !isCompletionSpace(source[end]) && source[end] != '|' {
		end++
	}
	return completionSelection{start: start, cursor: cursor, end: end, prefix: source[start:cursor]}
}

func completionMappingArgumentSelection(source string, cursor int) completionSelection {
	if cursor < 0 {
		cursor = 0
	} else if cursor > len(source) {
		cursor = len(source)
	}
	start, end := cursor, cursor
	for start > 0 && source[start-1] != '<' && source[start-1] != ' ' && source[start-1] != '\t' && source[start-1] != '\n' && source[start-1] != '\r' {
		start--
	}
	if start > 0 && source[start-1] == '<' {
		start--
	}
	for end < len(source) && source[end] != '>' && source[end] != ' ' && source[end] != '\t' && source[end] != '\n' && source[end] != '\r' {
		end++
	}
	if end < len(source) && source[end] == '>' {
		end++
	}
	return completionSelection{start: start, cursor: cursor, end: end, prefix: source[start:cursor]}
}

func completionHighlightSelection(source string, file *syntax.File, cursor int, value bool) completionSelection {
	if cursor < 0 {
		cursor = 0
	} else if cursor > len(source) {
		cursor = len(source)
	}
	start, end := cursor, cursor
	var attribute *syntax.HighlightAttribute
	want := completionContextHighlightKey
	if value {
		want = completionContextHighlightValue
	}
	walkCommands(file.Commands, func(command *syntax.Command) {
		contextKind, ok := completionHighlightContextAt(file, command, cursor)
		if attribute == nil && ok && contextKind == want {
			attribute = completionHighlightAttributeAt(command, cursor, value)
		}
	})
	if attribute != nil {
		if value && attribute.Value.Start < attribute.Value.End {
			start, end = attribute.Value.Start, attribute.Value.End
		} else if !value {
			start, end = attribute.Key.Start, attribute.Key.End
			if attribute.Equal.Start < attribute.Equal.End {
				end = attribute.Equal.End
			}
		}
	}
	if start != cursor || end != cursor {
		if value {
			if comma := strings.LastIndexByte(source[start:cursor], ','); comma >= 0 {
				start += comma + 1
			}
			if comma := strings.IndexByte(source[cursor:end], ','); comma >= 0 {
				end = cursor + comma
			}
		}
		return completionSelection{start: start, cursor: cursor, end: end, prefix: source[start:cursor]}
	}
	for start > 0 && !isCompletionSpace(source[start-1]) {
		start--
	}
	for end < len(source) && !isCompletionSpace(source[end]) {
		end++
	}
	equal := strings.IndexByte(source[start:end], '=')
	if value && equal >= 0 {
		start += equal + 1
		if comma := strings.LastIndexByte(source[start:cursor], ','); comma >= 0 {
			start += comma + 1
		}
		if comma := strings.IndexByte(source[cursor:end], ','); comma >= 0 {
			end = cursor + comma
		}
	} else if equal >= 0 {
		end = start + equal + 1
	}
	return completionSelection{start: start, cursor: cursor, end: end, prefix: source[start:cursor]}
}

func completionHighlightKey(file *syntax.File, source string, cursor, valueStart int) string {
	var key string
	walkCommands(file.Commands, func(command *syntax.Command) {
		contextKind, ok := completionHighlightContextAt(file, command, cursor)
		if key == "" && ok && contextKind == completionContextHighlightValue {
			if attribute := completionHighlightAttributeAt(command, cursor, true); attribute != nil {
				key = file.Text(attribute.Key)
			}
		}
	})
	if key != "" {
		return strings.ToLower(key)
	}
	start := valueStart
	for start > 0 && !isCompletionSpace(source[start-1]) {
		start--
	}
	key, _, _ = strings.Cut(source[start:valueStart], "=")
	return strings.ToLower(key)
}

func isCompletionIdentifierRune(r rune) bool {
	return r == '_' || r == '#' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func completionScopePrefixAt(source string, start int) string {
	if start < 2 || source[start-1] != ':' || !strings.ContainsRune("sglabwtv", rune(source[start-2])) {
		return ""
	}
	return source[start-2 : start]
}

func completionDeclarationLabel(declaration *analysis.Declaration, root *analysis.Scope, dialect syntax.Dialect, scopePrefix string) string {
	if declaration == nil || scopePrefix == "" || strings.HasPrefix(declaration.Name, scopePrefix) {
		if declaration == nil {
			return ""
		}
		return declaration.Name
	}
	if dialect != syntax.Legacy || strings.Contains(declaration.Name, ":") {
		return ""
	}
	if scopePrefix == "a:" && declaration.Parameter {
		return scopePrefix + declaration.Name
	}
	if scopePrefix == "l:" && !declaration.Parameter && declaration.Scope != root && (declaration.Kind == analysis.SymbolKindVariable || declaration.Kind == analysis.SymbolKindConstant) {
		return scopePrefix + declaration.Name
	}
	return ""
}

func completionFunctionSnippet(name string, parameters []string, enabled bool) (string, bool) {
	if !enabled {
		return name, false
	}
	var builder strings.Builder
	builder.WriteString(name)
	builder.WriteByte('(')
	for index, parameter := range parameters {
		if index > 0 {
			builder.WriteString(", ")
		}
		parameter = strings.NewReplacer("\\", "\\\\", "}", "\\}", "$", "\\$").Replace(parameter)
		fmt.Fprintf(&builder, "${%d:%s}", index+1, parameter)
	}
	builder.WriteString(")$0")
	return builder.String(), true
}

func completionUserFunctionSnippet(file *syntax.File, name string, enabled bool) (string, bool) {
	parameters, found := completionUserFunctionParameters(file, name)
	return completionFunctionSnippet(name, parameters, enabled && found)
}

func completionUserFunctionParameters(file *syntax.File, name string) ([]string, bool) {
	if file == nil {
		return nil, false
	}
	var parameters []string
	found := false
	walkCommands(file.Commands, func(command *syntax.Command) {
		if found || command.Function == nil || file.Text(command.Function.Name) != name {
			return
		}
		found = true
		for _, parameter := range command.Function.Parameters {
			if parameter.Name.Start < parameter.Name.End {
				label := file.Text(parameter.Name)
				if parameter.Variadic && !strings.HasPrefix(label, "...") {
					label = "..." + label
				}
				parameters = append(parameters, label)
			}
		}
	})
	return parameters, found
}

func completionBuiltinFunctionSnippet(function vimdata.BuiltinFunction, enabled bool) (string, bool) {
	_, parameters := formatBuiltinFunctionSignature(function)
	labels := completionRequiredParameterLabels(parameters, function.MinArgs)
	return completionFunctionSnippet(function.Name, labels, enabled)
}

func completionBuiltinMethodSnippet(function vimdata.BuiltinFunction, enabled bool) (string, bool) {
	_, parameters := formatBuiltinMethodSignature(function)
	required := function.MinArgs - 1
	if beforeReceiver := function.MethodArgument - 1; required < beforeReceiver {
		required = beforeReceiver
	}
	labels := completionRequiredParameterLabels(parameters, required)
	return completionFunctionSnippet(function.Name, labels, enabled)
}

func completionRequiredParameterLabels(parameters []protocol.ParameterInformation, required int) []string {
	labels := make([]string, 0, required)
	for _, parameter := range parameters {
		if len(labels) == required {
			break
		}
		if label, ok := parameter.Label.(protocol.String); ok {
			labels = append(labels, string(label))
		}
	}
	for len(labels) < required {
		labels = append(labels, fmt.Sprintf("arg%d", len(labels)+1))
	}
	return labels
}

func completionCandidates(items protocol.CompletionItemSlice, score int, source completionSource) map[string]completionCandidate {
	result := make(map[string]completionCandidate, len(items))
	for _, item := range items {
		itemScore := score
		if strings.Contains(item.Label, ".") {
			itemScore = itemScore * 9 / 10
		}
		result[item.Label] = completionCandidate{item: item, score: itemScore, source: source}
	}
	return result
}

type visibleCompletionDeclaration struct {
	declaration *analysis.Declaration
	scopeDepth  int
}

func visibleCompletionDeclarations(result *analysis.FileAnalysis, offset int) []visibleCompletionDeclaration {
	if result == nil {
		return nil
	}
	var scope *analysis.Scope
	for _, candidate := range result.Scopes {
		if candidate.Span.Start <= offset && offset <= candidate.Span.End && (scope == nil || candidate.Span.End-candidate.Span.Start < scope.Span.End-scope.Span.Start) {
			scope = candidate
		}
	}
	if scope == nil {
		scope = result.Root
	}
	seen := make(map[string]bool)
	var declarations []visibleCompletionDeclaration
	depth := 0
	for current := scope; current != nil; current, depth = current.Parent, depth+1 {
		for _, declaration := range slices.Backward(current.Declarations) {
			if seen[declaration.Name] || declaration.Span.Start > offset && declaration.Kind != analysis.SymbolKindFunction && declaration.Kind != analysis.SymbolKindMethod && declaration.Kind != analysis.SymbolKindConstructor {
				continue
			}
			seen[declaration.Name] = true
			declarations = append(declarations, visibleCompletionDeclaration{declaration: declaration, scopeDepth: depth})
		}
	}
	return declarations
}

func completionInsideCallable(result *analysis.FileAnalysis, offset int) bool {
	if result == nil {
		return false
	}
	for _, scope := range result.Scopes {
		if scope != nil && scope.Span.Start <= offset && offset <= scope.Span.End && (scope.Kind == syntax.BlockFunction || scope.Kind == syntax.BlockDef || scope.Lambda != nil) {
			return true
		}
	}
	return false
}

func completionOptionDetail(option vimdata.Option) string {
	var scope string
	switch option.Scope {
	case vimdata.OptionGlobal:
		scope = "global"
	case vimdata.OptionWindow:
		scope = "window"
	case vimdata.OptionBuffer:
		scope = "buffer"
	case vimdata.OptionGlobalLocal:
		scope = "global-local"
	}
	return "option: " + optionTypeName(option) + ", " + scope
}

func optionTypeName(option vimdata.Option) string {
	switch option.Type {
	case vimdata.OptionBool:
		return "bool"
	case vimdata.OptionNumber:
		return "number"
	case vimdata.OptionString:
		return "string"
	default:
		return analysis.ValueTypeAny
	}
}

func completionImportPathSelection(source string, offset int) completionSelection {
	if offset < 0 {
		offset = 0
	} else if offset > len(source) {
		offset = len(source)
	}
	start, end := offset, offset
	for start > 0 && source[start-1] != '\'' && source[start-1] != '"' && source[start-1] != '\n' && source[start-1] != '\r' {
		start--
	}
	for end < len(source) && source[end] != '\'' && source[end] != '"' && source[end] != '\n' && source[end] != '\r' {
		end++
	}
	return completionSelection{start: start, cursor: offset, end: end, prefix: source[start:offset]}
}

func importAutoloadAt(file *syntax.File, offset int) bool {
	autoload := false
	walkCommands(file.Commands, func(command *syntax.Command) {
		if command.Import != nil && command.Import.PathSpan.Start < offset && offset < command.Import.PathSpan.End {
			autoload = command.Import.Autoload
		}
	})
	return autoload
}

func importPathKind(directory bool) protocol.CompletionItemKind {
	if directory {
		return protocol.CompletionItemKindFolder
	}
	return protocol.CompletionItemKindFile
}

func completionObjectMembers(file *syntax.File, result *analysis.FileAnalysis, offset int) (protocol.CompletionItemSlice, bool) {
	member := completionMemberAt(file, offset)
	if member == nil || len(member.Children) == 0 || member.Children[0] == nil {
		return nil, false
	}
	typ := result.TypeOf(member.Children[0])
	if typ.Name == "" || typ.Name == analysis.ValueTypeAny || typ.Name == "dict" || typ.Name == "list" {
		return nil, false
	}
	var container *analysis.Symbol
	var find func([]*analysis.Symbol)
	find = func(symbols []*analysis.Symbol) {
		for _, symbol := range symbols {
			if container == nil && symbol.Name == typ.Name && (symbol.Kind == analysis.SymbolKindClass || symbol.Kind == analysis.SymbolKindInterface || symbol.Kind == analysis.SymbolKindEnum) {
				container = symbol
				return
			}
			find(symbol.Children)
		}
	}
	find(analysis.CollectSymbols(file))
	if container == nil {
		return nil, false
	}
	items := make(protocol.CompletionItemSlice, 0, len(container.Children))
	declarations := make(map[syntax.Span]*analysis.Declaration, len(result.Declarations))
	for _, declaration := range result.Declarations {
		declarations[declaration.Span] = declaration
	}
	deep := 0
	for _, symbol := range container.Children {
		kind := protocol.CompletionItemKindField
		switch symbol.Kind {
		case analysis.SymbolKindMethod, analysis.SymbolKindFunction, analysis.SymbolKindConstructor:
			kind = protocol.CompletionItemKindMethod
		case analysis.SymbolKindEnumMember, analysis.SymbolKindConstant:
			kind = protocol.CompletionItemKindConstant
		}
		items = append(items, protocol.CompletionItem{Label: strings.TrimPrefix(symbol.Name, container.Name+"."), Kind: kind, Detail: protocol.NewOptional(symbol.Detail)})
		declaration := declarations[symbol.SelectionRange]
		if declaration == nil || deep == 3 || !completionStaticType(declaration.Type) {
			continue
		}
		childContainer := completionContainer(analysis.CollectSymbols(file), declaration.Type.Name)
		if childContainer == nil {
			continue
		}
		for _, child := range childContainer.Children {
			if deep == 3 {
				break
			}
			items = append(items, protocol.CompletionItem{Label: symbol.Name + "." + strings.TrimPrefix(child.Name, childContainer.Name+"."), Kind: completionMemberKind(child.Kind), Detail: protocol.NewOptional(child.Detail)})
			deep++
		}
	}
	return items, deep > 0
}

func completionStaticType(typ analysis.ValueType) bool {
	return typ.Name != "" && typ.Name != analysis.ValueTypeAny && typ.Name != "dict" && typ.Name != "list"
}

func completionContainer(symbols []*analysis.Symbol, name string) *analysis.Symbol {
	for _, symbol := range symbols {
		if symbol.Name == name && (symbol.Kind == analysis.SymbolKindClass || symbol.Kind == analysis.SymbolKindInterface || symbol.Kind == analysis.SymbolKindEnum) {
			return symbol
		}
		if found := completionContainer(symbol.Children, name); found != nil {
			return found
		}
	}
	return nil
}

func completionMemberKind(kind analysis.SymbolKind) protocol.CompletionItemKind {
	if kind == analysis.SymbolKindMethod || kind == analysis.SymbolKindFunction || kind == analysis.SymbolKindConstructor {
		return protocol.CompletionItemKindMethod
	}
	if kind == analysis.SymbolKindEnumMember || kind == analysis.SymbolKindConstant {
		return protocol.CompletionItemKindConstant
	}
	return protocol.CompletionItemKindField
}

func completionMemberAt(file *syntax.File, offset int) *syntax.Expression {
	var result *syntax.Expression
	walkCommands(file.Commands, func(command *syntax.Command) {
		for _, expression := range append(command.Expressions, command.Targets...) {
			if found := memberExpressionAt(expression, offset); found != nil {
				result = found
			}
		}
		if command.Declaration != nil {
			if found := memberExpressionAt(command.Declaration.Initializer, offset); found != nil {
				result = found
			}
		}
	})
	return result
}

func completionTextMatches(prefix, label string) bool {
	prefix, label = completionComparableText(prefix, label)
	if prefix == "" {
		return true
	}
	if label == "" {
		return false
	}
	prefixRunes := []rune(prefix)
	labelRunes := []rune(label)
	// The first typed character must match the candidate's first character,
	// ignoring case. Remaining typed characters may match later as an ordered
	// subsequence.
	if unicode.ToLower(prefixRunes[0]) != unicode.ToLower(labelRunes[0]) {
		return false
	}
	if strings.EqualFold(label, prefix) || strings.HasPrefix(strings.ToLower(label), strings.ToLower(prefix)) {
		return true
	}
	prefixIndex := 1
	for labelIndex := 1; labelIndex < len(labelRunes) && prefixIndex < len(prefixRunes); labelIndex++ {
		if unicode.ToLower(prefixRunes[prefixIndex]) == unicode.ToLower(labelRunes[labelIndex]) {
			prefixIndex++
		}
	}
	return prefixIndex == len(prefixRunes)
}

func completionComparableText(prefix, label string) (string, string) {
	for {
		strippedScope := false
		for _, scope := range []string{"g:", "l:", "s:", "v:", "&"} {
			if strings.HasPrefix(prefix, scope) && strings.HasPrefix(label, scope) {
				prefix, label = strings.TrimPrefix(prefix, scope), strings.TrimPrefix(label, scope)
				strippedScope = true
				break
			}
		}
		if strippedScope {
			continue
		}
		prefixRune, prefixSize := utf8.DecodeRuneInString(prefix)
		labelRune, labelSize := utf8.DecodeRuneInString(label)
		if prefixRune == labelRune && prefixSize > 0 && !isCompletionIdentifierRune(prefixRune) {
			prefix, label = prefix[prefixSize:], label[labelSize:]
			continue
		}
		return prefix, label
	}
}

func (s *Server) completionList(snapshot *text.Snapshot, encoding text.Encoding, selection completionSelection, candidates map[string]completionCandidate) *protocol.CompletionList {
	items := make([]completionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if completionTextMatches(selection.prefix, candidate.item.Label) {
			items = append(items, candidate)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		if items[i].item.Label != items[j].item.Label {
			return items[i].item.Label < items[j].item.Label
		}
		return items[i].source < items[j].source
	})
	truncated := len(items) > maxCompletionItems
	if truncated {
		items = items[:maxCompletionItems]
	}
	s.mu.Lock()
	capabilities := s.completion
	s.mu.Unlock()
	result := make([]protocol.CompletionItem, 0, len(items))
	insertRange, insertValid := protocolRange(snapshot, encoding, syntax.Span{Start: selection.start, End: selection.cursor})
	replaceRange, replaceValid := protocolRange(snapshot, encoding, syntax.Span{Start: selection.start, End: selection.end})
	// CompletionList.ItemDefaults.editRange (LSP 3.17) lets the list carry the
	// one shared insert/replace range instead of repeating it on every item.
	// Every item in this list would carry exactly the same selection-derived
	// range, so it is safe only when the client declared the capability and the
	// shared range is valid and the list is non-empty (every candidate in
	// `items` becomes one item in `result`).
	useItemDefaults := capabilities.itemDefaultsEditRange && len(items) > 0 && insertValid && replaceValid
	var itemDefaults *protocol.CompletionItemDefaults
	if useItemDefaults {
		itemDefaults = &protocol.CompletionItemDefaults{}
		if capabilities.insertReplace {
			itemDefaults.EditRange = &protocol.EditRangeWithInsertReplace{Insert: insertRange, Replace: replaceRange}
		} else {
			replace := replaceRange
			itemDefaults.EditRange = &replace
		}
	}
	for index, candidate := range items {
		item := candidate.item
		insertText := item.Label
		if value, ok := item.InsertText.Get(); ok && value != "" {
			insertText = value
		}
		filterText := item.Label
		if value, ok := item.FilterText.Get(); ok && value != "" {
			filterText = value
		}
		item.SortText = protocol.NewOptional(fmt.Sprintf("%05d", index))
		item.FilterText = protocol.NewOptional(filterText)
		if candidate.item.Tags != nil && !capabilities.tags {
			item.Tags = nil
		}
		if capabilities.preselect && index == 0 {
			item.Preselect = protocol.NewOptional(true)
		}
		if insertValid && replaceValid {
			if useItemDefaults {
				item.TextEditText = protocol.NewOptional(insertText)
			} else if capabilities.insertReplace {
				item.TextEdit = &protocol.InsertReplaceEdit{NewText: insertText, Insert: insertRange, Replace: replaceRange}
			} else {
				item.TextEdit = &protocol.TextEdit{NewText: insertText, Range: replaceRange}
			}
		}
		result = append(result, item)
	}
	return &protocol.CompletionList{IsIncomplete: truncated, ItemDefaults: itemDefaults, Items: result}
}
