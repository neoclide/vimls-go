package workspace

import (
	"path/filepath"
	"testing"
)

func TestRewriteImportPathPreservesSpellingForm(t *testing.T) {
	root := t.TempDir()
	join := func(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }
	slash := filepath.ToSlash
	for _, test := range []struct {
		name      string
		from      string
		raw       string
		oldTarget string
		newTarget string
		want      string
		wantOK    bool
	}{
		{
			name: "relative sibling", from: join("main.vim"), raw: "'./libs.vim'",
			oldTarget: join("libs.vim"), newTarget: join("util.vim"), want: "./util.vim", wantOK: true,
		},
		{
			name: "relative into subdirectory", from: join("main.vim"), raw: "'./libs.vim'",
			oldTarget: join("libs.vim"), newTarget: join("sub", "util.vim"), want: "./sub/util.vim", wantOK: true,
		},
		{
			name: "relative out of subdirectory", from: join("main.vim"), raw: "'./sub/libs.vim'",
			oldTarget: join("sub", "libs.vim"), newTarget: join("util.vim"), want: "./util.vim", wantOK: true,
		},
		{
			name: "relative from nested importer", from: join("sub", "main.vim"), raw: "'./libs.vim'",
			oldTarget: join("sub", "libs.vim"), newTarget: join("util.vim"), want: "../util.vim", wantOK: true,
		},
		{
			name: "relative for renamed importer", from: join("sub", "main.vim"), raw: "'./libs.vim'",
			oldTarget: join("libs.vim"), newTarget: join("libs2.vim"), want: "../libs2.vim", wantOK: true,
		},
		{
			name: "runtimepath without directory", from: join("main.vim"), raw: "'libs.vim'",
			oldTarget: join("libs.vim"), newTarget: join("util.vim"), want: "util.vim", wantOK: true,
		},
		{
			name: "relative target unchanged", from: join("main.vim"), raw: "'./libs.vim'",
			oldTarget: join("libs.vim"), newTarget: join("libs.vim"), want: "./libs.vim", wantOK: true,
		},
		{
			name: "runtimepath target unchanged", from: join("sub", "main.vim"), raw: "'libs.vim'",
			oldTarget: join("import", "libs.vim"), newTarget: join("import", "libs.vim"), want: "libs.vim", wantOK: true,
		},
		{
			name: "absolute target unchanged", from: join("sub", "main.vim"), raw: "'" + slash(join("libs.vim")) + "'",
			oldTarget: join("libs.vim"), newTarget: join("libs.vim"), want: slash(join("libs.vim")), wantOK: true,
		},
		{
			name: "absolute", from: join("main.vim"), raw: "'" + slash(join("libs.vim")) + "'",
			oldTarget: join("libs.vim"), newTarget: join("sub", "util.vim"), want: slash(join("sub", "util.vim")), wantOK: true,
		},
		{
			name: "runtimepath", from: join("main.vim"), raw: "'libs.vim'",
			oldTarget: join("import", "libs.vim"), newTarget: join("import", "util.vim"), want: "util.vim", wantOK: true,
		},
		{
			name: "runtimepath subdirectory", from: join("main.vim"), raw: "'libs.vim'",
			oldTarget: join("import", "libs.vim"), newTarget: join("import", "sub", "util.vim"), want: "sub/util.vim", wantOK: true,
		},
		{
			name: "runtimepath with directory prefix", from: join("main.vim"), raw: "'a/libs.vim'",
			oldTarget: join("import", "a", "libs.vim"), newTarget: join("import", "a", "util.vim"), want: "a/util.vim", wantOK: true,
		},
		{
			name: "runtimepath escaping its directory", from: join("main.vim"), raw: "'libs.vim'",
			oldTarget: join("import", "libs.vim"), newTarget: join("util.vim"), wantOK: false,
		},
		{
			name: "double quoted relative", from: join("main.vim"), raw: "\"./libs.vim\"",
			oldTarget: join("libs.vim"), newTarget: join("util.vim"), want: "./util.vim", wantOK: true,
		},
		{
			name: "dynamic path", from: join("main.vim"), raw: "g:path",
			oldTarget: join("libs.vim"), newTarget: join("util.vim"), wantOK: false,
		},
		{
			name: "empty path", from: join("main.vim"), raw: "''",
			oldTarget: join("libs.vim"), newTarget: join("util.vim"), wantOK: false,
		},
		{
			name: "missing importer for relative path", from: "", raw: "'./libs.vim'",
			oldTarget: join("libs.vim"), newTarget: join("util.vim"), wantOK: false,
		},
		{
			name: "missing target", from: join("main.vim"), raw: "'./libs.vim'",
			oldTarget: "", newTarget: join("util.vim"), wantOK: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := RewriteImportPath(test.from, test.raw, test.oldTarget, test.newTarget)
			if ok != test.wantOK || got != test.want {
				t.Fatalf("RewriteImportPath() = %q, %v; want %q, %v", got, ok, test.want, test.wantOK)
			}
		})
	}
}

func TestImportPathName(t *testing.T) {
	for _, test := range []struct {
		raw    string
		want   string
		wantOK bool
	}{
		{raw: "'./libs.vim'", want: "libs", wantOK: true},
		{raw: "'libs.vim'", want: "libs", wantOK: true},
		{raw: "'./a/b/two.vim'", want: "two", wantOK: true},
		{raw: "\"/tmp/libs.vim\"", want: "libs", wantOK: true},
		{raw: "'./libs'", wantOK: false},
		{raw: "'./libs.vimx'", wantOK: false},
		{raw: "'./.vim'", wantOK: false},
		{raw: "g:path", wantOK: false},
	} {
		got, ok := ImportPathName(test.raw)
		if ok != test.wantOK || got != test.want {
			t.Fatalf("ImportPathName(%q) = %q, %v; want %q, %v", test.raw, got, ok, test.want, test.wantOK)
		}
	}
}
