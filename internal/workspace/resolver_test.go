package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/chemzqm/vimls-go/internal/syntax"
)

func TestPathResolverResolvesRelativeImport(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "plugin", "main.vim")
	target := filepath.Join(root, "plugin", "lib.vim")
	writeResolverFile(t, from, "vim9script\nimport './lib.vim' as lib\n")
	writeResolverFile(t, target, "vim9script\nexport def Run(): void\nenddef\n")
	file := syntax.Parse(mustResolverRead(t, from))
	if len(file.Commands) < 2 || file.Commands[1].Import == nil {
		t.Fatalf("parsed import = %#v", file.Commands)
	}
	resolver, err := NewPathResolver(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := resolver.ResolveImport(from, file, file.Commands[1].Import)
	if result.Dynamic || result.Path != filepath.Clean(target) || len(result.Candidates) != 1 {
		t.Fatalf("relative import = %#v, want %q", result, target)
	}
}

func TestPathResolverSearchesRuntimeImportAndAutoloadInOrder(t *testing.T) {
	root := t.TempDir()
	first := t.TempDir()
	second := t.TempDir()
	from := filepath.Join(root, "plugin.vim")
	writeResolverFile(t, from, "vim9script\nimport 'shared.vim' as shared\nimport autoload 'pkg/api.vim' as api\n")
	firstImport := filepath.Join(first, "import", "shared.vim")
	secondImport := filepath.Join(second, "import", "shared.vim")
	firstAutoload := filepath.Join(first, "autoload", "pkg", "api.vim")
	secondAutoload := filepath.Join(second, "autoload", "pkg", "api.vim")
	writeResolverFile(t, firstImport, "vim9script\n")
	writeResolverFile(t, secondImport, "vim9script\n")
	writeResolverFile(t, firstAutoload, "vim9script\n")
	writeResolverFile(t, secondAutoload, "vim9script\n")
	file := syntax.Parse(mustResolverRead(t, from))
	resolver, err := NewPathResolver(root, []string{second, first})
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []string{secondImport, secondAutoload} {
		result := resolver.ResolveImport(from, file, file.Commands[index+1].Import)
		if result.Path != filepath.Clean(want) || len(result.Candidates) != 2 {
			t.Fatalf("runtime import %d = %#v, want %q", index, result, want)
		}
		if result.Candidates[0] != filepath.Clean(want) {
			t.Fatalf("runtime candidates %d = %#v", index, result.Candidates)
		}
	}
}

func TestPathResolverKeepsCanonicalRuntimePath(t *testing.T) {
	root := t.TempDir()
	runtimePath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(runtimePath, "import", "canonical.vim")
	writeResolverFile(t, target, "vim9script\n")
	from := filepath.Join(root, "main.vim")
	writeResolverFile(t, from, "vim9script\nimport 'canonical.vim' as canonical\n")
	file := syntax.Parse(mustResolverRead(t, from))
	resolver, err := NewPathResolver(root, []string{runtimePath})
	if err != nil {
		t.Fatal(err)
	}
	result := resolver.ResolveImport(from, file, file.Commands[1].Import)
	if result.Path != target || len(result.Candidates) != 1 {
		t.Fatalf("canonical runtime path = %#v, want %q", result, target)
	}
}

func TestPathResolverAllowsSymlinkRootAndMissingRuntimePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink targets are not portable on Windows")
	}
	realRoot := t.TempDir()
	linkParent := t.TempDir()
	linkRoot := filepath.Join(linkParent, "workspace")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatal(err)
	}
	missingRuntime := filepath.Join(realRoot, "runtime-not-created")
	resolver, err := NewPathResolver(linkRoot, []string{missingRuntime})
	if err != nil {
		t.Fatal(err)
	}
	result := resolver.ResolveSource("", "later.vim")
	if len(result.Candidates) != 1 || result.Candidates[0] != filepath.Join(linkRoot, "later.vim") {
		t.Fatalf("symlink root candidate = %#v", result)
	}
	from := filepath.Join(linkRoot, "main.vim")
	file := syntax.Parse("vim9script\nimport 'later.vim' as later\n")
	result = resolver.ResolveImport(from, file, file.Commands[1].Import)
	if len(result.Candidates) != 1 || result.Candidates[0] != filepath.Join(missingRuntime, "import", "later.vim") {
		t.Fatalf("missing runtime candidate = %#v", result)
	}
}

func TestPathResolverDoesNotUseAfterImportOrGuessExtensions(t *testing.T) {
	root := t.TempDir()
	runtimePath := t.TempDir()
	from := filepath.Join(root, "main.vim")
	writeResolverFile(t, from, "vim9script\nimport 'plain' as plain\n")
	writeResolverFile(t, filepath.Join(runtimePath, "after", "import", "plain"), "vim9script\n")
	writeResolverFile(t, filepath.Join(runtimePath, "import", "plain.vim"), "vim9script\n")
	file := syntax.Parse(mustResolverRead(t, from))
	resolver, err := NewPathResolver(root, []string{runtimePath})
	if err != nil {
		t.Fatal(err)
	}
	result := resolver.ResolveImport(from, file, file.Commands[1].Import)
	if result.Path != "" || len(result.Candidates) != 1 || result.Candidates[0] != filepath.Join(runtimePath, "import", "plain") {
		t.Fatalf("extension/after handling = %#v", result)
	}
}

func TestPathResolverReportsMissingAndDynamicImportsConservatively(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "main.vim")
	writeResolverFile(t, from, "vim9script\nvar name = 'missing.vim'\nimport name as missing\nimport './missing.vim' as missing2\n")
	file := syntax.Parse(mustResolverRead(t, from))
	resolver, err := NewPathResolver(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	dynamic := resolver.ResolveImport(from, file, file.Commands[2].Import)
	if !dynamic.Dynamic || dynamic.Path != "" || len(dynamic.Candidates) != 0 {
		t.Fatalf("dynamic import = %#v", dynamic)
	}
	missing := resolver.ResolveImport(from, file, file.Commands[3].Import)
	if missing.Dynamic || missing.Path != "" || len(missing.Candidates) != 1 {
		t.Fatalf("missing import = %#v", missing)
	}
}

func TestPathResolverRejectsRootAndSymlinkEscapes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink targets are not portable on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	from := filepath.Join(root, "sub", "main.vim")
	writeResolverFile(t, from, "vim9script\nimport '../../outside.vim' as out\nimport './link.vim' as link\n")
	writeResolverFile(t, filepath.Join(outside, "outside.vim"), "vim9script\n")
	if err := os.Symlink(outside, filepath.Join(root, "sub", "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside.vim"), filepath.Join(root, "sub", "link.vim")); err != nil {
		t.Fatal(err)
	}
	file := syntax.Parse(mustResolverRead(t, from))
	resolver, err := NewPathResolver(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 2; index++ {
		result := resolver.ResolveImport(from, file, file.Commands[index].Import)
		if result.Path != "" || len(result.Candidates) != 0 {
			t.Fatalf("escape import %d = %#v", index, result)
		}
	}
	absolute := resolver.ResolveSource("", filepath.Join(outside, "outside.vim"))
	if absolute.Path != "" || len(absolute.Candidates) != 0 {
		t.Fatalf("absolute escape source = %#v", absolute)
	}
}

func TestPathResolverResolvesSourceRelativeToRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "scripts", "source.vim")
	spaceTarget := filepath.Join(root, "scripts", "source file.vim")
	quotedTarget := filepath.Join(root, "'quoted.vim'")
	trailingTarget := filepath.Join(root, "trailing ")
	writeResolverFile(t, target, "echo 'source'\n")
	writeResolverFile(t, spaceTarget, "echo 'space'\n")
	writeResolverFile(t, quotedTarget, "echo 'quoted'\n")
	writeResolverFile(t, trailingTarget, "echo 'trailing'\n")
	resolver, err := NewPathResolver(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []string{"scripts/source.vim", "scripts/source\\ file.vim", "'quoted.vim'", "trailing\\ "} {
		result := resolver.ResolveSource(filepath.Join(root, "other.vim"), spec)
		want := target
		if spec == "scripts/source\\ file.vim" {
			want = spaceTarget
		} else if spec == "'quoted.vim'" {
			want = quotedTarget
		} else if spec == "trailing\\ " {
			want = trailingTarget
		}
		if result.Path != filepath.Clean(want) || len(result.Candidates) != 1 {
			t.Fatalf("source %q = %#v", spec, result)
		}
	}
	missing := resolver.ResolveSource("", "scripts/missing")
	if missing.Dynamic || missing.Path != "" || len(missing.Candidates) != 1 {
		t.Fatalf("missing source = %#v", missing)
	}
}

func TestPathResolverUsesParsedSourceFilenameSemantics(t *testing.T) {
	root := t.TempDir()
	writeResolverFile(t, filepath.Join(root, "plain.vim"), "")
	writeResolverFile(t, filepath.Join(root, "'plain.vim'"), "")
	resolver, err := NewPathResolver(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	file := syntax.Parse("source 'plain.vim'\nsource \"plain.vim\"\n")
	if len(file.Commands) != 2 {
		t.Fatalf("commands = %#v", file.Commands)
	}
	first := resolver.ResolveSource("", file.Text(file.Commands[0].Argument))
	if first.Path != filepath.Join(root, "'plain.vim'") {
		t.Fatalf("single quote source = %#v", first)
	}
	second := resolver.ResolveSource("", file.Text(file.Commands[1].Argument))
	if second.Path != "" || second.Dynamic || len(second.Candidates) != 0 {
		t.Fatalf("double quote comment source = %#v", second)
	}
}

func TestPathResolverReportsDynamicSourceExpansions(t *testing.T) {
	root := t.TempDir()
	resolver, err := NewPathResolver(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []string{"%", "#", "$HOME/plugin.vim", "~/plugin.vim", "*.vim", "<sfile>", "`=name`"} {
		result := resolver.ResolveSource("", spec)
		if !result.Dynamic || result.Path != "" || len(result.Candidates) != 0 {
			t.Fatalf("dynamic source %q = %#v", spec, result)
		}
	}
}

func TestPathResolverRejectsInvalidRoot(t *testing.T) {
	if _, err := NewPathResolver("", nil); err == nil {
		t.Fatal("empty root unexpectedly accepted")
	}
	file := filepath.Join(t.TempDir(), "root.vim")
	writeResolverFile(t, file, "")
	if _, err := NewPathResolver(file, nil); err == nil {
		t.Fatal("file root unexpectedly accepted")
	}
}

func TestDecodeStaticPathUsesVimStringEscapes(t *testing.T) {
	tests := []struct {
		raw  string
		want string
		ok   bool
	}{
		{raw: `'a''b.vim'`, want: "a'b.vim", ok: true},
		{raw: `"dir\\file.vim"`, want: `dir\file.vim`, ok: true},
		{raw: `"space\x20name.vim"`, want: "space name.vim", ok: true},
		{raw: `"unicode-\u4F60.vim"`, want: "unicode-你.vim", ok: true},
		{raw: `"unknown-\q.vim"`, want: "unknown-q.vim", ok: true},
		{raw: `$"{name}.vim"`, ok: false},
		{raw: `"key-\<C-W>.vim"`, ok: false},
		{raw: `"nul-\x00.vim"`, ok: false},
		{raw: `"trailing\"`, ok: false},
	}
	for _, test := range tests {
		got, ok := decodeStaticPath(test.raw)
		if ok != test.ok || got != test.want {
			t.Fatalf("decodeStaticPath(%q) = %q, %v, want %q, %v", test.raw, got, ok, test.want, test.ok)
		}
	}
}

func writeResolverFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustResolverRead(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
