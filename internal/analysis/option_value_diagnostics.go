package analysis

import (
	"slices"
	"strconv"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

// Vim v9.2.1015 options.txt, option-value-function: assignment converts the
// function reference to its name; reading the option still returns a string.
func optionAcceptsFunction(name string) bool {
	if !strings.HasPrefix(name, "&") {
		return false
	}
	option, ok := vimdata.LookupOption(name)
	if !ok {
		return false
	}
	switch option.Name {
	case "completefunc", "findfunc", "imactivatefunc", "imstatusfunc", "omnifunc", "operatorfunc", "quickfixtextfunc", "tagfunc", "thesaurusfunc":
		return true
	}
	return false
}

func appendSetOptionValueDiagnostic(result *FileAnalysis, file *syntax.File, command *syntax.Command, item syntax.SetOption) {
	if result == nil || file == nil {
		return
	}
	if appendCompatibleSetOptionDiagnostic(result, file, command, item) || file.Text(item.Prefix) != "" {
		return
	}
	operator := file.Text(item.Operator)
	if operator != "=" && operator != ":" {
		return
	}
	option, ok := vimdata.LookupOption(file.Text(item.Name))
	if !ok || option.AvailableWhen != "1" {
		return
	}
	if command != nil && command.Canonical == "setglobal" && optionHasFlag(option, "P_NOGLOB") {
		return
	}
	raw := file.Text(item.Value)
	// Backslash and CTRL-V quoting are interpreted by :set before callbacks.
	// Until every quoting form is mapped byte-for-byte, do not guess its value.
	if strings.ContainsRune(raw, '\\') || strings.ContainsRune(raw, '\x16') {
		return
	}
	valueSpan := item.Value
	if valueSpan.Start == valueSpan.End {
		valueSpan = item.Span
	}
	value := raw
	if option.Type == vimdata.OptionNumber {
		number, ok := staticSetOptionNumber(raw)
		if !ok {
			if setNumberOptionMayUseKeyNotation(option.Name, raw) {
				return
			}
			result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
				Code: "vim/E521", Message: "Number required after =", Span: valueSpan,
			})
			return
		}
		if option.Validation.Kind == vimdata.ValidationNone {
			return
		}
		value = strconv.FormatInt(number, 10)
	} else if option.Validation.Kind == vimdata.ValidationNone {
		return
	}
	appendOptionValueDiagnostic(result, option.Validation, value, valueSpan)
}

func setNumberOptionMayUseKeyNotation(name, value string) bool {
	// Vim treats these as number options but also accepts a single character,
	// caret form, or key notation through :set.
	if name != "wildchar" && name != "wildcharm" {
		return false
	}
	return strings.HasPrefix(value, "<") || strings.HasPrefix(value, "^") || len(value) == 1
}

func appendOptionAssignmentValueDiagnostic(result *FileAnalysis, file *syntax.File, assignment *syntax.Expression, dialect syntax.Dialect) {
	if result == nil || file == nil || assignment == nil || assignment.Kind != syntax.ExpressionAssignment || len(assignment.Children) != 2 {
		return
	}
	target, valueExpression := assignment.Children[0], assignment.Children[1]
	if target == nil || target.Kind != syntax.ExpressionIdentifier || !strings.HasPrefix(target.Value, "&") {
		return
	}
	if appendCompatibleOptionAssignmentDiagnostic(result, file, assignment, dialect) {
		return
	}
	if file.Text(assignment.Operator) != "=" {
		return
	}
	option, ok := vimdata.LookupOption(target.Value)
	if !ok || option.AvailableWhen != "1" || option.Validation.Kind == vimdata.ValidationNone {
		return
	}
	if strings.HasPrefix(target.Value, "&g:") && optionHasFlag(option, "P_NOGLOB") {
		return
	}
	value, span, ok := staticOptionAssignmentValue(file, valueExpression, option.Validation.Kind, dialect)
	if !ok {
		return
	}
	appendOptionValueDiagnostic(result, option.Validation, value, span)
}

func staticOptionAssignmentValue(file *syntax.File, expression *syntax.Expression, kind vimdata.ValidationKind, dialect syntax.Dialect) (string, syntax.Span, bool) {
	if expression == nil {
		return "", syntax.Span{}, false
	}
	if kind == vimdata.ValidationNumberRange {
		switch expression.Kind {
		case syntax.ExpressionNumber:
			if number, ok := staticExpressionOptionNumber(expression.Value, dialect); ok {
				return strconv.FormatInt(number, 10), expression.Span, true
			}
		case syntax.ExpressionUnary:
			if len(expression.Children) == 1 && expression.Children[0] != nil && expression.Children[0].Kind == syntax.ExpressionNumber {
				if number, ok := staticExpressionOptionNumber(expression.Children[0].Value, dialect); ok {
					switch file.Text(expression.Operator) {
					case "-":
						return strconv.FormatInt(-number, 10), expression.Span, true
					case "+":
						return strconv.FormatInt(number, 10), expression.Span, true
					}
				}
			}
		}
		return "", syntax.Span{}, false
	}
	if expression.Kind != syntax.ExpressionString || len(expression.Value) < 2 {
		return "", syntax.Span{}, false
	}
	quote := expression.Value[0]
	if expression.Value[len(expression.Value)-1] != quote || quote != '\'' && quote != '"' {
		return "", syntax.Span{}, false
	}
	value := expression.Value[1 : len(expression.Value)-1]
	if quote == '"' && strings.ContainsRune(value, '\\') || quote == '\'' && strings.Contains(value, "''") {
		return "", syntax.Span{}, false
	}
	span := syntax.Span{Start: expression.Span.Start + 1, End: expression.Span.End - 1}
	if span.Start >= span.End {
		span = expression.Span
	}
	return value, span, true
}

func optionHasFlag(option vimdata.Option, flag string) bool {
	return slices.Contains(option.Flags, flag)
}

// staticSetOptionNumber matches :set's option-number grammar.  Its parser
// accepts a minus sign and the legacy numeric bases, but not Vim9 digit
// separators or a leading plus sign.
func staticSetOptionNumber(literal string) (int64, bool) {
	if strings.HasPrefix(literal, "+") || strings.ContainsRune(literal, '\'') {
		return 0, false
	}
	return staticOptionInteger(literal, false)
}

// staticExpressionOptionNumber matches an expression number.  Vim9 made a
// leading-zero literal decimal and permits apostrophe digit separators;
// legacy expressions retain the octal leading-zero form.
func staticExpressionOptionNumber(literal string, dialect syntax.Dialect) (int64, bool) {
	if dialect == syntax.Vim9 {
		literal = strings.ReplaceAll(literal, "'", "")
		return staticOptionInteger(literal, true)
	}
	if strings.ContainsRune(literal, '\'') {
		return 0, false
	}
	return staticOptionInteger(literal, false)
}

func staticOptionInteger(literal string, leadingZeroDecimal bool) (int64, bool) {
	if literal == "" {
		return 0, false
	}
	neg := false
	if strings.HasPrefix(literal, "-") {
		neg = true
		literal = literal[1:]
	}
	if literal == "" {
		return 0, false
	}
	base := 10
	digits := literal
	if len(literal) > 2 && strings.HasPrefix(literal, "0") {
		switch strings.ToLower(literal[:2]) {
		case "0x":
			base, digits = 16, literal[2:]
		case "0b":
			base, digits = 2, literal[2:]
		case "0o":
			base, digits = 8, literal[2:]
		}
	}
	if base == 10 && !leadingZeroDecimal && len(literal) > 1 && literal[0] == '0' {
		isOctal := true
		for index := 1; index < len(literal); index++ {
			if literal[index] < '0' || literal[index] > '7' {
				isOctal = false
				break
			}
		}
		if isOctal {
			base, digits = 8, literal
		}
	}
	if base == 10 && strings.ContainsAny(literal, ".eE") {
		return 0, false
	}
	if digits == "" {
		return 0, false
	}
	if neg {
		digits = "-" + digits
	}
	number, err := strconv.ParseInt(digits, base, 64)
	if err != nil {
		return 0, false
	}
	return number, true
}

func appendOptionValueDiagnostic(result *FileAnalysis, validation vimdata.OptionValidation, value string, valueSpan syntax.Span) {
	failure, invalid := vimdata.ValidateOptionValue(validation, value)
	if !invalid {
		return
	}
	appendOptionValueError(result, failure, valueSpan, validation.Kind == vimdata.ValidationNumberRange)
}

func appendOptionValueError(result *FileAnalysis, failure vimdata.OptionValueError, valueSpan syntax.Span, wholeValue bool) {
	span := syntax.Span{Start: valueSpan.Start + failure.Start, End: valueSpan.Start + failure.End}
	if wholeValue || span.Start >= span.End {
		span = valueSpan
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code:    "vim/" + failure.Code,
		Message: failure.Message,
		Span:    span,
	})
}
