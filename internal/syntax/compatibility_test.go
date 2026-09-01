package syntax

import "testing"

func TestCompatibilityDiagnosticsUseOfficialFeatureBoundaries(t *testing.T) {
	// Boundaries are verified against the first official Vim tags containing
	// the corresponding tests/source: enum 9.1.0219, :iput 9.1.1224, tuple
	// 9.1.1232, object<T> 9.1.1274 and generic functions 9.1.1577.
	file := Parse("vim9script\nenum E\n  One\nendenum\niput =1\nvar pair: tuple<number, string> = (1, 'x')\nvar item: object<Base>\ndef Id<T>(value: T): T\n  return value\nenddef\nvar result = Id<number>(1)\n")
	diagnostics := CompatibilityDiagnostics(file, Version{Major: 9, Minor: 1})
	if len(diagnostics) != 7 {
		t.Fatalf("9.1.0000 diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != "vimls/target-version" || diagnostic.Span.Start < 0 || diagnostic.Span.End > len(file.Source) {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}

	diagnostics = CompatibilityDiagnostics(file, Version{Major: 9, Minor: 1, Patch: 1232})
	if len(diagnostics) != 3 {
		t.Fatalf("9.1.1232 diagnostics = %#v", diagnostics)
	}
	diagnostics = CompatibilityDiagnostics(file, Version{Major: 9, Minor: 2, Patch: 1015})
	if len(diagnostics) != 0 {
		t.Fatalf("latest diagnostics = %#v", diagnostics)
	}
}

func TestCompatibilityDiagnosticsUseOfficialCommandBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		required Version
	}{
		{name: "pbuffer", source: "pbuffer 1\n", required: Version{Major: 9, Minor: 1, Patch: 934}},
		{name: "redrawtabpanel", source: "redrawtabpanel\n", required: Version{Major: 9, Minor: 1, Patch: 1391}},
		{name: "uniq", source: "uniq\n", required: Version{Major: 9, Minor: 1, Patch: 1477}},
		{name: "clipreset", source: "clipreset\n", required: Version{Major: 9, Minor: 1, Patch: 1485}},
		{name: "wlrestore", source: "wlrestore\n", required: Version{Major: 9, Minor: 1, Patch: 1485}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := (LegacyParser{}).Parse(test.source)
			if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Canonical != test.name {
				t.Fatalf("file = %#v", file)
			}

			before := test.required
			before.Patch--
			diagnostics := CompatibilityDiagnostics(file, before)
			if len(diagnostics) != 1 || diagnostics[0].Code != "vimls/target-version" || file.Text(diagnostics[0].Span) != test.name {
				t.Fatalf("before boundary diagnostics = %#v", diagnostics)
			}
			if diagnostics = CompatibilityDiagnostics(file, test.required); len(diagnostics) != 0 {
				t.Fatalf("at boundary diagnostics = %#v", diagnostics)
			}
		})
	}
}

func TestCompatibilityDiagnosticsReachLambdaBodies(t *testing.T) {
	file := Parse("vim9script\nvar Fn = () => {\n  var value: tuple<number> = (1,)\n  return value\n}\n")
	diagnostics := CompatibilityDiagnostics(file, Version{Major: 9, Minor: 1})
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if file.Text(diagnostic.Span) == "" {
			t.Fatalf("outer diagnostic span = %#v", diagnostic)
		}
	}
}

func TestInlineTextBodyCompatibilityBoundary(t *testing.T) {
	// Patch 9.1.0574 added using text after the command-line bar as the first
	// input line for :append, :change, and :insert (Vim commit 8c446da349).
	file := (LegacyParser{}).Parse("append |first line\n.\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].TextBody == nil {
		t.Fatalf("file = %#v", file)
	}
	diagnostics := CompatibilityDiagnostics(file, Version{Major: 9, Minor: 1, Patch: 573})
	if len(diagnostics) != 1 || diagnostics[0].Code != "vimls/target-version" || file.Text(diagnostics[0].Span) != "|" {
		t.Fatalf("9.1.0573 diagnostics = %#v", diagnostics)
	}
	if diagnostics = CompatibilityDiagnostics(file, Version{Major: 9, Minor: 1, Patch: 574}); len(diagnostics) != 0 {
		t.Fatalf("9.1.0574 diagnostics = %#v", diagnostics)
	}
}

func TestCommandBlockHeredocCompatibilityBoundary(t *testing.T) {
	// Patch 9.1.0312 added heredoc support to Vim9 :command blocks; 9.1.0313
	// fixed the first crash/recovery follow-up without changing the syntax.
	file := Parse("vim9script\ncommand SomeCommand {\n  g:value =<< trim END\n    value\n  END\n}\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 4 || file.Commands[2].Heredoc == nil {
		t.Fatalf("commands = %#v, diagnostics = %#v", file.Commands, file.Diagnostics)
	}
	diagnostics := CompatibilityDiagnostics(file, Version{Major: 9, Minor: 1, Patch: 311})
	if len(diagnostics) != 1 || diagnostics[0].Code != "vimls/target-version" || file.Text(diagnostics[0].Span) != "g:value =<< trim END" {
		t.Fatalf("9.1.0311 diagnostics = %#v", diagnostics)
	}
	if diagnostics = CompatibilityDiagnostics(file, Version{Major: 9, Minor: 1, Patch: 312}); len(diagnostics) != 0 {
		t.Fatalf("9.1.0312 diagnostics = %#v", diagnostics)
	}
}

func TestHighlightCTermFontCompatibilityBoundary(t *testing.T) {
	// Patch 9.1.0030 added the ctermfont highlight attribute.  The latest
	// parser always preserves it; only the target-version pass reports use
	// against an older Vim.
	file := (LegacyParser{}).Parse("hi Normal CTERMFONT=3\n")
	if len(file.Diagnostics) != 0 || len(file.Commands) != 1 || file.Commands[0].Highlight == nil {
		t.Fatalf("file = %#v", file)
	}
	diagnostics := CompatibilityDiagnostics(file, Version{Major: 9, Minor: 1, Patch: 29})
	if len(diagnostics) != 1 || diagnostics[0].Code != "vimls/target-version" || file.Text(diagnostics[0].Span) != "CTERMFONT" {
		t.Fatalf("9.1.0029 diagnostics = %#v", diagnostics)
	}
	if diagnostics = CompatibilityDiagnostics(file, Version{Major: 9, Minor: 1, Patch: 30}); len(diagnostics) != 0 {
		t.Fatalf("9.1.0030 diagnostics = %#v", diagnostics)
	}
}
