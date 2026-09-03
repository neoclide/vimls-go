package server

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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
// candidates at option, mapping-argument, autocmd-event and g: variable
// positions stay contextually relevant and deterministic, and the same text in
// the plugin role yields the same ordered result (the config role never
// changes completion semantics, only relevance).
func TestConfigCompletionOrderingInvariants(t *testing.T) {
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

	// Each scenario is one line of source; the cursor is placed at the end of
	// the partial token the client is completing.
	tests := []struct {
		name, source string
		line, column uint32
		contains     []string
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
			name:     "configuration g: variable position",
			source:   "let g:myConfig = 1\necho g:myC\n",
			line:     1,
			column:   uint32(len("echo g:myC")),
			contains: []string{"g:myConfig"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var configLabels, pluginLabels []string
			for role, path := range map[string]string{"config": filepath.Join(root, ".vimrc"), "plugin": pluginPath} {
				rolePath := path
				if err := os.WriteFile(rolePath, []byte(test.source), 0o644); err != nil {
					t.Fatal(err)
				}
				instance := newRootedServer(t, root)
				documentURI := uri.File(rolePath)
				instance.documents.Open(documentURI.String(), 1, test.source)
				labels := completionLabels(t, instance, documentURI, test.line, test.column)
				for _, want := range test.contains {
					found := false
					for _, label := range labels {
						if label == want {
							found = true
							break
						}
					}
					if !found {
						t.Fatalf("role %s missing %q in %#v", role, want, labels)
					}
				}
				if role == "config" {
					configLabels = labels
				} else {
					pluginLabels = labels
				}
			}
			if !reflect.DeepEqual(configLabels, pluginLabels) {
				t.Fatalf("config labels %#v differ from plugin labels %#v", configLabels, pluginLabels)
			}
		})
	}
}
