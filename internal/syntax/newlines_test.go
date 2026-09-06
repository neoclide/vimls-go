package syntax

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestPhysicalNewlineFormsPreserveSyntax(t *testing.T) {
	fixtures := []string{
		"\ufeffvim9script\n# docs\nvar xs = [\n  1,\n  # comment\n  2,\n]\ndef F(\n  value: number\n): number\n  return value\nenddef\n",
		"let xs = [1,\n  \\ 2]\n\" comment\nautocmd BufEnter * echo 1\n  \\ | echo 2\n",
		"vim9script\ndef F()\n  var s =<< trim END\n  text\n  END\n  echo s\nenddef\n",
		"vim9script\nvar F = () => {\n  # comment\n  return 1\n}\necho F()\n",
		"vim9script\nvar F = () => { \"bad\n  return 1\n}\necho F()\n",
		"vim9script\nautocmd BufEnter * {\n  echo 1\n}\n",
		"append\ntext\n.\nlet x = 1\n",
		"if !has('vim9script')\n  finish\nendif\nvim9script\nvar x = 1\n",
		"vim9script\ndef Bad<T(\necho super.()\nvar x = [\n  1,\nendif\n",
		"vim9script\nvar x: list<\n # comment\n number> = []\necho x\n",
	}
	for i, source := range fixtures {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			want := normalizedNewlineTree(t, Parse(source))
			for _, endings := range [][]string{{"\n"}, {"\r\n"}, {"\r"}, {"\n", "\r\n", "\r"}} {
				var input strings.Builder
				line := 0
				for _, character := range source {
					if character == '\n' {
						input.WriteString(endings[line%len(endings)])
						line++
					} else {
						input.WriteRune(character)
					}
				}
				file := Parse(input.String())
				if file.Source != input.String() {
					t.Fatal("parser changed source bytes")
				}
				assertFileSpans(t, file)
				if got := normalizedNewlineTree(t, file); !reflect.DeepEqual(got, want) {
					gotJSON, _ := json.Marshal(got)
					wantJSON, _ := json.Marshal(want)
					t.Fatalf("endings=%q\ngot %s\nwant %s", endings, gotJSON, wantJSON)
				}
			}
		})
	}
}

// Compare the public AST and every byte span after mapping physical newline
// spellings to LF. A span inside a CRLF pair cannot be represented by LSP.
func normalizedNewlineTree(t *testing.T, file *File) any {
	t.Helper()
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	var normalize func(any) any
	normalize = func(value any) any {
		switch value := value.(type) {
		case string:
			return strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
		case []any:
			for i := range value {
				value[i] = normalize(value[i])
			}
		case map[string]any:
			if start, ok := value["Start"].(float64); ok && len(value) == 2 {
				if end, ok := value["End"].(float64); ok {
					for key, number := range map[string]float64{"Start": start, "End": end} {
						offset := int(number)
						if offset < 0 || offset > len(file.Source) {
							t.Fatalf("invalid span offset %d", offset)
						}
						if offset > 0 && offset < len(file.Source) && file.Source[offset-1] == '\r' && file.Source[offset] == '\n' {
							t.Fatalf("span ends inside CRLF at %d", offset)
						}
						value[key] = float64(len(strings.ReplaceAll(file.Source[:offset], "\r\n", "\n")))
					}
					return value
				}
			}
			for key, child := range value {
				value[key] = normalize(child)
			}
		}
		return value
	}
	return normalize(value)
}
