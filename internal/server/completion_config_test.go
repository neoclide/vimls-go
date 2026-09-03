package server

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// completionLabels returns the ordered labels of a completion result.
func completionLabels(t *testing.T, instance *Server, documentURI uri.URI, line uint32, character uint32) []string {
	t.Helper()
	result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
		Position:     protocol.Position{Line: line, Character: character},
	}})
	if err != nil {
		t.Fatalf("completion at %d:%d: %v", line, character, err)
	}
	items := completionItems(t, result)
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels
}

// TestConfigCompletionOrderingInvariants verifies §7 P0 in config-file mode:
// completion consumes the path-derived role-aware analysis while candidates at
// option, mapping-argument, autocmd-event and g: variable positions stay
// contextually relevant and deterministic. The role preserves the candidate
// set; only top-level unscoped g: declarations get a config-specific tie-break.
func TestConfigCompletionOrderingInvariants(t *testing.T) {
	// Each scenario is one line of source; the cursor is placed at the end of
	// the partial token the client is completing.
	tests := []struct {
		name, source string
		line, column uint32
		contains     []string
		configBefore [2]string
		pluginBefore [2]string
	}{
		{
			name:     "set option name position",
			source:   "let g:myConfig = 1\nset tab\n",
			line:     1,
			column:   uint32(len("set tab")),
			contains: []string{"tabstop"},
		},
		{
			name:     "mapping argument position",
			source:   "let g:myConfig = 1\nnnoremap <si\n",
			line:     1,
			column:   uint32(len("nnoremap <si")),
			contains: []string{"<silent>"},
		},
		{
			name:     "autocmd event position",
			source:   "let g:myConfig = 1\naugroup g\nautocmd BufW\n",
			line:     2,
			column:   uint32(len("autocmd BufW")),
			contains: []string{"BufWinEnter", "BufWinLeave", "BufWipeout"},
		},
		{
			name:         "configuration g: variable relevance",
			source:       "let alpha = 1\nlet g:zConfig = 1\necho \n",
			line:         2,
			column:       uint32(len("echo ")),
			contains:     []string{"alpha", "g:zConfig"},
			configBefore: [2]string{"g:zConfig", "alpha"},
			pluginBefore: [2]string{"alpha", "g:zConfig"},
		},
		{
			name:         "callable local relevance is unchanged",
			source:       "let g:zConfig = 1\nfunction! Run() abort\n  let alpha = 1\n  echo \nendfunction\n",
			line:         3,
			column:       uint32(len("  echo ")),
			contains:     []string{"alpha", "g:zConfig"},
			configBefore: [2]string{"alpha", "g:zConfig"},
			pluginBefore: [2]string{"alpha", "g:zConfig"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var configLabels, pluginLabels []string
			for _, role := range []string{"config", "plugin"} {
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, ".vimrc")
				if role == "plugin" {
					path = filepath.Join(root, "plugin", "demo.vim")
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				rolePath := path
				if err := os.WriteFile(rolePath, []byte(test.source), 0o644); err != nil {
					t.Fatal(err)
				}
				instance := newRootedServer(t, root)
				documentURI := uri.File(rolePath)
				instance.documents.Open(documentURI.String(), 1, test.source)
				labels := completionLabels(t, instance, documentURI, test.line, test.column)
				instance.publishMu.Lock()
				parsed := instance.parsed[documentURI.String()]
				instance.publishMu.Unlock()
				if got, want := parsed.configFile, role == "config"; got != want {
					t.Fatalf("role %s cached configFile = %v, want %v", role, got, want)
				}
				for _, want := range test.contains {
					if !slices.Contains(labels, want) {
						t.Fatalf("role %s missing %q in %#v", role, want, labels)
					}
				}
				if role == "config" {
					configLabels = labels
				} else {
					pluginLabels = labels
				}
			}
			configSet := append([]string(nil), configLabels...)
			pluginSet := append([]string(nil), pluginLabels...)
			sort.Strings(configSet)
			sort.Strings(pluginSet)
			if !reflect.DeepEqual(configSet, pluginSet) {
				t.Fatalf("config candidate set %#v differs from plugin candidate set %#v", configSet, pluginSet)
			}
			for role, assertion := range map[string]struct {
				labels []string
				before [2]string
			}{
				"config": {labels: configLabels, before: test.configBefore},
				"plugin": {labels: pluginLabels, before: test.pluginBefore},
			} {
				if assertion.before == [2]string{} {
					continue
				}
				first := slices.Index(assertion.labels, assertion.before[0])
				second := slices.Index(assertion.labels, assertion.before[1])
				if first < 0 || second < 0 || first >= second {
					t.Fatalf("role %s labels order %#v, want %q before %q", role, assertion.labels, assertion.before[0], assertion.before[1])
				}
			}
		})
	}
}

// snippetText returns the snippet insert text of an item or "".
func snippetText(item *protocol.CompletionItem) string {
	if value, ok := item.InsertText.Get(); ok {
		return value
	}
	if edit, ok := item.TextEdit.(*protocol.TextEdit); ok {
		return edit.NewText
	}
	return ""
}

// TestConfigSnippetTemplates verifies §7 P1 for user configuration files: the
// mapping skeleton appears only in config files, the legacy :function block
// omits "!", and neither defaults to <unique> or a bang.
func TestConfigSnippetTemplates(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pluginPath := filepath.Join(root, "plugin", "demo.vim")
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte("let g:pluginSet = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	itemWithLabel := func(t *testing.T, instance *Server, documentURI uri.URI, source string, line, character uint32, label string) *protocol.CompletionItem {
		t.Helper()
		instance.completion.snippet = true
		instance.documents.Open(documentURI.String(), 1, source)
		result, err := instance.Completion(context.Background(), &protocol.CompletionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			Position:     protocol.Position{Line: line, Character: character},
		}})
		if err != nil {
			t.Fatalf("completion: %v", err)
		}
		for _, item := range completionItems(t, result) {
			if item.Label == label {
				return &item
			}
		}
		return nil
	}

	configURI := uri.File(filepath.Join(root, ".vimrc"))
	pluginURI := uri.File(pluginPath)

	// Config files offer a <Leader> mapping skeleton (LHS position) without
	// <unique>.
	config := newRootedServer(t, root)
	item := itemWithLabel(t, config, configURI, "nnoremap \n", 0, uint32(len("nnoremap ")), "<Leader>")
	if item == nil {
		t.Fatal("config mapping skeleton missing")
	}
	if text := snippetText(item); !strings.HasPrefix(text, "<Leader>") || strings.Contains(text, "<unique>") || !strings.Contains(text, "<Cmd>") {
		t.Fatalf("mapping skeleton = %q", text)
	}
	if item.InsertTextFormat != protocol.InsertTextFormatSnippet {
		t.Fatalf("mapping skeleton format = %v", item.InsertTextFormat)
	}

	// Plugin files do not offer the config mapping skeleton.
	plugin := newRootedServer(t, root)
	if item := itemWithLabel(t, plugin, pluginURI, "nnoremap \n", 0, uint32(len("nnoremap ")), "<Leader>"); item != nil {
		t.Fatalf("plugin file offered mapping skeleton: %#v", item)
	}

	// The :function block omits "!" in config files but keeps it in plugins.
	configFn := itemWithLabel(t, newRootedServer(t, root), configURI, "function\n", 0, uint32(len("function")), "function")
	if configFn == nil || strings.Contains(snippetText(configFn), "function!") {
		t.Fatalf("config function block = %#v", configFn)
	}
	pluginFn := itemWithLabel(t, newRootedServer(t, root), pluginURI, "function\n", 0, uint32(len("function")), "function")
	if pluginFn == nil || !strings.Contains(snippetText(pluginFn), "function!") {
		t.Fatalf("plugin function block = %#v", pluginFn)
	}

	configCommand := itemWithLabel(t, newRootedServer(t, root), configURI, "cmd\n", 0, uint32(len("cmd")), "cmd")
	if configCommand == nil || strings.Contains(snippetText(configCommand), "command!") {
		t.Fatalf("config command snippet = %#v", configCommand)
	}
	pluginCommand := itemWithLabel(t, newRootedServer(t, root), pluginURI, "cmd\n", 0, uint32(len("cmd")), "cmd")
	if pluginCommand == nil || !strings.Contains(snippetText(pluginCommand), "command!") {
		t.Fatalf("plugin command snippet = %#v", pluginCommand)
	}
}

// TestConfigColorschemeCompletionKeepsContextAndOrder adds the §7 colorscheme
// row to the role-aware completion matrix.  Colorscheme lookup is deliberately
// role-neutral; the config role must still retain its syntax context and the
// runtimepath order used for tied prefix candidates.
func TestConfigColorschemeCompletionKeepsContextAndOrder(t *testing.T) {
	root := t.TempDir()
	runtime := t.TempDir()
	for _, name := range []string{"desert", "desolate"} {
		writeWorkspaceFile(t, runtime, filepath.Join("colors", name+".vim"), "")
	}
	source := "colorscheme des\n"
	path := writeWorkspaceFile(t, root, ".vimrc", source)
	instance := initializeWorkspaceServer(t, root)
	instance.setRuntimePaths([]string{runtime})
	instance.refreshWorkspaceResolver()
	instance.scheduleWorkspaceRebuild()
	instance.workspaceWG.Wait()
	documentURI := uri.File(path)
	instance.documents.Open(documentURI.String(), 1, source)
	labels := completionLabels(t, instance, documentURI, 0, uint32(len("colorscheme des")))
	first, second := slices.Index(labels, "desert"), slices.Index(labels, "desolate")
	if first < 0 || second < 0 || first >= second {
		t.Fatalf("config colorscheme labels = %#v, want deterministic desert before desolate", labels)
	}
	// An extra argument is expression context, never colorscheme completion.
	instance.documents.Open(documentURI.String(), 2, "colorscheme desert extra\n")
	labels = completionLabels(t, instance, documentURI, 0, uint32(len("colorscheme desert extra")))
	if slices.Contains(labels, "desert") || slices.Contains(labels, "desolate") {
		t.Fatalf("non-colorscheme context returned colors: %#v", labels)
	}
}
