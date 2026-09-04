package syntax

import "testing"

func TestVimlsDiagnosticRuleCatalog(t *testing.T) {
	want := map[string]DiagnosticSeverity{
		"vimls/autocmd-group-not-cleared":      DiagnosticWarning,
		"vimls/autocmd-outside-augroup":        DiagnosticWarning,
		"vimls/autoload-function-not-found":    DiagnosticWarning,
		"vimls/catch-error-message":            DiagnosticWarning,
		"vimls/complex-autocmd":                DiagnosticHint,
		"vimls/complex-command":                DiagnosticHint,
		"vimls/configuration-overwrite":        DiagnosticWarning,
		"vimls/direct-user-keymap":             DiagnosticHint,
		"vimls/echoerr":                        DiagnosticHint,
		"vimls/explicit-local-scope":           DiagnosticHint,
		"vimls/function-without-abort":         DiagnosticHint,
		"vimls/global-function-not-indexed":    DiagnosticHint,
		"vimls/global-internal-state":          DiagnosticHint,
		"vimls/implicit-pattern-case":          DiagnosticHint,
		"vimls/implicit-regex-magic":           DiagnosticHint,
		"vimls/implicit-string-case":           DiagnosticHint,
		"vimls/mapping-script-local-reference": DiagnosticWarning,
		"vimls/mapping-without-unique":         DiagnosticHint,
		"vimls/match-command":                  DiagnosticHint,
		"vimls/normal-without-bang":            DiagnosticWarning,
		"vimls/recursive-map":                  DiagnosticWarning,
		"vimls/set-vs-setlocal":                DiagnosticWarning,
	}
	for code, severity := range want {
		definition, ok := LookupVimlsDiagnostic(code)
		if !ok {
			t.Errorf("missing diagnostic definition %q", code)
			continue
		}
		if definition.Severity != severity {
			t.Errorf("diagnostic %q has severity %d, want %d", code, definition.Severity, severity)
		}
	}
}

func TestVimlsDiagnosticDefinitions(t *testing.T) {
	previous := ""
	seen := make(map[string]bool, len(VimlsDiagnosticDefinitions))
	for _, definition := range VimlsDiagnosticDefinitions {
		if definition.Code <= previous {
			t.Fatalf("diagnostic definitions are not strictly code-sorted: %q after %q", definition.Code, previous)
		}
		if len(definition.Code) <= len("vimls/") || definition.Code[:len("vimls/")] != "vimls/" {
			t.Errorf("invalid vimls diagnostic code %q", definition.Code)
		}
		if definition.Message == "" {
			t.Errorf("diagnostic %q has an empty default message", definition.Code)
		}
		if definition.Severity == DiagnosticError {
			t.Errorf("vimls diagnostic %q must not be an error", definition.Code)
		}
		if seen[definition.Code] {
			t.Errorf("duplicate diagnostic code %q", definition.Code)
		}
		seen[definition.Code] = true
		previous = definition.Code
	}

	if _, ok := LookupVimlsDiagnostic("vim/E1012"); ok {
		t.Fatal("Vim error unexpectedly appeared in the vimls diagnostic catalog")
	}
}
