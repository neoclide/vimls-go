package server

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestWillRenameFilesCapability(t *testing.T) {
	for _, supported := range []bool{false, true} {
		t.Run(map[bool]string{false: "unsupported", true: "supported"}[supported], func(t *testing.T) {
			instance := New(nil, nil, nil)
			t.Cleanup(instance.stopAnalysis)
			var capabilities protocol.ClientCapabilities
			if supported {
				value := true
				capabilities.Workspace = &protocol.WorkspaceClientCapabilities{FileOperations: &protocol.FileOperationClientCapabilities{WillRename: &value}}
			}
			result, err := instance.Initialize(context.Background(), &protocol.InitializeParams{Capabilities: capabilities, InitializationOptions: protocol.LSPAny([]byte(`{"runtimepath":[]}`))})
			if err != nil {
				t.Fatal(err)
			}
			if result.Capabilities.Workspace == nil {
				t.Fatal("workspace options missing")
			}
			operations := result.Capabilities.Workspace.FileOperations
			if !supported {
				if operations != nil {
					t.Fatalf("file operations advertised without client support: %#v", operations)
				}
				return
			}
			if operations == nil || len(operations.WillRename.Filters) != 1 {
				t.Fatalf("file operations = %#v", operations)
			}
			filter := operations.WillRename.Filters[0]
			if filter.Scheme == nil || *filter.Scheme != "file" || filter.Pattern.Glob != willRenameImportGlob || filter.Pattern.Matches != protocol.FileOperationPatternKindFile {
				t.Fatalf("filter = %#v", filter)
			}
		})
	}
}

func TestWillRenameFilesRewritesRelativeImport(t *testing.T) {
	root := t.TempDir()
	library := "vim9script\nexport def Two(): number\n  return 2\nenddef\n"
	libPath := writeWorkspaceFile(t, root, "lib.vim", library)
	source := "vim9script\nimport './lib.vim' as lib\necho lib.Two()\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	edit, err := instance.WillRenameFiles(context.Background(), renameFileParams(libPath, filepath.Join(root, "util.vim")))
	if err != nil {
		t.Fatal(err)
	}
	updated := applyRenameEdits(t, edit, map[string]string{mainPath: source})
	if len(updated) != 1 {
		t.Fatalf("edited documents = %#v", updated)
	}
	want := "vim9script\nimport './util.vim' as lib\necho lib.Two()\n"
	if updated[mainPath] != want {
		t.Fatalf("main.vim = %q, want %q", updated[mainPath], want)
	}
}

func TestWillRenameFilesRewritesDerivedNamespace(t *testing.T) {
	root := t.TempDir()
	library := "vim9script\nexport def Two(): number\n  return 2\nenddef\n"
	libPath := writeWorkspaceFile(t, root, "lib.vim", library)
	source := "vim9script\nimport './lib.vim'\necho lib.Two()\necho lib.Two()\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	edit, err := instance.WillRenameFiles(context.Background(), renameFileParams(libPath, filepath.Join(root, "util.vim")))
	if err != nil {
		t.Fatal(err)
	}
	updated := applyRenameEdits(t, edit, map[string]string{mainPath: source})
	want := "vim9script\nimport './util.vim'\necho util.Two()\necho util.Two()\n"
	if updated[mainPath] != want {
		t.Fatalf("main.vim = %q, want %q", updated[mainPath], want)
	}
}

func TestWillRenameFilesRewritesDerivedNamespaceIntoSubdirectory(t *testing.T) {
	root := t.TempDir()
	libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Two(): number\n  return 2\nenddef\n")
	source := "vim9script\nimport './lib.vim'\necho lib.Two()\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	edit, err := instance.WillRenameFiles(context.Background(), renameFileParams(libPath, filepath.Join(root, "sub", "util.vim")))
	if err != nil {
		t.Fatal(err)
	}
	updated := applyRenameEdits(t, edit, map[string]string{mainPath: source})
	want := "vim9script\nimport './sub/util.vim'\necho util.Two()\n"
	if updated[mainPath] != want {
		t.Fatalf("main.vim = %q, want %q", updated[mainPath], want)
	}
}

func TestWillRenameFilesRewritesRenamedImporterRelativeImport(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Two(): number\n  return 2\nenddef\n")
	source := "vim9script\nimport './lib.vim' as lib\necho lib.Two()\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	// Renaming only the importer still changes how its own relative import
	// resolves, so the literal has to move with it.
	edit, err := instance.WillRenameFiles(context.Background(), renameFileParams(mainPath, filepath.Join(root, "sub", "main.vim")))
	if err != nil {
		t.Fatal(err)
	}
	updated := applyRenameEdits(t, edit, map[string]string{mainPath: source})
	want := "vim9script\nimport '../lib.vim' as lib\necho lib.Two()\n"
	if updated[mainPath] != want {
		t.Fatalf("main.vim = %q, want %q", updated[mainPath], want)
	}
}

func TestWillRenameFilesRewritesRenamedImporterOfRenamedTarget(t *testing.T) {
	root := t.TempDir()
	libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Two(): number\n  return 2\nenddef\n")
	source := "vim9script\nimport './lib.vim' as lib\necho lib.Two()\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	edit, err := instance.WillRenameFiles(context.Background(), renameFileParams(
		libPath, filepath.Join(root, "libs.vim"),
		mainPath, filepath.Join(root, "sub", "main.vim"),
	))
	if err != nil {
		t.Fatal(err)
	}
	updated := applyRenameEdits(t, edit, map[string]string{mainPath: source})
	want := "vim9script\nimport '../libs.vim' as lib\necho lib.Two()\n"
	if updated[mainPath] != want {
		t.Fatalf("main.vim = %q, want %q", updated[mainPath], want)
	}
}

func TestWillRenameFilesRewritesEveryImporter(t *testing.T) {
	root := t.TempDir()
	libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Two(): number\n  return 2\nenddef\n")
	first := "vim9script\nimport './lib.vim' as lib\necho lib.Two()\n"
	second := "vim9script\nimport './lib.vim' as two\necho two.Two()\n"
	firstPath := writeWorkspaceFile(t, root, "a.vim", first)
	secondPath := writeWorkspaceFile(t, root, "b.vim", second)
	instance := initializeWorkspaceServer(t, root)
	edit, err := instance.WillRenameFiles(context.Background(), renameFileParams(libPath, filepath.Join(root, "util.vim")))
	if err != nil {
		t.Fatal(err)
	}
	updated := applyRenameEdits(t, edit, map[string]string{firstPath: first, secondPath: second})
	if len(updated) != 2 {
		t.Fatalf("edited documents = %#v", updated)
	}
	if updated[firstPath] != "vim9script\nimport './util.vim' as lib\necho lib.Two()\n" {
		t.Fatalf("a.vim = %q", updated[firstPath])
	}
	if updated[secondPath] != "vim9script\nimport './util.vim' as two\necho two.Two()\n" {
		t.Fatalf("b.vim = %q", updated[secondPath])
	}
	uris := make([]string, 0, len(edit.DocumentChanges))
	for _, change := range edit.DocumentChanges {
		uris = append(uris, change.(*protocol.TextDocumentEdit).TextDocument.URI.String())
	}
	if !sort.StringsAreSorted(uris) {
		t.Fatalf("document changes are not ordered: %#v", uris)
	}
}

func TestWillRenameFilesUsesOpenDocumentContent(t *testing.T) {
	root := t.TempDir()
	libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Two(): number\n  return 2\nenddef\n")
	disk := "vim9script\nimport './lib.vim' as lib\necho lib.Two()\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", disk)
	instance := initializeWorkspaceServer(t, root)
	open := "vim9script\n# leading comment\nimport './lib.vim' as lib\necho lib.Two()\n"
	mainURI := uri.File(mainPath)
	if err := instance.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: mainURI, Version: 4, Text: open}}); err != nil {
		t.Fatal(err)
	}
	edit, err := instance.WillRenameFiles(context.Background(), renameFileParams(libPath, filepath.Join(root, "util.vim")))
	if err != nil {
		t.Fatal(err)
	}
	updated := applyRenameEdits(t, edit, map[string]string{mainPath: open})
	want := "vim9script\n# leading comment\nimport './util.vim' as lib\necho lib.Two()\n"
	if updated[mainPath] != want {
		t.Fatalf("main.vim = %q, want %q", updated[mainPath], want)
	}
	documentEdit := edit.DocumentChanges[0].(*protocol.TextDocumentEdit)
	if documentEdit.TextDocument.Version == nil || *documentEdit.TextDocument.Version != 4 {
		t.Fatalf("version = %#v", documentEdit.TextDocument.Version)
	}
}

func TestWillRenameFilesRewritesBoundNamespaceForms(t *testing.T) {
	for _, test := range []struct {
		name, source, want string
	}{
		{
			name:   "member call",
			source: "vim9script\nimport './lib.vim'\necho lib.Two()\n",
			want:   "vim9script\nimport './util.vim'\necho util.Two()\n",
		},
		{
			name:   "imported member",
			source: "vim9script\nimport './lib.vim'\nimport lib.Two\n",
			want:   "vim9script\nimport './util.vim'\nimport util.Two\n",
		},
		{
			name:   "namespace value",
			source: "vim9script\nimport './lib.vim'\nvar script = lib\n",
			want:   "vim9script\nimport './util.vim'\nvar script = util\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Two(): number\n  return 2\nenddef\n")
			mainPath := writeWorkspaceFile(t, root, "main.vim", test.source)
			instance := initializeWorkspaceServer(t, root)
			edit, err := instance.WillRenameFiles(context.Background(), renameFileParams(libPath, filepath.Join(root, "util.vim")))
			if err != nil {
				t.Fatal(err)
			}
			updated := applyRenameEdits(t, edit, map[string]string{mainPath: test.source})
			if updated[mainPath] != test.want {
				t.Fatalf("main.vim = %q, want %q", updated[mainPath], test.want)
			}
		})
	}
}

func TestWillRenameFilesWithholdsUnprovableNamespace(t *testing.T) {
	for _, test := range []struct {
		name, source string
	}{
		{
			// A comment mentions the namespace, so the set of textual
			// occurrences is larger than the set of bound references. Renaming
			// only the bound ones would leave the document internally
			// inconsistent, so no edit is offered at all.
			name:   "comment mentions namespace",
			source: "vim9script\nimport './lib.vim'\n# see lib.Two\necho lib.Two()\n",
		},
		{
			// A type annotation is not a bound reference, so the bound
			// references alone cannot be proven to be every use site.
			name:   "type annotation",
			source: "vim9script\nimport './lib.vim'\nvar value: lib.MyType\n",
		},
		{
			// A script-local spelling does not bind to the import declaration,
			// so replacing the new name alone would drop its prefix.
			name:   "script local spelling",
			source: "vim9script\nimport './lib.vim'\ns:lib.Two()\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Two(): number\n  return 2\nenddef\n")
			writeWorkspaceFile(t, root, "main.vim", test.source)
			instance := initializeWorkspaceServer(t, root)
			edit, err := instance.WillRenameFiles(context.Background(), renameFileParams(libPath, filepath.Join(root, "util.vim")))
			if err != nil {
				t.Fatal(err)
			}
			if len(edit.DocumentChanges) != 0 {
				t.Fatalf("document changes = %#v", edit.DocumentChanges)
			}
		})
	}
}

func TestWillRenameFilesWithholdsDerivedNamespaceThatIsNotAnIdentifier(t *testing.T) {
	root := t.TempDir()
	libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Two(): number\n  return 2\nenddef\n")
	source := "vim9script\nimport './lib.vim'\necho lib.Two()\n"
	writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	edit, err := instance.WillRenameFiles(context.Background(), renameFileParams(libPath, filepath.Join(root, "my-lib.vim")))
	if err != nil {
		t.Fatal(err)
	}
	if len(edit.DocumentChanges) != 0 {
		t.Fatalf("document changes = %#v", edit.DocumentChanges)
	}
}

func TestWillRenameFilesSkipsUnverifiableClosedDocument(t *testing.T) {
	root := t.TempDir()
	libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Two(): number\n  return 2\nenddef\n")
	source := "vim9script\nimport './lib.vim' as lib\necho lib.Two()\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	params := renameFileParams(libPath, filepath.Join(root, "util.vim"))
	// Control: an unchanged closed document is edited.
	edit, err := instance.WillRenameFiles(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if len(edit.DocumentChanges) != 1 {
		t.Fatalf("control document changes = %#v", edit.DocumentChanges)
	}
	// A closed document whose disk content no longer matches the indexed source
	// cannot be edited from that reading.
	if err := os.WriteFile(mainPath, []byte("vim9script\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	edit, err = instance.WillRenameFiles(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if len(edit.DocumentChanges) != 0 {
		t.Fatalf("document changes = %#v", edit.DocumentChanges)
	}
}

func TestWillRenameFilesIncompleteIndexProducesNoEdits(t *testing.T) {
	root := t.TempDir()
	libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Two(): number\n  return 2\nenddef\n")
	writeWorkspaceFile(t, root, "main.vim", "vim9script\nimport './lib.vim' as lib\necho lib.Two()\n")
	instance := initializeWorkspaceServer(t, root)
	params := renameFileParams(libPath, filepath.Join(root, "util.vim"))
	// Control: a complete index produces the rewrite.
	edit, err := instance.WillRenameFiles(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if len(edit.DocumentChanges) != 1 {
		t.Fatalf("control document changes = %#v", edit.DocumentChanges)
	}
	// An incomplete index cannot prove the importer set, so no import is
	// rewritten. The rename itself is still not refused.
	instance.workspaceIndex.SetComplete(false)
	edit, err = instance.WillRenameFiles(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if edit == nil || len(edit.DocumentChanges) != 0 {
		t.Fatalf("edit = %#v", edit)
	}
}

func TestWillRenameFilesIgnoresUnrelatedOperations(t *testing.T) {
	root := t.TempDir()
	libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Two(): number\n  return 2\nenddef\n")
	source := "vim9script\nimport './lib.vim' as lib\necho lib.Two()\n"
	writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	for _, test := range []struct {
		name   string
		params *protocol.RenameFilesParams
	}{
		{name: "no files", params: &protocol.RenameFilesParams{}},
		{name: "unmoved file", params: renameFileParams(libPath, libPath)},
		{name: "unknown file", params: renameFileParams(filepath.Join(root, "other.vim"), filepath.Join(root, "renamed.vim"))},
		{
			name: "conflicting destinations",
			params: &protocol.RenameFilesParams{Files: []protocol.FileRename{
				{OldURI: uri.File(libPath).String(), NewURI: uri.File(filepath.Join(root, "one.vim")).String()},
				{OldURI: uri.File(libPath).String(), NewURI: uri.File(filepath.Join(root, "two.vim")).String()},
			}},
		},
		{name: "non file scheme", params: &protocol.RenameFilesParams{Files: []protocol.FileRename{{OldURI: "untitled:Untitled-1", NewURI: "untitled:Untitled-2"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			edit, err := instance.WillRenameFiles(context.Background(), test.params)
			if err != nil {
				t.Fatal(err)
			}
			if edit == nil || len(edit.DocumentChanges) != 0 {
				t.Fatalf("edit = %#v", edit)
			}
		})
	}
}

func TestWillRenameFilesRewritesEveryImportOfTheRenamedFile(t *testing.T) {
	root := t.TempDir()
	libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Two(): number\n  return 2\nenddef\n")
	source := "vim9script\nimport './lib.vim' as one\nimport './lib.vim' as two\necho one.Two()\necho two.Two()\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	edit, err := instance.WillRenameFiles(context.Background(), renameFileParams(libPath, filepath.Join(root, "util.vim")))
	if err != nil {
		t.Fatal(err)
	}
	updated := applyRenameEdits(t, edit, map[string]string{mainPath: source})
	want := "vim9script\nimport './util.vim' as one\nimport './util.vim' as two\necho one.Two()\necho two.Two()\n"
	if updated[mainPath] != want {
		t.Fatalf("main.vim = %q, want %q", updated[mainPath], want)
	}
}

func TestWillRenameFilesKeepsDoubleQuotedLiteral(t *testing.T) {
	root := t.TempDir()
	libPath := writeWorkspaceFile(t, root, "lib.vim", "vim9script\nexport def Two(): number\n  return 2\nenddef\n")
	source := "vim9script\nimport \"./lib.vim\" as lib\necho lib.Two()\n"
	mainPath := writeWorkspaceFile(t, root, "main.vim", source)
	instance := initializeWorkspaceServer(t, root)
	edit, err := instance.WillRenameFiles(context.Background(), renameFileParams(libPath, filepath.Join(root, "util.vim")))
	if err != nil {
		t.Fatal(err)
	}
	updated := applyRenameEdits(t, edit, map[string]string{mainPath: source})
	want := "vim9script\nimport \"./util.vim\" as lib\necho lib.Two()\n"
	if updated[mainPath] != want {
		t.Fatalf("main.vim = %q, want %q", updated[mainPath], want)
	}
}

func renameFileParams(paths ...string) *protocol.RenameFilesParams {
	params := &protocol.RenameFilesParams{}
	for index := 0; index+1 < len(paths); index += 2 {
		params.Files = append(params.Files, protocol.FileRename{
			OldURI: uri.File(paths[index]).String(),
			NewURI: uri.File(paths[index+1]).String(),
		})
	}
	return params
}

// applyRenameEdits returns each edited document's content with its text edits
// applied, keyed by the caller's own path spelling. Applying the edits verifies
// their ranges as well as their replacement text.
func applyRenameEdits(t *testing.T, edit *protocol.WorkspaceEdit, sources map[string]string) map[string]string {
	t.Helper()
	original := make(map[string]string, len(sources))
	for path := range sources {
		original[mustWorkspaceCanonicalPath(t, path)] = path
	}
	updated := make(map[string]string, len(edit.DocumentChanges))
	for _, change := range edit.DocumentChanges {
		documentEdit, ok := change.(*protocol.TextDocumentEdit)
		if !ok {
			t.Fatalf("document change = %#v", change)
		}
		path, ok := workspaceURIPath(documentEdit.TextDocument.URI)
		if !ok {
			t.Fatalf("document change URI = %s", documentEdit.TextDocument.URI)
		}
		path, ok = original[path]
		if !ok {
			t.Fatalf("no recorded source for %s", path)
		}
		updated[path] = applyTextEdits(t, sources[path], documentEdit.Edits)
	}
	return updated
}

func applyTextEdits(t *testing.T, source string, edits []protocol.TextDocumentEditElement) string {
	t.Helper()
	snapshot := text.NewSnapshot("file:///edited.vim", 0, nil, source)
	type change struct {
		start, end int
		text       string
	}
	changes := make([]change, 0, len(edits))
	for _, element := range edits {
		textEdit, ok := element.(*protocol.TextEdit)
		if !ok {
			t.Fatalf("edit element = %#v", element)
		}
		start, err := snapshot.Offset(fromProtocolPosition(textEdit.Range.Start), text.UTF16)
		if err != nil {
			t.Fatal(err)
		}
		end, err := snapshot.Offset(fromProtocolPosition(textEdit.Range.End), text.UTF16)
		if err != nil {
			t.Fatal(err)
		}
		changes = append(changes, change{start: start, end: end, text: textEdit.NewText})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].start < changes[j].start })
	var builder strings.Builder
	previous := 0
	for _, change := range changes {
		if change.start < previous {
			t.Fatalf("overlapping edits at offset %d", change.start)
		}
		builder.WriteString(source[previous:change.start])
		builder.WriteString(change.text)
		previous = change.end
	}
	builder.WriteString(source[previous:])
	return builder.String()
}
