package analysis

import (
	"github.com/neoclide/vimls-go/internal/syntax"
	"strings"
)

// StaticType is an immutable, source-independent type fact. Its zero value is
// unknown. Neither construction nor ValueType exposes shared mutable storage.
type StaticType struct{ value *ValueType }

const staticTypeNodeLimit = 4096

func FreezeType(typ ValueType) StaticType {
	budget := staticTypeNodeLimit
	value := copyStaticType(typ, 0, &budget)
	if value.Name == "" {
		return StaticType{}
	}
	return StaticType{value: &value}
}

func (typ StaticType) ValueType() ValueType {
	if typ.value == nil {
		return ValueType{}
	}
	budget := staticTypeNodeLimit
	return copyStaticType(*typ.value, 0, &budget)
}

func copyStaticType(typ ValueType, depth int, budget *int) ValueType {
	if depth >= 64 || typ.imported || *budget <= len(typ.Arguments) {
		return ValueType{}
	}
	// Charge both visited nodes and reserved argument slots. A recursive
	// function type can share cyclic slices/pointers; depth alone would permit
	// exponential expansion when detaching that graph into an immutable tree.
	*budget -= 1 + len(typ.Arguments)
	// Only builtin identities are meaningful without the declaring script.
	switch typ.Name {
	case "number":
		typ.Name = "number"
	case "float":
		typ.Name = "float"
	case "bool":
		typ.Name = "bool"
	case "string":
		typ.Name = "string"
	case "blob":
		typ.Name = "blob"
	case "list":
		typ.Name = "list"
	case "dict":
		typ.Name = "dict"
	case "tuple":
		typ.Name = "tuple"
	case "func":
		typ.Name = "func"
	case "job":
		typ.Name = "job"
	case "channel":
		typ.Name = "channel"
	case "void":
		typ.Name = "void"
	case "any":
		typ.Name = "any"
	case "null":
		typ.Name = "null"
	case "none":
		typ.Name = "none"
	case "special":
		typ.Name = "special"
	default:
		return ValueType{}
	}
	if typ.Arguments != nil {
		arguments := make([]ValueType, len(typ.Arguments))
		for i, argument := range typ.Arguments {
			arguments[i] = copyStaticType(argument, depth+1, budget)
		}
		typ.Arguments = arguments
	}
	if typ.Return != nil {
		result := copyStaticType(*typ.Return, depth+1, budget)
		typ.Return = &result
	}
	return typ
}

// ExportTypes owns a frozen member table for one indexed source revision.
// A nil table means no eligible exports. Callers may share it across snapshots.
type ExportTypes struct{ members map[string]StaticType }

func NewExportTypes(members map[string]StaticType) *ExportTypes {
	if len(members) == 0 {
		return nil
	}
	copy := make(map[string]StaticType, len(members))
	for name, typ := range members {
		copy[strings.Clone(name)] = typ
	}
	return &ExportTypes{members: copy}
}

type ImportTypeBinding struct {
	Declaration syntax.Span
	Exports     *ExportTypes
}

// ImportTypes is a comparable immutable input identity, also when every target
// is unresolved. Construct once per dependency snapshot, not per expression.
type ImportTypes struct{ data *importTypeData }
type importTypeData struct{ bindings map[syntax.Span]*ExportTypes }

func NewImportTypes(bindings []ImportTypeBinding) ImportTypes {
	data := &importTypeData{bindings: make(map[syntax.Span]*ExportTypes, len(bindings))}
	for _, binding := range bindings {
		data.bindings[binding.Declaration] = binding.Exports
	}
	return ImportTypes{data: data}
}

func (result *FileAnalysis) ImportTypes() ImportTypes { return result.importTypes }

func (state *typeState) importedMemberType(expression *syntax.Expression, scope *Scope) (ValueType, bool) {
	if !state.result.hasImports || len(expression.Children) != 1 || state.result.File.Text(expression.Operator) != "." {
		return ValueType{}, false
	}
	receiver := expression.Children[0]
	if receiver == nil || receiver.Kind != syntax.ExpressionIdentifier {
		return ValueType{}, false
	}
	var declaration *Declaration
	if reference := state.references[receiver.Span]; reference != nil {
		declaration = reference.Declaration
	} else {
		declaration = resolve(scope, receiver.Value, receiver.Span.Start, false, nil)
	}
	if declaration == nil || declaration.Kind != SymbolKindImport {
		return ValueType{}, false
	}
	// Track provenance even without resolved inputs, so cold and enriched
	// indexing make the same conservative decision about re-exports.
	unknown := ValueType{imported: true}
	if state.result.importTypes.data == nil {
		return unknown, true
	}
	exports := state.result.importTypes.data.bindings[declaration.Span]
	if exports == nil {
		return unknown, true
	}
	fact := exports.members[expression.Value]
	if fact.value == nil {
		return unknown, true
	}
	if typ, ok := state.importedTypes[fact]; ok {
		return typ, true
	}
	typ := fact.ValueType()
	markImportedType(&typ)
	if state.importedTypes == nil {
		state.importedTypes = make(map[StaticType]ValueType)
	}
	state.importedTypes[fact] = typ
	return typ, true
}

func markImportedType(typ *ValueType) {
	typ.imported = true
	for i := range typ.Arguments {
		markImportedType(&typ.Arguments[i])
	}
	if typ.Return != nil {
		markImportedType(typ.Return)
	}
}
