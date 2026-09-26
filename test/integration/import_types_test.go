package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Exercise indexed closed importers and an unsaved leaf through the real LSP
// process. The importer itself never changes while its transitive type does.
func TestTransitiveAutoloadTypesSubprocess(t *testing.T) {
	root := t.TempDir()
	namedSource := `vim9script
import autoload './named_bridge.vim' as bridge
import './model.vim' as model
import './other.vim' as other
interface Accepts
  def Get(): model.Item
endinterface
class Local extends model.Child implements Accepts
  var held: model.Item = bridge.Value
  def Get(): bridge.Item
    return bridge.Value
  enddef
endclass
def Check()
  var same: model.Child = bridge.Value
  var base: model.Item = bridge.Value
  var alias: bridge.Item = bridge.Values[0]
  var iface: bridge.Named = bridge.Value
  var call: model.Child = bridge.Make()
  var enumAlias: bridge.State = bridge.Current
  var enumName: string = bridge.Current.name
  var enumIndex: number = bridge.Current.ordinal
  var enumValue: bridge.State = model.State.values[0]
  var enumDirect: bridge.State = model.State.Ready
  var enumAliasValue: model.State = bridge.State.Ready
  var constructedAlias: model.Item = bridge.Item.new()
  var nothing: model.Item = null_object
  var localBase: bridge.Named = Local.new()
  var [unpacked: model.Item] = bridge.Values
  var casted: model.Item = <model.Child>bridge.Value
  var Fn = (value: model.Child): model.Item => value
  var fnValue: model.Item = Fn(bridge.Value)
  for value: model.Item in bridge.Values
    echo value
  endfor
  var wrongNominal: other.Item = bridge.Value
  var wrongEnum: number = bridge.Current
  var WrongLambda = (): other.Item => bridge.Value
  var WrongPrimitiveLambda = (): string => 1
enddef
`
	for name, source := range map[string]string{
		"leaf.vim":    "vim9script\nexport var Value = 1\n",
		"bridge.vim":  "vim9script\nimport './leaf.vim'\nexport var Value = leaf.Value\n",
		"forward.vim": "vim9script\nimport './bridge.vim'\nexport var Value = bridge.Value\n",
		"main.vim":    "vim9script\nimport autoload './forward.vim'\nvar value: string = forward.Value\necho value\n",
		"model.vim": `vim9script
export interface Named
  def Label(): string
endinterface
export class Item implements Named
  var id = 7
  def Label(): string
    return 'item'
  enddef
endclass
export class Child extends Item
  var extra = true
endclass
export enum State
  Ready,
  Waiting
endenum
export var Value = Child.new()
export var Values = [Value]
export var Current = State.Ready
export def Make(): Child
  return Child.new()
enddef
`,
		"named_bridge.vim": `vim9script
import './model.vim' as model
export type Item = model.Item
export type Named = model.Named
export type State = model.State
export var Value = model.Value
export var Values = model.Values
export var Current = model.Current
export var Make = model.Make
`,
		"other.vim": `vim9script
export class Item
  var id = 7
endclass
`,
		"named_main.vim": namedSource,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mainURI := canonicalFileURI(t, filepath.Join(root, "main.vim")).String()
	leafURI := canonicalFileURI(t, filepath.Join(root, "leaf.vim")).String()
	namedURI := canonicalFileURI(t, filepath.Join(root, "named_main.vim")).String()
	wantTypeErrors := []int{}
	for line, source := range strings.Split(namedSource, "\n") {
		if strings.Contains(strings.ToLower(source), "var wrong") {
			wantTypeErrors = append(wantTypeErrors, line)
		}
	}
	ctx, cancel := subprocessContext(t, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, vimlsBinary)
	command.Dir = root
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
	writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"rootUri":%q,"capabilities":{"textDocument":{"diagnostic":{}}},"initializationOptions":{"runtimepath":[%q]}}}`, canonicalFileURI(t, root).String(), t.TempDir()))
	if response := readResponse(t, client, "1"); response["error"] != nil {
		t.Fatalf("initialize: %s", response)
	}
	writeJSON(t, client, `{"jsonrpc":"2.0","method":"initialized","params":{}}`)
	for step, mismatch := range []bool{true, false, true} {
		switch step {
		case 1:
			writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%q,"languageId":"vim","version":1,"text":"vim9script\nexport var Value = 'changed'\n"}}}`, leafURI))
		case 2:
			writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":%q,"version":2},"contentChanges":[{"text":"vim9script\nexport var Value = 2\n"}]}}`, leafURI))
		}
		id := fmt.Sprint(step + 2)
		writeJSON(t, client, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":"workspace/diagnostic","params":{"previousResultIds":[]}}`, id))
		response := readPullResponse(t, client, client, &stderr, id)
		var report struct {
			Items []struct {
				URI   string `json:"uri"`
				Items []struct {
					Code     string `json:"code"`
					Message  string `json:"message"`
					Severity int    `json:"severity"`
					Range    struct {
						Start struct {
							Line int `json:"line"`
						} `json:"start"`
					} `json:"range"`
				} `json:"items"`
			} `json:"items"`
		}
		if err := json.Unmarshal(response["result"], &report); err != nil {
			t.Fatalf("step %d: decode report: %v: %s", step, err, response)
		}
		found, gotMismatch := false, false
		namedFound := false
		namedTypeErrors := []int{}
		for _, item := range report.Items {
			if item.URI == mainURI {
				found = true
				for _, diagnostic := range item.Items {
					gotMismatch = gotMismatch || diagnostic.Code == "vim/E1012"
				}
			}
			if item.URI == namedURI {
				namedFound = true
				for _, diagnostic := range item.Items {
					if diagnostic.Severity == 1 && diagnostic.Code != "vim/E1012" {
						t.Fatalf("unexpected named-type error: %#v", diagnostic)
					}
					if diagnostic.Code == "vim/E1012" {
						namedTypeErrors = append(namedTypeErrors, diagnostic.Range.Start.Line)
					}
				}
			}
		}
		if !found || gotMismatch != mismatch {
			t.Fatalf("step %d: main found=%t, E1012=%t, want=%t: %s", step, found, gotMismatch, mismatch, response)
		}
		if !namedFound || !slices.Equal(namedTypeErrors, wantTypeErrors) {
			t.Fatalf("step %d: named type E1012 lines = %v, want %v: %s", step, namedTypeErrors, wantTypeErrors, response)
		}
	}
	writeJSON(t, client, `{"jsonrpc":"2.0","id":5,"method":"shutdown"}`)
	if response := readPullResponse(t, client, client, &stderr, "5"); string(response["result"]) != "null" {
		t.Fatalf("shutdown: %s", response)
	}
	writeJSON(t, client, `{"jsonrpc":"2.0","method":"exit"}`)
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	waitCommand(t, ctx, command, 5*time.Second, &stderr)
}
