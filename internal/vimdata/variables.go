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
	index, ok := builtinVariableIndex[name]
	if !ok {
		return Variable{}, false
	}
	return builtinVariables[index], true
}

var builtinVariableIndex, builtinVariableOrder = buildVariableIndex()

func buildVariableIndex() (map[string]int, []int) {
	index := make(map[string]int, len(builtinVariables))
	order := make([]int, len(builtinVariables))
	for i, variable := range builtinVariables {
		index[variable.Name] = i
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return builtinVariables[order[i]].Name < builtinVariables[order[j]].Name })
	return index, order
}

// Variables returns the pinned vimvars[] table by canonical v: name. Callers
// own the returned slice.
func Variables() []Variable {
	result := make([]Variable, len(builtinVariables))
	for i, index := range builtinVariableOrder {
		result[i] = builtinVariables[index]
	}
	return result
}
