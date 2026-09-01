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
	MethodArgument      int // one-based receiver position; zero disables method syntax
	ReturnType          FunctionReturnType
	ReturnHelper        string // exact evalfunc.c ret_* helper
	Documentation       string
	DocumentationSource string
	// ArgumentChecks preserves Vim's evalfunc.c checker names in argument
	// order. A nil slice means Vim has no static checker for this function.
	ArgumentChecks []string
}

// DisplayName returns the conservative Vim type name represented by the
// metadata. Unknown intentionally has no display value.
func (t FunctionReturnType) DisplayName() string {
	switch t {
	case ReturnAny:
		return "any"
	case ReturnVoid:
		return "void"
	case ReturnBool:
		return "bool"
	case ReturnNumber:
		return "number"
	case ReturnFloat:
		return "float"
	case ReturnString:
		return "string"
	case ReturnBlob:
		return "blob"
	case ReturnList:
		return "list<any>"
	case ReturnDict:
		return "dict<any>"
	case ReturnNumberOrBool:
		return "number|bool"
	case ReturnChannel:
		return "channel"
	case ReturnJob:
		return "job"
	case ReturnTuple:
		return "tuple<any>"
	case ReturnFunction:
		return "func"
	default:
		return ""
	}
}

// BuiltinFunctions returns the pinned built-in function table in its source
// order. The returned slice is a copy and may be modified by the caller.
func BuiltinFunctions() []BuiltinFunction {
	result := make([]BuiltinFunction, len(builtinFunctions))
	copy(result, builtinFunctions[:])
	return result
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
