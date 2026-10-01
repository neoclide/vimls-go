package analysis

import (
	"slices"
	"sort"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// FileAnalysis is the protocol-independent lexical information collected from
// one syntax tree.  All spans are byte spans in File.Source.
type FileAnalysis struct {
	importTypes  ImportTypes
	progress     *analysisProgress
	File         *syntax.File
	Root         *Scope
	Scopes       []*Scope
	Declarations []*Declaration
	References   []*Reference
	// configFile marks a user configuration file (vimrc or an explicit
	// configFiles document). The role is decided outside analysis from the
	// document path; it only adjusts vimls-owned configuration diagnostics and
	// never changes the syntax tree or lexical/semantic structures.
	configFile bool
	hasImports bool
	// Diagnostics contains protocol-independent semantic diagnostics with byte
	// spans in File.Source.
	Diagnostics     []syntax.Diagnostic
	expressionTypes map[*syntax.Expression]ValueType
	namedTypes      map[string]ValueType
	commandScopes   map[*syntax.Command]*Scope
	lambdaScopes    map[*syntax.Expression]*Scope
	lambdaBodies    map[*syntax.Expression]bool
	unknownOptions  map[syntax.Span]bool
	// suppressedSyntaxDiagnostics contains provisional parser diagnostics that
	// Vim replaces after resolving names or types.
	suppressedSyntaxDiagnostics map[syntax.Diagnostic]bool
	enumValueExempt             map[syntax.Span]bool
	typeAliasExempt             map[syntax.Span]bool
	classValueExempt            map[syntax.Span]bool
	superMemberExempt           map[syntax.Span]bool
	classAliases                map[string]string
	classes                     map[string]*syntax.Command
	sourceIdentity              string
}

// Scope is a lexical region. Root has Block == -1 and an empty Kind. Other
// scopes correspond to a block in either the top-level syntax.File or an
// embedded CommandList; CommandList identifies the latter's local index.
type Scope struct {
	Block        int
	Kind         syntax.BlockKind
	Span         syntax.Span
	Parent       *Scope
	Children     []*Scope
	Declarations []*Declaration
	// CommandList identifies the syntax list whose local Block index is used
	// by this scope.  It is nil for blocks in the top-level syntax.File.
	CommandList *syntax.CommandList
	// Lambda identifies an expression-owned lexical scope.  Lambda scopes use
	// Block == -1 because they are not syntax command blocks.
	Lambda *syntax.Expression
}

// Declaration is a name introduced in a scope.  Mutable is false for
// constants and for declarations that cannot be assigned to (functions,
// types, imports, and aggregate members).
type Declaration struct {
	Name               string
	Kind               SymbolKind
	Span               syntax.Span
	Mutable            bool
	Deprecated         bool
	TypeParameterCount int
	constBinding       bool
	unusedCandidate    bool
	// Parameter distinguishes a function or lambda argument from an ordinary
	// mutable variable without changing its navigation symbol kind.
	Parameter bool
	Scope     *Scope
	Type      ValueType
}

// Reference is an identifier occurrence.  Declaration is nil when the name
// is dynamic, explicitly scoped to a different namespace, or not visible yet.
type Reference struct {
	Name             string
	Span             syntax.Span
	Declaration      *Declaration
	functionCallee   bool
	assignmentTarget bool
	scope            *Scope
	dialect          syntax.Dialect
}

// Analyze collects lexical scopes, declarations, and same-file references.
// It deliberately does not report undefined names: an unresolved reference
// is a valid result for dynamic legacy Vim script and for incomplete input.
func Analyze(file *syntax.File) *FileAnalysis {
	result, _ := AnalyzeWithOptions(file, Options{})
	return result
}

// AnalyzeConfigFile analyzes one document in user-configuration-file mode.
// Semantic structures are identical to Analyze; only the vimls-owned
// configuration diagnostics differ (see style_diagnostics.go). Callers that
// know from the document path that IsConfigFile is true use this entry point.
func AnalyzeConfigFile(file *syntax.File) *FileAnalysis {
	result, _ := AnalyzeWithOptions(file, Options{ConfigFile: true})
	return result
}

// Options supplies immutable external facts without introducing workspace or
// process dependencies into analysis.
type Options struct {
	ConfigFile     bool
	Imports        ImportTypes
	SourceIdentity string
	Yield          func() error
}

// AnalyzeWithOptions performs full analysis with immutable external facts.
// At phase and batched traversal boundaries, options.Yield may suspend analysis
// or return an error to discard the private partial result. The callback is not
// retained in the returned analysis. Callers must not hold locks needed by work
// they are yielding to.
func AnalyzeWithOptions(file *syntax.File, options Options) (*FileAnalysis, error) {
	configFile, yield := options.ConfigFile, options.Yield
	if err := runAnalysisPhases(yield); err != nil {
		return nil, err
	}
	result := newFileAnalysis(file, configFile)
	result.importTypes = options.Imports
	result.sourceIdentity = options.SourceIdentity
	if yield != nil {
		result.progress = &analysisProgress{yield: yield}
		defer func() { result.progress = nil }()
		originalYield := yield
		yield = func() error {
			if result.progress.err != nil {
				return result.progress.err
			}
			return originalYield()
		}
	}
	if file == nil {
		return result, nil
	}
	root := result.Root
	if err := runAnalysisPhases(yield,
		func() { collectCommandScopes(result, root, file.Commands, file.Blocks, nil) },
		func() { collectLambdaScopesCommands(result, root, file.Commands) },

		// First collect every declaration.  This is separate from reference
		// walking so a function can be referenced before its definition, as Vim
		// permits, without making variables forward-visible.
		func() { collectEmbeddedDeclarations(result, root, file.Commands) },
		func() { collectLambdaDeclarations(result) },
		func() { collectOverwriteRiskDiagnostics(result, file.Commands) },
		func() { collectNameDeclarationConflictDiagnostics(result) },
		func() { collectVim9ScriptFunctionDeletionDiagnostics(result, file.Commands, root) },
		func() { collectVim9LegacyScriptVariableDiagnostics(result) },

		// A malformed or partially parsed enum value may remain an opaque command.
		// The enum block is still authoritative for its one-name-per-line members.
		func() { collectOpaqueEnumDeclarations(result, file.Commands, file.Blocks) },
		func() { collectDuplicateEnumValueDiagnostics(result) },
		func() { collectArgumentRedeclarationDiagnostics(result) },
		func() { collectArgumentShadowDiagnostics(result) },
		func() { collectLegacyConstExistingVariableDiagnostics(result, file.Commands) },
		func() { collectVim9RedeclarationDiagnostics(result) },
		func() { collectVim9NameAlreadyDefinedDiagnostics(result, file.Commands) },
		func() { collectImportedItemRedefinitionDiagnostics(result, file.Commands) },
		func() { collectVim9ScriptItemRedefinitionDiagnostics(result, file.Commands) },
		func() { collectAggregateLocalRedeclarationDiagnostics(result) },
		func() { collectDuplicateTypeAliasDiagnostics(result) },
		func() { collectAbstractConstructorDiagnostics(result) },
		func() { collectDuplicateMethodDiagnostics(result) },
		func() { collectUnimplementedAbstractMethodDiagnostics(result) },
		func() { collectMethodAccessLevelDiagnostics(result) },
		func() { collectGenericMethodOverrideDiagnostics(result) },
		func() { collectMethodTypeMismatchDiagnostics(result) },
		func() { collectDuplicateClassVariableDiagnostics(result) },
		func() { collectPublicUnderscoreVariableDiagnostics(result) },
		func() { collectPublicProtectedMemberNameDiagnostics(result) },
		func() { collectConstructorDefaultValueDiagnostics(result) },
		func() { collectUninitializedObjectVariableDiagnostics(result) },
		func() { collectTypeDiagnostics(result) },
		func() { collectInterfaceVariableAccessDiagnostics(result) },
		func() { collectReturnOutsideFunctionDiagnostics(result) },
		func() { collectMissingReturnValueDiagnostics(result, file.Commands, file.Blocks) },
		func() { collectUnreachableCodeDiagnostics(result) },
		func() { collectLoopNestingDiagnostics(result) },

		func() { sortDeclarations(result) },
	); err != nil {
		return nil, err
	}
	for index := range file.Commands {
		if index%32 == 0 {
			if err := runAnalysisPhases(yield); err != nil {
				return nil, err
			}
		}
		command := &file.Commands[index]
		scope := result.commandScopes[command]
		if scope == nil {
			scope = root
		}
		walkCommand(result, file, command, scope)
	}
	sort.SliceStable(result.References, func(i, j int) bool {
		return result.References[i].Span.Start < result.References[j].Span.Start
	})
	if err := runAnalysisPhases(yield,
		func() { collectDeprecatedReferenceDiagnostics(result) },
		func() { collectImportNamespaceDiagnostics(result) },
		func() { inferTypes(result) },
		func() { collectNullReceiverDiagnostics(result) },
		func() { collectExtendedAggregateDiagnostics(result) },
		func() { collectImplementedInterfaceNameDiagnostics(result) },
		func() { collectImplementedInterfaceMembersDiagnostics(result) },
		func() { collectVariableTypeMismatchDiagnostics(result) },
		func() { collectVim9DestructuringDiagnostics(result, file.Commands) },
		func() { collectLegacyListCardinalityDiagnostics(result, file.Commands) },
		func() { collectFuncrefVariableNameDiagnostics(result) },
		func() { collectMissingDictionaryKeyDiagnostics(result, file.Commands, root) },
		func() { collectDeferDiagnostics(result, file.Commands, root) },
		func() { collectOperatorDiagnostics(result, file.Commands, root) },
		func() { collectAggregateAccessDiagnostics(result) },
		func() { collectVoidValueDiagnostics(result, file.Commands) },
		func() { collectTypeMismatchDiagnostics(result, file.Commands, root) },
		func() { collectBuiltinArgumentTypeDiagnostics(result, file.Commands, root) },
		func() { collectAssignmentDiagnostics(result, file.Commands, root) },
		func() { collectNameOnlyExpressionDiagnostics(result, file.Commands, root) },
		func() { collectUnusedDiagnostics(result) },
		func() { collectStyleDiagnostics(result) },
		func() { collectVariableTypeChangeDiagnostics(result) },
	); err != nil {
		return nil, err
	}
	if result.configFile {
		collectConfigFileDiagnostics(result)
	}
	suppressUnexpandedBodyDiagnostics(result)
	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		return result.Diagnostics[i].Span.Start < result.Diagnostics[j].Span.Start
	})
	if result.progress != nil && result.progress.err != nil {
		return nil, result.progress.err
	}
	return result, nil
}

// Progress is private to a running full analysis and is removed before return.
// Batching keeps the callback out of the per-node hot path in ordinary analysis.
type analysisProgress struct {
	yield func() error
	steps uint64
	err   error
}

func (result *FileAnalysis) analysisStep() bool {
	if result == nil {
		return true
	}
	p := result.progress
	if p == nil {
		return true
	}
	if p.err != nil {
		return false
	}
	p.steps++
	if p.steps%64 == 0 {
		p.err = p.yield()
	}
	return p.err == nil
}

// runAnalysisPhases keeps phase order explicit while allowing background work
// to yield without restarting or exposing partially populated results.
func runAnalysisPhases(yield func() error, phases ...func()) error {
	if yield != nil {
		if err := yield(); err != nil {
			return err
		}
	}
	for index, phase := range phases {
		if index > 0 && yield != nil {
			if err := yield(); err != nil {
				return err
			}
		}
		phase()
	}
	return nil
}

// newFileAnalysis allocates exclusively owned state. Completion facts and full
// analysis must never share mutable scopes, declarations or inferred types.
func newFileAnalysis(file *syntax.File, configFile bool) *FileAnalysis {
	result := &FileAnalysis{File: file, configFile: configFile, suppressedSyntaxDiagnostics: make(map[syntax.Diagnostic]bool)}
	root := &Scope{Block: -1}
	if file != nil {
		root.Span = syntax.Span{End: len(file.Source)}
	}
	result.Root = root
	result.Scopes = []*Scope{root}
	if file == nil {
		return result
	}

	result.commandScopes = make(map[*syntax.Command]*Scope)
	result.lambdaScopes = make(map[*syntax.Expression]*Scope)
	result.lambdaBodies = make(map[*syntax.Expression]bool)
	result.unknownOptions = make(map[syntax.Span]bool)
	result.enumValueExempt = make(map[syntax.Span]bool)
	result.typeAliasExempt = make(map[syntax.Span]bool)
	result.classValueExempt = make(map[syntax.Span]bool)
	result.superMemberExempt = make(map[syntax.Span]bool)
	result.classes = localAggregates(file, syntax.BlockClass)
	result.classAliases = localClassAliases(file, result.classes)
	return result
}

// Expansion can change expression arity and even command boundaries. Retain
// the raw AST for navigation, but do not diagnose unexpanded payloads as Vim.
// Vim 9.2.1132 also discards command/autocmd blocks in a skipped branch.
func suppressUnexpandedBodyDiagnostics(result *FileAnalysis) {
	var opaque []syntax.Span
	var uncertainArity []syntax.Span
	replacements := func(span syntax.Span) []syntax.Span {
		var spans []syntax.Span
		for offset := span.Start; offset < span.End; offset++ {
			if result.File.Source[offset] != '<' {
				continue
			}
			close := strings.IndexByte(result.File.Source[offset:span.End], '>')
			if close < 0 {
				break
			}
			if syntax.IsUserCommandReplacementAt(result.File.Source, offset, span.End) {
				spans = append(spans, syntax.Span{Start: offset, End: offset + close + 1})
			}
			offset += close
		}
		return spans
	}
	var collect func([]syntax.Command, []syntax.Block, bool)
	collect = func(commands []syntax.Command, blocks []syntax.Block, userBody bool) {
		for index := range commands {
			command := &commands[index]
			if (command.Canonical == "command" || command.Canonical == "autocmd") && commandInSkippedBranch(index, commands, blocks) {
				if command.Embedded != nil {
					opaque = append(opaque, command.Embedded.Span)
				} else if command.Block >= 0 && command.Block < len(blocks) && blocks[command.Block].Kind == syntax.BlockCommand {
					block := blocks[command.Block]
					if block.End >= 0 && block.End < len(commands) {
						opaque = append(opaque, syntax.Span{Start: command.Span.End, End: commands[block.End].Name.Start})
					}
				}
			}
			inUserBody := userBody
			for block := command.Block; block >= 0 && block < len(blocks); block = blocks[block].Parent {
				if blocks[block].Kind == syntax.BlockCommand && blocks[block].Header != index && blocks[block].End != index {
					inUserBody = true
					break
				}
			}
			if inUserBody {
				spans := replacements(command.Argument)
				if command.Set != nil {
					// Static options in the same command remain independently
					// checkable, even when another item is replaced at invocation.
					opaque = append(opaque, spans...)
				} else if len(spans) > 0 {
					// Earlier literal arguments retain their known positions/types;
					// later arguments and total arity depend on expansion.
					opaque = append(opaque, syntax.Span{Start: spans[0].Start, End: command.Span.End})
					uncertainArity = append(uncertainArity, command.Span)
				}
			}
			if command.Embedded != nil {
				if command.Mapping != nil {
					// <SID> has a known script-local meaning and does not change
					// the command structure. Other key notation remains opaque.
					body := strings.ReplaceAll(strings.ToLower(result.File.Text(command.Embedded.Span)), "<sid>", "")
					if strings.Contains(body, "<") {
						// Mapping headers and neighbouring commands stay checked.
						opaque = append(opaque, command.Embedded.Span)
						continue
					}
				}
				collect(command.Embedded.Commands, command.Embedded.Blocks, inUserBody || command.Canonical == "command")
			}
		}
	}
	collect(result.File.Commands, result.File.Blocks, false)
	if len(opaque) == 0 {
		return
	}
	suppressed := func(diagnostic syntax.Diagnostic) bool {
		if diagnostic.Code == "vim/E119" || diagnostic.Code == "vim/E118" || diagnostic.Code == "vim/E740" {
			for _, span := range uncertainArity {
				if span.Start <= diagnostic.Span.Start && diagnostic.Span.End <= span.End {
					return true
				}
			}
		}
		for _, span := range opaque {
			if span.Start <= diagnostic.Span.Start && diagnostic.Span.End <= span.End {
				return true
			}
		}
		return false
	}
	result.Diagnostics = slices.DeleteFunc(result.Diagnostics, suppressed)
	for _, diagnostic := range result.File.Diagnostics {
		if suppressed(diagnostic) {
			if result.suppressedSyntaxDiagnostics == nil {
				result.suppressedSyntaxDiagnostics = make(map[syntax.Diagnostic]bool)
			}
			result.suppressedSyntaxDiagnostics[diagnostic] = true
		}
	}
}

// Only literal conditions prove that Vim skips a stored command body. Stop at
// a deferred definition: its outer condition is not its invocation context.
func commandInSkippedBranch(index int, commands []syntax.Command, blocks []syntax.Block) bool {
	condition := func(command *syntax.Command) (bool, bool) {
		if len(command.Expressions) != 1 {
			return false, false
		}
		expression := command.Expressions[0]
		for expression != nil && expression.Kind == syntax.ExpressionParenthesized && len(expression.Children) == 1 {
			expression = expression.Children[0]
		}
		if value, ok := staticNumberValue(expression); ok && (value == 0 || value == 1) {
			return value != 0, true
		}
		if expression != nil && expression.Kind == syntax.ExpressionIdentifier {
			switch expression.Value {
			case "v:false":
				return false, true
			case "v:true":
				return true, true
			case "false", "true":
				return expression.Value == "true", command.Dialect == syntax.Vim9
			}
		}
		return false, false
	}
	for blockIndex := commands[index].Block; blockIndex >= 0 && blockIndex < len(blocks); blockIndex = blocks[blockIndex].Parent {
		block := blocks[blockIndex]
		if block.Header == index {
			continue
		}
		switch block.Kind {
		case syntax.BlockDef, syntax.BlockFunction, syntax.BlockCommand:
			return false
		case syntax.BlockIf, syntax.BlockWhile:
			branch := block.Header
			if branch < 0 || branch >= len(commands) {
				continue
			}
			for _, next := range block.Branches {
				if next < 0 || next >= index {
					break
				}
				if value, known := condition(&commands[branch]); known && value {
					return true
				}
				branch = next
			}
			if value, known := condition(&commands[branch]); known && !value {
				return true
			}
		}
	}
	return false
}

// CombinedDiagnostics returns parser and semantic diagnostics with the narrow
// semantic replacements that require resolved names or types. It does not mutate file.
func CombinedDiagnostics(file *syntax.File, result *FileAnalysis) []syntax.Diagnostic {
	if file == nil {
		return nil
	}
	diagnostics := make([]syntax.Diagnostic, 0, len(file.Diagnostics))
	for _, diagnostic := range file.Diagnostics {
		if result != nil && result.suppressedSyntaxDiagnostics[diagnostic] {
			continue
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	if result != nil {
		diagnostics = append(diagnostics, result.Diagnostics...)
	}
	return diagnostics
}

func syntaxDiagnosticOverlaps(diagnostics []syntax.Diagnostic, span syntax.Span) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Span.Start <= span.End && diagnostic.Span.End >= span.Start {
			return true
		}
	}
	return false
}
