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
// the emission site for a specific delimiter, command, or target version.
var VimlsDiagnosticDefinitions = [...]DiagnosticDefinition{
	{Code: "vimls/diagnostics-truncated", Message: "additional diagnostics were omitted", Severity: DiagnosticError},
	{Code: "vimls/embedded-command-depth", Message: "embedded command nesting exceeds parser limit", Severity: DiagnosticError},
	{Code: "vimls/expression-too-deep", Message: "expression nesting exceeds parser limit", Severity: DiagnosticError},
	{Code: "vimls/file-too-large", Message: "file exceeds the 4 MiB analysis limit", Severity: DiagnosticError},
	{Code: "vimls/invalid-atom", Message: "invalid atom", Severity: DiagnosticError},
	{Code: "vimls/invalid-member-tail", Message: "member name has trailing characters", Severity: DiagnosticError},
	{Code: "vimls/invalid-parenthesized-expression", Message: "parenthesized expression requires one value", Severity: DiagnosticError},
	{Code: "vimls/missing-argument", Message: "command requires an argument", Severity: DiagnosticError},
	{Code: "vimls/missing-call-comma", Message: "missing comma before call argument", Severity: DiagnosticError},
	{Code: "vimls/missing-delimiter", Message: "expected a closing delimiter", Severity: DiagnosticError},
	{Code: "vimls/missing-end", Message: "block is missing its end command", Severity: DiagnosticError},
	{Code: "vimls/missing-expression", Message: "expected expression", Severity: DiagnosticError},
	{Code: "vimls/missing-generic-end", Message: "expected > after generic type parameters", Severity: DiagnosticError},
	{Code: "vimls/missing-interpolation-end", Message: "expected } in interpolated string", Severity: DiagnosticError},
	{Code: "vimls/missing-list-end", Message: "missing end of list", Severity: DiagnosticError},
	{Code: "vimls/missing-member", Message: "expected member name", Severity: DiagnosticError},
	{Code: "vimls/missing-method-call", Message: "expected argument list after callable", Severity: DiagnosticError},
	{Code: "vimls/missing-parameter-end", Message: "expected ) after function parameters", Severity: DiagnosticError},
	{Code: "vimls/missing-ternary-colon", Message: "expected : in ternary expression", Severity: DiagnosticError},
	{Code: "vimls/missing-type", Message: "expected Vim9 type", Severity: DiagnosticError},
	{Code: "vimls/missing-type-delimiter", Message: "expected a closing type delimiter", Severity: DiagnosticError},
	{Code: "vimls/target-version", Message: "feature requires a newer Vim target version", Severity: DiagnosticError},
	{Code: "vimls/trailing-expression", Message: "unexpected text after expression", Severity: DiagnosticError},
	{Code: "vimls/trailing-type", Message: "unexpected text after type", Severity: DiagnosticError},
	{Code: "vimls/type-too-deep", Message: "type nesting exceeds parser limit", Severity: DiagnosticError},
	{Code: "vimls/unexpected-branch", Message: "branch command has no matching block", Severity: DiagnosticError},
	{Code: "vimls/unexpected-end", Message: "end command has no matching block", Severity: DiagnosticError},
	{Code: "vimls/unexpected-token", Message: "unexpected token in expression", Severity: DiagnosticError},
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
