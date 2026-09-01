package vimdata

import "sort"

// VariableFlags are the properties stored in Vim's pinned vimvars[] table.
type VariableFlags uint8

const (
	VariableCompatible VariableFlags = 1 << iota
	VariableReadOnly
	VariableSandboxReadOnly
)

// Variable describes one predefined v: variable. Type uses Vim's type names;
// "special" is the internal null/none category. Broad containers use
// list<any> or dict<any> when Vim has no narrower static type.
type Variable struct {
	Name                string
	Type                string
	Flags               VariableFlags
	Documentation       string
	DocumentationSource string
}

// LookupVariable resolves an exact predefined v: variable name.
func LookupVariable(name string) (Variable, bool) {
	if len(name) < 3 || name[0] != 'v' || name[1] != ':' {
		return Variable{}, false
	}
	for _, variable := range builtinVariables {
		if variable.Name == name {
			return variable, true
		}
	}
	return Variable{}, false
}

// Variables returns the pinned vimvars[] table by canonical v: name. Callers
// own the returned slice.
func Variables() []Variable {
	result := append([]Variable(nil), builtinVariables[:]...)
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
