package vimdata

import "testing"

var benchmarkMetadataFound bool
var benchmarkOption Option
var benchmarkFunction BuiltinFunction
var benchmarkVariable Variable
var benchmarkEvent AutocmdEvent
var benchmarkOptions []Option
var benchmarkVariables []Variable
var benchmarkOptionValues []string
var benchmarkMetadataIndex map[string]int
var benchmarkMetadataOrder []int

func BenchmarkMetadataLookup(b *testing.B) {
	for _, name := range []string{"abs", "map", "xor", "missing_function"} {
		b.Run("function/"+name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkFunction, benchmarkMetadataFound = LookupFunction(name)
			}
		})
	}
	for _, name := range []string{"aleph", "number", "t_xs", "missing_option", "nu", "&l:nu", "<t_xs>"} {
		b.Run("option/"+name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkOption, benchmarkMetadataFound = LookupOption(name)
			}
		})
		b.Run("option_exists/"+name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkMetadataFound = IsOption(name)
			}
		})
	}
	for _, name := range []string{"v:count", "v:version", "v:missing"} {
		b.Run("variable/"+name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkVariable, benchmarkMetadataFound = LookupVariable(name)
			}
		})
	}
	for _, name := range []string{"BufAdd", "OptionSet", "WinScrolled", "missing_event", "WINSCROLLED"} {
		b.Run("event/"+name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkEvent, benchmarkMetadataFound = LookupAutocmdEvent(name)
			}
		})
	}
}

func BenchmarkMetadataEnumeration(b *testing.B) {
	b.Run("options", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkOptions = Options()
		}
	})
	b.Run("variables", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkVariables = Variables()
		}
	})
	b.Run("option_values", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkOptionValues = OptionValues("ambiwidth")
		}
	})
}

func BenchmarkMetadataIndexInitialization(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchmarkMetadataIndex = buildFunctionIndex()
		benchmarkMetadataIndex, benchmarkMetadataOrder = buildOptionIndex(builtinOptions[:])
		benchmarkMetadataIndex, benchmarkMetadataOrder = buildVariableIndex()
		benchmarkMetadataIndex = buildAutocmdEventIndex()
	}
}
