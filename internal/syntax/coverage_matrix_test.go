package syntax

import "testing"

// TestCoverageCommandMatrix keeps recovery paths exercised for commands that
// are commonly only partially present while a user edits a script.  The
// assertions intentionally concern the parser's public safety contract:
// every result must retain in-source spans regardless of whether Vim would
// accept the completed command.
func TestCoverageCommandMatrix(t *testing.T) {
	legacy := []string{
		"scriptversion 4\nlet g:x = {-> 1}\nunlet! g:x\n",
		"augroup demo\nautocmd!\nautocmd BufRead * ++once echo 'read'\naugroup END\n",
		"command! -bang -bar -count=2 -nargs=* Demo echo <q-args>\n",
		"function! s:Fn(a, ...) abort dict closure\n  let l:x = a:a\nendfunction\n",
		"try\n  throw 'x'\ncatch /^x$/\nfinally\n  echo 'done'\nendtry\n",
		"for [a, b; rest] in [[1, 2]]\n  continue\nendfor\n",
		"while 1\n  break\nendwhile\n",
		"silent! keepjumps 2,3substitute /a/b/ge | echo 'x'\n",
		"syntax match Demo /foo/ containedin=ALL nextgroup=Other\nhighlight link Demo Comment\n",
		"setlocal wildignore+=*.tmp,*.bak\nsetglobal tabstop=8\nset invnumber\n",
		"map <silent><expr> <Leader>x getline('.') .. ':x'\nunmap! <Leader>x\n",
		"global/^x/normal! A!\nvglobal/foo/delete _\n",
		"read ++edit ++fileformat=dos file.txt\nwrite! ++bin >> out.txt\n",
		"call feedkeys(" + `"x"` + ", 'n')\nexecute 'echo' 'x'\n",
		"lua << EOF\nprint('x')\nEOF\n",
		"python3 << trim EOF\nprint('x')\nEOF\n",
		"loadkeymap\na b\n# comment\n",
	}
	vim9 := []string{
		"vim9script\nexport var Value: number = 1\nconst Name = 'x'\nfinal List = [1, 2]\n",
		"vim9script\ndef Add(a: number, b: number = 1): number\n  return a + b\nenddef\n",
		"vim9script\nclass Child extends Base implements One, Two\n  public static var x: number = 0\n  def Method<T>(value: T): T\n    return value\n  enddef\nendclass\n",
		"vim9script\ninterface Runner\n  def Run(value: string): void\nendinterface\n",
		"vim9script\nenum Color\n  Red\n  Blue = 2\nendenum\n",
		"vim9script\nimport autoload 'foo/bar.vim' as bar\nexport def Use(): void\n  bar.Call()\nenddef\n",
		"vim9script\nvar result = items->map((_, v) => v + 1)->filter((_, v) => v > 0)\n",
		"vim9script\nif true\n  echo $'value: {1 + 2}'\nelif false\nelse\nendif\n",
		"vim9script\ntry\n  throw 'x'\ncatch /x/\nfinally\nendtry\n",
		"vim9script\ncommand! -nargs=* Demo { args -> echo args }\n",
		"vim9script\nlegacy echo 'legacy'\nvim9cmd var local = 1\n",
		"vim9script\n#{ key: [1, {-> 2}], other: null }\n",
	}
	for _, source := range legacy {
		assertFileCoverageBounds(t, (LegacyParser{}).Parse(source))
	}
	for _, source := range vim9 {
		assertFileCoverageBounds(t, (Vim9Parser{}).Parse(source))
	}

	// Combine syntactically meaningful prefixes and payloads rather than using
	// arbitrary bytes: this reaches scanner recovery decisions at command
	// boundaries that occur only when several Ex features meet on one line.
	commands := []string{"echo", "let", "var", "def", "command", "autocmd", "syntax", "highlight", "set", "map", "global", "substitute", "function", "class", "import", "call", "execute"}
	prefixes := []string{"", "1,2", "silent!", "keepjumps", "vertical", "legacy", "vim9cmd", "filter /x/"}
	payloads := []string{"", " x", " x | echo y", "! x", " /x/y/ge", " {-> x}", " << EOF\nx\nEOF", " # trailing", " ++once x", " (x: number)"}
	for index := range 1600 {
		command := commands[index%len(commands)]
		prefix := prefixes[(index/len(commands))%len(prefixes)]
		payload := payloads[(index/(len(commands)*len(prefixes)))%len(payloads)]
		source := prefix
		if source != "" {
			source += " "
		}
		source += command + payload + "\n"
		if index%2 == 0 {
			assertFileCoverageBounds(t, (LegacyParser{}).Parse(source))
		} else {
			assertFileCoverageBounds(t, (Vim9Parser{}).Parse("vim9script\n"+source))
		}
	}
	// Deterministic malformed token sequences exercise bounded recovery for
	// invalid UTF-8, punctuation, and line endings. This is deliberately a
	// corpus test rather than a fuzz target: it is fast and reproducible.
	alphabet := []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 :|\\/\"'#[]{}()<>?+-=,;\r\n\x80\xff")
	state := uint32(0x6d2b79f5)
	for index := range 3000 {
		bytes := make([]byte, 8+index%57)
		for offset := range bytes {
			state = state*1664525 + 1013904223
			bytes[offset] = alphabet[state%uint32(len(alphabet))]
		}
		assertFileCoverageBounds(t, Parse(string(bytes)))
	}
}

func assertFileCoverageBounds(t *testing.T, file *File) {
	t.Helper()
	for _, token := range file.Tokens {
		if token.Span.Start < 0 || token.Span.End < token.Span.Start || token.Span.End > len(file.Source) {
			t.Fatalf("invalid token span %#v in %q", token.Span, file.Source)
		}
	}
	for _, diagnostic := range file.Diagnostics {
		if diagnostic.Span.Start < 0 || diagnostic.Span.End < diagnostic.Span.Start || diagnostic.Span.End > len(file.Source) {
			t.Fatalf("invalid diagnostic span %#v in %q", diagnostic.Span, file.Source)
		}
	}
}
