package server

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// An expression reference must not enable a partial rename of an import alias
// while its variable, parameter, return or nested type references are omitted.
func TestLinkedEditingRangeWithholdsExplicitImportAliases(t *testing.T) {
	for _, tc := range []struct {
		name, body string
	}{
		{"variable type", "var value: types.Widget = types.Widget.new()\n"},
		{"parameter type", "def Use(value: types.Widget)\n  echo types.Widget.new()\nenddef\n"},
		{"return type", "def Make(): types.Widget\n  return types.Widget.new()\nenddef\n"},
		{"nested type", "var values: list<types.Widget> = [types.Widget.new()]\n"},
		// This is deliberately conservative until type-name binding is complete.
		{"expression only", "var value = types.Widget.new()\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "vim9script\nimport './types.vim' as types\n" + tc.body
			instance, documentURI := openNavigationDocument(t, text.UTF16, source)
			for _, offset := range []int{
				strings.Index(source, "as types") + len("as "),
				strings.LastIndex(source, "types.Widget.new()"),
			} {
				checkLinkedEditingSafetyAt(t, instance, documentURI, source, "types", offset, false)
			}
		})
	}
}

func TestLinkedEditingRangeSharedScopesInsideFunction(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"g:state", false},
		{"b:state", false},
		{"w:state", false},
		{"t:state", false},
		{"v:errmsg", false},
		{"s:state", true},
		{"state", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "function! s:Setup() abort\n  let " + tc.name + " = ''\n  echo " + tc.name + "\nendfunction\n"
			instance, documentURI := openNavigationDocument(t, text.UTF16, source)
			for _, offset := range []int{strings.Index(source, tc.name), strings.LastIndex(source, tc.name)} {
				checkLinkedEditingSafetyAt(t, instance, documentURI, source, tc.name, offset, tc.want)
			}
		})
	}
}

// Check that the fixture really has a bound declaration and a reference, so a
// negative test cannot pass merely because navigation failed to resolve it.
func checkLinkedEditingSafetyAt(t *testing.T, instance *Server, documentURI uri.URI, source, name string, offset int, want bool) {
	t.Helper()
	if offset < 0 || offset+len(name) > len(source) || source[offset:offset+len(name)] != name {
		t.Fatalf("invalid fixture offset %d for %q", offset, name)
	}
	snapshot := text.NewSnapshot(documentURI.String(), 0, nil, source)
	rangeValue, ok := protocolRange(snapshot, text.UTF16, syntax.Span{Start: offset, End: offset + len(name)})
	if !ok {
		t.Fatal("invalid fixture range")
	}
	document, err := instance.navigationAt(context.Background(), documentURI.String(), rangeValue.Start)
	if err != nil || document == nil || document.declaration == nil || document.declaration.Name != name {
		t.Fatalf("fixture does not resolve %q at %+v: document=%#v, err=%v", name, rangeValue.Start, document, err)
	}
	if len(document.occurrences(true)) < 2 {
		t.Fatalf("fixture has fewer than two bound occurrences of %q", name)
	}
	ranges := linkedEditingRanges(t, instance, documentURI, rangeValue.Start)
	if (ranges != nil) != want {
		t.Fatalf("linked editing at %+v = %#v, want available=%t", rangeValue.Start, ranges, want)
	}
}

func TestWillRenameFilesDoesNotTreatSymlinkAsMovedTarget(t *testing.T) {
	root := t.TempDir()
	depPath := writeWorkspaceFile(t, root, "dep.vim", "vim9script\nexport var value = 1\n")
	source := "vim9script\nimport './dep.vim' as dep\necho dep.value\n"
	libPath := writeWorkspaceFile(t, root, "lib.vim", source)
	linkPath := filepath.Join(root, "link.vim")
	createEditingSafetySymlink(t, libPath, linkPath)
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	instance := initializeWorkspaceServer(t, root)

	// Control: moving the real importer must still rewrite its relative import.
	control, err := instance.WillRenameFiles(context.Background(), renameFileParams(libPath, filepath.Join(root, "sub", "lib.vim")))
	if err != nil {
		t.Fatal(err)
	}
	updated := applyRenameEdits(t, control, map[string]string{libPath: source})
	if want := "vim9script\nimport '../dep.vim' as dep\necho dep.value\n"; updated[libPath] != want {
		t.Fatalf("real-file control = %q, want %q", updated[libPath], want)
	}

	linkDestination := filepath.Join(root, "sub", "link.vim")
	depDestination := filepath.Join(root, "dep2.vim")
	for _, tc := range []struct {
		name   string
		params *protocol.RenameFilesParams
	}{
		{"move link only", renameFileParams(linkPath, linkDestination)},
		{"replace link", renameFileParams(depPath, linkPath)},
		{"link first in batch", renameFileParams(linkPath, linkDestination, depPath, depDestination)},
		{"link last in batch", renameFileParams(depPath, depDestination, linkPath, linkDestination)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edit, err := instance.WillRenameFiles(context.Background(), tc.params)
			if err != nil {
				t.Fatal(err)
			}
			if edit == nil || len(edit.DocumentChanges) != 0 || len(edit.Changes) != 0 {
				t.Fatalf("symlink operation produced import edits: %#v", edit)
			}
		})
	}
}

func TestRenameFileTargetsAllowsSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	oldPath := writeWorkspaceFile(t, root, "real/lib.vim", "vim9script\n")
	alias := filepath.Join(root, "alias")
	createEditingSafetySymlink(t, filepath.Join(root, "real"), alias)
	newPath := filepath.Join(root, "real", "sub", "lib.vim")
	params := renameFileParams(filepath.Join(alias, "lib.vim"), filepath.Join(alias, "sub", "lib.vim"))
	renamed := renameFileTargets(params.Files)
	oldCanonical := mustWorkspaceCanonicalPath(t, oldPath)
	newCanonical := mustWorkspaceCanonicalPath(t, newPath)
	if len(renamed) != 1 || renamed[oldCanonical] != newCanonical {
		t.Fatalf("regular file through symlinked parent = %#v, want %s -> %s", renamed, oldCanonical, newCanonical)
	}
}

func TestRenameFileTargetsWithholdsDanglingSymlinkBatch(t *testing.T) {
	root := t.TempDir()
	oldPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\n")
	linkPath := filepath.Join(root, "dangling.vim")
	createEditingSafetySymlink(t, filepath.Join(root, "missing.vim"), linkPath)
	params := renameFileParams(oldPath, filepath.Join(root, "util.vim"), linkPath, filepath.Join(root, "moved.vim"))
	if renamed := renameFileTargets(params.Files); len(renamed) != 0 {
		t.Fatalf("dangling symlink must withhold the whole batch: %#v", renamed)
	}
}

func createEditingSafetySymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symbolic links unavailable: %v", err)
		}
		t.Fatal(err)
	}
}
