package server

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
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
// contextually relevant and deterministic. The role changes diagnostic policy,
// not the completion candidate set or its contextual order.
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
