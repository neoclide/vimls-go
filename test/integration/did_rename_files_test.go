package integration_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.lsp.dev/protocol"
)

func TestDidRenameFilesSubprocess(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "lib.vim")
	newPath := filepath.Join(root, "util.vim")
	mainPath := filepath.Join(root, "main.vim")
	runtime := t.TempDir()
	for path, source := range map[string]string{
		oldPath:  "vim9script\nexport var Value = 1\n",
		mainPath: "vim9script\nimport './lib.vim' as lib\necho lib.Value\n",
	} {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := subprocessContext(t, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, vimlsBinary)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr safeBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	client := newTestClient(t, stdout, stdin, &stderr, ctx)
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"rootUri":%q,"capabilities":{"workspace":{"applyEdit":true,"fileOperations":{"didRename":true,"willRename":true},"workspaceEdit":{"documentChanges":true}}},"initializationOptions":{"runtimepath":[%q]}}}`, canonicalFileURI(t, root), runtime))
	initialize := readResponse(t, client, "1")
	if strings.Contains(string(initialize["result"]), `"executeCommandProvider"`) {
		t.Fatalf("unexpected execute command capability: %s", initialize)
	}
	var initialized protocol.InitializeResult
	if err := protocol.Unmarshal(initialize["result"], &initialized); err != nil {
		t.Fatal(err)
	}
	operations := initialized.Capabilities.Workspace.FileOperations
	if operations == nil || len(operations.DidRename.Filters) != 1 || len(operations.WillRename.Filters) != 1 {
		t.Fatalf("file operation registration = %#v", operations)
	}
	writeJSON(t, client, `{"jsonrpc":"2.0","method":"initialized","params":{}}`)
	writeJSON(t, client, `{"jsonrpc":"2.0","id":2,"method":"workspace/symbol","params":{"query":"Value"}}`)
	if response := readResponse(t, client, "2"); response["error"] != nil {
		t.Fatalf("index barrier = %s", response)
	}
	// Clear runtime roots after the workspace index is ready. The response
	// waits for runtime work, and later startup reconciliation is then a no-op.
	writeJSON(t, client, `{"jsonrpc":"2.0","id":"runtime","method":"vimls/didChangeRuntimepath","params":{"runtimepath":[]}}`)
	if response := readResponse(t, client, `"runtime"`); response["error"] != nil {
		t.Fatalf("runtime barrier = %s", response)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	notification := fmt.Sprintf(`{"jsonrpc":"2.0","method":"workspace/didRenameFiles","params":{"files":[{"oldUri":%q,"newUri":%q}]}}`, canonicalFileURI(t, oldPath), canonicalFileURI(t, newPath))
	writeJSON(t, client, notification)
	request := readJSON(t, client)
	if string(request["method"]) != `"workspace/applyEdit"` || request["id"] == nil {
		t.Fatalf("expected applyEdit request, got %s", request)
	}
	var params protocol.ApplyWorkspaceEditParams
	if err := protocol.Unmarshal(request["params"], &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Edit.DocumentChanges) != 1 {
		t.Fatalf("edit = %#v", params.Edit)
	}
	document := params.Edit.DocumentChanges[0].(*protocol.TextDocumentEdit)
	if document.TextDocument.URI != canonicalFileURI(t, mainPath) || len(document.Edits) != 1 || document.Edits[0].(*protocol.TextEdit).NewText != "'./util.vim'" {
		t.Fatalf("document edit = %#v", document)
	}
	// The apply request is a barrier. Subsequent client requests must still run
	// while the notification waits for the client's reply.
	writeJSON(t, client, `{"jsonrpc":"2.0","id":3,"method":"workspace/symbol","params":{"query":"Value"}}`)
	if response := readResponse(t, client, "3"); response["error"] != nil {
		t.Fatalf("request while applying = %s", response)
	}
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"applied":true}}`, request["id"]))

	// Leave another apply request unanswered: shutdown must cancel it without
	// replying to either didRenameFiles notification.
	writeJSON(t, client, notification)
	if request := readJSON(t, client); string(request["method"]) != `"workspace/applyEdit"` {
		t.Fatalf("expected pending applyEdit, got %s", request)
	}
	writeJSON(t, client, `{"jsonrpc":"2.0","id":4,"method":"shutdown"}`)
	if response := readResponse(t, client, "4"); string(response["result"]) != "null" || response["error"] != nil {
		t.Fatalf("shutdown = %s", response)
	}
	writeJSON(t, client, `{"jsonrpc":"2.0","method":"exit"}`)
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	waitCommand(t, ctx, command, 5*time.Second, &stderr)
	if strings.Contains(stderr.String(), "update imports after file rename:") {
		t.Fatalf("unexpected rename error: %s", stderr.String())
	}
}
