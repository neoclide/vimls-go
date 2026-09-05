package integration_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/jsonrpc"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

var vimlsBinary string

func TestMain(m *testing.M) {
	// Release acceptance must exercise the unpacked artifact, not silently
	// rebuild a different binary. Ordinary tests keep their clean source build.
	if supplied := os.Getenv("VIMLS_TEST_BINARY"); supplied != "" {
		var err error
		vimlsBinary, err = filepath.Abs(supplied)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid VIMLS_TEST_BINARY: %v\n", err)
			os.Exit(1)
		}
		os.Exit(m.Run())
	}
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get repository root: %v\n", err)
		os.Exit(1)
	}
	tempDir, err := os.MkdirTemp("", "vimls-integration-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tempDir)

	binPath := filepath.Join(tempDir, "vimls")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	cmd := exec.Command("go", "build", "-mod=readonly", "-o", binPath, "./cmd/vimls")
	cmd.Dir = repositoryRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build vimls: %v\noutput: %s\n", err, out)
		os.Exit(1)
	}
	vimlsBinary = binPath

	code := m.Run()
	_ = os.RemoveAll(tempDir)
	os.Exit(code)
}

func TestLSPSubprocess(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceRoot, "workspace.vim"), []byte("vim9script\nvar workspaceName = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspaceLib := filepath.Join(workspaceRoot, "lib.vim")
	if err := os.WriteFile(workspaceLib, []byte("vim9script\nexport def Run(): number\n  return 1\nenddef\nexport class Box\n  def new(value: number)\n  enddef\n  def Resize(width: number, height: number = 1): number\n    return width * height\n  enddef\n  static def Build(name: string): Box\n    return Box.new(1)\n  enddef\nendclass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspaceMain := filepath.Join(workspaceRoot, "main.vim")
	workspaceMainSource := "vim9script\nimport './lib.vim' as lib\necho lib.Run()\necho lib.Box.new(1)\necho lib.Box.Build('x')\nvar box: lib.Box = lib.Box.new(1)\necho box.Resize(2, 3)\n"
	if err := os.WriteFile(workspaceMain, []byte(workspaceMainSource), 0o600); err != nil {
		t.Fatal(err)
	}
	hierarchyPath := filepath.Join(workspaceRoot, "hierarchy.vim")
	hierarchySource := "vim9script\ninterface I\n  def Run()\nendinterface\nclass C implements I\n  def Run()\n  enddef\nendclass\ndef Target()\nenddef\ndef Caller()\n  echo '😀é' | Target()\nenddef\n"
	if err := os.WriteFile(hierarchyPath, []byte(hierarchySource), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(runtimeRoot, "plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	legacyPlugin := `" Return the supplied value for runtimepath integration coverage.
function! RuntimeGlobal(value) abort
  return a:value
endfunction
command! -nargs=1 RuntimeEcho echo RuntimeGlobal(<args>)
`
	if err := os.WriteFile(filepath.Join(runtimeRoot, "plugin", "runtime.vim"), []byte(legacyPlugin), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(runtimeRoot, "autoload"), 0o700); err != nil {
		t.Fatal(err)
	}
	legacyAutoload := `" Convert the supplied value to upper case.
function! acme#Format(value) abort
  return toupper(a:value)
endfunction
`
	if err := os.WriteFile(filepath.Join(runtimeRoot, "autoload", "acme.vim"), []byte(legacyAutoload), 0o600); err != nil {
		t.Fatal(err)
	}
	vim9Autoload := "vim9script\nexport def Run(): string\n  return 'ok'\nenddef\n"
	if err := os.WriteFile(filepath.Join(runtimeRoot, "autoload", "newapi.vim"), []byte(vim9Autoload), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(runtimeRoot, "colors"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeRoot, "colors", "default.vim"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(runtimeRoot, "doc"), 0o700); err != nil {
		t.Fatal(err)
	}
	builtinHelp := "abs({expr}) *abs()*\nReturn the absolute value.\nget({list}, {idx} [, {default}]) *get()*\nReturn an item from a list.\n*:echo*\nRuntime echo command help.\n"
	if err := os.WriteFile(filepath.Join(runtimeRoot, "doc", "builtin.txt"), []byte(builtinHelp), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := subprocessContext(t, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, vimlsBinary)
	command.Dir = repositoryRoot
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
	writer := client
	reader := client
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{"workspace":{"workspaceEdit":{"documentChanges":true},"didChangeWatchedFiles":{"dynamicRegistration":true,"relativePatternSupport":true}},"textDocument":{"documentSymbol":{"hierarchicalDocumentSymbolSupport":true},"completion":{"completionItem":{"snippetSupport":true}},"hover":{"contentFormat":["markdown"]},"signatureHelp":{"signatureInformation":{"documentationFormat":["plaintext"]}},"rename":{"prepareSupport":true},"codeAction":{"codeActionLiteralSupport":{"codeActionKind":{"valueSet":["quickfix"]}}}}},"rootUri":%q,"initializationOptions":{"runtimepath":[%q]}}}`, uri.File(workspaceRoot), runtimeRoot))
	initialize := readJSON(t, reader)
	if string(initialize["id"]) != "1" || !strings.Contains(string(initialize["result"]), `"name":"vimls"`) || !strings.Contains(string(initialize["result"]), `"documentSymbolProvider":true`) || !strings.Contains(string(initialize["result"]), `"foldingRangeProvider":true`) || !strings.Contains(string(initialize["result"]), `"selectionRangeProvider":true`) || !strings.Contains(string(initialize["result"]), `"workspaceSymbolProvider":true`) || !strings.Contains(string(initialize["result"]), `"completionProvider"`) || !strings.Contains(string(initialize["result"]), `"triggerCharacters":[".",":","&","#","<","+","\"","'","-"]`) || !strings.Contains(string(initialize["result"]), `"signatureHelpProvider"`) || !strings.Contains(string(initialize["result"]), `"semanticTokensProvider"`) || !strings.Contains(string(initialize["result"]), `"full":{"delta":true}`) || !strings.Contains(string(initialize["result"]), `"range":true`) || !strings.Contains(string(initialize["result"]), `"renameProvider"`) || !strings.Contains(string(initialize["result"]), `"documentLinkProvider"`) || !strings.Contains(string(initialize["result"]), `"codeActionProvider"`) || !strings.Contains(string(initialize["result"]), `"inlayHintProvider":true`) || !strings.Contains(string(initialize["result"]), `"codeLensProvider":{"resolveProvider":true}`) || !strings.Contains(string(initialize["result"]), `"documentFormattingProvider":true`) || !strings.Contains(string(initialize["result"]), `"documentRangeFormattingProvider":true`) || !strings.Contains(string(initialize["result"]), `"documentOnTypeFormattingProvider":{"firstTriggerCharacter":"\n","moreTriggerCharacter":["\\"]}`) || !strings.Contains(string(initialize["result"]), `"typeDefinitionProvider":true`) || !strings.Contains(string(initialize["result"]), `"implementationProvider":true`) || !strings.Contains(string(initialize["result"]), `"callHierarchyProvider":true`) || !strings.Contains(string(initialize["result"]), `"typeHierarchyProvider":true`) {
		t.Fatalf("initialize response = %s", initialize)
	}

	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"initialized","params":{}}`)
	registration := readJSON(t, reader)
	if string(registration["method"]) != `"client/registerCapability"` {
		t.Fatalf("watch registration = %s", registration)
	}
	assertVimWatchRegistration(t, registration["params"], []string{workspaceRoot})
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":null}`, registration["id"]))
	workspaceDeadline := time.Now().Add(10 * time.Second)
	var workspaceSymbols map[string]json.RawMessage
	for requestID := 10; ; requestID++ {
		writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"workspace/symbol","params":{"query":"workspaceName"}}`, requestID))
		workspaceSymbols = readJSON(t, reader)
		if strings.Contains(string(workspaceSymbols["result"]), `"name":"workspaceName"`) {
			break
		}
		if time.Now().After(workspaceDeadline) {
			t.Fatalf("workspace symbols = %s", workspaceSymbols)
		}
	}
	// Workspace symbols becoming ready no longer implies external runtime
	// parsing has finished. Await a runtimepath request before index-backed queries.
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":99989,"method":"vimls/didChangeRuntimepath","params":{"runtimepath":[%q]}}`, runtimeRoot))
	if response := readResponse(t, reader, "99989"); string(response["result"]) != "null" {
		t.Fatalf("runtimepath readiness = %s", response)
	}
	hierarchyURI := canonicalFileURI(t, hierarchyPath)
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%q,"languageId":"vim","version":1,"text":%q}}}`, hierarchyURI, hierarchySource))
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":20,"method":"textDocument/implementation","params":{"textDocument":{"uri":%q},"position":{"line":1,"character":10}}}`, hierarchyURI))
	implementation := readResponse(t, reader, "20")
	if !strings.Contains(string(implementation["result"]), `"start":{"line":4,"character":6}`) {
		t.Fatalf("implementation = %s", implementation)
	}

	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":21,"method":"textDocument/prepareTypeHierarchy","params":{"textDocument":{"uri":%q},"position":{"line":1,"character":10}}}`, hierarchyURI))
	typePrepare := readResponse(t, reader, "21")
	typeItem := firstRawArrayItem(t, typePrepare["result"])
	if !strings.Contains(string(typeItem), `"name":"I"`) || !strings.Contains(string(typeItem), `"data":`) {
		t.Fatalf("type prepare = %s", typePrepare)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":22,"method":"typeHierarchy/subtypes","params":{"item":%s}}`, typeItem))
	typeSubtypes := readResponse(t, reader, "22")
	if !strings.Contains(string(typeSubtypes["result"]), `"name":"C"`) {
		t.Fatalf("type subtypes = %s", typeSubtypes)
	}
	tamperedTypeItem := strings.Replace(string(typeItem), `"name":"I"`, `"name":"Tampered"`, 1)
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":220,"method":"typeHierarchy/subtypes","params":{"item":%s}}`, tamperedTypeItem))
	tamperedSubtypes := readResponse(t, reader, "220")
	if strings.TrimSpace(string(tamperedSubtypes["result"])) != "[]" {
		t.Fatalf("tampered type item = %s", tamperedSubtypes)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":23,"method":"textDocument/prepareTypeHierarchy","params":{"textDocument":{"uri":%q},"position":{"line":4,"character":6}}}`, hierarchyURI))
	classPrepare := readResponse(t, reader, "23")
	classItem := firstRawArrayItem(t, classPrepare["result"])
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":24,"method":"typeHierarchy/supertypes","params":{"item":%s}}`, classItem))
	typeSupertypes := readResponse(t, reader, "24")
	if !strings.Contains(string(typeSupertypes["result"]), `"name":"I"`) {
		t.Fatalf("type supertypes = %s", typeSupertypes)
	}

	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":25,"method":"textDocument/prepareCallHierarchy","params":{"textDocument":{"uri":%q},"position":{"line":8,"character":5}}}`, hierarchyURI))
	callPrepare := readResponse(t, reader, "25")
	callItem := firstRawArrayItem(t, callPrepare["result"])
	if !strings.Contains(string(callItem), `"name":"Target"`) || !strings.Contains(string(callItem), `"data":`) {
		t.Fatalf("call prepare = %s", callPrepare)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":26,"method":"callHierarchy/incomingCalls","params":{"item":%s}}`, callItem))
	incomingCalls := readResponse(t, reader, "26")
	if !strings.Contains(string(incomingCalls["result"]), `"name":"Caller"`) || !strings.Contains(string(incomingCalls["result"]), `"fromRanges":[{"start":{"line":11,"character":16}`) {
		t.Fatalf("incoming calls = %s", incomingCalls)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":27,"method":"textDocument/prepareCallHierarchy","params":{"textDocument":{"uri":%q},"position":{"line":10,"character":5}}}`, hierarchyURI))
	callerPrepare := readResponse(t, reader, "27")
	callerItem := firstRawArrayItem(t, callerPrepare["result"])
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":28,"method":"callHierarchy/outgoingCalls","params":{"item":%s}}`, callerItem))
	outgoingCalls := readResponse(t, reader, "28")
	if !strings.Contains(string(outgoingCalls["result"]), `"name":"Target"`) || !strings.Contains(string(outgoingCalls["result"]), `"fromRanges":[{"start":{"line":11,"character":16}`) {
		t.Fatalf("outgoing calls = %s", outgoingCalls)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":29,"method":"textDocument/implementation","params":{"textDocument":{"uri":%q},"position":{"line":4,"character":6}}}`, hierarchyURI))
	emptyImplementation := readResponse(t, reader, "29")
	if strings.TrimSpace(string(emptyImplementation["result"])) != "[]" {
		t.Fatalf("concrete implementation = %s", emptyImplementation)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///symbols.vim","languageId":"vim","version":1,"text":"vim9script\nvar value: number = 1\nclass Widget\n  def new()\n    if true\n      echo value\n    endif\n  enddef\nendclass\ndef Add(left: number, right: number): number\n  return left + right\nenddef\necho Add(1, 2)\n"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":2,"method":"textDocument/documentSymbol","params":{"textDocument":{"uri":"file:///symbols.vim"}}}`)
	symbols := readJSON(t, reader)
	if string(symbols["id"]) != "2" || !strings.Contains(string(symbols["result"]), `"name":"value"`) || !strings.Contains(string(symbols["result"]), `"name":"Widget"`) || !strings.Contains(string(symbols["result"]), `"name":"new"`) {
		t.Fatalf("document symbols = %s", symbols)
	}

	writeJSON(t, writer, `{"jsonrpc":"2.0","id":3,"method":"textDocument/definition","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":5,"character":12}}}`)
	definition := readJSON(t, reader)
	if string(definition["id"]) != "3" || !strings.Contains(string(definition["result"]), `"start":{"line":1,"character":4}`) {
		t.Fatalf("definition = %s", definition)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":4,"method":"textDocument/declaration","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":5,"character":12}}}`)
	declaration := readJSON(t, reader)
	if string(declaration["id"]) != "4" || !strings.Contains(string(declaration["result"]), `"start":{"line":1,"character":4}`) {
		t.Fatalf("declaration = %s", declaration)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":5,"method":"textDocument/references","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":5,"character":12},"context":{"includeDeclaration":true}}}`)
	references := readJSON(t, reader)
	if string(references["id"]) != "5" || !strings.Contains(string(references["result"]), `"line":1,"character":4`) || !strings.Contains(string(references["result"]), `"line":5,"character":11`) {
		t.Fatalf("references = %s", references)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":50,"method":"textDocument/codeLens","params":{"textDocument":{"uri":"file:///symbols.vim"}}}`)
	codeLenses := readResponse(t, reader, "50")
	var unresolvedCodeLenses []json.RawMessage
	if err := json.Unmarshal(codeLenses["result"], &unresolvedCodeLenses); err != nil {
		t.Fatalf("decode code lenses: %v; response = %s", err, codeLenses)
	}
	var addCodeLens json.RawMessage
	for _, lens := range unresolvedCodeLenses {
		if strings.Contains(string(lens), `"start":{"line":9,"character":4}`) {
			addCodeLens = lens
			break
		}
	}
	if len(addCodeLens) == 0 || strings.Contains(string(addCodeLens), `"command":`) || !strings.Contains(string(addCodeLens), `"data":`) {
		t.Fatalf("unresolved Add code lens = %s", codeLenses)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":51,"method":"codeLens/resolve","params":%s}`, addCodeLens))
	resolvedCodeLens := readResponse(t, reader, "51")
	resolvedCodeLensResult := string(resolvedCodeLens["result"])
	if !strings.Contains(resolvedCodeLensResult, `"title":"1 reference"`) || !strings.Contains(resolvedCodeLensResult, `"command":"editor.action.showReferences"`) || !strings.Contains(resolvedCodeLensResult, `"arguments":["file:///symbols.vim",{"line":9,"character":4},[{"uri":"file:///symbols.vim","range":{"start":{"line":12,"character":5}`) {
		t.Fatalf("resolved Add code lens = %s", resolvedCodeLens)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":6,"method":"textDocument/documentHighlight","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":5,"character":12}}}`)
	highlights := readJSON(t, reader)
	if string(highlights["id"]) != "6" || !strings.Contains(string(highlights["result"]), `"line":1,"character":4`) || !strings.Contains(string(highlights["result"]), `"line":5,"character":11`) {
		t.Fatalf("document highlights = %s", highlights)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":7,"method":"textDocument/hover","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":5,"character":12}}}`)
	hover := readJSON(t, reader)
	if string(hover["id"]) != "7" || !strings.Contains(string(hover["result"]), `"kind":"markdown"`) || !strings.Contains(string(hover["result"]), `**value** A number variable.`) {
		t.Fatalf("hover = %s", hover)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":8,"method":"textDocument/foldingRange","params":{"textDocument":{"uri":"file:///symbols.vim"}}}`)
	folding := readJSON(t, reader)
	if string(folding["id"]) != "8" || !strings.Contains(string(folding["result"]), `"startLine":2`) || !strings.Contains(string(folding["result"]), `"endLine":8`) {
		t.Fatalf("folding ranges = %s", folding)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":9,"method":"textDocument/selectionRange","params":{"textDocument":{"uri":"file:///symbols.vim"},"positions":[{"line":5,"character":12}]}}`)
	selection := readJSON(t, reader)
	if string(selection["id"]) != "9" || !strings.Contains(string(selection["result"]), `"start":{"line":5,"character":11}`) {
		t.Fatalf("selection ranges = %s", selection)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":90,"method":"textDocument/completion","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":12,"character":5}}}`)
	completion := readJSON(t, reader)
	if string(completion["id"]) != "90" || !strings.Contains(string(completion["result"]), `"isIncomplete":false`) || !strings.Contains(string(completion["result"]), `"label":"Add"`) || !strings.Contains(string(completion["result"]), `"label":"abs"`) || !strings.Contains(string(completion["result"]), `"textEdit"`) {
		t.Fatalf("completion = %s", completion)
	}
	resolveDeadline := time.Now().Add(2 * time.Second)
	var completionResolve map[string]json.RawMessage
	for requestID := 9100; ; requestID++ {
		writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"completionItem/resolve","params":%s}`, requestID, completionItemJSON(t, completion, "abs")))
		completionResolve = readJSON(t, reader)
		if strings.Contains(string(completionResolve["result"]), `"documentation"`) {
			break
		}
		if time.Now().After(resolveDeadline) {
			t.Fatalf("completion resolve = %s", completionResolve)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(string(completionResolve["result"]), `builtin function`) {
		t.Fatalf("completion resolve = %s", completionResolve)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///completion-command.vim","languageId":"vim","version":1,"text":"ec"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":910,"method":"textDocument/completion","params":{"textDocument":{"uri":"file:///completion-command.vim"},"position":{"line":0,"character":2}}}`)
	commandCompletion := readJSON(t, reader)
	if string(commandCompletion["id"]) != "910" || !strings.Contains(string(commandCompletion["result"]), `"label":"echo"`) {
		t.Fatalf("command completion = %s", commandCompletion)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":913,"method":"completionItem/resolve","params":%s}`, completionItemJSON(t, commandCompletion, "echo")))
	commandResolve := readJSON(t, reader)
	if string(commandResolve["id"]) != "913" || !strings.Contains(string(commandResolve["result"]), `"detail":"Ex command"`) || !strings.Contains(string(commandResolve["result"]), `Runtime echo command help.`) {
		t.Fatalf("command resolve = %s", commandResolve)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":912,"method":"textDocument/hover","params":{"textDocument":{"uri":"file:///completion-command.vim"},"position":{"line":0,"character":1}}}`)
	commandHover := readJSON(t, reader)
	if string(commandHover["id"]) != "912" || strings.Count(string(commandHover["result"]), `Runtime echo command help.`) != 1 || !strings.Contains(string(commandHover["result"]), `builtin.txt:5`) {
		t.Fatalf("command hover = %s", commandHover)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///completion-string-hover.vim","languageId":"vim","version":1,"text":"vim9script\necho has('gui_running')\necho expand('<cfile>')\n"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":914,"method":"textDocument/hover","params":{"textDocument":{"uri":"file:///completion-string-hover.vim"},"position":{"line":1,"character":11}}}`)
	hasHover := readJSON(t, reader)
	if string(hasHover["id"]) != "914" || !strings.Contains(string(hasHover["result"]), `has() feature`) || !strings.Contains(string(hasHover["result"]), `gui_running`) {
		t.Fatalf("has() hover = %s", hasHover)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":915,"method":"textDocument/hover","params":{"textDocument":{"uri":"file:///completion-string-hover.vim"},"position":{"line":2,"character":14}}}`)
	expandHover := readJSON(t, reader)
	if string(expandHover["id"]) != "915" || !strings.Contains(string(expandHover["result"]), `expand() special`) || !strings.Contains(string(expandHover["result"]), `<cfile>`) {
		t.Fatalf("expand() hover = %s", expandHover)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///completion-utf16.vim","languageId":"vim","version":1,"text":"vim9script\necho \"💩\" | echo strlen('')"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":911,"method":"textDocument/completion","params":{"textDocument":{"uri":"file:///completion-utf16.vim"},"position":{"line":1,"character":21}}}`)
	utf16Completion := readJSON(t, reader)
	if string(utf16Completion["id"]) != "911" || !strings.Contains(string(utf16Completion["result"]), `"label":"strlen"`) || !strings.Contains(string(utf16Completion["result"]), `"start":{"line":1,"character":17}`) || !strings.Contains(string(utf16Completion["result"]), `"end":{"line":1,"character":23}`) {
		t.Fatalf("UTF-16 completion = %s", utf16Completion)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":92,"method":"textDocument/signatureHelp","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":12,"character":12}}}`)
	signature := readJSON(t, reader)
	if string(signature["id"]) != "92" || !strings.Contains(string(signature["result"]), `Add(left: number, right: number)`) || !strings.Contains(string(signature["result"]), `"activeParameter":1`) {
		t.Fatalf("signature help = %s", signature)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///builtin-signature.vim","languageId":"vim","version":1,"text":"vim9script\necho get([], 'x', 0)\necho []->get(0, 1)\nvar Callback: func(number, ?string): bool\necho Callback(1, 'x')\necho &number v:version\n"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":920,"method":"textDocument/signatureHelp","params":{"textDocument":{"uri":"file:///builtin-signature.vim"},"position":{"line":1,"character":19}}}`)
	builtinSignature := readJSON(t, reader)
	if string(builtinSignature["id"]) != "920" || !strings.Contains(string(builtinSignature["result"]), `get({list}, {idx} [, {default}]): any`) || !strings.Contains(string(builtinSignature["result"]), `"documentation":{"kind":"plaintext"`) || !strings.Contains(string(builtinSignature["result"]), `"activeParameter":2`) {
		t.Fatalf("builtin signature help = %s", builtinSignature)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":921,"method":"textDocument/signatureHelp","params":{"textDocument":{"uri":"file:///builtin-signature.vim"},"position":{"line":2,"character":18}}}`)
	builtinMethodSignature := readJSON(t, reader)
	if string(builtinMethodSignature["id"]) != "921" || !strings.Contains(string(builtinMethodSignature["result"]), `get({idx}, [{default}]): any`) || !strings.Contains(string(builtinMethodSignature["result"]), `"activeParameter":1`) {
		t.Fatalf("builtin method signature help = %s", builtinMethodSignature)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":922,"method":"textDocument/signatureHelp","params":{"textDocument":{"uri":"file:///builtin-signature.vim"},"position":{"line":4,"character":20}}}`)
	functionValueSignature := readJSON(t, reader)
	if string(functionValueSignature["id"]) != "922" || !strings.Contains(string(functionValueSignature["result"]), `Callback(number, ?string): bool`) || !strings.Contains(string(functionValueSignature["result"]), `"activeParameter":1`) {
		t.Fatalf("function value signature help = %s", functionValueSignature)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":925,"method":"textDocument/hover","params":{"textDocument":{"uri":"file:///builtin-signature.vim"},"position":{"line":5,"character":7}}}`)
	optionHover := readJSON(t, reader)
	var optionResult struct {
		Contents []string       `json:"contents"`
		Range    protocol.Range `json:"range"`
	}
	if err := json.Unmarshal(optionHover["result"], &optionResult); err != nil {
		t.Fatalf("option hover is not separate Markdown documents: %s, %v", optionHover, err)
	}
	if string(optionHover["id"]) != "925" || len(optionResult.Contents) != 2 ||
		optionResult.Contents[0] != "'number' 'nu' boolean (default off)\nScope: **local to window**" ||
		!strings.HasPrefix(optionResult.Contents[1], "Print the line number") ||
		optionResult.Range != (protocol.Range{Start: protocol.Position{Line: 5, Character: 5}, End: protocol.Position{Line: 5, Character: 12}}) {
		t.Fatalf("option hover = %s", optionHover)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///class-signature.vim","languageId":"vim","version":1,"text":"vim9script\nclass Box\n  def new(value: number)\n  enddef\n  def Resize(width: number, height: number = 1): number\n    return width * height\n  enddef\nendclass\nvar box = Box.new(1)\necho box.Resize(2, 3)\n"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":923,"method":"textDocument/signatureHelp","params":{"textDocument":{"uri":"file:///class-signature.vim"},"position":{"line":8,"character":19}}}`)
	constructorSignature := readJSON(t, reader)
	if string(constructorSignature["id"]) != "923" || !strings.Contains(string(constructorSignature["result"]), `new(value: number)`) {
		t.Fatalf("constructor signature help = %s", constructorSignature)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":924,"method":"textDocument/signatureHelp","params":{"textDocument":{"uri":"file:///class-signature.vim"},"position":{"line":9,"character":20}}}`)
	classMethodSignature := readJSON(t, reader)
	if string(classMethodSignature["id"]) != "924" || !strings.Contains(string(classMethodSignature["result"]), `Resize(width: number, height: number = 1): number`) || !strings.Contains(string(classMethodSignature["result"]), `"activeParameter":1`) {
		t.Fatalf("class method signature help = %s", classMethodSignature)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":93,"method":"textDocument/prepareRename","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":5,"character":12}}}`)
	prepareRename := readJSON(t, reader)
	if string(prepareRename["id"]) != "93" || !strings.Contains(string(prepareRename["result"]), `"line":5,"character":11`) {
		t.Fatalf("prepare rename = %s", prepareRename)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":94,"method":"textDocument/rename","params":{"textDocument":{"uri":"file:///symbols.vim"},"position":{"line":5,"character":12},"newName":"renamed"}}`)
	rename := readJSON(t, reader)
	if string(rename["id"]) != "94" || !strings.Contains(string(rename["result"]), `"documentChanges"`) || !strings.Contains(string(rename["result"]), `"newText":"renamed"`) {
		t.Fatalf("rename = %s", rename)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":95,"method":"textDocument/semanticTokens/full","params":{"textDocument":{"uri":"file:///symbols.vim"}}}`)
	semanticTokens := readJSON(t, reader)
	var semanticFull struct {
		ResultID string `json:"resultId"`
	}
	if string(semanticTokens["id"]) != "95" || !strings.Contains(string(semanticTokens["result"]), `"data":[`) || json.Unmarshal(semanticTokens["result"], &semanticFull) != nil || semanticFull.ResultID == "" {
		t.Fatalf("semantic tokens = %s", semanticTokens)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":951,"method":"textDocument/semanticTokens/full/delta","params":{"textDocument":{"uri":"file:///symbols.vim"},"previousResultId":%q}}`, semanticFull.ResultID))
	semanticTokensDelta := readJSON(t, reader)
	if string(semanticTokensDelta["id"]) != "951" || !strings.Contains(string(semanticTokensDelta["result"]), `"resultId":`) || !strings.Contains(string(semanticTokensDelta["result"]), `"edits":[]`) {
		t.Fatalf("semantic token delta = %s", semanticTokensDelta)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":96,"method":"textDocument/inlayHint","params":{"textDocument":{"uri":"file:///symbols.vim"},"range":{"start":{"line":0,"character":0},"end":{"line":13,"character":0}}}}`)
	inlayHints := readJSON(t, reader)
	if string(inlayHints["id"]) != "96" || !strings.Contains(string(inlayHints["result"]), `": number"`) {
		t.Fatalf("inlay hints = %s", inlayHints)
	}

	writeJSON(t, writer, `{"jsonrpc":"2.0","id":99999,"method":"workspace/symbol","params":{"query":"RuntimeGlobal"}}`)
	runtimeSymbols := readJSON(t, reader)
	if string(runtimeSymbols["id"]) != "99999" || string(runtimeSymbols["result"]) != "[]" {
		t.Fatalf("runtimepath symbols = %s", runtimeSymbols)
	}
	legacyMain := "let g:result = RuntimeGlobal(1)\necho acme#Format('x')\n"

	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///legacy-main.vim","languageId":"vim","version":1,"text":%q}}}`, legacyMain))
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":99990,"method":"textDocument/definition","params":{"textDocument":{"uri":"file:///legacy-main.vim"},"position":{"line":1,"character":10}}}`)
	legacyDefinition := readResponse(t, reader, "99990")
	if !strings.Contains(string(legacyDefinition["result"]), fmt.Sprintf(`"uri":%q`, canonicalFileURI(t, filepath.Join(runtimeRoot, "autoload", "acme.vim")))) || !strings.Contains(string(legacyDefinition["result"]), `"start":{"line":1,"character":10}`) {
		t.Fatalf("legacy autoload definition = %s", legacyDefinition)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":99991,"method":"textDocument/hover","params":{"textDocument":{"uri":"file:///legacy-main.vim"},"position":{"line":0,"character":18}}}`)
	legacyHover := readResponse(t, reader, "99991")
	if !strings.Contains(string(legacyHover["result"]), `RuntimeGlobal(value)`) || !strings.Contains(string(legacyHover["result"]), `Return the supplied value`) {
		t.Fatalf("legacy runtime hover = %s", legacyHover)
	}
	// Legacy values are dynamically typed; the type definition must stay empty.
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":99992,"method":"textDocument/typeDefinition","params":{"textDocument":{"uri":"file:///legacy-main.vim"},"position":{"line":0,"character":6}}}`)
	legacyTypeDefinition := readResponse(t, reader, "99992")
	if string(legacyTypeDefinition["result"]) != "[]" {
		t.Fatalf("legacy type definition = %s", legacyTypeDefinition)
	}
	autoloadCaller := "function! AutoloadCaller()\n  call newapi#Run()\nendfunction\n"
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///autoload-caller.vim","languageId":"vim","version":1,"text":%q}}}`, autoloadCaller))
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":99994,"method":"textDocument/prepareCallHierarchy","params":{"textDocument":{"uri":"file:///autoload-caller.vim"},"position":{"line":1,"character":14}}}`)
	autoloadPrepare := readResponse(t, reader, "99994")
	autoloadItem := firstRawArrayItem(t, autoloadPrepare["result"])
	if !strings.Contains(string(autoloadItem), `"name":"Run"`) || !strings.Contains(string(autoloadItem), fmt.Sprintf(`"uri":%q`, canonicalFileURI(t, filepath.Join(runtimeRoot, "autoload", "newapi.vim")))) {
		t.Fatalf("Vim9 autoload call prepare = %s", autoloadPrepare)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":99995,"method":"callHierarchy/incomingCalls","params":{"item":%s}}`, autoloadItem))
	autoloadIncoming := readResponse(t, reader, "99995")
	if !strings.Contains(string(autoloadIncoming["result"]), `"name":"AutoloadCaller"`) {
		t.Fatalf("Vim9 autoload incoming calls = %s", autoloadIncoming)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///legacy-completion.vim","languageId":"vim","version":1,"text":"echo RuntimeG"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":99993,"method":"textDocument/completion","params":{"textDocument":{"uri":"file:///legacy-completion.vim"},"position":{"line":0,"character":13}}}`)
	legacyCompletion := readResponse(t, reader, "99993")
	if !strings.Contains(string(legacyCompletion["result"]), `"label":"RuntimeGlobal"`) || !strings.Contains(string(legacyCompletion["result"]), `"detail":"RuntimeGlobal(value)"`) || !strings.Contains(string(legacyCompletion["result"]), `Return the supplied value`) {
		t.Fatalf("legacy runtime completion = %s", legacyCompletion)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%q,"languageId":"vim","version":1,"text":%q}}}`, uri.File(workspaceMain), workspaceMainSource))
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":100000,"method":"textDocument/definition","params":{"textDocument":{"uri":%q},"position":{"line":2,"character":10}}}`, uri.File(workspaceMain)))
	crossDefinition := readResponse(t, reader, "100000")
	if string(crossDefinition["id"]) != "100000" || !strings.Contains(string(crossDefinition["result"]), fmt.Sprintf(`"uri":%q`, canonicalFileURI(t, workspaceLib))) || !strings.Contains(string(crossDefinition["result"]), `"start":{"line":1,"character":11}`) {
		t.Fatalf("cross-file definition = %s", crossDefinition)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":100001,"method":"textDocument/references","params":{"textDocument":{"uri":%q},"position":{"line":2,"character":10},"context":{"includeDeclaration":true}}}`, uri.File(workspaceMain)))
	crossReferences := readResponse(t, reader, "100001")
	if string(crossReferences["id"]) != "100001" || !strings.Contains(string(crossReferences["result"]), fmt.Sprintf(`"uri":%q`, canonicalFileURI(t, workspaceLib))) || !strings.Contains(string(crossReferences["result"]), fmt.Sprintf(`"uri":%q`, canonicalFileURI(t, workspaceMain))) {
		t.Fatalf("cross-file references = %s", crossReferences)
	}
	// The cursor rests on the lib.Box annotation of `var box: lib.Box`, so the
	// type definition must land on the exported class in lib.vim.
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":100050,"method":"textDocument/typeDefinition","params":{"textDocument":{"uri":%q},"position":{"line":5,"character":13}}}`, uri.File(workspaceMain)))
	crossTypeDefinition := readResponse(t, reader, "100050")
	if string(crossTypeDefinition["id"]) != "100050" || !strings.Contains(string(crossTypeDefinition["result"]), fmt.Sprintf(`"uri":%q`, canonicalFileURI(t, workspaceLib))) || !strings.Contains(string(crossTypeDefinition["result"]), `"start":{"line":4,"character":13}`) {
		t.Fatalf("cross-file type definition = %s", crossTypeDefinition)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":100002,"method":"textDocument/documentLink","params":{"textDocument":{"uri":%q}}}`, uri.File(workspaceMain)))
	documentLinks := readResponse(t, reader, "100002")
	if string(documentLinks["id"]) != "100002" || !strings.Contains(string(documentLinks["result"]), fmt.Sprintf(`"target":%q`, canonicalFileURI(t, workspaceLib))) {
		t.Fatalf("document links = %s", documentLinks)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":100003,"method":"textDocument/completion","params":{"textDocument":{"uri":%q},"position":{"line":2,"character":9}}}`, uri.File(workspaceMain)))
	memberCompletion := readResponse(t, reader, "100003")
	if string(memberCompletion["id"]) != "100003" || !strings.Contains(string(memberCompletion["result"]), `"label":"Run"`) {
		t.Fatalf("member completion = %s", memberCompletion)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":100006,"method":"textDocument/signatureHelp","params":{"textDocument":{"uri":%q},"position":{"line":2,"character":13}}}`, uri.File(workspaceMain)))
	importedSignature := readResponse(t, reader, "100006")
	if string(importedSignature["id"]) != "100006" || !strings.Contains(string(importedSignature["result"]), `Run(): number`) {
		t.Fatalf("imported signature help = %s", importedSignature)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":100007,"method":"textDocument/signatureHelp","params":{"textDocument":{"uri":%q},"position":{"line":3,"character":18}}}`, uri.File(workspaceMain)))
	importedConstructorSignature := readResponse(t, reader, "100007")
	if string(importedConstructorSignature["id"]) != "100007" || !strings.Contains(string(importedConstructorSignature["result"]), `new(value: number)`) {
		t.Fatalf("imported constructor signature help = %s", importedConstructorSignature)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":100008,"method":"textDocument/signatureHelp","params":{"textDocument":{"uri":%q},"position":{"line":4,"character":22}}}`, uri.File(workspaceMain)))
	importedClassMethodSignature := readResponse(t, reader, "100008")
	if string(importedClassMethodSignature["id"]) != "100008" || !strings.Contains(string(importedClassMethodSignature["result"]), `Build(name: string): Box`) {
		t.Fatalf("imported class method signature help = %s", importedClassMethodSignature)
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":100009,"method":"textDocument/signatureHelp","params":{"textDocument":{"uri":%q},"position":{"line":6,"character":20}}}`, uri.File(workspaceMain)))
	importedObjectMethodSignature := readResponse(t, reader, "100009")
	if string(importedObjectMethodSignature["id"]) != "100009" || !strings.Contains(string(importedObjectMethodSignature["result"]), `Resize(width: number, height: number = 1): number`) || !strings.Contains(string(importedObjectMethodSignature["result"]), `"activeParameter":1`) {
		t.Fatalf("imported object method signature help = %s", importedObjectMethodSignature)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///missing-end.vim","languageId":"vim","version":1,"text":"vim9script\nif true\n  echo 'x'\n"}}}`)
	readPublishedDiagnostic(t, reader, "file:///missing-end.vim", "vim/E171")
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":100004,"method":"textDocument/codeAction","params":{"textDocument":{"uri":"file:///missing-end.vim"},"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":2}},"context":{"diagnostics":[{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":2}},"code":"vim/E171","message":"missing :endif"}],"only":["quickfix"]}}}`)
	codeActions := readResponse(t, reader, "100004")
	if !strings.Contains(string(codeActions["result"]), `"title":"Insert :endif"`) || !strings.Contains(string(codeActions["result"]), `"newText":"endif\n"`) {
		t.Fatalf("code actions = %s", codeActions)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///snippet.vim","languageId":"vim","version":1,"text":"vim9script\ndef Add(left: number, right: number)\nenddef\necho Ad\n"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":100010,"method":"textDocument/completion","params":{"textDocument":{"uri":"file:///snippet.vim"},"position":{"line":3,"character":7}}}`)
	snippetCompletion := readResponse(t, reader, "100010")
	if !strings.Contains(string(snippetCompletion["result"]), `"label":"Add"`) || !strings.Contains(string(snippetCompletion["result"]), `"insertTextFormat":2`) || !strings.Contains(string(snippetCompletion["result"]), `"newText":"Add(${1:left}, ${2:right})$0"`) {
		t.Fatalf("snippet completion = %s", snippetCompletion)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///mapping.vim","languageId":"vim","version":1,"text":"nmap <bu"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":100011,"method":"textDocument/completion","params":{"textDocument":{"uri":"file:///mapping.vim"},"position":{"line":0,"character":8}}}`)
	mappingCompletion := readResponse(t, reader, "100011")
	if !strings.Contains(string(mappingCompletion["result"]), `"label":"<buffer>"`) || !strings.Contains(string(mappingCompletion["result"]), `"newText":"<buffer>"`) || !strings.Contains(string(mappingCompletion["result"]), `"start":{"line":0,"character":5}`) {
		t.Fatalf("mapping completion = %s", mappingCompletion)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///highlight.vim","languageId":"vim","version":1,"text":"highlight Normal cterm=bold,und"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":100012,"method":"textDocument/completion","params":{"textDocument":{"uri":"file:///highlight.vim"},"position":{"line":0,"character":31}}}`)
	highlightCompletion := readResponse(t, reader, "100012")
	if !strings.Contains(string(highlightCompletion["result"]), `"label":"underline"`) || !strings.Contains(string(highlightCompletion["result"]), `"newText":"underline"`) || !strings.Contains(string(highlightCompletion["result"]), `"start":{"line":0,"character":28}`) {
		t.Fatalf("highlight completion = %s", highlightCompletion)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///colorscheme.vim","languageId":"vim","version":1,"text":"colorscheme defa"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":100013,"method":"textDocument/completion","params":{"textDocument":{"uri":"file:///colorscheme.vim"},"position":{"line":0,"character":16}}}`)
	colorschemeCompletion := readResponse(t, reader, "100013")
	if !strings.Contains(string(colorschemeCompletion["result"]), `"label":"default"`) || !strings.Contains(string(colorschemeCompletion["result"]), `"newText":"default"`) || !strings.Contains(string(colorschemeCompletion["result"]), `"start":{"line":0,"character":12}`) {
		t.Fatalf("colorscheme completion = %s", colorschemeCompletion)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///format-vim9.vim","languageId":"vim","version":1,"text":"vim9script\nif true\necho 1\nendif\n"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":100014,"method":"textDocument/formatting","params":{"textDocument":{"uri":"file:///format-vim9.vim"},"options":{"tabSize":2,"insertSpaces":true}}}`)
	formatting := readResponse(t, reader, "100014")
	if !strings.Contains(string(formatting["result"]), `"start":{"line":2,"character":0}`) || !strings.Contains(string(formatting["result"]), `"end":{"line":2,"character":0}`) || !strings.Contains(string(formatting["result"]), `"newText":"  "`) {
		t.Fatalf("document formatting = %s", formatting)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///format-legacy.vim","languageId":"vim","version":1,"text":"function! Demo()\nlet value = 1\nendfunction\n"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":100015,"method":"textDocument/rangeFormatting","params":{"textDocument":{"uri":"file:///format-legacy.vim"},"range":{"start":{"line":1,"character":0},"end":{"line":2,"character":0}},"options":{"tabSize":2,"insertSpaces":true}}}`)
	rangeFormatting := readResponse(t, reader, "100015")
	if !strings.Contains(string(rangeFormatting["result"]), `"start":{"line":1,"character":0}`) || !strings.Contains(string(rangeFormatting["result"]), `"end":{"line":1,"character":0}`) || !strings.Contains(string(rangeFormatting["result"]), `"newText":"  "`) {
		t.Fatalf("range formatting = %s", rangeFormatting)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///ontype-vim9.vim","languageId":"vim","version":1,"text":"vim9script\ndef Demo()\n\nenddef\n"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":100016,"method":"textDocument/onTypeFormatting","params":{"textDocument":{"uri":"file:///ontype-vim9.vim"},"position":{"line":2,"character":0},"ch":"\n","options":{"tabSize":2,"insertSpaces":true}}}`)
	onTypeVim9 := readResponse(t, reader, "100016")
	if !strings.Contains(string(onTypeVim9["result"]), `"start":{"line":2,"character":0}`) || !strings.Contains(string(onTypeVim9["result"]), `"end":{"line":2,"character":0}`) || !strings.Contains(string(onTypeVim9["result"]), `"newText":"  "`) {
		t.Fatalf("vim9 on-type formatting = %s", onTypeVim9)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///ontype-legacy.vim","languageId":"vim","version":1,"text":"function! Demo()\n\nendfunction\n"}}}`)
	writeJSON(t, writer, `{"jsonrpc":"2.0","id":100017,"method":"textDocument/onTypeFormatting","params":{"textDocument":{"uri":"file:///ontype-legacy.vim"},"position":{"line":1,"character":0},"ch":"\n","options":{"tabSize":2,"insertSpaces":true}}}`)
	onTypeLegacy := readResponse(t, reader, "100017")
	if !strings.Contains(string(onTypeLegacy["result"]), `"start":{"line":1,"character":0}`) || !strings.Contains(string(onTypeLegacy["result"]), `"end":{"line":1,"character":0}`) || !strings.Contains(string(onTypeLegacy["result"]), `"newText":"  "`) {
		t.Fatalf("legacy on-type formatting = %s", onTypeLegacy)
	}

	writeJSON(t, writer, `{"jsonrpc":"2.0","id":100005,"method":"shutdown"}`)
	shutdown := readResponse(t, reader, "100005")
	if string(shutdown["result"]) != "null" {
		t.Fatalf("shutdown response = %s", shutdown)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"exit"}`)
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	waitCommand(t, ctx, command, 5*time.Second, &stderr)
	if ctx.Err() != nil {
		t.Fatalf("server timed out: %v", ctx.Err())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestDocumentPullDiagnosticsSubprocess(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := t.TempDir()
	closedPath := filepath.Join(workspaceRoot, "closed.vim")
	if err := os.WriteFile(closedPath, []byte("vim9script\necho missingClosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	openPath := filepath.Join(workspaceRoot, "open.vim")
	documentURI := canonicalFileURI(t, openPath).String()
	ctx, cancel := subprocessContext(t, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, vimlsBinary)
	command.Dir = repositoryRoot
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
	writer := client
	reader := client
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"rootUri":%q,"capabilities":{"textDocument":{"diagnostic":{}}},"initializationOptions":{"runtimepath":[]}}}`, canonicalFileURI(t, workspaceRoot).String()))
	initialize := readResponse(t, reader, "1")
	if !strings.Contains(string(initialize["result"]), `"diagnosticProvider":{"interFileDependencies":true,"workspaceDiagnostics":true}`) {
		t.Fatalf("initialize response = %s", initialize)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"initialized","params":{}}`)
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%q,"languageId":"vim","version":1,"text":"if true\n"}}}`, documentURI))
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"textDocument/diagnostic","params":{"textDocument":{"uri":%q}}}`, documentURI))
	first := readPullResponse(t, reader, writer, &stderr, "2")
	var full struct {
		Kind     string `json:"kind"`
		ResultID string `json:"resultId"`
	}
	if err := json.Unmarshal(first["result"], &full); err != nil {
		t.Fatalf("decode first diagnostic response: %v, response: %s", err, first)
	}
	if full.Kind != "full" || full.ResultID == "" {
		t.Fatalf("first diagnostic response = %s", first)
	}

	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"textDocument/diagnostic","params":{"textDocument":{"uri":%q},"previousResultId":%q}}`, documentURI, full.ResultID))
	unchanged := readPullResponse(t, reader, writer, &stderr, "3")
	if !strings.Contains(string(unchanged["result"]), `"kind":"unchanged"`) || !strings.Contains(string(unchanged["result"]), fmt.Sprintf(`"resultId":%q`, full.ResultID)) {
		t.Fatalf("unchanged diagnostic response = %s", unchanged)
	}

	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":%q,"version":2},"contentChanges":[{"text":"if true\nendif\n"}]}}`, documentURI))
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":4,"method":"textDocument/diagnostic","params":{"textDocument":{"uri":%q},"previousResultId":%q}}`, documentURI, full.ResultID))
	changed := readPullResponse(t, reader, writer, &stderr, "4")
	var changedFull struct {
		Kind     string `json:"kind"`
		ResultID string `json:"resultId"`
	}
	if err := json.Unmarshal(changed["result"], &changedFull); err != nil {
		t.Fatal(err)
	}
	if changedFull.Kind != "full" || changedFull.ResultID == "" || changedFull.ResultID == full.ResultID {
		t.Fatalf("changed diagnostic response = %s", changed)
	}

	writeJSON(t, writer, `{"jsonrpc":"2.0","id":5,"method":"workspace/diagnostic","params":{"previousResultIds":[]}}`)
	workspaceReport := readPullResponse(t, reader, writer, &stderr, "5")
	var workspaceResult struct {
		Items []struct {
			URI      string            `json:"uri"`
			Version  *int32            `json:"version"`
			Kind     string            `json:"kind"`
			ResultID string            `json:"resultId"`
			Items    []json.RawMessage `json:"items"`
		} `json:"items"`
	}
	if err := json.Unmarshal(workspaceReport["result"], &workspaceResult); err != nil {
		t.Fatalf("decode workspace diagnostic response: %v, response: %s", err, workspaceReport)
	}
	if len(workspaceResult.Items) != 2 {
		t.Fatalf("workspace diagnostic response = %s", workspaceReport)
	}
	closedURI := canonicalFileURI(t, closedPath).String()
	if workspaceResult.Items[0].URI != closedURI || workspaceResult.Items[0].Version != nil || workspaceResult.Items[0].Kind != "full" || workspaceResult.Items[0].ResultID == "" || len(workspaceResult.Items[0].Items) == 0 {
		t.Fatalf("closed workspace diagnostic = %#v", workspaceResult.Items[0])
	}
	if workspaceResult.Items[1].URI != documentURI || workspaceResult.Items[1].Version == nil || *workspaceResult.Items[1].Version != 2 || workspaceResult.Items[1].Kind != "full" || workspaceResult.Items[1].ResultID == "" {
		t.Fatalf("open workspace diagnostic = %#v", workspaceResult.Items[1])
	}
	writeJSON(t, writer, fmt.Sprintf(`{"jsonrpc":"2.0","id":6,"method":"workspace/diagnostic","params":{"previousResultIds":[{"uri":%q,"value":%q},{"uri":%q,"value":%q}]}}`, closedURI, workspaceResult.Items[0].ResultID, documentURI, workspaceResult.Items[1].ResultID))
	unchangedWorkspace := readPullResponse(t, reader, writer, &stderr, "6")
	if strings.Count(string(unchangedWorkspace["result"]), `"kind":"unchanged"`) != 2 {
		t.Fatalf("unchanged workspace diagnostic response = %s", unchangedWorkspace)
	}

	writeJSON(t, writer, `{"jsonrpc":"2.0","id":7,"method":"shutdown"}`)
	shutdown := readPullResponse(t, reader, writer, &stderr, "7")
	if string(shutdown["result"]) != "null" {
		t.Fatalf("shutdown response = %s", shutdown)
	}
	writeJSON(t, writer, `{"jsonrpc":"2.0","method":"exit"}`)
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	waitCommand(t, ctx, command, 5*time.Second, &stderr)
	if ctx.Err() != nil {
		t.Fatalf("server timed out: %v", ctx.Err())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestRangesFormattingSubprocess(t *testing.T) {
	type lspPosition struct {
		Line      uint32 `json:"line"`
		Character uint32 `json:"character"`
	}
	type lspRange struct {
		Start lspPosition `json:"start"`
		End   lspPosition `json:"end"`
	}
	type lspTextEdit struct {
		Range   lspRange `json:"range"`
		NewText string   `json:"newText"`
	}
	positionLess := func(left, right lspPosition) bool {
		return left.Line < right.Line || left.Line == right.Line && left.Character < right.Character
	}
	// inside reports whether the edit lies fully inside the end-exclusive
	// requested range [start,end), mirroring the server containment rule.
	inside := func(edit lspTextEdit, start, end lspPosition) bool {
		return !positionLess(edit.Range.Start, start) && !positionLess(end, edit.Range.End) && positionLess(edit.Range.Start, end)
	}

	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := t.TempDir()
	documentURI := canonicalFileURI(t, filepath.Join(workspaceRoot, "ranges.vim")).String()
	ctx, cancel := subprocessContext(t, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, vimlsBinary)
	command.Dir = repositoryRoot
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
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{"textDocument":{"rangeFormatting":{"rangesSupport":true}}},"rootUri":%q,"initializationOptions":{"runtimepath":[]}}}`, canonicalFileURI(t, workspaceRoot).String()))
	initialize := readResponse(t, client, "1")
	if !strings.Contains(string(initialize["result"]), `"documentRangeFormattingProvider":{"rangesSupport":true}`) {
		t.Fatalf("initialize response = %s", initialize)
	}
	writeJSON(t, client, `{"jsonrpc":"2.0","method":"initialized","params":{}}`)

	// Two separate mis-indented regions: the body of the first if is one space
	// too shallow and the second if body is unindented, so whole-document
	// formatting edits two different lines.
	source := "vim9script\nif true\n echo 1\nendif\nif false\necho 2\nendif\n"
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%q,"languageId":"vim","version":1,"text":%q}}}`, documentURI, source))

	firstRangeStart := lspPosition{Line: 2}
	firstRangeEnd := lspPosition{Line: 3}
	secondRangeStart := lspPosition{Line: 5}
	secondRangeEnd := lspPosition{Line: 6}
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"textDocument/rangesFormatting","params":{"textDocument":{"uri":%q},"ranges":[{"start":{"line":%d,"character":%d},"end":{"line":%d,"character":%d}},{"start":{"line":%d,"character":%d},"end":{"line":%d,"character":%d}}],"options":{"tabSize":2,"insertSpaces":true}}}`, documentURI, firstRangeStart.Line, firstRangeStart.Character, firstRangeEnd.Line, firstRangeEnd.Character, secondRangeStart.Line, secondRangeStart.Character, secondRangeEnd.Line, secondRangeEnd.Character))
	first := readResponse(t, client, "2")
	var edits []lspTextEdit
	if err := json.Unmarshal(first["result"], &edits); err != nil {
		t.Fatalf("decode ranges formatting response: %v, response: %s", err, first)
	}
	if len(edits) != 2 {
		t.Fatalf("ranges formatting edits = %#v, want 2", edits)
	}
	firstInside := inside(edits[0], firstRangeStart, firstRangeEnd)
	firstInsideSecond := inside(edits[0], secondRangeStart, secondRangeEnd)
	secondInside := inside(edits[1], secondRangeStart, secondRangeEnd)
	secondInsideFirst := inside(edits[1], firstRangeStart, firstRangeEnd)
	if !firstInside || firstInsideSecond || !secondInside || secondInsideFirst {
		t.Fatalf("edits %#v not exactly inside their requested ranges", edits)
	}
	if positionLess(edits[1].Range.Start, edits[0].Range.Start) || positionLess(edits[1].Range.Start, edits[0].Range.End) {
		t.Fatalf("edits %#v are not ordered and non-overlapping", edits)
	}
	for index, edit := range edits {
		if edit.NewText != "  " {
			t.Fatalf("edit %d newText = %q, want two-space indent", index, edit.NewText)
		}
	}

	// An empty ranges array formats nothing and returns an empty array.
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"textDocument/rangesFormatting","params":{"textDocument":{"uri":%q},"ranges":[],"options":{"tabSize":2,"insertSpaces":true}}}`, documentURI))
	empty := readResponse(t, client, "3")
	if string(empty["result"]) != "[]" {
		t.Fatalf("empty ranges formatting response = %s", empty)
	}

	writeJSON(t, client, `{"jsonrpc":"2.0","id":4,"method":"shutdown"}`)
	shutdown := readResponse(t, client, "4")
	if string(shutdown["result"]) != "null" {
		t.Fatalf("shutdown response = %s", shutdown)
	}
	writeJSON(t, client, `{"jsonrpc":"2.0","method":"exit"}`)
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	waitCommand(t, ctx, command, 5*time.Second, &stderr)
	if ctx.Err() != nil {
		t.Fatalf("server timed out: %v", ctx.Err())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func assertVimWatchRegistration(t *testing.T, raw json.RawMessage, roots []string) {
	t.Helper()
	var params protocol.RegistrationParams
	if err := protocol.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Registrations) != 1 {
		t.Fatalf("registrations = %#v", params.Registrations)
	}
	registration := params.Registrations[0]
	if registration.ID != "vimls-watch-vim-files" || registration.Method != protocol.MethodWorkspaceDidChangeWatchedFiles {
		t.Fatalf("registration = %#v", registration)
	}
	var options protocol.DidChangeWatchedFilesRegistrationOptions
	if err := protocol.Unmarshal(registration.RegisterOptions, &options); err != nil {
		t.Fatal(err)
	}
	if len(options.Watchers) != len(roots) {
		t.Fatalf("watchers = %#v", options.Watchers)
	}
	canonicalRoots := make([]string, len(roots))
	for index, root := range roots {
		canonical, err := workspace.CanonicalPath(root)
		if err != nil {
			t.Fatal(err)
		}
		canonicalRoots[index] = canonical
	}
	sort.Strings(canonicalRoots)
	wantKind := protocol.WatchKindCreate | protocol.WatchKindChange | protocol.WatchKindDelete
	for index, root := range canonicalRoots {
		pattern, ok := options.Watchers[index].GlobPattern.(*protocol.RelativePattern)
		if !ok || pattern.BaseURI != protocol.URI(uri.File(root)) || pattern.Pattern != protocol.Pattern(workspace.VimFileWatchPattern()) || options.Watchers[index].Kind != wantKind {
			t.Fatalf("watcher %d = %#v", index, options.Watchers[index])
		}
	}
}

func canonicalFileURI(t *testing.T, path string) uri.URI {
	t.Helper()
	canonical, err := workspace.CanonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	return uri.File(canonical)
}

func TestVersionSubprocess(t *testing.T) {
	ctx, cancel := subprocessContext(t, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, vimlsBinary, "--version")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("version failed: %v, output: %s", err, output)
	}
	expected := "dev"
	if os.Getenv("VIMLS_TEST_BINARY") != "" && os.Getenv("VIMLS_TEST_VERSION") != "" {
		expected = os.Getenv("VIMLS_TEST_VERSION")
	}
	if string(output) != "vimls "+expected+"\n" {
		t.Fatalf("version output = %q", output)
	}
}

func TestTCPSubprocess(t *testing.T) {
	ctx, cancel := subprocessContext(t, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, vimlsBinary, "--listen", "127.0.0.1:0")
	stderr, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stderr).ReadString('\n')
	if err != nil {
		t.Fatalf("read listen address: %v", err)
	}
	const prefix = "vimls: listening on tcp://"
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("listen output = %q", line)
	}
	address := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	connection, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", address, err)
	}
	var serverStderr safeBuffer
	go func() {
		_, _ = io.Copy(&serverStderr, stderr)
	}()
	client := newTestClient(t, connection, connection, &serverStderr, ctx)
	runSharedEditingScenario(t, client, t.TempDir())
	_ = connection.Close()
	waitCommand(t, ctx, command, 5*time.Second, &serverStderr)
	if ctx.Err() != nil {
		t.Fatalf("TCP server timed out: %v", ctx.Err())
	}
}

func TestStdioSharedScenario(t *testing.T) {
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
	runSharedEditingScenario(t, client, t.TempDir())
	waitCommand(t, ctx, command, 5*time.Second, &stderr)
	if ctx.Err() != nil {
		t.Fatalf("stdio server timed out: %v", ctx.Err())
	}
}

type safeBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *safeBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func subprocessContext(t *testing.T, budget time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	parent := t.Context()
	if deadline, ok := t.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 500*time.Millisecond {
			remaining -= 500 * time.Millisecond
		}
		if remaining < budget {
			budget = remaining
		}
	}
	return context.WithTimeout(parent, budget)
}

func waitCommand(t *testing.T, ctx context.Context, cmd *exec.Cmd, timeout time.Duration, stderr *safeBuffer) {
	t.Helper()
	if ctx != nil {
		if d, ok := ctx.Deadline(); ok {
			rem := time.Until(d)
			const killReserve = 500 * time.Millisecond
			if rem <= killReserve {
				t.Fatalf("lifetime context exhausted before waitCommand: remaining %v <= %v, stderr: %s", rem, killReserve, stderr.String())
				return
			}
			available := rem - killReserve
			if timeout <= 0 || available < timeout {
				timeout = available
			}
		}
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server failed: %v, stderr: %s", err, stderr.String())
		}
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		t.Fatalf("timed out waiting %v for server exit, stderr: %s", timeout, stderr.String())
	}
}

type transcriptEntry struct {
	time time.Time
	dir  string
	body string
}

type frameResult struct {
	body []byte
	err  error
}

type testHelper interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

type testClient struct {
	t              testHelper
	writer         *jsonrpc.Writer
	stderr         *safeBuffer
	lifetimeCtx    context.Context
	readChan       chan frameResult
	transcript     []transcriptEntry
	mu             sync.Mutex
	defaultTimeout time.Duration
}

func newTestClient(t testHelper, r io.Reader, w io.Writer, stderr *safeBuffer, lifetimeCtx ...context.Context) *testClient {
	var lCtx context.Context
	if len(lifetimeCtx) > 0 {
		lCtx = lifetimeCtx[0]
	}
	opTimeout := 10 * time.Second
	if lCtx != nil {
		if d, ok := lCtx.Deadline(); ok {
			remaining := time.Until(d)
			const cleanupReserve = 3 * time.Second
			if remaining <= cleanupReserve {
				t.Fatalf("subprocess lifetime budget %v insufficient for cleanup reserve %v", remaining, cleanupReserve)
			}
			available := remaining - cleanupReserve
			if available < opTimeout {
				opTimeout = available
			}
		}
	}
	c := &testClient{
		t:              t,
		writer:         jsonrpc.NewWriter(w),
		stderr:         stderr,
		lifetimeCtx:    lCtx,
		readChan:       make(chan frameResult, 128),
		defaultTimeout: opTimeout,
	}
	go func() {
		reader := jsonrpc.NewReader(r)
		for {
			body, err := reader.Read()
			c.readChan <- frameResult{body: body, err: err}
			if err != nil {
				return
			}
		}
	}()
	return c
}

func (c *testClient) recordTranscript(dir, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transcript = append(c.transcript, transcriptEntry{
		time: time.Now(),
		dir:  dir,
		body: body,
	})
	if len(c.transcript) > 30 {
		c.transcript = c.transcript[len(c.transcript)-30:]
	}
}

func (c *testClient) formatFailure(msg string) string {
	var sb strings.Builder
	sb.WriteString("\n=== LSP SUBPROCESS TEST FAILURE ===\n")
	sb.WriteString(msg)
	sb.WriteByte('\n')
	sb.WriteString("\n--- RECENT TRANSCRIPT (last frames) ---\n")
	c.mu.Lock()
	if len(c.transcript) == 0 {
		sb.WriteString("<no frames recorded>\n")
	} else {
		for _, entry := range c.transcript {
			body := entry.body
			if len(body) > 300 {
				body = body[:300] + "... [truncated]"
			}
			fmt.Fprintf(&sb, "[%s] %s %s\n", entry.time.Format("15:04:05.000"), entry.dir, body)
		}
	}
	c.mu.Unlock()
	sb.WriteString("\n--- SERVER STDERR ---\n")
	if c.stderr != nil && c.stderr.Len() > 0 {
		sb.WriteString(c.stderr.String())
	} else {
		sb.WriteString("<empty>\n")
	}
	sb.WriteString("====================================\n")
	return sb.String()
}

func (c *testClient) failf(format string, args ...any) {
	c.t.Helper()
	c.t.Fatal(c.formatFailure(fmt.Sprintf(format, args...)))
}

func (c *testClient) write(body string) {
	c.t.Helper()
	c.recordTranscript("->", body)
	if err := c.writer.Write([]byte(body)); err != nil {
		c.failf("write failed: %v", err)
	}
}

func (c *testClient) readTimeout(d time.Duration, waitingFor string) map[string]json.RawMessage {
	c.t.Helper()
	if c.lifetimeCtx != nil {
		if deadline, ok := c.lifetimeCtx.Deadline(); ok {
			rem := time.Until(deadline)
			const cleanupReserve = 3 * time.Second
			if rem <= cleanupReserve {
				c.failf("subprocess lifetime context exhausted while waiting for %s (remaining %v <= cleanup reserve %v)", waitingFor, rem, cleanupReserve)
				return nil
			}
			available := rem - cleanupReserve
			if d <= 0 || available < d {
				d = available
			}
		}
	}
	var timer <-chan time.Time
	if d > 0 {
		timerObj := time.NewTimer(d)
		defer timerObj.Stop()
		timer = timerObj.C
	}
	var ctxDone <-chan struct{}
	if c.lifetimeCtx != nil {
		ctxDone = c.lifetimeCtx.Done()
	}

	for {
		select {
		case res := <-c.readChan:
			if res.err != nil {
				c.failf("read failed while waiting for %s: %v", waitingFor, res.err)
				return nil
			}
			c.recordTranscript("<-", string(res.body))
			var msg map[string]json.RawMessage
			if err := json.Unmarshal(res.body, &msg); err != nil {
				c.failf("unmarshal frame failed while waiting for %s: %v, raw: %q", waitingFor, err, res.body)
				return nil
			}
			// Directory scan logs are asynchronous and remain in the transcript;
			// they are not responses to the feature request being asserted.
			if string(msg["method"]) == `"window/logMessage"` {
				continue
			}
			return msg
		case <-timer:
			c.failf("timed out after %v waiting for %s", d, waitingFor)
			return nil
		case <-ctxDone:
			c.failf("subprocess lifetime context canceled while waiting for %s: %v", waitingFor, c.lifetimeCtx.Err())
			return nil
		}
	}
}

func writeJSON(t testHelper, target any, body string) {
	t.Helper()
	if c, ok := target.(*testClient); ok {
		c.write(body)
		return
	}
	if w, ok := target.(*jsonrpc.Writer); ok {
		if err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("unexpected target for writeJSON: %T", target)
}

func readJSON(t testHelper, target any) map[string]json.RawMessage {
	t.Helper()
	if c, ok := target.(*testClient); ok {
		return c.readTimeout(c.defaultTimeout, "next message")
	}
	if r, ok := target.(*jsonrpc.Reader); ok {
		body, err := r.Read()
		if err != nil {
			if err == io.EOF {
				t.Fatal("unexpected server EOF")
			}
			t.Fatal(err)
		}
		var message map[string]json.RawMessage
		if err := json.Unmarshal(body, &message); err != nil {
			t.Fatal(err)
		}
		return message
	}
	t.Fatalf("unexpected target for readJSON: %T", target)
	return nil
}

func completionItemJSON(t testHelper, response map[string]json.RawMessage, label string) string {
	t.Helper()
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(response["result"], &list); err != nil {
		t.Fatalf("unmarshal completion list: %v", err)
	}
	for _, raw := range list.Items {
		var item struct {
			Label string `json:"label"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatalf("unmarshal completion item: %v", err)
		}
		if item.Label != label {
			continue
		}
		return string(raw)
	}
	t.Fatalf("completion item %q not found", label)
	return ""
}

func readResponse(t testHelper, target any, id string) map[string]json.RawMessage {
	t.Helper()
	if c, ok := target.(*testClient); ok {
		deadline := time.Now().Add(c.defaultTimeout)
		for {
			rem := time.Until(deadline)
			if rem <= 0 {
				c.failf("timed out waiting for response id=%s", id)
				return nil
			}
			message := c.readTimeout(rem, fmt.Sprintf("response id=%s", id))
			if string(message["id"]) == id {
				return message
			}
			if _, notification := message["method"]; !notification {
				c.failf("unexpected response while waiting for %s: %s", id, message)
			}
		}
	}
	r := target.(*jsonrpc.Reader)
	for {
		message := readJSON(t, r)
		if string(message["id"]) == id {
			return message
		}
		if _, notification := message["method"]; !notification {
			t.Fatalf("unexpected response while waiting for %s: %s", id, message)
		}
	}
}

func readPullResponse(t testHelper, target any, writer any, _ *safeBuffer, id string) map[string]json.RawMessage {
	t.Helper()
	if c, ok := target.(*testClient); ok {
		deadline := time.Now().Add(c.defaultTimeout)
		for {
			rem := time.Until(deadline)
			if rem <= 0 {
				c.failf("timed out waiting for pull response id=%s", id)
				return nil
			}
			message := c.readTimeout(rem, fmt.Sprintf("pull response id=%s", id))
			if string(message["id"]) == id {
				return message
			}
			switch string(message["method"]) {
			case `"workspace/diagnostic/refresh"`:
				writeJSON(t, c, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":null}`, message["id"]))
			case `"textDocument/publishDiagnostics"`:
				c.failf("pull client received push diagnostics: %s", message)
			case "":
				c.failf("unexpected response while waiting for %s: %s", id, message)
			}
		}
	}
	r := target.(*jsonrpc.Reader)
	for {
		body, err := r.Read()
		if err != nil {
			t.Fatalf("read response %s: %v", id, err)
		}
		var message map[string]json.RawMessage
		if err := json.Unmarshal(body, &message); err != nil {
			t.Fatalf("decode response %s: %v, body: %q", id, err, body)
		}
		if string(message["id"]) == id {
			return message
		}
		switch string(message["method"]) {
		case `"workspace/diagnostic/refresh"`:
			if w, ok := writer.(*jsonrpc.Writer); ok {
				writeJSON(t, w, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":null}`, message["id"]))
			}
		case `"textDocument/publishDiagnostics"`:
			t.Fatalf("pull client received push diagnostics: %s", message)
		case "":
			t.Fatalf("unexpected response while waiting for %s: %s", id, message)
		}
	}
}

func firstRawArrayItem(t testHelper, raw json.RawMessage) json.RawMessage {
	t.Helper()
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatalf("empty array result: %s", raw)
	}
	return items[0]
}

func readPublishedDiagnostic(t testHelper, target any, documentURI, code string) {
	t.Helper()
	if c, ok := target.(*testClient); ok {
		deadline := time.Now().Add(c.defaultTimeout)
		for {
			rem := time.Until(deadline)
			if rem <= 0 {
				c.failf("timed out waiting for diagnostic uri=%s code=%s", documentURI, code)
				return
			}
			message := c.readTimeout(rem, fmt.Sprintf("diagnostic uri=%s code=%s", documentURI, code))
			if string(message["method"]) != `"textDocument/publishDiagnostics"` {
				if _, response := message["id"]; response {
					c.failf("unexpected response while waiting for diagnostics: %s", message)
				}
				continue
			}
			payload := string(message["params"])
			if !strings.Contains(payload, fmt.Sprintf(`"uri":%q`, documentURI)) {
				continue
			}
			if code == "" {
				if strings.Contains(payload, `"diagnostics":[]`) {
					return
				}
			} else if strings.Contains(payload, fmt.Sprintf(`"code":%q`, code)) {
				return
			}
		}
	}
	r := target.(*jsonrpc.Reader)
	for {
		message := readJSON(t, r)
		if string(message["method"]) != `"textDocument/publishDiagnostics"` {
			if _, response := message["id"]; response {
				t.Fatalf("unexpected response while waiting for diagnostics: %s", message)
			}
			continue
		}
		payload := string(message["params"])
		if strings.Contains(payload, fmt.Sprintf(`"uri":%q`, documentURI)) && strings.Contains(payload, fmt.Sprintf(`"code":%q`, code)) {
			return
		}
	}
}

func runSharedEditingScenario(t *testing.T, client *testClient, workspaceRoot string) {
	t.Helper()
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{},"rootUri":%q,"initializationOptions":{"runtimepath":[]}}}`, uri.File(workspaceRoot)))
	initResp := readResponse(t, client, "1")
	if string(initResp["id"]) != "1" {
		t.Fatalf("initialize response = %s", initResp)
	}
	writeJSON(t, client, `{"jsonrpc":"2.0","method":"initialized","params":{}}`)

	docPath := filepath.Join(workspaceRoot, "shared.vim")
	docURI := uri.File(docPath).String()
	if err := os.WriteFile(docPath, []byte("vim9script\nvar diskOnly = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%q,"languageId":"vim","version":1,"text":"vim9script\nvar sharedVal: number = 'err'\n"}}}`, docURI))
	readPublishedDiagnostic(t, client, docURI, "vim/E1012")

	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":%q,"version":2},"contentChanges":[{"text":"vim9script\nvar sharedVal: number = 42\necho sharedVal\n"}]}}`, docURI))
	readPublishedDiagnostic(t, client, docURI, "")

	// UTF-16 positions after an astral character and a combining sequence must
	// survive navigation, rename and a ranged edit of the unsaved disk overlay.
	unicodeSource := "vim9script\nvar sharedVal = 42\necho '😀é' .. sharedVal\n"
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":%q,"version":3},"contentChanges":[{"text":%q}]}}`, docURI, unicodeSource))
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":30,"method":"textDocument/definition","params":{"textDocument":{"uri":%q},"position":{"line":2,"character":16}}}`, docURI))
	definition := readResponse(t, client, "30")
	var locations []protocol.Location
	if json.Unmarshal(definition["result"], &locations) != nil || len(locations) != 1 || locations[0].Range.Start.Line != 1 || locations[0].Range.Start.Character != 4 {
		t.Fatalf("Unicode overlay definition = %s", definition)
	}
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":31,"method":"textDocument/rename","params":{"textDocument":{"uri":%q},"position":{"line":2,"character":16},"newName":"renamed"}}`, docURI))
	rename := readResponse(t, client, "31")
	var edit struct {
		Changes map[string][]protocol.TextEdit `json:"changes"`
	}
	if json.Unmarshal(rename["result"], &edit) != nil || len(edit.Changes) != 1 || len(edit.Changes[docURI]) != 2 {
		t.Fatalf("Unicode overlay rename = %s", rename)
	}
	for _, change := range edit.Changes[docURI] {
		wantStart := uint32(4)
		if change.Range.Start.Line == 2 {
			wantStart = 15
		} else if change.Range.Start.Line != 1 {
			t.Fatalf("unexpected rename line: %#v", change)
		}
		if change.NewText != "renamed" || change.Range.Start.Character != wantStart || change.Range.End.Character != wantStart+9 {
			t.Fatalf("Unicode rename range = %#v", change)
		}
	}
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":%q,"version":4},"contentChanges":[{"range":{"start":{"line":2,"character":6},"end":{"line":2,"character":10}},"text":"x"}]}}`, docURI))
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":32,"method":"textDocument/references","params":{"textDocument":{"uri":%q},"position":{"line":2,"character":13},"context":{"includeDeclaration":false}}}`, docURI))
	references := readResponse(t, client, "32")
	if json.Unmarshal(references["result"], &locations) != nil || len(locations) != 1 || locations[0].Range.Start.Line != 2 || locations[0].Range.Start.Character != 12 || locations[0].Range.End.Character != 21 {
		t.Fatalf("Unicode ranged edit references = %s", references)
	}

	writeJSON(t, client, `{"jsonrpc":"2.0","id":2,"method":"shutdown"}`)
	shutdown := readResponse(t, client, "2")
	if string(shutdown["id"]) != "2" {
		t.Fatalf("shutdown response = %s", shutdown)
	}
	writeJSON(t, client, `{"jsonrpc":"2.0","method":"exit"}`)
}

type fakeT struct {
	failed  bool
	failMsg string
}

func (f *fakeT) Helper() {}

func (f *fakeT) Fatal(args ...any) {
	f.failed = true
	f.failMsg = fmt.Sprint(args...)
}

func (f *fakeT) Fatalf(format string, args ...any) {
	f.failed = true
	f.failMsg = fmt.Sprintf(format, args...)
}

func TestSubprocessTimeoutTranscript(t *testing.T) {
	ctx, cancel := subprocessContext(t, 10*time.Second)
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
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()

	mock := &fakeT{}
	client := newTestClient(mock, stdout, stdin, &stderr, ctx)
	client.defaultTimeout = 50 * time.Millisecond

	writeJSON(mock, client, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`)
	_ = readResponse(mock, client, "1")

	// Trigger real subprocess stderr output by sending a didSave for an unopened file
	writeJSON(mock, client, `{"jsonrpc":"2.0","method":"textDocument/didSave","params":{"textDocument":{"uri":"file:///unopened_sentinel.vim"}}}`)

	sentinel := "vimls: ignored save for file:///unopened_sentinel.vim"
	sentinelDeadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(stderr.String(), sentinel) {
		if time.Now().After(sentinelDeadline) {
			t.Fatalf("timed out waiting for subprocess stderr sentinel %q, got: %s", sentinel, stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for a non-existent response ID 99999
	_ = readResponse(mock, client, "99999")
	if !mock.failed {
		t.Fatal("expected testClient to fail on timeout")
	}
	if !strings.Contains(mock.failMsg, "response id=99999") {
		t.Fatalf("expected failure to mention response id=99999, got: %s", mock.failMsg)
	}
	if !strings.Contains(mock.failMsg, "RECENT TRANSCRIPT") || !strings.Contains(mock.failMsg, "initialize") {
		t.Fatalf("expected failure to contain recent transcript, got: %s", mock.failMsg)
	}
	if !strings.Contains(mock.failMsg, "SERVER STDERR") {
		t.Fatalf("expected failure to contain server stderr, got: %s", mock.failMsg)
	}
	if !strings.Contains(mock.failMsg, "vimls: ignored save for file:///unopened_sentinel.vim") {
		t.Fatalf("expected failure to contain real subprocess stderr sentinel, got: %s", mock.failMsg)
	}
}

func TestSafeBufferConcurrentReadWrite(t *testing.T) {
	var buf safeBuffer
	const workers = 8
	const iterations = 500

	var wg sync.WaitGroup
	for workerID := range workers {
		wg.Go(func() {
			for j := range iterations {
				_, _ = fmt.Fprintf(&buf, "w%d-line%d\n", workerID, j)
			}
		})
	}

	for range workers {
		wg.Go(func() {
			for range iterations {
				_ = buf.Len()
				_ = buf.String()
			}
		})
	}

	wg.Wait()
	if buf.Len() == 0 {
		t.Fatal("expected non-empty buffer after concurrent writes")
	}
}

func TestTestClientSubphaseTimeoutDoesNotConsumeLifetime(t *testing.T) {
	lifetimeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	serverR, clientW := net.Pipe()
	clientR, _ := net.Pipe()
	t.Cleanup(func() {
		_ = serverR.Close()
		_ = clientW.Close()
		_ = clientR.Close()
	})

	var stderr safeBuffer
	mock := &fakeT{}
	client := newTestClient(mock, clientR, clientW, &stderr, lifetimeCtx)

	// Trigger a 30ms subphase read timeout
	msg := client.readTimeout(30*time.Millisecond, "stalled-message")
	if msg != nil {
		t.Fatalf("expected nil message, got %#v", msg)
	}
	if !mock.failed {
		t.Fatal("expected mock test helper to fail on subphase timeout")
	}
	if !strings.Contains(mock.failMsg, "timed out after 30ms waiting for stalled-message") {
		t.Fatalf("unexpected failure message: %s", mock.failMsg)
	}
	// Verify that the lifetime context is still intact and not canceled!
	if lifetimeCtx.Err() != nil {
		t.Fatalf("expected lifetime context to be active, got: %v", lifetimeCtx.Err())
	}
}
