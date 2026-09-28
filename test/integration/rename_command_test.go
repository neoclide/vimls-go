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

func TestRenameCommandSubprocess(t *testing.T) {
	root := t.TempDir()
	oldPath, newPath := filepath.Join(root, "lib.vim"), filepath.Join(root, "util.vim")
	mainPath := filepath.Join(root, "main.vim")
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
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"rootUri":%q,"capabilities":{"workspace":{"applyEdit":true,"workspaceEdit":{"documentChanges":true}}},"initializationOptions":{"runtimepath":[%q]}}}`, canonicalFileURI(t, root), root))
	initialize := readResponse(t, client, "1")
	if !strings.Contains(string(initialize["result"]), `"executeCommandProvider":{"commands":["vimls.updateImportsOnRename"]}`) {
		t.Fatalf("initialize = %s", initialize)
	}
	writeJSON(t, client, `{"jsonrpc":"2.0","method":"initialized","params":{}}`)
	// A workspace request waits for the original index to finish.
	writeJSON(t, client, `{"jsonrpc":"2.0","id":2,"method":"workspace/symbol","params":{"query":"Value"}}`)
	if response := readResponse(t, client, "2"); response["error"] != nil {
		t.Fatalf("index barrier = %s", response)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	for index, reply := range []string{`{"applied":false,"failureReason":"declined"}`, `{"applied":true}`} {
		id := fmt.Sprint(index + 3)
		writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":"workspace/executeCommand","params":{"command":"vimls.updateImportsOnRename","arguments":[{"files":[{"oldUri":%q,"newUri":%q}]}]}}`, id, canonicalFileURI(t, oldPath), canonicalFileURI(t, newPath)))
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
		writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, request["id"], reply))
		if response := readResponse(t, client, id); string(response["result"]) != reply || response["error"] != nil {
			t.Fatalf("command result = %s", response)
		}
	}
	// Receiving applyEdit is a barrier: shutdown must cancel the command even
	// when the client leaves this server-to-client request unanswered.
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":5,"method":"workspace/executeCommand","params":{"command":"vimls.updateImportsOnRename","arguments":[{"files":[{"oldUri":%q,"newUri":%q}]}]}}`, canonicalFileURI(t, oldPath), canonicalFileURI(t, newPath)))
	if request := readJSON(t, client); string(request["method"]) != `"workspace/applyEdit"` {
		t.Fatalf("expected pending applyEdit, got %s", request)
	}
	writeJSON(t, client, `{"jsonrpc":"2.0","id":6,"method":"shutdown"}`)
	commandDone, shutdownDone := false, false
	for !commandDone || !shutdownDone {
		response := readJSON(t, client)
		if response["method"] != nil {
			continue // The outgoing apply request may itself be cancelled.
		}
		switch string(response["id"]) {
		case "5":
			if !strings.Contains(string(response["error"]), `"code":-32800`) {
				t.Fatalf("pending command = %s", response)
			}
			commandDone = true
		case "6":
			if string(response["result"]) != "null" {
				t.Fatalf("shutdown = %s", response)
			}
			shutdownDone = true
		default:
			t.Fatalf("unexpected response = %s", response)
		}
	}
	writeJSON(t, client, `{"jsonrpc":"2.0","method":"exit"}`)
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	waitCommand(t, ctx, command, 5*time.Second, &stderr)
}
