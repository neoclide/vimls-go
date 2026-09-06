package oracle_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoclide/vimls-go/internal/syntax"
)

// LSP sees editor buffer lines. Force Vim's buffer fileformat here: :source
// of raw CR files on modern Unix is a separate, platform-dependent contract.
func TestPinnedVimBufferNewlines(t *testing.T) {
	vim := os.Getenv("VIM_EXECUTABLE")
	if vim == "" {
		t.Skip("set VIM_EXECUTABLE to the pinned Vim v9.2.1015 binary")
	}
	vim, err := filepath.Abs(vim)
	if err != nil {
		t.Fatal(err)
	}
	const source = "vim9script\nvar values = [\n  1,\n  2,\n]\nassert_equal([1, 2], values)\n"
	for _, test := range []struct{ format, newline string }{{"unix", "\n"}, {"dos", "\r\n"}, {"mac", "\r"}} {
		t.Run(test.format, func(t *testing.T) {
			temporary := t.TempDir()
			fixture := filepath.Join(temporary, "buffer.vim")
			driver := filepath.Join(temporary, "driver.vim")
			recordPath := filepath.Join(temporary, "record.txt")
			input := strings.ReplaceAll(source, "\n", test.newline)
			file := syntax.Parse(input)
			if file.Dialect != syntax.Vim9 || len(file.Diagnostics) != 0 {
				t.Fatalf("parser diagnostics=%#v dialect=%v", file.Diagnostics, file.Dialect)
			}
			if err := os.WriteFile(fixture, []byte(input), 0o600); err != nil {
				t.Fatal(err)
			}
			bufferDriver := strings.Replace(oracleDriver,
				"execute 'source ' .. fnameescape($VIMLS_ORACLE_FIXTURE)",
				"execute 'edit ++ff=' .. $VIMLS_ORACLE_FF .. ' ' .. fnameescape($VIMLS_ORACLE_FIXTURE)\n  source", 1)
			if err := os.WriteFile(driver, []byte(bufferDriver), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), vimOracleTimeout)
			defer cancel()
			command := exec.CommandContext(ctx, vim, "-Nu", "NONE", "-U", "NONE", "-n", "-es", "-X", "-i", "NONE", "-S", driver)
			command.Dir = temporary
			command.Env = append(os.Environ(), "VIMLS_ORACLE_FIXTURE="+fixture, "VIMLS_ORACLE_OUTPUT="+recordPath, "VIMLS_ORACLE_FF="+test.format)
			output, runErr := command.CombinedOutput()
			record, readErr := os.ReadFile(recordPath)
			exit := -1
			if command.ProcessState != nil {
				exit = command.ProcessState.ExitCode()
			}
			t.Logf("fileformat=%s exit_status=%d output=%q\n%s", test.format, exit, output, record)
			if runErr != nil || readErr != nil {
				t.Fatalf("oracle run=%v record=%v", runErr, readErr)
			}
			for _, want := range []string{"v:version=902", "patch-9.2.1015=1", "patch-9.2.1016=0", "v:errors=[]"} {
				if !strings.Contains(string(record), want+"\n") {
					t.Errorf("record lacks %q", want)
				}
			}
		})
	}
}
