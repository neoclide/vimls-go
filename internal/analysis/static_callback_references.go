package analysis

import (
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// callbackFunctionOptions are string options whose value is the name of a
// callback function (see docs/userconfig.md). Options that take an expression
// string (e.g. 'formatexpr') are intentionally excluded: they are not a bare
// function-name reference.
var callbackFunctionOptions = map[string]bool{
	"completefunc": true,
	"omnifunc":     true,
	"operatorfunc": true,
	"tagfunc":      true,
}

// appendStaticFunctionReference records a function-name reference at span and
// resolves it like an ordinary identifier reference.
func appendStaticFunctionReference(result *FileAnalysis, file *syntax.File, scope *Scope, name string, span syntax.Span) {
	if scope == nil || !staticFunctionName(name, file.Dialect) {
		return
	}
	declaration := resolve(scope, name, span.Start, true, nil)
	result.References = append(result.References, &Reference{
		Name: strings.Clone(name), Span: span,
		Declaration: declaration, functionCallee: true, scope: scope, dialect: file.Dialect,
	})
}

// staticFunctionName reports whether name looks like a statically addressable
// Vim function name: optional "g:"/"s:"/"<SID>" prefix, then either one plain
// segment or any number of "#"-separated autoload segments. Legacy global
// names start uppercase; only Legacy script-local names may start lowercase.
func staticFunctionName(name string, dialect syntax.Dialect) bool {
	if name == "" {
		return false
	}
	scriptLocal := false
	if strings.HasPrefix(name, "s:") {
		name = name[2:]
		scriptLocal = true
	} else if len(name) > len("<SID>") && strings.EqualFold(name[:len("<SID>")], "<SID>") {
		name = name[len("<SID>"):]
		scriptLocal = true
	} else if strings.HasPrefix(name, "g:") {
		name = name[2:]
	}
	autoload := strings.Contains(name, "#")
	segments := strings.Split(name, "#")
	for index, segment := range segments {
		if segment == "" {
			return false
		}
		first := segment[0]
		letter := first >= 'A' && first <= 'Z' || first >= 'a' && first <= 'z'
		if !letter {
			return false
		}
		if !autoload && index == 0 && !(scriptLocal && dialect == syntax.Legacy) && !(first >= 'A' && first <= 'Z') {
			return false
		}
		for position := 1; position < len(segment); position++ {
			character := segment[position]
			if !(character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_') {
				return false
			}
		}
	}
	return true
}

// appendMappingCmdReferences records static function calls in a
// <Cmd>...<CR> mapping RHS, which Vim executes as an Ex command-line payload
// (§7 P1).
func appendMappingCmdReferences(result *FileAnalysis, file *syntax.File, scope *Scope, mapping *syntax.Mapping) {
	if mapping == nil || mapping.RHS.Start == mapping.RHS.End || mapping.RHSExpression != nil {
		return
	}
	rhs := file.Text(mapping.RHS)
	lower := strings.ToLower(rhs)
	cmdStart := strings.Index(lower, "<cmd>")
	if cmdStart < 0 {
		return
	}
	payloadStart := cmdStart + len("<cmd>")
	rest := rhs[payloadStart:]
	// <Cmd>{payload}<CR> has no "/" in the closing key code; use the last
	// <CR> so payload key codes themselves are not confused with it.
	crStart := strings.LastIndex(strings.ToLower(rest), "<cr>")
	if crStart < 0 {
		return
	}
	payload := rest[:crStart]
	var parsed *syntax.File
	if file.Dialect == syntax.Vim9 {
		parsed = (syntax.Vim9Parser{}).Parse(payload)
	} else {
		parsed = (syntax.LegacyParser{}).Parse(payload)
	}
	if len(parsed.Diagnostics) != 0 || len(parsed.Commands) != 1 {
		return
	}
	command := &parsed.Commands[0]
	if command.Canonical != "call" || len(command.Expressions) != 1 {
		return
	}
	call := command.Expressions[0]
	if call == nil || call.Kind != syntax.ExpressionCall || len(call.Children) == 0 {
		return
	}
	callee := call.Children[0]
	if callee == nil || callee.Kind != syntax.ExpressionIdentifier || !staticFunctionName(callee.Value, file.Dialect) {
		return
	}
	start := mapping.RHS.Start + payloadStart + callee.Span.Start
	appendStaticFunctionReference(result, file, scope, callee.Value, syntax.Span{Start: start, End: start + callee.Span.End - callee.Span.Start})
}

// appendCallbackOptionReferences records a static function name used as the
// value of a callback option, both for :set name=Func and
// :let &name = 'Func'.
func appendCallbackOptionReferences(result *FileAnalysis, file *syntax.File, scope *Scope, command *syntax.Command) {
	if command.Set != nil {
		for _, option := range command.Set.Options {
			if option.Value.Start == option.Value.End {
				continue
			}
			if !callbackFunctionOptions[normalizeOptionName(file.Text(option.Name))] {
				continue
			}
			operator := strings.TrimSpace(file.Text(option.Operator))
			if operator != "=" && operator != ":" {
				continue
			}
			value := strings.TrimSpace(file.Text(option.Value))
			if !staticFunctionName(value, file.Dialect) {
				continue
			}
			appendStaticFunctionReference(result, file, scope, value, option.Value)
		}
		return
	}
	declaration := command.Declaration
	if declaration == nil || declaration.Name.Start == declaration.Name.End {
		return
	}
	name := file.Text(declaration.Name)
	if !strings.HasPrefix(name, "&") || !callbackFunctionOptions[normalizeOptionName(strings.TrimPrefix(name, "&"))] {
		return
	}
	initializer := declaration.Initializer
	if initializer == nil || initializer.Kind != syntax.ExpressionString {
		return
	}
	value := strings.Trim(file.Text(initializer.Span), "'\"")
	if !staticFunctionName(value, file.Dialect) {
		return
	}
	appendStaticFunctionReference(result, file, scope, value, initializer.Span)
}

func normalizeOptionName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasPrefix(name, "no"), strings.HasPrefix(name, "inv"):
		return ""
	case strings.HasPrefix(name, "&l:"):
		return name[3:]
	case strings.HasPrefix(name, "&g:"):
		return name[3:]
	case strings.HasPrefix(name, "&"):
		return name[1:]
	}
	return name
}
