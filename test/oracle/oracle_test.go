package oracle_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimdata"
)

const vimOracleTimeout = 5 * time.Second

const oracleDriver = `set nomore
let v:errors = []
let s:vim_patch = 'patch-' .. $VIMLS_VIM_VERSION
let s:vim_next_patch = 'patch-' .. $VIMLS_VIM_NEXT_PATCH
if v:version != str2nr($VIMLS_VIM_VVERSION) || !has(s:vim_patch) || has(s:vim_next_patch)
  call add(v:errors, 'expected exact Vim patch ' .. $VIMLS_VIM_TAG)
endif
if !has('eval') || exists(':vim9script') != 2
  call add(v:errors, 'required +eval/Vim9 support is missing')
endif
try
  execute 'source ' .. fnameescape($VIMLS_ORACLE_FIXTURE)
catch
  call add(v:errors, v:exception .. ' @ ' .. v:throwpoint)
endtry
let s:version = split(execute('version'), "\n")[0]
let s:messages = substitute(execute('messages'), "\n", '\\n', 'g')
call writefile([
      \ 'version=' .. s:version,
      \ 'v:version=' .. v:version,
      \ s:vim_patch .. '=' .. has(s:vim_patch),
      \ s:vim_next_patch .. '=' .. has(s:vim_next_patch),
      \ 'v:errors=' .. string(v:errors),
      \ 'unsupported_options=' .. string(get(g:, 'vimls_unsupported_options', [])),
      \ 'missing_features=' .. string(get(g:, 'vimls_missing_features', [])),
      \ 'messages=' .. s:messages,
      \ ], $VIMLS_ORACLE_OUTPUT)
if !empty(v:errors)
  cquit 1
endif
qa!
`

const formattingOracleDriver = `set nocompatible
set nomore
let v:errors = []
let s:vim_patch = 'patch-' .. $VIMLS_VIM_VERSION
let s:vim_next_patch = 'patch-' .. $VIMLS_VIM_NEXT_PATCH
if v:version != str2nr($VIMLS_VIM_VVERSION) || !has(s:vim_patch) || has(s:vim_next_patch)
  call add(v:errors, 'expected exact Vim patch ' .. $VIMLS_VIM_TAG)
endif
try
  filetype indent on
  execute 'edit ' .. fnameescape($VIMLS_FORMAT_INPUT)
  setlocal filetype=vim shiftwidth=4 tabstop=8 softtabstop=4 expandtab
  if &l:indentexpr ==# ''
    call add(v:errors, 'Vim indentexpr was not installed')
  else
    silent normal! gg=G
    execute 'silent write! ' .. fnameescape($VIMLS_FORMAT_OUTPUT)
  endif
catch
  call add(v:errors, v:exception .. ' @ ' .. v:throwpoint)
endtry
call writefile([
      \ 'version=' .. split(execute('version'), "\n")[0],
      \ 'v:version=' .. v:version,
      \ s:vim_patch .. '=' .. has(s:vim_patch),
      \ s:vim_next_patch .. '=' .. has(s:vim_next_patch),
      \ 'v:errors=' .. string(v:errors),
      \ 'messages=' .. substitute(execute('messages'), "\n", '\\n', 'g'),
      \ ], $VIMLS_ORACLE_OUTPUT)
if !empty(v:errors)
  cquit 1
endif
qa!
`

func TestPinnedVimOracle(t *testing.T) {
	vim := os.Getenv("VIM_EXECUTABLE")
	if vim == "" {
		t.Skip("set VIM_EXECUTABLE to the pinned Vim binary")
	}
	vim, err := filepath.Abs(vim)
	if err != nil {
		t.Fatal(err)
	}
	fixtures, err := filepath.Glob(filepath.Join("testdata", "*.vim"))
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("oracle fixtures: %v, count=%d", err, len(fixtures))
	}
	fixtures = append(fixtures, filepath.Join("..", "..", "internal", "vimdata", "options_set_generated.vim"))
	for _, fixture := range fixtures {
		t.Run(strings.TrimSuffix(filepath.Base(fixture), ".vim"), func(t *testing.T) {
			fixturePath, err := filepath.Abs(fixture)
			if err != nil {
				t.Fatal(err)
			}
			temporary := t.TempDir()
			driver := filepath.Join(temporary, "driver.vim")
			recordPath := filepath.Join(temporary, "oracle.txt")
			if err := os.WriteFile(driver, []byte(oracleDriver), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), vimOracleTimeout)
			defer cancel()
			command := exec.CommandContext(ctx, vim, "-Nu", "NONE", "-U", "NONE", "-n", "-es", "-X", "-i", "NONE", "-S", driver)
			command.Dir = temporary
			command.Env = append(append(os.Environ(), vimOracleEnvironment()...), "VIMLS_ORACLE_FIXTURE="+fixturePath, "VIMLS_ORACLE_OUTPUT="+recordPath)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			runErr := command.Run()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatalf("Vim oracle fixture %s timed out after %s; stdout=%q stderr=%q", filepath.Base(fixturePath), vimOracleTimeout, stdout.String(), stderr.String())
			}
			exitStatus := 0
			if runErr != nil {
				var exitError *exec.ExitError
				if !errors.As(runErr, &exitError) {
					t.Fatal(runErr)
				}
				exitStatus = exitError.ExitCode()
			}
			record, readErr := os.ReadFile(recordPath)
			t.Logf("fixture=%s exit_status=%d stdout=%q stderr=%q\n%s", filepath.Base(fixturePath), exitStatus, stdout.String(), stderr.String(), record)
			if readErr != nil {
				t.Fatalf("read oracle record: %v", readErr)
			}
			for _, want := range vimOracleEvidence() {
				if !strings.Contains(string(record), want+"\n") {
					t.Errorf("record does not contain %q", want)
				}
			}
			if exitStatus != 0 {
				t.Errorf("Vim exited with status %d: %v", exitStatus, runErr)
			}
		})
	}
}

func TestPinnedVimFormattingOracle(t *testing.T) {
	vim := os.Getenv("VIM_EXECUTABLE")
	if vim == "" {
		t.Skip("set VIM_EXECUTABLE to the pinned Vim binary")
	}
	var err error
	vim, err = filepath.Abs(vim)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "Legacy owned lines",
			source: "function! Some()\n" +
				"let value = 1\n" +
				"if value\n" +
				"echo value\n" +
				"else\n" +
				"echo 0\n" +
				"endif\n" +
				"endfunction\n" +
				"let text =\n" +
				"\\ 'one' .\n" +
				"\\ 'two'\n",
		},
		{
			name: "Vim9 owned lines",
			source: "vim9script\n" +
				"def Some(\n" +
				"value: number\n" +
				"): number\n" +
				"if value > 0\n" +
				"var items = [\n" +
				"value,\n" +
				"]\n" +
				"var result = value\n" +
				"+ items[0]\n" +
				"return result\n" +
				"endif\n" +
				"return 0\n" +
				"enddef\n" +
				"var config = {\n" +
				"active: {\n" +
				"items: [\n" +
				"1,\n" +
				"],\n" +
				"},\n" +
				"}\n" +
				"var nested = Outer(\n" +
				"Inner(\n" +
				"1\n" +
				")\n" +
				")\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := runFormattingOracle(t, vim, test.source)
			got := test.source
			edits := syntax.IndentEdits(syntax.Parse(got), syntax.IndentOptions{TabSize: 4, InsertSpaces: true})
			for _, edit := range slices.Backward(edits) {
				got = got[:edit.Span.Start] + edit.NewText + got[edit.Span.End:]
			}
			if got != want {
				t.Fatalf("vimls-go formatting:\n%s\nVim %s formatting:\n%s", got, vimdata.VimSourceTag, want)
			}
		})
	}
}

func runFormattingOracle(t *testing.T, vim, source string) string {
	t.Helper()
	temporary := t.TempDir()
	input := filepath.Join(temporary, "input.vim")
	output := filepath.Join(temporary, "formatted.vim")
	driver := filepath.Join(temporary, "driver.vim")
	recordPath := filepath.Join(temporary, "oracle.txt")
	if err := os.WriteFile(input, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(driver, []byte(formattingOracleDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), vimOracleTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, vim, "-Nu", "NONE", "-U", "NONE", "-n", "-es", "-X", "-i", "NONE", "-S", driver)
	command.Dir = temporary
	command.Env = append(append(os.Environ(), vimOracleEnvironment()...), "VIMLS_FORMAT_INPUT="+input, "VIMLS_FORMAT_OUTPUT="+output, "VIMLS_ORACLE_OUTPUT="+recordPath)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	runErr := command.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("Vim formatting oracle timed out after %s; stdout=%q stderr=%q", vimOracleTimeout, stdout.String(), stderr.String())
	}
	record, recordErr := os.ReadFile(recordPath)
	if recordErr != nil {
		t.Fatalf("read formatting oracle record: %v; run=%v stdout=%q stderr=%q", recordErr, runErr, stdout.String(), stderr.String())
	}
	exitStatus := 0
	if runErr != nil {
		var exitError *exec.ExitError
		if !errors.As(runErr, &exitError) {
			t.Fatal(runErr)
		}
		exitStatus = exitError.ExitCode()
	}
	t.Logf("exit_status=%d stdout=%q stderr=%q\n%s", exitStatus, stdout.String(), stderr.String(), record)
	if exitStatus != 0 {
		t.Fatalf("formatting oracle failed: %v\n%s", runErr, record)
	}
	for _, want := range vimOracleEvidence() {
		if !strings.Contains(string(record), want+"\n") {
			t.Fatalf("formatting oracle record lacks %q: %s", want, record)
		}
	}
	formatted, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	return string(formatted)
}

func vimOracleEnvironment() []string {
	version, next := oracleVimVersions()
	return []string{
		"VIMLS_VIM_TAG=" + vimdata.VimSourceTag,
		"VIMLS_VIM_VERSION=" + version.String(),
		"VIMLS_VIM_NEXT_PATCH=" + next.String(),
		"VIMLS_VIM_VVERSION=" + strconv.Itoa(version.Major*100+version.Minor),
	}
}

func vimOracleEvidence() []string {
	version, next := oracleVimVersions()
	return []string{
		"v:version=" + strconv.Itoa(version.Major*100+version.Minor),
		"patch-" + version.String() + "=1",
		"patch-" + next.String() + "=0",
		"v:errors=[]",
	}
}

func oracleVimVersions() (vimdata.VimVersion, vimdata.VimVersion) {
	version, _ := vimdata.ParseVimVersion(vimdata.VimSourceTag)
	next := version
	next.Patch++
	return version, next
}
