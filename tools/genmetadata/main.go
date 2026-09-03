package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	vimTag    = "v9.2.1015"
	vimCommit = "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae"
)

func main() {
	vimRoot := flag.String("vim-root", os.Getenv("VIM_SOURCE"), "path to the official Vim Git checkout")
	outputDir := flag.String("output-dir", "internal/vimdata", "directory for generated Go files")
	flag.Parse()
	if *vimRoot == "" {
		fatalf("set -vim-root or VIM_SOURCE")
	}
	if err := verifyRevision(*vimRoot); err != nil {
		fatalf("%v", err)
	}

	if err := generateCommands(*vimRoot, filepath.Join(*outputDir, "commands_generated.go")); err != nil {
		fatalf("generate commands: %v", err)
	}
	if err := generateFunctions(*vimRoot, filepath.Join(*outputDir, "functions_generated.go")); err != nil {
		fatalf("generate functions: %v", err)
	}
	if err := generateOptions(*vimRoot, filepath.Join(*outputDir, "options_generated.go"), filepath.Join(*outputDir, "options_set_generated.vim")); err != nil {
		fatalf("generate options: %v", err)
	}
	if err := generateVariables(*vimRoot, filepath.Join(*outputDir, "variables_generated.go")); err != nil {
		fatalf("generate variables: %v", err)
	}
}

func verifyRevision(root string) error {
	resolved, err := exec.Command("git", "-C", root, "rev-list", "-n", "1", vimTag).Output()
	if err != nil || strings.TrimSpace(string(resolved)) != vimCommit {
		return fmt.Errorf("%s in %s does not resolve to pinned commit %s", vimTag, root, vimCommit)
	}
	return nil
}

func readRevisionFile(root, path string) ([]byte, error) {
	output, err := exec.Command("git", "-C", root, "show", vimTag+":"+path).Output()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return output, nil
}

func revisionFilesMatching(root, pattern string, pathspecs ...string) ([]string, error) {
	arguments := []string{"-C", root, "grep", "-l", "-E", pattern, vimTag, "--"}
	arguments = append(arguments, pathspecs...)
	output, err := exec.Command("git", arguments...).Output()
	if err != nil {
		return nil, fmt.Errorf("find files in %s: %w", vimTag, err)
	}
	prefix := vimTag + ":"
	lines := strings.Fields(string(output))
	for i := range lines {
		lines[i] = strings.TrimPrefix(lines[i], prefix)
	}
	return lines, nil
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "genmetadata: "+format+"\n", arguments...)
	os.Exit(1)
}
