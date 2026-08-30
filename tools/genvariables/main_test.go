package main

import (
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
