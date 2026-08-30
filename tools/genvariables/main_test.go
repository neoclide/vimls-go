package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSourceFixture(t *testing.T) {
	source := []byte(`vimvars[VV_LEN] =
{
    {VV_NAME("count", VAR_NUMBER), NULL, VV_COMPAT+VV_RO},
    {VV_NAME("oldfiles", VAR_LIST), &t_list_string, 0},
};`)
	variables, err := parseSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(variables) != 2 || variables[0].Name != "v:count" || variables[0].Type != "number" || strings.Join(variables[0].Flags, "|") != "VariableCompatible|VariableReadOnly" || variables[1].Name != "v:oldfiles" || variables[1].Type != "list<string>" {
		t.Fatalf("variables = %#v", variables)
	}
}

func TestPinnedRevisionAndGeneratedTable(t *testing.T) {
	root := "/Users/chemzqm/lib/vim"
	resolved, err := exec.Command("git", "-C", root, "rev-list", "-n", "1", vimTag).Output()
	if err != nil || strings.TrimSpace(string(resolved)) != vimCommit {
		t.Skip("pinned Vim checkout is not available")
	}
	source, err := readRevisionFile(root, "src/evalvars.c")
	if err != nil {
		t.Fatal(err)
	}
	variables, err := parseSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(variables) != 118 {
		t.Fatalf("variables = %d, want 118", len(variables))
	}
	if err := addDocumentation(root, variables); err != nil {
		t.Fatal(err)
	}
	for _, variable := range variables {
		if variable.Documentation == "" || variable.DocumentationSource != "eval.txt" {
			t.Fatalf("%s documentation = %q from %q", variable.Name, variable.Documentation, variable.DocumentationSource)
		}
	}
	generated := filepath.Join(t.TempDir(), "variables_generated.go")
	if err := writeOutput(generated, variables); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(generated)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "internal", "vimdata", "variables_generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("variables_generated.go is stale; run tools/genvariables")
	}
}
