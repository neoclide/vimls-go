package syntax

// DiagnosticSeverity is the protocol-independent severity of a diagnostic.
// Values intentionally do not mirror LSP constants; the server performs the
// conversion at the protocol boundary.
type DiagnosticSeverity uint8

const (
	DiagnosticError DiagnosticSeverity = iota
	DiagnosticWarning
	DiagnosticInformation
	DiagnosticHint
)

// DiagnosticDefinition is one diagnostic owned by vimls-go. Vim diagnostics
// use their native vim/E123 codes and are deliberately not part of this list.
// Message is the stable default; an occurrence may add contextual detail.
type DiagnosticDefinition struct {
	Code     string
	Message  string
	Severity DiagnosticSeverity
}

// VimlsDiagnosticDefinitions is the complete, code-sorted list of diagnostics
// owned by vimls-go. Keep entries here even when their messages are refined at
// the emission site for a specific delimiter or command.
var VimlsDiagnosticDefinitions = [...]DiagnosticDefinition{
	{Code: "vimls/autocmd-group-not-cleared", Message: "augroup does not clear existing autocommands", Severity: DiagnosticWarning},
	{Code: "vimls/autocmd-outside-augroup", Message: "autocommand is not contained in an augroup", Severity: DiagnosticWarning},
	{Code: "vimls/autoload-function-not-found", Message: "autoload function not found in current runtimepath", Severity: DiagnosticWarning},
	{Code: "vimls/catch-error-message", Message: "catching human-readable error text is fragile", Severity: DiagnosticWarning},
	{Code: "vimls/complex-autocmd", Message: "complex autocommand body; consider delegating to a function", Severity: DiagnosticHint},
	{Code: "vimls/complex-command", Message: "complex user command body; consider delegating to a function", Severity: DiagnosticHint},
	{Code: "vimls/config-loaded-guard", Message: "a loaded guard skips the rest of the file on a later :source; edits below may not take effect", Severity: DiagnosticHint},
	{Code: "vimls/config-mapleader-order", Message: "mapping is defined before the leader assignment; <Leader> is expanded when the mapping is defined", Severity: DiagnosticWarning},
	{Code: "vimls/configuration-overwrite", Message: "unconditional configuration assignment may overwrite a user value", Severity: DiagnosticWarning},
	{Code: "vimls/deprecated", Message: "symbol is deprecated", Severity: DiagnosticHint},
	{Code: "vimls/diagnostics-truncated", Message: "additional diagnostics were omitted", Severity: DiagnosticWarning},
	{Code: "vimls/direct-user-keymap", Message: "user key mapping reduces configurability; consider exposing a <Plug> mapping", Severity: DiagnosticHint},
	{Code: "vimls/duplicate-mapping", Message: "mapping for the same key is defined more than once; the later definition overwrites the earlier one", Severity: DiagnosticWarning},
	{Code: "vimls/echoerr", Message: "echoerr always raises an error; use it only for intended failures", Severity: DiagnosticHint},
	{Code: "vimls/embedded-command-depth", Message: "embedded command nesting exceeds parser limit", Severity: DiagnosticInformation},
	{Code: "vimls/explicit-local-scope", Message: "use an explicit local scope for this variable", Severity: DiagnosticHint},
	{Code: "vimls/expression-too-deep", Message: "expression nesting exceeds parser limit", Severity: DiagnosticInformation},
	{Code: "vimls/file-too-large", Message: "file exceeds the 4 MiB analysis limit", Severity: DiagnosticWarning},
	{Code: "vimls/function-without-abort", Message: "function does not use abort", Severity: DiagnosticHint},
	{Code: "vimls/global-function-not-indexed", Message: "global function not found in workspace index", Severity: DiagnosticHint},
	{Code: "vimls/global-internal-state", Message: "short global variable appears to be plugin-internal state", Severity: DiagnosticHint},
	{Code: "vimls/implicit-pattern-case", Message: "pattern match depends on 'ignorecase'", Severity: DiagnosticHint},
	{Code: "vimls/implicit-regex-magic", Message: "pattern relies on Vim's magic setting", Severity: DiagnosticHint},
	{Code: "vimls/implicit-string-case", Message: "string comparison depends on 'ignorecase'", Severity: DiagnosticHint},
	{Code: "vimls/invalid-atom", Message: "invalid atom", Severity: DiagnosticInformation},
	{Code: "vimls/invalid-member-tail", Message: "member name has trailing characters", Severity: DiagnosticInformation},
	{Code: "vimls/invalid-parenthesized-expression", Message: "parenthesized expression requires one value", Severity: DiagnosticInformation},
	{Code: "vimls/mapping-script-local-reference", Message: "mapping references a script-local name that may not be available", Severity: DiagnosticWarning},
	{Code: "vimls/mapping-without-unique", Message: "mapping may overwrite an existing mapping; consider <unique>", Severity: DiagnosticHint},
	{Code: "vimls/match-command", Message: ":match uses shared match slots; prefer matchadd() in plugin code", Severity: DiagnosticHint},
	{Code: "vimls/missing-call-comma", Message: "missing comma before call argument", Severity: DiagnosticInformation},
	{Code: "vimls/missing-delimiter", Message: "expected a closing delimiter", Severity: DiagnosticInformation},
	{Code: "vimls/missing-expression", Message: "expected expression", Severity: DiagnosticInformation},
	{Code: "vimls/missing-interpolation-end", Message: "expected } in interpolated string", Severity: DiagnosticInformation},
	{Code: "vimls/missing-list-end", Message: "missing end of list", Severity: DiagnosticInformation},
	{Code: "vimls/missing-member", Message: "expected member name", Severity: DiagnosticInformation},
	{Code: "vimls/missing-method-call", Message: "expected argument list after callable", Severity: DiagnosticInformation},
	{Code: "vimls/missing-option-value", Message: "option requires a value; :set without an operator displays the current value", Severity: DiagnosticWarning},
	{Code: "vimls/missing-ternary-colon", Message: "expected : in ternary expression", Severity: DiagnosticInformation},
	{Code: "vimls/missing-type", Message: "expected Vim9 type", Severity: DiagnosticInformation},
	{Code: "vimls/normal-without-bang", Message: ":normal may invoke user-defined mappings; prefer :normal!", Severity: DiagnosticWarning},
	{Code: "vimls/recursive-map", Message: "mapping may recursively expand user mappings", Severity: DiagnosticWarning},
	{Code: "vimls/set-vs-setlocal", Message: ":set may modify a global option; consider :setlocal", Severity: DiagnosticWarning},
	{Code: "vimls/trailing-expression", Message: "unexpected text after expression", Severity: DiagnosticInformation},
	{Code: "vimls/trailing-type", Message: "unexpected text after type", Severity: DiagnosticInformation},
	{Code: "vimls/type-too-deep", Message: "type nesting exceeds parser limit", Severity: DiagnosticInformation},
	{Code: "vimls/unexpected-token", Message: "unexpected token in expression", Severity: DiagnosticInformation},
	{Code: "vimls/unknown-autocmd-event", Message: "unknown autocommand event", Severity: DiagnosticHint},
	{Code: "vimls/unused-variable", Message: "variable is declared but never used", Severity: DiagnosticHint},
}

// LookupVimlsDiagnostic returns metadata for a vimls-owned diagnostic code.
func LookupVimlsDiagnostic(code string) (DiagnosticDefinition, bool) {
	for _, definition := range VimlsDiagnosticDefinitions {
		if definition.Code == code {
			return definition, true
		}
	}
	return DiagnosticDefinition{}, false
}
