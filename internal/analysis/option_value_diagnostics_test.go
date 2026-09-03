package analysis

import (
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

func TestOptionValueDiagnostics(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		code    string
		message string
		span    string
	}{
		{name: "set exact", source: "set bh=bogus\n", code: "vim/E474", message: "Invalid argument", span: "bogus"},
		{name: "setlocal exact", source: "setlocal bh=bogus\n", code: "vim/E474", message: "Invalid argument", span: "bogus"},
		{name: "setglobal colon exact", source: "setglobal bg:bogus\n", code: "vim/E474", message: "Invalid argument", span: "bogus"},
		{name: "set comma list", source: "set belloff=all,bogus\n", code: "vim/E474", message: "Invalid argument", span: "all,bogus"},
		{name: "set flag list", source: "set cpo=a@\n", code: "vim/E539", message: "Illegal character <@>", span: "@"},
		{name: "set flag list multibyte", source: "set cpo=a★\n", code: "vim/E539", message: "Illegal character <<e2>>", span: "★"},
		{name: "set number minimum", source: "set msc=0\n", code: "vim/E487", message: "Argument must be positive", span: "0"},
		{name: "set number maximum", source: "set msc=10000\n", code: "vim/E474", message: "Invalid argument", span: "10000"},
		{name: "set leading zero decimal fallback", source: "set msc=099999\n", code: "vim/E474", message: "Invalid argument", span: "099999"},
		{name: "set number hex", source: "set msc=0x10000\n", code: "vim/E474", message: "Invalid argument", span: "0x10000"},
		{name: "legacy option assignment", source: "let &bh = 'bogus'\n", code: "vim/E474", message: "Invalid argument", span: "bogus"},
		{name: "legacy leading zero decimal fallback", source: "let &msc = 099999\n", code: "vim/E474", message: "Invalid argument", span: "099999"},
		{name: "global option assignment", source: "let &g:bg = 'bogus'\n", code: "vim/E474", message: "Invalid argument", span: "bogus"},
		{name: "vim9 option assignment", source: "vim9script\n&l:bh = 'bogus'\n", code: "vim/E474", message: "Invalid argument", span: "bogus"},
		{name: "vim9 negative number", source: "vim9script\n&msc = -1\n", code: "vim/E487", message: "Argument must be positive", span: "-1"},
		{name: "vim9 hexadecimal number", source: "vim9script\n&msc = 0x0\n", code: "vim/E487", message: "Argument must be positive", span: "0x0"},
		{name: "vim9 hexadecimal maximum", source: "vim9script\n&msc = 0x10000\n", code: "vim/E474", message: "Invalid argument", span: "0x10000"},
		{name: "vim9 uppercase hexadecimal digit", source: "vim9script\n&msc = 0xBEEF\n", code: "vim/E474", message: "Invalid argument", span: "0xBEEF"},
		{name: "vim9 lowercase hexadecimal digit", source: "vim9script\n&msc = 0xbeef\n", code: "vim/E474", message: "Invalid argument", span: "0xbeef"},
		{name: "vim9 leading zero decimal", source: "vim9script\n&msc = 010000\n", code: "vim/E474", message: "Invalid argument", span: "010000"},
		{name: "vim9 digit separator", source: "vim9script\n&msc = 0'0\n", code: "vim/E487", message: "Argument must be positive", span: "0'0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := syntax.Parse(test.source)
			analysis := Analyze(file)
			var got []syntax.Diagnostic
			for _, diagnostic := range analysis.Diagnostics {
				if diagnostic.Code == "vim/E474" || diagnostic.Code == "vim/E487" || diagnostic.Code == "vim/E539" {
					got = append(got, diagnostic)
				}
			}
			if len(got) != 1 || got[0].Code != test.code || got[0].Message != test.message || file.Text(got[0].Span) != test.span {
				t.Fatalf("diagnostics = %#v, want %s %q on %q", got, test.code, test.message, test.span)
			}
		})
	}
}

func TestOptionValueDiagnosticsSkipNoGlobalAndInvalidNumberSyntax(t *testing.T) {
	source := "setglobal bh=bogus bt=bogus\n" +
		"let &g:bh = 'bogus'\n" +
		"let &g:bt = 'bogus'\n" +
		"set msc=+0 msc=0'0\n" +
		"let &msc = 010000\n"
	file := syntax.Parse(source)
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E474" || diagnostic.Code == "vim/E487" || diagnostic.Code == "vim/E539" {
			t.Fatalf("unexpected option value diagnostic: %#v", diagnostic)
		}
	}
}

func TestOptionValueDiagnosticsSkipValidAndDynamicValues(t *testing.T) {
	source := "set bh=hide belloff=all,error cpo=aA msc=1 msc=010 emoji=single\n" +
		"set bh+=bogus bh=bo\\gus\n" +
		"let value = 'bogus'\n" +
		"let &bh = value\n" +
		"let &bh = 'bo' . 'gus'\n" +
		"set foldclose=bogus\n"
	file := syntax.Parse(source)
	for _, diagnostic := range Analyze(file).Diagnostics {
		if diagnostic.Code == "vim/E474" || diagnostic.Code == "vim/E487" || diagnostic.Code == "vim/E539" {
			t.Fatalf("unexpected option value diagnostic: %#v", diagnostic)
		}
	}
}

func TestOptionValueDiagnosticsMatchConfigAndPluginRoles(t *testing.T) {
	file := syntax.Parse("set bh=bogus\n")
	plugin := Analyze(file)
	config := AnalyzeConfigFile(file)
	for _, result := range []*FileAnalysis{plugin, config} {
		found := false
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "vim/E474" && file.Text(diagnostic.Span) == "bogus" {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing E474 in diagnostics: %#v", result.Diagnostics)
		}
	}
}
