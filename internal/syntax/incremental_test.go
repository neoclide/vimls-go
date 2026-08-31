package syntax

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

type incrementalEditCase struct {
	name string
	old  string
	new  string
}

var incrementalEditCases = []incrementalEditCase{
	{name: "legacy tail", old: "let first = 1\nlet last = 2\n", new: "let first = 1\nlet last = 20\n"},
	{name: "vim9 middle", old: "vim9script\nvar first = 1\nvar last = 2\n", new: "vim9script\nvar first = 10\nvar last = 2\n"},
	{name: "dialect", old: "vim9script\nvar value = 1\n", new: "var value = 1\n"},
	{name: "continuation", old: "vim9script\nvar value = [1,\n  2]\necho value\n", new: "vim9script\nvar value = [1,\n  20]\necho value\n"},
	{name: "heredoc", old: "let value =<< END\none\nEND\necho value\n", new: "let value =<< END\ntwo\nEND\necho value\n"},
	{name: "finish", old: "let before = 1\nfinish\ndead\n", new: "let before = 2\nfinish\ndead\n"},
}

func checkIncrementalParser(t *testing.T, parse func(*File, string) *File, test incrementalEditCase) {
	t.Helper()
	previous := Parse(test.old)
	beforeJSON := marshalSyntax(t, previous)
	beforeAliases := syntaxAliases(previous)

	got := parse(previous, test.new)
	want := Parse(test.new)
	if string(marshalSyntax(t, got)) != string(marshalSyntax(t, want)) {
		t.Fatalf("%s: incremental syntax differs from full parse\ngot:  %s\nwant: %s", test.name, marshalSyntax(t, got), marshalSyntax(t, want))
	}
	if gotAliases, wantAliases := syntaxAliases(got), syntaxAliases(want); !slices.Equal(gotAliases, wantAliases) {
		t.Fatalf("%s: alias topology differs\ngot:  %v\nwant: %v", test.name, gotAliases, wantAliases)
	}
	checkSyntaxSpans(t, got)
	if afterJSON := marshalSyntax(t, previous); string(afterJSON) != string(beforeJSON) {
		t.Fatalf("%s: previous syntax changed\nbefore: %s\nafter:  %s", test.name, beforeJSON, afterJSON)
	}
	if afterAliases := syntaxAliases(previous); !slices.Equal(afterAliases, beforeAliases) {
		t.Fatalf("%s: previous alias topology changed\nbefore: %v\nafter:  %v", test.name, beforeAliases, afterAliases)
	}
}

func marshalSyntax(t *testing.T, file *File) []byte {
	t.Helper()
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

var (
	spanReflectionType        = reflect.TypeOf(Span{})
	expressionReflectionType  = reflect.TypeOf((*Expression)(nil))
	typeReflectionType        = reflect.TypeOf((*Type)(nil))
	fileReflectionType        = reflect.TypeOf((*File)(nil))
	commandListReflectionType = reflect.TypeOf((*CommandList)(nil))
)

func syntaxAliases(file *File) []string {
	aliases := make(map[uintptr][]string)
	seen := make(map[uintptr]bool)
	var walk func(reflect.Value, string)
	walk = func(value reflect.Value, path string) {
		if !value.IsValid() {
			return
		}
		if value.Kind() == reflect.Interface {
			if !value.IsNil() {
				walk(value.Elem(), path)
			}
			return
		}
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return
			}
			pointer := value.Pointer()
			if trackedAliasType(value.Type()) {
				aliases[pointer] = append(aliases[pointer], path)
			}
			if seen[pointer] {
				return
			}
			seen[pointer] = true
			walk(value.Elem(), path)
			return
		}
		switch value.Kind() {
		case reflect.Struct:
			valueType := value.Type()
			for index := 0; index < value.NumField(); index++ {
				field := valueType.Field(index)
				if field.PkgPath == "" {
					walk(value.Field(index), path+"."+field.Name)
				}
			}
		case reflect.Slice, reflect.Array:
			for index := 0; index < value.Len(); index++ {
				walk(value.Index(index), fmt.Sprintf("%s[%d]", path, index))
			}
		}
	}
	walk(reflect.ValueOf(file), "File")
	result := make([]string, 0, len(aliases))
	for _, paths := range aliases {
		if len(paths) > 1 {
			slices.Sort(paths)
			result = append(result, strings.Join(paths, "="))
		}
	}
	slices.Sort(result)
	return result
}

func trackedAliasType(value reflect.Type) bool {
	return value == expressionReflectionType || value == typeReflectionType || value == fileReflectionType || value == commandListReflectionType
}

func checkSyntaxSpans(t *testing.T, file *File) {
	t.Helper()
	seen := make(map[uintptr]bool)
	var walk func(reflect.Value, string)
	walk = func(value reflect.Value, path string) {
		if !value.IsValid() {
			return
		}
		if value.Kind() == reflect.Interface {
			if !value.IsNil() {
				walk(value.Elem(), path)
			}
			return
		}
		if value.Kind() == reflect.Pointer {
			if value.IsNil() || seen[value.Pointer()] {
				return
			}
			seen[value.Pointer()] = true
			walk(value.Elem(), path)
			return
		}
		if value.Type() == spanReflectionType {
			span := value.Interface().(Span)
			if span.Start < 0 || span.End < span.Start || span.End > len(file.Source) {
				t.Errorf("%s: span %#v outside source of %d bytes", path, span, len(file.Source))
				return
			}
			if text := file.Text(span); len(text) != span.End-span.Start {
				t.Errorf("%s: Text(%#v) has %d bytes", path, span, len(text))
			}
			return
		}
		switch value.Kind() {
		case reflect.Struct:
			valueType := value.Type()
			for index := 0; index < value.NumField(); index++ {
				field := valueType.Field(index)
				if field.PkgPath == "" {
					walk(value.Field(index), path+"."+field.Name)
				}
			}
		case reflect.Slice, reflect.Array:
			for index := 0; index < value.Len(); index++ {
				walk(value.Index(index), fmt.Sprintf("%s[%d]", path, index))
			}
		}
	}
	walk(reflect.ValueOf(file), "File")
}

func TestIncrementalComparisonHelpers(t *testing.T) {
	for _, test := range incrementalEditCases {
		t.Run(test.name, func(t *testing.T) {
			checkIncrementalParser(t, func(_ *File, source string) *File { return Parse(source) }, test)
		})
	}
}

func TestReparseMatchesFullParse(t *testing.T) {
	for _, test := range incrementalEditCases {
		t.Run(test.name, func(t *testing.T) {
			checkIncrementalParser(t, Reparse, test)
		})
	}
}

func TestReparseReturnsPreviousForIdenticalSource(t *testing.T) {
	previous := Parse("let value = 1\n")
	if got := Reparse(previous, previous.Source); got != previous {
		t.Fatal("identical source did not return previous file")
	}
}

func TestReparseReusesIndependentUnits(t *testing.T) {
	oldSource := "let one = 1\nlet two = 2\nlet three = 3\nlet four = 4\n"
	newSource := "let one = 1\nlet two = 20\nlet three = 3\nlet four = 4\n"
	result := Reparse(Parse(oldSource), newSource)
	if result.incremental == nil || result.incremental.reused == 0 {
		t.Fatalf("incremental metadata = %#v", result.incremental)
	}
	checkIncrementalParser(t, Reparse, incrementalEditCase{name: "reused units", old: oldSource, new: newSource})
}

func TestReparseFallsBackForStatefulSyntax(t *testing.T) {
	tests := []incrementalEditCase{
		{name: "scriptversion", old: "scriptversion 4\nlet value = 1\n", new: "scriptversion 4\nlet value = 2\n"},
		{name: "legacy function", old: "function Func()\n  let value = 1\nendfunction\n", new: "function Func()\n  let value = 2\nendfunction\n"},
		{name: "vim9 def", old: "vim9script\ndef Func()\n  var value = 1\nenddef\n", new: "vim9script\ndef Func()\n  var value = 2\nenddef\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			previous := Parse(test.old)
			if previous.incremental == nil || previous.incremental.eligible {
				t.Fatalf("incremental metadata = %#v", previous.incremental)
			}
			checkIncrementalParser(t, Reparse, test)
		})
	}
}

func TestReparseHundredEdits(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	source := "let one = 1000\nlet two = 2000\nlet three = 3000\nlet four = 4000\n"
	previous := Parse(source)
	for step := 0; step < 100; step++ {
		positions := make([]int, 0)
		for index := range source {
			if source[index] >= '0' && source[index] <= '9' {
				positions = append(positions, index)
			}
		}
		position := positions[random.Intn(len(positions))]
		replacement := byte('0' + random.Intn(10))
		source = source[:position] + string(replacement) + source[position+1:]
		got := Reparse(previous, source)
		want := Parse(source)
		if string(marshalSyntax(t, got)) != string(marshalSyntax(t, want)) {
			t.Fatalf("step %d differs", step)
		}
		checkSyntaxSpans(t, got)
		previous = got
	}
}

func TestParseUnitsKeepMultilineOwnersTogether(t *testing.T) {
	tests := []struct {
		name   string
		source string
		text   string
	}{
		{name: "legacy continuation", source: "let value = [1,\n\\ 2]\necho value\n", text: "let value = [1,\n\\ 2]\n"},
		{name: "vim9 continuation", source: "vim9script\nvar value = [1,\n  2]\necho value\n", text: "var value = [1,\n  2]\n"},
		{name: "heredoc", source: "let value =<< END\none\nEND\necho value\n", text: "let value =<< END\none\nEND\n"},
		{name: "text body", source: "append\none\n.\necho 'after'\n", text: "append\none\n.\n"},
		{name: "keymap", source: "loadkeymap\na b\nc d\n", text: "loadkeymap\na b\nc d\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := Parse(test.source)
			if file.incremental == nil {
				t.Fatal("missing incremental metadata")
			}
			found := false
			for _, unit := range file.incremental.units {
				if file.Text(unit.span) == test.text {
					found = true
					if unit.independent {
						t.Fatal("multiline unit marked independent")
					}
				}
			}
			if !found {
				t.Fatalf("units = %#v", file.incremental.units)
			}
		})
	}
}

func TestParseUnitScannerState(t *testing.T) {
	file := Parse("scriptversion 4\ndef Func()\n  var value = 1\nenddef\necho 'after'\n")
	if len(file.incremental.units) < 5 {
		t.Fatalf("units = %#v", file.incremental.units)
	}
	def := file.incremental.units[1]
	if def.entry.scriptVersion != 4 || def.exit.activeDialect != Vim9 || len(def.exit.dialectStack) != 1 {
		t.Fatalf("def state = entry %#v exit %#v", def.entry, def.exit)
	}
	enddef := file.incremental.units[3]
	if enddef.exit.activeDialect != Legacy || len(enddef.exit.dialectStack) != 0 {
		t.Fatalf("enddef exit = %#v", enddef.exit)
	}
}

func TestReparseConcurrentReaders(t *testing.T) {
	oldSource := "let one = 1000\nlet two = 2000\nlet three = 3000\nlet four = 4000\n"
	previous := Parse(oldSource)
	before := marshalSyntax(t, previous)
	var wait sync.WaitGroup
	for digit := byte('1'); digit <= '8'; digit++ {
		wait.Add(1)
		go func(digit byte) {
			defer wait.Done()
			source := oldSource[:42] + string(digit) + oldSource[43:]
			got := Reparse(previous, source)
			want := Parse(source)
			gotJSON, gotError := json.Marshal(got)
			wantJSON, wantError := json.Marshal(want)
			if gotError != nil || wantError != nil || string(gotJSON) != string(wantJSON) {
				t.Errorf("digit %q differs: got error %v, want error %v", digit, gotError, wantError)
			}
		}(digit)
	}
	wait.Wait()
	if after := marshalSyntax(t, previous); string(after) != string(before) {
		t.Fatal("concurrent reparses changed previous file")
	}
}

func TestReparseOfficialCorpusEdits(t *testing.T) {
	corpus := readGeneratedOfficialCorpus(t)
	for _, test := range corpus.Cases {
		edits := []string{" " + test.Source, test.Source + "\n"}
		if len(test.Source) > 0 {
			middle := len(test.Source) / 2
			edits = append(edits, test.Source[:middle]+" "+test.Source[middle+1:])
		}
		previous := Parse(test.Source)
		for index, source := range edits {
			got, want := Reparse(previous, source), Parse(source)
			gotJSON, wantJSON := marshalSyntax(t, got), marshalSyntax(t, want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("%s edit %d differs\ngot:  %s\nwant: %s", test.Origin, index, gotJSON, wantJSON)
			}
			checkSyntaxSpans(t, got)
		}
	}
}

func FuzzIncrementalParse(f *testing.F) {
	seeds := []string{
		"let one = 1\nlet two = 2\nlet three = 3\n",
		"vim9script\nvar one = 1\nvar two = 2\necho one + two\n",
		"vim9script\ndef Func()\n  var value = [1,\n    2]\nenddef\n",
		"let value =<< END\none\nEND\necho value\n",
		"if true\n  finish\nendif\necho 'after'\n",
	}
	for _, source := range seeds {
		f.Add(source, uint64(len(source)/2), uint64(len(source)/2), "x")
	}
	f.Fuzz(func(t *testing.T, oldSource string, rawStart, rawEnd uint64, replacement string) {
		if len(oldSource)+len(replacement) > 64<<10 {
			t.Skip()
		}
		start := int(rawStart % uint64(len(oldSource)+1))
		end := int(rawEnd % uint64(len(oldSource)+1))
		if start > end {
			start, end = end, start
		}
		newSource := oldSource[:start] + replacement + oldSource[end:]
		checkIncrementalParser(t, Reparse, incrementalEditCase{name: "fuzz", old: oldSource, new: newSource})
	})
}
