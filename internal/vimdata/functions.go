package vimdata

// FunctionReturnType is the broad Vim value category returned by a builtin
// function. Unknown is used when Vim computes the return type dynamically or
// the source table uses a return helper that this package does not classify.
type FunctionReturnType uint8

const (
	ReturnUnknown FunctionReturnType = iota
	ReturnAny
	ReturnVoid
	ReturnBool
	ReturnNumber
	ReturnFloat
	ReturnString
	ReturnBlob
	ReturnList
	ReturnDict
	ReturnNumberOrBool
	ReturnChannel
	ReturnJob
	ReturnTuple
	ReturnFunction
)

// BuiltinFunction describes one builtin function from the pinned Vim source.
// MaxArgs is -1 for Vim's variadic functions (f_max_argc == VARGS).
type BuiltinFunction struct {
	Name                string
	MinArgs             int
	MaxArgs             int
	ReturnType          FunctionReturnType
	Documentation       string
	DocumentationSource string
	// ArgumentChecks preserves Vim's evalfunc.c checker names in argument
	// order. A nil slice means Vim has no static checker for this function.
	ArgumentChecks []string
}

// LookupFunction resolves a builtin function by its canonical name. Vim
// function names are case-sensitive and do not use Ex-command abbreviations.
func LookupFunction(name string) (BuiltinFunction, bool) {
	if name == "" {
		return BuiltinFunction{}, false
	}
	for _, function := range builtinFunctions {
		if function.Name == name {
			return function, true
		}
	}
	return BuiltinFunction{}, false
}

// BuiltinFunctionCount reports the number of functions in the pinned table.
func BuiltinFunctionCount() int { return len(builtinFunctions) }
