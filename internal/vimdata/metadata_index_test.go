package vimdata

import (
	"reflect"
	"strings"
	"testing"
)

func TestMetadataExistenceAndOwnership(t *testing.T) {
	for _, option := range builtinOptions {
		for _, name := range []string{option.Name, option.ShortName, "&g:" + option.Name, "&l:" + option.Name} {
			_, want := LookupOption(name)
			if got := IsOption(name); got != want {
				t.Fatalf("IsOption(%q) = %v, want %v", name, got, want)
			}
		}
	}
	for _, function := range builtinFunctions {
		if !IsFunction(function.Name) {
			t.Fatalf("IsFunction(%q) = false", function.Name)
		}
	}
	for _, name := range []string{"", "ABS", "missing_function"} {
		if IsFunction(name) {
			t.Fatalf("IsFunction(%q) = true", name)
		}
	}
	for _, name := range []string{"", "NUMBER", "missing_option", "numb"} {
		if IsOption(name) {
			t.Fatalf("IsOption(%q) = true", name)
		}
	}
	if allocations := testing.AllocsPerRun(100, func() {
		benchmarkMetadataFound = IsOption("&l:nu") && IsFunction("abs")
	}); allocations != 0 {
		t.Fatalf("existence queries allocate: %g", allocations)
	}
	values := OptionValues("ambiwidth")
	want := append([]string(nil), values...)
	values[0] = "changed"
	if got := OptionValues("ambiwidth"); !reflect.DeepEqual(got, want) {
		t.Fatalf("option values exposed global metadata: %v", got)
	}
}

func TestOptionIndexCanonicalPrecedence(t *testing.T) {
	index, order := buildOptionIndex([]Option{
		{Name: "zeta", ShortName: "alpha"},
		{Name: "alpha", ShortName: "a"},
		{Name: "beta"},
	})
	if index["alpha"] != 1 || index["a"] != 1 || !reflect.DeepEqual(order, []int{1, 2, 0}) {
		t.Fatalf("index=%v order=%v", index, order)
	}
	if _, ok := index[""]; ok {
		t.Fatal("empty abbreviation indexed")
	}
}

func TestAutocmdIndexPreservesEqualFold(t *testing.T) {
	names := []string{"", "missing", "ſourcecmd", "KeyInputPre", strings.Repeat("A", 65), "\xff"}
	for _, event := range autocmdEvents {
		names = append(names, event.Name, strings.ToLower(event.Name), strings.ToUpper(event.Name))
	}
	for _, name := range names {
		var want AutocmdEvent
		found := false
		for _, event := range autocmdEvents {
			if strings.EqualFold(name, event.Name) {
				want, found = event, true
				break
			}
		}
		if got, ok := LookupAutocmdEvent(name); ok != found || got != want {
			t.Fatalf("LookupAutocmdEvent(%q)=%#v,%v; want %#v,%v", name, got, ok, want, found)
		}
	}
}
