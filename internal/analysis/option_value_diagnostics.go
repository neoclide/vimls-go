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
	if appendCompatibleSetOptionDiagnostic(result, file, command, item) {
		return
	}
	operator := file.Text(item.Operator)
	if operator != "=" && operator != ":" && operator != "+=" && operator != "-=" && operator != "^=" {
		return
	}
	option, ok := vimdata.LookupOption(file.Text(item.Name))
	if !ok {
		return
	}
	if option.Type == vimdata.OptionBool {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/E474", Message: "Invalid argument", Span: item.Span})
		return
	}
	if command != nil && command.Canonical == "setglobal" && optionHasFlag(option, "P_NOGLOB") {
		return
	}
	if file.Text(item.Prefix) != "" {
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
		if operator != "=" && operator != ":" || option.Validation.Kind == vimdata.ValidationNone {
			return
		}
		value = strconv.FormatInt(number, 10)
	} else if operator != "=" && operator != ":" || option.Validation.Kind == vimdata.ValidationNone {
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
	if !ok || option.Validation.Kind == vimdata.ValidationNone {
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

// optionAssignment identifies direct, pinned options handled separately from
// ordinary variable assignments. Compatibility overlays retain their own rules.
func optionAssignment(expression *syntax.Expression) (vimdata.Option, bool) {
	if expression == nil || expression.Kind != syntax.ExpressionAssignment || len(expression.Children) != 2 {
		return vimdata.Option{}, false
	}
	target := expression.Children[0]
	if target == nil || target.Kind != syntax.ExpressionIdentifier || !strings.HasPrefix(target.Value, "&") {
		return vimdata.Option{}, false
	}
	if _, ok := vimdata.LookupOptionCompatibility(target.Value); ok {
		return vimdata.Option{}, false
	}
	option, ok := vimdata.LookupOption(target.Value)
	return option, ok
}

// Vim v9.2.1015 evalvars.c:ex_let_option and vim9compile.c distinguish
// interpreted conversion from compiled assignment compatibility.
func appendOptionAssignmentTypeDiagnostic(result *FileAnalysis, scope *Scope, expression *syntax.Expression, dialect syntax.Dialect, option vimdata.Option) {
	if expressionContainsMissing(expression) {
		return
	}
	target, rhs := expression.Children[0], expression.Children[1]
	actual := result.TypeOf(rhs)
	if isUnknownType(actual) || optionAcceptsCompatibleType(target.Value, actual) {
		return
	}
	op := result.File.Text(expression.Operator)
	concat := op == ".=" || op == "..="
	compiled := dialect == syntax.Vim9 && scopeUsesDefTypeRules(scope)
	add := func(code, message string, span syntax.Span) {
		result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{Code: "vim/" + code, Message: message, Span: span})
	}
	if compiled {
		// Project policy: use one code for invalid compiled boolean compounds,
		// rather than Vim's RHS-dependent type and operator errors.
		if option.Type == vimdata.OptionBool && op != "=" {
			add("E521", "Compound assignment is not supported for a boolean option", expression.Operator)
			return
		}
		if concat && option.Type != vimdata.OptionString {
			add("E1019", "Can only concatenate to string", target.Span)
			return
		}
		if optionAcceptsFunction(target.Value) && (actual.Name == "func" || actual.Name == "partial") && op == "=" {
			return
		}
		if option.Type == vimdata.OptionBool && actual.Name == "number" {
			if value, known := staticNumberValue(rhs); known && (value == 0 || value == 1) && op == "=" {
				return
			}
		}
		expected := builtinOptionValueType(option)
		if !assignmentTypesCompatible(expected, actual) {
			appendTypeMismatchDiagnostic(result, expected, rhs)
		} else if op != "=" && !concat && option.Type != vimdata.OptionNumber {
			switch op {
			case "+=":
				add("E1051", "Wrong argument type for +", expression.Operator)
			case "%=":
				add("E1035", "% requires number arguments", expression.Operator)
			default:
				add("E1036", strings.TrimSuffix(op, "=")+" requires number or float arguments", expression.Operator)
			}
		}
		return
	}
	if op != "=" && (concat && option.Type != vimdata.OptionString || !concat && option.Type == vimdata.OptionString) {
		add("E734", "Wrong variable type for "+strings.TrimSuffix(strings.ReplaceAll(op, "..", "."), "=")+"=", expression.Operator)
		return
	}
	if option.Type == vimdata.OptionString {
		if optionAcceptsFunction(target.Value) && (actual.Name == "func" || actual.Name == "partial") {
			return
		}
		if actual.Name == "bool" || isSpecialType(actual) || dialect == syntax.Vim9 && actual.Name == "number" {
			add("E928", "String required", rhs.Span)
		} else if diagnostic, ok := stringConversionDiagnostic(actual, rhs.Span); ok {
			result.Diagnostics = append(result.Diagnostics, diagnostic)
		}
		return
	}
	if actual.Name == "string" {
		// Numeric compound assignments discard the converted string before
		// set_option_value; Legacy boolean compounds do so as well.
		if op != "=" && (option.Type == vimdata.OptionNumber || dialect == syntax.Legacy) {
			return
		}
		literal := rhs
		for literal.Kind == syntax.ExpressionParenthesized && len(literal.Children) == 1 {
			literal = literal.Children[0]
		}
		value, known := syntax.StaticDictionaryIndexKey(literal)
		if !known || value != "" && strings.Trim(value, "0") == "" {
			return
		}
		if dialect == syntax.Legacy && legacyOptionStringNonzero(value) {
			return
		}
		add("E521", "Number required: &"+option.Name+" = '"+value+"'", rhs.Span)
		return
	}
	if actual.Name == "number" {
		if dialect == syntax.Vim9 && option.Type == vimdata.OptionBool {
			if diagnostic, ok := numberAsBoolDiagnostic(rhs); ok {
				result.Diagnostics = append(result.Diagnostics, diagnostic)
			}
		}
		return
	}
	if actual.Name == "bool" || isSpecialType(actual) {
		if dialect != syntax.Vim9 || option.Type == vimdata.OptionBool {
			return
		}
		if actual.Name == "bool" {
			add("E1138", "Using a Bool as a Number", rhs.Span)
			return
		}
	}
	if actual.Name == "float" {
		add("E805", "Using a Float as a Number", rhs.Span)
	} else {
		if actual.Name == "partial" {
			actual.Name = "func"
		}
		if diagnostic, ok := numericConversionDiagnostic(actual, rhs.Span); ok {
			result.Diagnostics = append(result.Diagnostics, diagnostic)
		}
	}
}

// ex_let_option uses vim_str2nr(STR2NR_ALL): only a leading minus is
// consumed, not whitespace or plus. For E521 we only need to know whether
// the numeric prefix is nonzero, so avoid integer overflow altogether.
func legacyOptionStringNonzero(value string) bool {
	value = strings.ToLower(strings.TrimPrefix(value, "-"))
	digit := func(c byte) int { return strings.IndexByte("0123456789abcdef", c) }
	base := 10
	if len(value) >= 3 && value[0] == '0' {
		switch value[1] {
		case 'x':
			base = 16
		case 'b':
			base = 2
		case 'o':
			base = 8
		}
		if base != 10 && digit(value[2]) >= 0 && digit(value[2]) < base {
			value = value[2:]
		} else {
			base = 10
		}
	}
	// Unprefixed octal versus decimal does not affect zero detection: Vim
	// switches the whole numeric prefix to decimal when it contains 8 or 9.
	for i := 0; i < len(value); i++ {
		d := digit(value[i])
		if d < 0 || d >= base {
			break
		}
		if d != 0 {
			return true
		}
	}
	return false
}
