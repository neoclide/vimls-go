package syntax

import (
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/vimdata"
)

func TestParserDeterministicRecoveryCombinations(t *testing.T) {
	// This is a fixed, reviewable mutation corpus rather than random fuzzing:
	// it keeps recovery paths reproducible while combining command heads,
	// delimiters, operators, and continuation tails seen during editing.
	parts := []string{"var", "def", "class", "enum", "syntax", "autocmd", "command", "set", "substitute", "global", "import", "echo", "(", "[", "{", "'text'", "# comment", "|", "->", "??", "=", "\\", "\n"}
	state := uint32(0x6d2b79f5)
	next := func() int {
		state = state*1664525 + 1013904223
		return int(state % uint32(len(parts)))
	}
	for index := range 512 {
		var source strings.Builder
		if index&1 == 0 {
			source.WriteString("vim9script\n")
		}
		for count := range 8 {
			source.WriteString(parts[next()])
			if count%3 != 2 {
				source.WriteByte(' ')
			}
		}
		text := source.String()
		for _, parser := range []func(string) *File{(LegacyParser{}).Parse, (Vim9Parser{}).Parse} {
			file := parser(text)
			if file == nil || file.Source != text {
				t.Fatalf("case %d did not retain source", index)
			}
			assertFileSpansAt(t, file, "deterministic recovery")
		}
	}
}

// This matrix intentionally uses complete Ex lines rather than invoking scanner
// helpers directly.  It protects the command scanner's recovery boundaries: a
// complex or malformed command must not consume a following command.
func TestScannerCommandRecoveryMatrix(t *testing.T) {
	tests := []struct {
		name       string
		parser     func(string) *File
		source     string
		canonicals []string
		check      func(*testing.T, *File)
	}{
		{
			name:       "legacy augroup autocmd and user command",
			parser:     func(source string) *File { return (LegacyParser{}).Parse(source) },
			source:     "augroup! MatrixGroup\n  autocmd! BufRead,BufNewFile *.vim ++nested ++once echo 'loaded'\naugroup END\ncommand! -nargs=? -bang -bar -range=% -complete=customlist,Complete Matrix echo <q-args>\necho after\n",
			canonicals: []string{"augroup", "autocmd", "augroup", "command", "echo"},
			check: func(t *testing.T, file *File) {
				t.Helper()
				autocmd := file.Commands[1].Autocmd
				if autocmd == nil || autocmd.Operation != AutocmdReplace || len(autocmd.Events) != 2 || len(autocmd.Modifiers) != 2 || file.Text(autocmd.Pattern) != "*.vim" {
					t.Fatalf("autocmd = %#v", autocmd)
				}
				definition := file.Commands[3].UserCommand
				if definition == nil || file.Text(definition.Name) != "Matrix" || !strings.Contains(file.Text(definition.Body), "<q-args>") || len(definition.Attributes) != 5 {
					t.Fatalf("user command = %#v", definition)
				}
			},
		},
		{
			name:       "legacy map syntax highlight set global and substitute",
			parser:     func(source string) *File { return (LegacyParser{}).Parse(source) },
			source:     "nnoremap <expr> <buffer> <silent> <nowait> lhs get({'key': {-> 'value'}}, 'key', {})['key']()\nsyntax region Matrix start=/\\%(^\\|\\s\\)foo/ skip=/\\\\./ end=/bar$/ contains=MatrixNext keepend\nsyntax sync match MatrixSync grouphere Matrix /begin/\nhighlight default Matrix guifg='red' cterm=bold\nsetlocal noignorecase invlist tabstop=4 wildignore+=*.tmp\nglobal /foo\\/bar/ substitute/foo/bar/gc | echo later\necho after\n",
			canonicals: []string{"nnoremap", "syntax", "syntax", "highlight", "setlocal", "global", "echo"},
			check: func(t *testing.T, file *File) {
				t.Helper()
				mapping := file.Commands[0].Mapping
				if mapping == nil || !mapping.Expr || !mapping.Buffer || !mapping.Silent || !mapping.Nowait || mapping.RHSExpression == nil {
					t.Fatalf("mapping = %#v", mapping)
				}
				region := file.Commands[1].Syntax
				if region == nil || region.Kind != SyntaxRegion || len(region.Patterns) != 3 || len(region.Options) != 2 {
					t.Fatalf("region = %#v", region)
				}
				if file.Commands[3].Highlight == nil || len(file.Commands[3].Highlight.Attributes) != 2 || file.Commands[4].Set == nil || len(file.Commands[4].Set.Options) != 4 {
					t.Fatalf("highlight = %#v set = %#v", file.Commands[3].Highlight, file.Commands[4].Set)
				}
				if file.Commands[5].Embedded == nil || len(file.Commands[5].Embedded.Commands) != 2 || file.Commands[5].Embedded.Commands[0].Substitute == nil || file.Commands[6].Canonical != "echo" {
					t.Fatalf("global = %#v command after global = %#v", file.Commands[5], file.Commands[6])
				}
			},
		},
		{
			name:       "vim9 collected autocmd heredoc and nested expressions",
			parser:     func(source string) *File { return (Vim9Parser{}).Parse(source) },
			source:     "vim9script\nautocmd MatrixGroup BufEnter *.vim ++once {\n  var handler: func(string): string = (name: string): string => $'hello {name}'\n  echo handler('vim')\n}\nvar payload =<< trim END\n  one | opaque\n  # opaque\nEND\nvar values = [1, 2]->map((value) => value * 2)->filter((_, value) => value > 2)\necho values\n",
			canonicals: []string{"vim9script", "autocmd", "var", "var", "echo"},
			check: func(t *testing.T, file *File) {
				t.Helper()
				if file.Commands[1].Autocmd == nil || file.Commands[1].Embedded == nil || len(file.Commands[1].Embedded.Commands) != 2 {
					t.Fatalf("vim9 autocmd = %#v", file.Commands[1])
				}
				if file.Commands[2].Heredoc == nil || !file.Commands[2].Heredoc.Trim || file.Text(file.Commands[2].Heredoc.Body) != "  one | opaque\n  # opaque" {
					t.Fatalf("heredoc = %#v", file.Commands[2].Heredoc)
				}
				if len(file.Commands[3].Expressions) != 1 || file.Commands[3].Expressions[0] == nil {
					t.Fatalf("method expression = %#v", file.Commands[3].Expressions)
				}
			},
		},
		{
			name:       "malformed commands retain following command",
			parser:     func(source string) *File { return (Vim9Parser{}).Parse(source) },
			source:     "vim9script\nsyntax region Broken start=/foo/ end=\ncommand! -nargs= Broken\nvar broken = Fn<number>(\nautocmd BufEnter *.vim {\n  echo 'unterminated'\necho recovered\n",
			canonicals: []string{"vim9script", "syntax", "command", "var", "echo", "echo"},
			check: func(t *testing.T, file *File) {
				t.Helper()
				if len(file.Diagnostics) == 0 {
					t.Fatal("malformed matrix input must retain diagnostic recovery")
				}
				if file.Commands[len(file.Commands)-1].Canonical != "echo" || file.Text(file.Commands[len(file.Commands)-1].Argument) != "recovered" {
					t.Fatalf("recovered command = %#v", file.Commands[len(file.Commands)-1])
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parser(test.source)
			if file == nil || file.Source != test.source {
				t.Fatalf("source was not retained: %#v", file)
			}
			if len(file.Commands) != len(test.canonicals) {
				t.Fatalf("commands = %#v, want %d", file.Commands, len(test.canonicals))
			}
			for index, canonical := range test.canonicals {
				if file.Commands[index].Canonical != canonical {
					t.Errorf("command %d canonical = %q, want %q", index, file.Commands[index].Canonical, canonical)
				}
			}
			test.check(t, file)
			assertFileSpansAt(t, file, test.name)
		})
	}
}

// These forms take distinct scanner paths but share the same important parser
// contract: they are retained as commands and never consume the next command.
func TestScannerRareCommandFamiliesRetainFollowingCommand(t *testing.T) {
	tests := []struct {
		name, source string
		parser       func(string) *File
	}{
		{"legacy control flow", "for item in [1, 2]\n  echo item\nendfor\nwhile 0\nendwhile\ntry\n  throw 'x'\ncatch /x/\nfinally\nendtry\necho after\n", func(s string) *File { return (LegacyParser{}).Parse(s) }},
		{"legacy execution commands", "argdo echo argv()\nwindo echo winnr()\ntabdo echo tabpagenr()\nbufdo echo bufnr('%')\nfolddoopen echo 'open'\nfolddoclosed echo 'closed'\necho after\n", func(s string) *File { return (LegacyParser{}).Parse(s) }},
		{"vim9 class enum and interface", "vim9script\ninterface Face\n  def Run(value: number): string\nendinterface\nclass Base\n  var value: number\n  def new(value: number)\n    this.value = value\n  enddef\nendclass\nenum Choice\n  One\n  Two = 2\nendenum\necho after\n", func(s string) *File { return (Vim9Parser{}).Parse(s) }},
		{"vim9 command modifiers", "vim9script\nlegacy echo 'legacy'\nvim9cmd echo 'vim9'\nsilent! keepalt keepjumps noautocmd echo 'quiet'\naboveleft vertical botright new\necho after\n", func(s string) *File { return (Vim9Parser{}).Parse(s) }},
		{"vim9 expression payloads", "vim9script\nvar dict = {key: [1, 2], nested: {-> 'value'}}\nvar text = $'value: {dict.key[0]}'\nvar value = dict->get('key')->len()\nvar optional = value ?? 0\necho after\n", func(s string) *File { return (Vim9Parser{}).Parse(s) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.parser(test.source)
			if file == nil || len(file.Commands) == 0 || file.Commands[len(file.Commands)-1].Canonical != "echo" || file.Text(file.Commands[len(file.Commands)-1].Argument) != "after" {
				t.Fatalf("commands = %#v diagnostics = %#v", file.Commands, file.Diagnostics)
			}
			assertFileSpansAt(t, file, test.name)
		})
	}
}

// TestScannerFixedSeedRecoveryStress covers malformed edit-time text that is
// too broad for an individual fixture. It is deterministic and checks the
// safety contract only: parsing must retain the immutable source and every
// diagnostic must remain within it.
func TestScannerFixedSeedRecoveryStress(t *testing.T) {
	alphabet := []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 !@#$%^&*()-_=+[]{}<>/\\|:;,.?\"'\n\t")
	state := uint32(0xa5a5f00d)
	next := func() byte {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		return alphabet[state%uint32(len(alphabet))]
	}
	for index := range 2048 {
		var source strings.Builder
		if index&1 == 0 {
			source.WriteString("vim9script\n")
		}
		for range 96 {
			source.WriteByte(next())
		}
		input := source.String()
		for _, parser := range []func(string) *File{(LegacyParser{}).Parse, (Vim9Parser{}).Parse} {
			file := parser(input)
			if file == nil || file.Source != input {
				t.Fatalf("case %d did not retain source", index)
			}
			for _, diagnostic := range file.Diagnostics {
				if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(input) {
					t.Fatalf("case %d diagnostic = %#v", index, diagnostic)
				}
			}
		}
	}
}

func TestScannerFixedSeedCommandStress(t *testing.T) {
	metadata := vimdata.Commands()
	heads := make([]string, len(metadata))
	for index, command := range metadata {
		heads[index] = command.Name
	}
	parts := []string{"", "!", " 1", " /a[b/c]d/", " {x: [1, 2]}", " ->map((x) => x)", " =<< END", " | echo next", " # comment", " \" comment", " ++once", " <args>", " (value: number)", " /foo/bar/g", " [a-z]", " \\&", " %", " @a", " << trim END"}
	state := uint32(0x1f123bb5)
	next := func(limit int) int {
		state = state*1664525 + 1013904223
		return int(state % uint32(limit))
	}
	for index := range 2048 {
		var source strings.Builder
		if index&1 == 0 {
			source.WriteString("vim9script\n")
		}
		for range 5 {
			source.WriteString(heads[next(len(heads))])
			for range 4 {
				source.WriteString(parts[next(len(parts))])
			}
			source.WriteByte('\n')
		}
		input := source.String()
		for _, parser := range []func(string) *File{(LegacyParser{}).Parse, (Vim9Parser{}).Parse} {
			file := parser(input)
			if file == nil || file.Source != input {
				t.Fatalf("case %d did not retain source", index)
			}
		}
	}
}
