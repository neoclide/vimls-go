package syntax

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCuratedLegacyAndVim9SourceCorpus(t *testing.T) {
	tests := []struct {
		path        string
		dialect     Dialect
		minCommands int
		minBlocks   int
	}{
		{path: filepath.Join("..", "..", "testdata", "legacy", "basic.vim"), dialect: Legacy, minCommands: 20, minBlocks: 4},
		{path: filepath.Join("..", "..", "testdata", "vim9", "basic.vim"), dialect: Vim9, minCommands: 19, minBlocks: 5},
	}
	for _, test := range tests {
		t.Run(filepath.Base(filepath.Dir(test.path)), func(t *testing.T) {
			source, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			file := Parse(string(source))
			if file.Dialect != test.dialect || len(file.Commands) < test.minCommands || len(file.Blocks) < test.minBlocks {
				t.Fatalf("dialect = %s, commands = %d, blocks = %d", file.Dialect, len(file.Commands), len(file.Blocks))
			}
			if len(file.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %#v", file.Diagnostics)
			}
		})
	}
}

func FuzzFileParsersNeverPanic(f *testing.F) {
	f.Add("vim9script\nvar value = [1, 2]\n", true)
	f.Add("function! Test()\n  let x = {'a': 1}\nendfunction\n", false)
	f.Add("vim9script\ndef Broken(\n[[[[\n", true)
	f.Fuzz(func(t *testing.T, source string, vim9 bool) {
		var file *File
		if vim9 {
			file = (Vim9Parser{}).Parse(source)
		} else {
			file = (LegacyParser{}).Parse(source)
		}
		if file == nil || file.Source != source {
			t.Fatal("parser did not retain source")
		}
	})
}
