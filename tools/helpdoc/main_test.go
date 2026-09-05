package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRuntimepathReport(t *testing.T) {
	root := t.TempDir()
	docDir := filepath.Join(root, "doc")
	if err := os.MkdirAll(filepath.Join(docDir, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"one.txt":         "*g:enabled*\nEnable it.\n*Thing()*\nDo it.\n",
		"two.txt":         "*plugin#run*\nRun it.\n",
		"skip.jax":        "*g:translated*\nTranslation.\n",
		"nested/skip.txt": "*g:nested*\nNested.\n",
	} {
		if err := os.WriteFile(filepath.Join(docDir, name), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(docDir, "one.txt"), filepath.Join(docDir, "copy.txt")); err != nil {
		t.Fatal(err)
	}
	rtpFile := filepath.Join(root, "rtp.txt")
	if err := os.WriteFile(rtpFile, []byte(root+",\n"+alias+",\n"+filepath.Join(root, "missing")), 0644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "result.md")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-runtimepath-file", rtpFile, "-output", output, "-repeat", "2"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	var summary report
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Files != 2 || summary.Entries != 3 || len(summary.Roots) != 2 || len(summary.Runs) != 2 || len(summary.Warnings) != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "## `plugin#run`") || strings.Contains(string(data), "g:nested") || summary.OutputBytes != len(data) {
		t.Fatalf("report = %s", data)
	}
}

func TestRuntimeRootsEscapedComma(t *testing.T) {
	root := filepath.Join(t.TempDir(), "with,comma")
	roots, err := runtimeRoots(strings.ReplaceAll(root, ",", `\,`))
	if err != nil || len(roots) != 1 || roots[0] != root {
		t.Fatalf("roots = %v, %v", roots, err)
	}
}

func TestRunRejectsInvalidFlagsAndOutputFailure(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"-runtimepath", "/tmp", "-output", "/tmp/unused", "-repeat", "0"},
		{"-runtimepath", "/tmp", "-runtimepath-file", "x", "-output", "x"},
		{"-runtimepath", t.TempDir(), "-output", filepath.Join(t.TempDir(), "missing", "out.md"), "-repeat", "1"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code == 0 || stderr.Len() == 0 {
			t.Fatalf("args %v: code %d, stderr %s", args, code, &stderr)
		}
	}
}
