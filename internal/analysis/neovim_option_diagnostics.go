package analysis

import (
	"strconv"
	"strings"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

func appendNeovimOptionHint(result *FileAnalysis, context syntax.EditorContext, span syntax.Span) {
	if context == syntax.EditorNeovim {
		return
	}
	result.Diagnostics = append(result.Diagnostics, syntax.Diagnostic{
		Code: "vimls/neovim-only-option", Message: "this option setting is Neovim-only; guard it with has('nvim')", Span: span,
	})
}

func appendCompatibleSetOptionDiagnostic(result *FileAnalysis, file *syntax.File, command *syntax.Command, item syntax.SetOption) bool {
	compat, ok := vimdata.LookupOptionCompatibility(file.Text(item.Name))
	if !ok {
		return false
	}
	only := compat.Vim.Name == ""
	context := command.EditorContext
	operator := file.Text(item.Operator)
	if file.Text(item.Prefix) != "" || operator == "!" {
		appendOptionValueError(result, vimdata.OptionValueError{Code: "E474", Message: "Invalid argument"}, item.Span, true)
		return true
	}
	valueSpan := item.Value
	if valueSpan.Start == valueSpan.End {
		valueSpan = item.Span
	}
	value := file.Text(item.Value)
	check := operator == "=" || operator == ":"
	if operator == "+=" || operator == "^=" {
		// A list/flag insertion can be checked as a fragment. Other compound
		// operators require the previous value. Removal may be a harmless no-op.
		switch compat.Neovim.Validation.Kind {
		case vimdata.ValidationCommaList, vimdata.ValidationFlagList, vimdata.ValidationFillChars:
			check = true
		}
	}
	if !check || strings.ContainsAny(value, "\\\x16") {
		if only {
			appendNeovimOptionHint(result, context, item.Name)
		}
		return true
	}
	appendCompatibleOptionValueDiagnostic(result, compat, context, value, valueSpan, true)
	return true
}

func appendCompatibleOptionAssignmentDiagnostic(result *FileAnalysis, file *syntax.File, assignment *syntax.Expression, dialect syntax.Dialect) bool {
	target, expression := assignment.Children[0], assignment.Children[1]
	compat, ok := vimdata.LookupOptionCompatibility(target.Value)
	if !ok {
		return false
	}
	if file.Text(assignment.Operator) != "=" {
		if compat.Vim.Name == "" {
			appendNeovimOptionHint(result, assignment.EditorContext, target.Span)
		}
		return true
	}
	if literal := unwrapParenthesizedExpression(expression); literal != nil {
		switch literal.Kind {
		case syntax.ExpressionList, syntax.ExpressionTuple, syntax.ExpressionDictionary, syntax.ExpressionBlob:
			// These are type errors, not Neovim extensions. The assignment
			// checker reports them after inference; do not also add a Hint.
			return true
		}
	}
	// Decode strings and numbers independently: foldcolumn is numeric in Vim
	// and a string in Neovim. Do not let Vim's type choose the literal decoder.
	value, span, known := staticOptionAssignmentValue(file, expression, vimdata.ValidationExact, dialect)
	if known && dialect == syntax.Vim9 && compat.Neovim.Type == vimdata.OptionNumber && (compat.Vim.Name == "" || compat.Vim.Type == vimdata.OptionNumber) {
		// The existing Vim9 assignment checker owns string-to-number type
		// errors. Do not add a second option-value error or a compatibility hint.
		return true
	}
	if !known {
		value, span, known = staticOptionAssignmentValue(file, expression, vimdata.ValidationNumberRange, dialect)
	}
	if known {
		appendCompatibleOptionValueDiagnostic(result, compat, assignment.EditorContext, value, span, false)
	} else if compat.Vim.Name == "" {
		appendNeovimOptionHint(result, assignment.EditorContext, target.Span)
	}
	return true
}

func appendCompatibleOptionValueDiagnostic(result *FileAnalysis, compat vimdata.OptionCompatibility, context syntax.EditorContext, value string, span syntax.Span, set bool) {
	validate := func(option vimdata.Option) (vimdata.OptionValueError, bool) {
		decoded := value
		if option.Type == vimdata.OptionNumber {
			number, err := strconv.ParseInt(value, 10, 64)
			valid := err == nil
			if set {
				number, valid = staticSetOptionNumber(value)
			}
			if !valid {
				return vimdata.OptionValueError{Code: "E521", Message: "Number required after =", End: len(value)}, true
			}
			decoded = strconv.FormatInt(number, 10)
		}
		return vimdata.ValidateOptionValue(option.Validation, decoded)
	}
	vimError, vimInvalid := validate(compat.Vim)
	if compat.Vim.Name != "" && !vimInvalid {
		// Vim accepts laststatus=3 but does not implement its documented
		// Neovim global-statusline meaning. This is a semantic compatibility
		// hint, not evidence that Vim rejects other numeric values.
		if compat.Vim.Name == "laststatus" {
			if number, ok := staticSetOptionNumber(value); ok && number == 3 {
				appendNeovimOptionHint(result, context, span)
			}
		}
		return
	}
	nvimError, nvimInvalid := validate(compat.Neovim)
	if !nvimInvalid {
		appendNeovimOptionHint(result, context, span)
		return
	}
	failure := vimError
	if compat.Vim.Name == "" || compat.Vim.Type != compat.Neovim.Type {
		failure = nvimError
	}
	appendOptionValueError(result, failure, span, compat.Neovim.Type == vimdata.OptionNumber)
}
