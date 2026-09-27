package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitHelperArguments(t *testing.T) {
	tests := []struct {
		name   string
		source string
		open   int
		close  int
		want   []string
		ok     bool
	}{
		{
			name:   "empty",
			source: "Check()",
			open:   5,
			close:  7,
			want:   []string{},
			ok:     true,
		},
		{
			name:   "trimmed and multiline",
			source: "Check( first,\n second \t, third )tail",
			want:   []string{"first", "second", "third"},
			ok:     true,
		},
		{
			name:   "nested delimiters",
			source: "Check([1, 2], {'x': (3, 4)}, call(5, [6]))",
			want:   []string{"[1, 2]", "{'x': (3, 4)}", "call(5, [6])"},
			ok:     true,
		},
		{
			name:   "quoted commas and delimiters",
			source: `Check('a,b)''c', "x,\")", {'a': "}"})`,
			want:   []string{`'a,b)''c'`, `"x,\")"`, `{'a': "}"}`},
			ok:     true,
		},
		{
			name:   "trailing comma",
			source: "Check(first, second,\n )",
			want:   []string{"first", "second"},
			ok:     true,
		},
		{
			name:   "invalid bounds",
			source: "Check(first)",
			open:   -1,
			close:  12,
			ok:     false,
		},
		{
			name:   "wrong opening byte",
			source: "Check[first]",
			open:   5,
			close:  12,
			ok:     false,
		},
		{
			name:   "wrong closing byte",
			source: "Check(first]",
			open:   5,
			close:  12,
			ok:     false,
		},
		{
			name:   "leading empty argument",
			source: "Check(, first)",
			ok:     false,
		},
		{
			name:   "middle empty argument",
			source: "Check(first,, second)",
			ok:     false,
		},
		{
			name:   "unbalanced nested delimiter",
			source: "Check([first, second)",
			ok:     false,
		},
		{
			name:   "unterminated single quote",
			source: "Check('first)",
			ok:     false,
		},
		{
			name:   "unterminated double quote",
			source: "Check(\"first\\)",
			ok:     false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			open, close := test.open, test.close
			switch test.name {
			case "invalid bounds", "wrong opening byte", "wrong closing byte":
			default:
				open = strings.IndexByte(test.source, '(')
				close = strings.LastIndexByte(test.source, ')') + 1
			}
			got, gotOK := splitHelperArguments([]byte(test.source), open, close)
			if gotOK != test.ok {
				t.Fatalf("ok = %v, want %v (arguments %#v)", gotOK, test.ok, got)
			}
			if !test.ok {
				return
			}
			actual := make([]string, len(got))
			for index, argument := range got {
				actual[index] = test.source[argument.Start:argument.End]
			}
			if !reflect.DeepEqual(actual, test.want) {
				t.Fatalf("arguments = %#v, want %#v", actual, test.want)
			}
		})
	}
}

func TestDecodeStaticStringList(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
		ok     bool
	}{
		{name: "empty", source: "[]", want: []string{}, ok: true},
		{name: "single quoted", source: `['one', 'it''s', 'back\\slash']`, want: []string{"one", "it's", `back\\slash`}, ok: true},
		{name: "double quoted", source: `["a\\b", "a\"b", "line\nnext", "tab\tend"]`, want: []string{"a\\b", "a\"b", "line\nnext", "tab\tend"}, ok: true},
		{name: "numeric escapes", source: `["\x41\X42\u43\U00000044\101"]`, want: []string{"ABCDA"}, ok: true},
		{name: "unicode content", source: `['你好', "世界"]`, want: []string{"你好", "世界"}, ok: true},
		{name: "multiline whitespace", source: "[\n  'one',\n\t\"two\"\n]", want: []string{"one", "two"}, ok: true},
		{name: "legacy continuation", source: `[
  \ 'one',
\ "two",
]`, want: []string{"one", "two"}, ok: true},
		{name: "argument whitespace", source: "  [ 'one' ]  ", want: []string{"one"}, ok: true},
		{name: "expression", source: "[name]", ok: false},
		{name: "concatenation", source: `["one" .. "two"]`, ok: false},
		{name: "interpolation", source: `[$"one {name}"]`, ok: false},
		{name: "comment", source: `["one", "two" "comment"]`, ok: false},
		{name: "number", source: `[1]`, ok: false},
		{name: "nested list", source: `[['one']]`, ok: false},
		{name: "trailing garbage", source: `["one"] + []`, ok: false},
		{name: "missing closing list", source: `["one"`, ok: false},
		{name: "missing closing string", source: `["one]`, ok: false},
		{name: "unknown escape", source: `["one\q"]`, ok: false},
		{name: "malformed hex escape", source: `["one\x"]`, ok: false},
		{name: "malformed unicode escape", source: `["one\u-"]`, ok: false},
		{name: "nul octal escape", source: `["one\000two"]`, ok: false},
		{name: "nul hex escape", source: `["one\x00two"]`, ok: false},
		{name: "same line continuation rejected", source: `[\ 'one']`, ok: false},
		{name: "invalid span", source: `["one"]`, want: nil, ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			argument := helperArgument{Start: 0, End: len(test.source)}
			if test.name == "invalid span" {
				argument = helperArgument{Start: -1, End: len(test.source)}
			}
			got, gotOK := decodeStaticStringList([]byte(test.source), argument)
			if gotOK != test.ok {
				t.Fatalf("ok = %v, want %v (values %#v)", gotOK, test.ok, got)
			}
			if test.ok && !reflect.DeepEqual(got, test.want) {
				t.Fatalf("values = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestDecodeStaticStringListDoesNotReadOutsideSpan(t *testing.T) {
	source := []byte(`prefix ["inside"] suffix`)
	values, ok := decodeStaticStringList(source, helperArgument{Start: 7, End: 17})
	if !ok || !reflect.DeepEqual(values, []string{"inside"}) {
		t.Fatalf("values = %#v, ok = %v", values, ok)
	}
}

func TestDecodeStaticStringListPreservesInvalidUTF8Bytes(t *testing.T) {
	source := []byte{'[', '\'', 0xff, 0xfe, '\'', ']'}
	values, ok := decodeStaticStringList(source, helperArgument{Start: 0, End: len(source)})
	if !ok || len(values) != 1 || len(values[0]) != 2 || values[0][0] != 0xff || values[0][1] != 0xfe {
		t.Fatalf("values = %#v, ok = %v", values, ok)
	}
}
