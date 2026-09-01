package oracle_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const oracleDriver = `set nomore
let v:errors = []
if v:version != 902 || !has('patch-9.2.1015') || has('patch-9.2.1016')
  call add(v:errors, 'expected exact Vim patch v9.2.1015')
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
      \ 'patch-9.2.1015=' .. has('patch-9.2.1015'),
      \ 'patch-9.2.1016=' .. has('patch-9.2.1016'),
      \ 'v:errors=' .. string(v:errors),
      \ 'messages=' .. s:messages,
      \ ], $VIMLS_ORACLE_OUTPUT)
if !empty(v:errors)
  cquit 1
endif
qa!
`

func TestPinnedVimOracle(t *testing.T) {
	vim := os.Getenv("VIM_EXECUTABLE")
	if vim == "" {
		t.Skip("set VIM_EXECUTABLE to the pinned Vim v9.2.1015 binary")
	}
	vim, err := filepath.Abs(vim)
	if err != nil {
		t.Fatal(err)
	}
	fixtures, err := filepath.Glob(filepath.Join("testdata", "*.vim"))
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("oracle fixtures: %v, count=%d", err, len(fixtures))
	}
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
			command := exec.Command(vim, "-Nu", "NONE", "-U", "NONE", "-n", "-es", "-X", "-i", "NONE", "-S", driver)
			command.Dir = temporary
			command.Env = append(os.Environ(), "VIMLS_ORACLE_FIXTURE="+fixturePath, "VIMLS_ORACLE_OUTPUT="+recordPath)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			runErr := command.Run()
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
			for _, want := range []string{"v:version=902", "patch-9.2.1015=1", "patch-9.2.1016=0", "v:errors=[]"} {
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
