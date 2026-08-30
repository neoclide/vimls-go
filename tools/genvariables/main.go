// Command genvariables generates Vim's predefined v: variable metadata.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chemzqm/vimls-go/tools/internal/vimhelp"
)

const (
	vimTag    = "v9.2.1015"
	vimCommit = "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae"
)

var variablePattern = regexp.MustCompile(`\{VV_NAME\("([^"]+)",\s*(VAR_[A-Z]+)\),\s*([^,]+),\s*([^}]+)\}`)

type variable struct {
	Name                string
	Type                string
	Flags               []string
	Documentation       string
	DocumentationSource string
}

func main() {
	vimRoot := flag.String("vim-root", "", "path to the official Vim Git checkout")
	output := flag.String("output", "internal/vimdata/variables_generated.go", "generated Go file")
	flag.Parse()
	if *vimRoot == "" {
		*vimRoot = os.Getenv("VIM_SOURCE")
	}
	if *vimRoot == "" {
		fatalf("set -vim-root or VIM_SOURCE")
	}
	if err := verifyRevision(*vimRoot); err != nil {
		fatalf("%v", err)
	}
	source, err := readRevisionFile(*vimRoot, "src/evalvars.c")
	if err != nil {
		fatalf("%v", err)
	}
	variables, err := parseSource(source)
	if err != nil {
		fatalf("parse evalvars.c: %v", err)
	}
	if len(variables) != 118 {
		fatalf("found %d v: variables, want 118", len(variables))
	}
	if err := addDocumentation(*vimRoot, variables); err != nil {
		fatalf("read v: variable documentation: %v", err)
	}
	if err := writeOutput(*output, variables); err != nil {
		fatalf("write output: %v", err)
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

func parseSource(source []byte) ([]variable, error) {
	start := bytes.Index(source, []byte("vimvars[VV_LEN] ="))
	if start < 0 {
		return nil, fmt.Errorf("vimvars table not found")
	}
	end := bytes.Index(source[start:], []byte("\n};"))
	if end < 0 {
		return nil, fmt.Errorf("vimvars table terminator not found")
	}
	matches := variablePattern.FindAllSubmatch(source[start:start+end], -1)
	variables := make([]variable, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, match := range matches {
		name := "v:" + string(match[1])
		if seen[name] {
			return nil, fmt.Errorf("duplicate variable %s", name)
		}
		seen[name] = true
		typ, err := variableType(string(match[2]), strings.TrimSpace(string(match[3])))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		flags, err := variableFlags(strings.TrimSpace(string(match[4])))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		variables = append(variables, variable{Name: name, Type: typ, Flags: flags})
	}
	if len(variables) == 0 {
		return nil, fmt.Errorf("vimvars table is empty")
	}
	return variables, nil
}

func variableType(kind, declared string) (string, error) {
	if declared != "NULL" {
		switch declared {
		case "&t_list_string":
			return "list<string>", nil
		case "&t_dict_string":
			return "dict<string>", nil
		case "&t_list_dict_any":
			return "list<dict<any>>", nil
		default:
			return "", fmt.Errorf("unsupported declared type %s", declared)
		}
	}
	switch kind {
	case "VAR_UNKNOWN":
		return "any", nil
	case "VAR_NUMBER":
		return "number", nil
	case "VAR_STRING":
		return "string", nil
	case "VAR_BOOL":
		return "bool", nil
	case "VAR_SPECIAL":
		return "special", nil
	case "VAR_LIST":
		return "list<any>", nil
	case "VAR_DICT":
		return "dict<any>", nil
	default:
		return "", fmt.Errorf("unsupported variable type %s", kind)
	}
}

func variableFlags(source string) ([]string, error) {
	if source == "0" {
		return nil, nil
	}
	var flags []string
	for _, flag := range strings.Split(source, "+") {
		switch strings.TrimSpace(flag) {
		case "VV_COMPAT":
			flags = append(flags, "VariableCompatible")
		case "VV_RO":
			flags = append(flags, "VariableReadOnly")
		case "VV_RO_SBX":
			flags = append(flags, "VariableSandboxReadOnly")
		default:
			return nil, fmt.Errorf("unsupported variable flags %s", source)
		}
	}
	return flags, nil
}

func addDocumentation(root string, variables []variable) error {
	source, err := readRevisionFile(root, "runtime/doc/eval.txt")
	if err != nil {
		return err
	}
	tags := make([]string, 0, len(variables))
	for _, variable := range variables {
		tags = append(tags, variable.Name)
	}
	docs, err := vimhelp.Extract("eval.txt", source, tags)
	if err != nil {
		return err
	}
	for index := range variables {
		doc := docs[variables[index].Name]
		if doc.Markdown == "" || doc.Source == "" {
			return fmt.Errorf("documentation for %s is empty", variables[index].Name)
		}
		variables[index].Documentation = doc.Markdown
		variables[index].DocumentationSource = doc.Source
	}
	return nil
}

func writeOutput(path string, variables []variable) error {
	var generated bytes.Buffer
	fmt.Fprintf(&generated, "// Code generated by tools/genvariables from Vim %s (%s); DO NOT EDIT.\n", vimTag, vimCommit)
	fmt.Fprintln(&generated, "// Documentation is derived from Vim runtime help; see Vim's LICENSE.")
	fmt.Fprintln(&generated, "package vimdata")
	fmt.Fprintln(&generated)
	fmt.Fprintf(&generated, "const (\n\tVariableVimTag = %q\n\tVariableVimCommit = %q\n)\n\n", vimTag, vimCommit)
	fmt.Fprintln(&generated, "var builtinVariables = [...]Variable{")
	for _, variable := range variables {
		flags := "0"
		if len(variable.Flags) > 0 {
			flags = strings.Join(variable.Flags, " | ")
		}
		fmt.Fprintf(&generated, "\t{Name: %q, Type: %q, Flags: %s, Documentation: %q, DocumentationSource: %q},\n", variable.Name, variable.Type, flags, variable.Documentation, variable.DocumentationSource)
	}
	fmt.Fprintln(&generated, "}")
	formatted, err := format.Source(generated.Bytes())
	if err != nil {
		return fmt.Errorf("format output: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	return os.WriteFile(path, formatted, 0o644)
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "genvariables: "+format+"\n", arguments...)
	os.Exit(1)
}
