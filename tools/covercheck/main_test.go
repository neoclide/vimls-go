package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadProfile(t *testing.T) {
	path := writeProfile(t, "mode: atomic\nexample.go:1.1,2.1 2 0\nexample.go:1.1,2.1 2 1\nexample.go:3.1,4.1 3 0\n")
	covered, total, err := readProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if covered != 2 || total != 5 {
		t.Fatalf("covered/total = %d/%d", covered, total)
	}
}

func TestReadProfileRejectsInvalidInput(t *testing.T) {
	for _, profile := range []string{
		"",
		"not a profile\n",
		"mode: atomic\n",
		"mode: atomic\nbad row\n",
		"mode: atomic\nexample.go:1.1,2.1 nope 1\n",
		"mode: atomic\nexample.go:1.1,2.1 1 nope\n",
		"mode: atomic\nexample.go:1.1,2.1 1 0\nexample.go:1.1,2.1 2 1\n",
	} {
		path := writeProfile(t, profile)
		if _, _, err := readProfile(path); err == nil {
			t.Fatalf("readProfile(%q) succeeded", profile)
		}
	}
}

func TestReadProfileMissingFile(t *testing.T) {
	if _, _, err := readProfile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing profile succeeded")
	}
}

func writeProfile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "coverage.out")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
