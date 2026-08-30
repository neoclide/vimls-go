package syntax

import "testing"

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
		if seen[definition.Code] {
			t.Errorf("duplicate diagnostic code %q", definition.Code)
		}
		seen[definition.Code] = true
		previous = definition.Code
	}

	unknown, ok := LookupVimlsDiagnostic(DiagnosticUnknownOption)
	if !ok || unknown.Message != "Unknown option" || unknown.Severity != DiagnosticWarning {
		t.Fatalf("unknown-option definition = %#v, %v", unknown, ok)
	}
	if _, ok := LookupVimlsDiagnostic("vim/E1012"); ok {
		t.Fatal("Vim error unexpectedly appeared in the vimls diagnostic catalog")
	}
}
