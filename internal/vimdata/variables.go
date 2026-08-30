package vimdata

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

// BuiltinVariableCount reports the number of variables in Vim's vimvars[].
func BuiltinVariableCount() int { return len(builtinVariables) }
