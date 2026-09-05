// Package syntax parses legacy Vim script and Vim9 script without executing it.
package syntax

type Dialect uint8

const (
	Legacy Dialect = iota
	Vim9
)

func (d Dialect) String() string {
	if d == Vim9 {
		return "vim9"
	}
	return "legacy"
}

// Span is a half-open byte range in the original source.
type Span struct {
	Start int
	End   int
}

type TokenKind uint8

const (
	TokenBOM TokenKind = iota
	TokenWhitespace
	TokenNewline
	TokenComment
	TokenColon
	TokenRange
	TokenModifier
	TokenCommand
	TokenBang
	TokenArgument
	TokenSeparator
	TokenContinuation
	TokenHeredoc
	TokenOpaque
)

type Token struct {
	Kind TokenKind
	Span Span
}

type CommandKind uint8

const (
	CommandBuiltin CommandKind = iota
	CommandUser
	CommandUnknown
	CommandExpression
	CommandEmpty
	CommandBlockEnd
	CommandBlockStart
)

// Command is one Ex command. Argument retains the exact source range instead
// of normalizing text needed by later dialect-specific parsers.
type Command struct {
	Kind    CommandKind
	Dialect Dialect
	// baseDialect is the enclosing context before legacy/vim9cmd modifiers.
	// Recursive do_cmdline() payloads re-enter this context.
	baseDialect        Dialect
	detailsOpaque      bool
	expressionsParsed  bool
	ScriptVersion      uint8
	Span               Span
	Range              Span
	Name               Span
	TypedName          string
	Canonical          string
	Modifiers          []Modifier
	Bang               Span
	Count              Span
	Argument           Span
	Embedded           *CommandList
	Declaration        *Declaration
	Expressions        []*Expression
	Targets            []*Expression
	Block              int
	Heredoc            *Heredoc
	TextBody           *TextBody
	Function           *Function
	Aggregate          *Aggregate
	TypeAlias          *TypeAlias
	EnumValues         []EnumValue
	Import             *Import
	For                *ForLoop
	Keymap             *LoadKeymap
	Mapping            *Mapping
	Highlight          *Highlight
	Syntax             *SyntaxCommand
	Set                *SetCommand
	Augroup            Span
	UserCommand        *UserCommandDefinition
	Substitute         *Substitute
	Autocmd            *AutocmdCommand
	logical            *logicalCommandView
	boundaryExpression *expressionBoundary
	collectedBlockVim9 bool
	hasNextStatement   bool
}

// AutocmdCommand is the command-specific syntax of :autocmd. The group and
// event head is intentionally represented by spans rather than resolved
// names: Vim decides whether the first word is a group at execution time by
// consulting its mutable augroup table. Pattern is the raw, unnormalised
// pattern consumed by autocmd.c.
type AutocmdCommand struct {
	Operation AutocmdOperation
	// Head is the first non-space word after :autocmd. It is the conservative
	// representation of Vim's runtime group/event ambiguity. Group is a
	// syntactic candidate only (when the second word is a known event), not a
	// resolution against Vim's mutable augroup table.
	Head      Span
	Group     Span
	Events    []Span
	Pattern   Span
	Modifiers []AutocmdModifier
}

type AutocmdOperation uint8

const (
	AutocmdQuery AutocmdOperation = iota
	AutocmdClear
	AutocmdDefine
	AutocmdReplace
)

type AutocmdModifierKind uint8

const (
	AutocmdOnce AutocmdModifierKind = iota
	AutocmdNested
)

type AutocmdModifier struct {
	Kind AutocmdModifierKind
	Span Span
}

// Highlight is the command-specific syntax of :highlight.  Command.Argument
// remains the authoritative raw source; these spans expose the group, link and
// attribute parts without executing or consulting Vim's mutable highlight
// table.  For a standalone NONE attribute Key contains "NONE" while Equal and
// Value are empty.  A quoted Value excludes its surrounding single quotes.
type Highlight struct {
	Kind       HighlightKind
	Default    Span
	Operation  Span
	Group      Span
	LinkTarget Span
	Attributes []HighlightAttribute
}

type HighlightKind uint8

const (
	HighlightList HighlightKind = iota
	HighlightQuery
	HighlightClear
	HighlightDefine
	HighlightLink
)

type HighlightAttribute struct {
	Key    Span
	Equal  Span
	Value  Span
	Quoted bool
}

// SetCommand is the command-specific syntax of :set, :setlocal and
// :setglobal. Command.Argument remains the authoritative raw source. Options
// preserve the option spelling and operator/value bytes without consulting
// Vim's mutable option table.
type SetCommand struct {
	Options []SetOption
}

// SetOption is one whitespace-separated item in a set command. Prefix is
// either "no" or "inv" when present; Name, Operator and Value retain their
// original byte spans. Span excludes surrounding item whitespace.
type SetOption struct {
	Span     Span
	Prefix   Span
	Name     Span
	Operator Span
	Value    Span
}

// UserCommandDefinition retains the structured :command header. Attributes
// are present for both complete definitions and incomplete/listing forms so
// editor features can identify the header without reparsing its text.
type UserCommandDefinition struct {
	Attributes []UserCommandAttribute
	Name       Span
	Body       Span
}

type UserCommandAttribute struct {
	Span  Span
	Name  Span
	Equal Span
	Value Span
}

// SyntaxCommand is the command-specific syntax shared by :syntax keyword,
// :syntax match, :syntax region, :syntax cluster, :syntax include,
// :syntax clear/list, :syntax sync and simple mode commands. Command.Argument remains the
// authoritative raw source. Keywords preserves keyword items, clear/list
// group operands, fixed operands such as the match/ignore case mode, and the
// complete raw filename payload of include.
// Options and patterns retain their original order through byte spans,
// including when options appear on both sides of a match pattern or are
// interleaved with region start, skip and end patterns.
type SyntaxCommand struct {
	Kind       SyntaxKind
	Subcommand Span
	Group      Span
	Keywords   []Span
	Options    []SyntaxOption
	Patterns   []SyntaxPattern
}

type SyntaxKind uint8

const (
	SyntaxKeyword SyntaxKind = iota
	SyntaxMatch
	SyntaxRegion
	SyntaxCluster
	SyntaxCase
	SyntaxConceal
	SyntaxSpell
	SyntaxInclude
	SyntaxClear
	SyntaxList
	SyntaxSync
	SyntaxSyncMatch
	SyntaxSyncRegion
	SyntaxIsKeyword
	SyntaxFoldlevel
	SyntaxEnable
	SyntaxManual
	SyntaxOn
	SyntaxOff
	SyntaxReset
)

// SyntaxOption preserves both the complete value and its comma-separated
// items. Equal is empty for flag options. Values is populated only for option
// values whose grammar is a list of syntax groups.
type SyntaxOption struct {
	Name   Span
	Equal  Span
	Value  Span
	Values []Span
}

type SyntaxPatternKind uint8

const (
	SyntaxMatchPattern SyntaxPatternKind = iota
	SyntaxStartPattern
	SyntaxSkipPattern
	SyntaxEndPattern
	SyntaxLineContPattern
)

// SyntaxPattern separates a regexp from the delimiters and optional Vim
// syntax offsets that surround it. Key and Equal are present for region
// start=, skip= and end= items and empty for :syntax match. A missing closing
// delimiter leaves CloseDelimiter empty while Pattern retains the partial
// regexp through the end of the command.
type SyntaxPattern struct {
	Kind           SyntaxPatternKind
	Key            Span
	Equal          Span
	OpenDelimiter  Span
	Pattern        Span
	CloseDelimiter Span
	Offsets        Span
}

// Substitute is the command-specific syntax of :substitute, :smagic,
// :snomagic and :~. Unlike Command.Argument, these spans retain the
// delimiters and payload boundaries Vim's substitute consumer discovers
// before Ex can look for a bar or a comment. Empty and missing parts are
// represented by their zero-width span together with the corresponding
// Missing* bit.
type Substitute struct {
	Delimiter             Span
	Pattern               Span
	PatternDelimiter      Span
	Replacement           Span
	ReplacementDelimiter  Span
	Flags                 Span
	Count                 Span
	PreviousPattern       Span
	ReplacementPrefix     Span
	ExpressionSpan        Span
	Expression            *Expression
	ReplacementExpression bool
	FlagBits              SubstituteFlags
	Magic                 SubstituteMagic
	MissingPattern        bool
	MissingReplacement    bool
	LegacyPrevious        bool
	InvalidVim9Backslash  bool
	diagnostics           []Diagnostic
}

// SubstituteMagic records the magic override implied by :smagic or :snomagic.
type SubstituteMagic uint8

const (
	SubstituteMagicDefault SubstituteMagic = iota
	SubstituteMagicOn
	SubstituteMagicOff
)

// SubstituteFlags is the decoded form of the contiguous flag run.  The
// source Flags span remains authoritative; these bits are for semantic-token
// and analysis consumers that should not rescan the argument.
type SubstituteFlags uint16

const (
	SubstituteFlagAll SubstituteFlags = 1 << iota
	SubstituteFlagConfirm
	SubstituteFlagCount
	SubstituteFlagError
	SubstituteFlagLastPattern
	SubstituteFlagPrint
	SubstituteFlagNumber
	SubstituteFlagList
	SubstituteFlagIgnoreCase
	SubstituteFlagMatchCase
	SubstituteFlagKeepOptions
)

// MappingKind describes the operation performed by a mapping command.  The
// command name and mode are retained separately because Vim has many aliases
// (for example, :nnoremap and :noremap!).
type MappingKind uint8

const (
	MappingDefine MappingKind = iota
	MappingNoremap
	MappingUnmap
	MappingClear
)

// MappingMode is Vim's mode set for a mapping command.  Langmap is kept as a
// distinct bit: Vim applies it while entering text, command-line arguments,
// and other language-map contexts, but exposes it as mode "l".
type MappingMode uint16

const (
	MappingModeNormal MappingMode = 1 << iota
	MappingModeInsert
	MappingModeCommandLine
	MappingModeVisual
	MappingModeSelect
	MappingModeOperator
	MappingModeTerminal
	MappingModeLangmap
)

const MappingModeNormalVisualSelectOperator = MappingModeNormal | MappingModeVisual | MappingModeSelect | MappingModeOperator
const MappingModeInsertCommandLine = MappingModeInsert | MappingModeCommandLine

// Mapping is the typed syntax for :map/:noremap/:unmap and their abbreviation
// variants. LHS and RHS always point into the original source, including
// significant escaped and trailing whitespace.
type Mapping struct {
	Kind         MappingKind
	Mode         MappingMode
	Bang         bool
	Abbreviation bool

	Buffer  bool
	Nowait  bool
	Silent  bool
	Special bool
	Script  bool
	Expr    bool
	Unique  bool

	Modifiers []Span

	LHS Span
	RHS Span
	// RHSExpression is populated only for <expr> mappings. RHS remains the
	// authoritative raw span so callers can preserve Vim's exact mapping text.
	RHSExpression *Expression
	Query         bool
	Clear         bool
}

type Declaration struct {
	Name        Span
	Type        Span
	Assignment  Span
	Target      *Expression
	Initializer *Expression
	ParsedType  *Type
	Bindings    []Binding
}

type Binding struct {
	Name       Span
	Type       Span
	ParsedType *Type
	Rest       bool
}

type ForLoop struct {
	Bindings     []Binding
	In           Span
	Iterable     *Expression
	IterableSpan Span
}

type Function struct {
	Name           Span
	TypeParameters []TypeParameter
	Parameters     []Parameter
	ReturnType     *Type
	ReturnTypeSpan Span
	// AttributeTail starts immediately after a complete legacy :function
	// parameter list and covers the parsed attribute input. Attributes keeps
	// the exact span of each recognized range, abort, dict, or closure word.
	AttributeTail Span
	Attributes    []Span
}

type TypeParameter struct {
	Name string
	Span Span
}

type Parameter struct {
	Name Span
	// Target is populated for Vim9 constructor shorthand parameters in the
	// exact this.member form. Name remains the complete source span for
	// compatibility with callers that treat parameters as named spans.
	Target      *Expression
	Variadic    bool
	Type        *Type
	TypeSpan    Span
	Default     *Expression
	DefaultSpan Span
}

type Aggregate struct {
	Kind       BlockKind
	Name       Span
	Extends    []Span
	Implements []Span
	// Members contains indexes in the containing File or CommandList Commands slice.
	Members []int
}

type TypeAlias struct {
	Name       Span
	Assignment Span
	Type       *Type
	TypeSpan   Span
}

type EnumValue struct {
	Name        Span
	Initializer *Expression
	Arguments   []*Expression
}

type Import struct {
	Autoload bool
	Path     *Expression
	PathSpan Span
	Alias    Span
}

type Heredoc struct {
	Header     Span // trim/eval flags and explicit opening marker
	Marker     string
	Trim       bool
	Eval       bool
	Deferred   bool
	Body       Span
	EndMarker  Span
	Incomplete bool
}

// TextBody is the literal source consumed by legacy :append, :change, and
// :insert. Lines are physical source lines and are never parsed as Ex commands.
// Separator is the optional command-line `|` that introduces the first text
// line on Vim 9.1.0574 and newer. EndMarker is the terminating line containing
// exactly one dot. Incomplete is true when no marker was found; the body may
// then end at a conservative loose-recovery boundary rather than at EOF.
type TextBody struct {
	Separator  Span
	Body       Span
	Lines      []Span
	EndMarker  Span
	Incomplete bool
}

// LoadKeymap is the opaque-to-Ex payload consumed by :loadkeymap.  Keymap
// entries are two whitespace-separated words; comments and blank lines are
// retained by Body but do not become commands or entries.
type LoadKeymap struct {
	Body    Span
	Entries []KeymapEntry
}

type KeymapEntry struct {
	From Span
	To   Span
}

type CommandList struct {
	Span     Span
	Commands []Command
	Blocks   []Block
}

type BlockKind string

const (
	BlockIf        BlockKind = "if"
	BlockFor       BlockKind = "for"
	BlockWhile     BlockKind = "while"
	BlockTry       BlockKind = "try"
	BlockFunction  BlockKind = "function"
	BlockDef       BlockKind = "def"
	BlockClass     BlockKind = "class"
	BlockInterface BlockKind = "interface"
	BlockEnum      BlockKind = "enum"
	BlockAugroup   BlockKind = "augroup"
	BlockCommand   BlockKind = "command"
	BlockScope     BlockKind = "scope"
)

type Block struct {
	Kind     BlockKind
	Span     Span
	Header   int
	End      int
	Parent   int
	Branches []int
}

type Modifier struct {
	Name   string
	Span   Span
	Bang   Span
	Filter *FilterModifier
}

// FilterModifier is the regexp payload owned by :filter.  It is allocated
// only for that modifier so ordinary modifiers do not carry cold regexp spans.
type FilterModifier struct {
	Delimiter Span
	Pattern   Span
	Flags     Span
}

type Diagnostic struct {
	Code    string
	Message string
	Span    Span
	Related RelatedDiagnostic
	// Severity overrides the code's default severity for this occurrence. It
	// is used by role-aware analyses (for example a config-file mode that
	// lowers a warning to a hint) without changing the stable code identity.
	// nil means the code's registered default severity applies.
	Severity *DiagnosticSeverity
}

// RelatedDiagnostic is one authoritative source location associated with a
// diagnostic. Source and Span remain byte-based so protocol encoding conversion
// stays at the server boundary. The zero value means no related location.
type RelatedDiagnostic struct {
	URI     string
	Source  string
	Message string
	Span    Span
}

type File struct {
	Dialect     Dialect
	Source      string
	lambdaBody  bool
	Commands    []Command
	Tokens      []Token
	Blocks      []Block
	Diagnostics []Diagnostic
	OpaqueTail  Span
}

func (f *File) Text(span Span) string {
	if span.Start < 0 || span.End < span.Start || span.End > len(f.Source) {
		return ""
	}
	return f.Source[span.Start:span.End]
}

// Parse chooses Vim9 only when the first effective command is vim9script.
// Blank lines, a UTF-8 BOM, and legacy comment lines do not count as commands.
func Parse(source string) *File {
	if startsWithVim9Script(source) {
		return (Vim9Parser{}).Parse(source)
	}
	return (LegacyParser{}).Parse(source)
}

// LegacyParser is the independent legacy Vim script parser entry point.
type LegacyParser struct{}

func (LegacyParser) Parse(source string) *File {
	return parseSource(source, Legacy)
}

// Vim9Parser is the independent Vim9 parser entry point. File-level callers
// normally use Parse so that the required vim9script trigger is honored.
type Vim9Parser struct{}

func (Vim9Parser) Parse(source string) *File {
	return parseSource(source, Vim9)
}
