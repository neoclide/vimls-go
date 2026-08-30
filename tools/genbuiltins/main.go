// Command genbuiltins generates Vim's builtin function metadata.
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
	"sort"
	"strconv"
	"strings"
)

const (
	vimTag    = "v9.2.1015"
	vimCommit = "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae"
)

// A table row starts with a quoted name, arity fields, method-argument flag,
// and argument-check table. The return implementation may contain guarded C
// preprocessor branches, so only the stable prefix and ret_* token are read.
var functionStart = regexp.MustCompile(`^\s*\{"([^"\\]*(?:\\.[^"\\]*)*)"\s*,\s*([0-9]+)\s*,\s*([^,\s]+)\s*,\s*[^,]+,\s*([^,\s]+)\s*,`)
var returnHelper = regexp.MustCompile(`\b(ret_[A-Za-z0-9_]+)\b`)
var argumentArray = regexp.MustCompile(`(?s)static\s+argcheck_T\s+([A-Za-z0-9_]+)\[\]\s*=\s*\{([^}]*)\};`)
var cComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
var cIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type builtin struct {
	Name           string
	MinArgs        int
	MaxArgs        int
	ReturnType     string
	ArgumentChecks []string
}

func main() {
	vimRoot := flag.String("vim-root", "", "path to the official Vim Git checkout")
	output := flag.String("output", "internal/vimdata/functions_generated.go", "generated Go file")
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
	source, err := exec.Command("git", "-C", *vimRoot, "show", vimTag+":src/evalfunc.c").Output()
	if err != nil {
		fatalf("read evalfunc.c: %v", err)
	}
	functions, err := parseSource(source)
	if err != nil {
		fatalf("parse evalfunc.c: %v", err)
	}
	if len(functions) != 591 {
		fatalf("found %d builtin functions, want 591", len(functions))
	}
	if err := writeOutput(*output, functions); err != nil {
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

func parseSource(source []byte) ([]builtin, error) {
	argumentChecks, err := parseArgumentChecks(source)
	if err != nil {
		return nil, err
	}
	start := bytes.Index(source, []byte("static const funcentry_T global_functions[]"))
	if start < 0 {
		return nil, fmt.Errorf("global_functions table not found")
	}
	end := bytes.Index(source[start:], []byte("\n};"))
	if end < 0 {
		return nil, fmt.Errorf("global_functions table terminator not found")
	}
	end += start
	lines := strings.Split(string(source[start:end]), "\n")
	functions := make([]builtin, 0, 600)
	usedArgumentChecks := make(map[string]bool, len(argumentChecks))
	for index := 0; index < len(lines); index++ {
		match := functionStart.FindStringSubmatch(lines[index])
		if match == nil {
			continue
		}
		minArgs, err := strconv.Atoi(match[2])
		if err != nil {
			return nil, fmt.Errorf("%s min argc: %w", match[1], err)
		}
		maxArgs := -1
		if match[3] != "VARGS" {
			maxArgs, err = strconv.Atoi(match[3])
			if err != nil {
				return nil, fmt.Errorf("%s max argc: %w", match[1], err)
			}
		}
		// Rows are terminated by the next row or a preprocessor branch. The
		// return helper is the only ret_* token in a row in the Vim table.
		row := lines[index]
		for next := index + 1; next < len(lines); next++ {
			if functionStart.MatchString(lines[next]) {
				break
			}
			row += "\n" + lines[next]
			if strings.Contains(lines[next], "},") {
				break
			}
		}
		helper := returnHelper.FindStringSubmatch(row)
		returnType := "ReturnUnknown"
		if helper != nil {
			returnType = returnTypeName(helper[1])
		}
		var checks []string
		if match[4] != "NULL" {
			var ok bool
			checks, ok = argumentChecks[match[4]]
			if !ok {
				return nil, fmt.Errorf("%s argument check table %s not found", match[1], match[4])
			}
			usedArgumentChecks[match[4]] = true
			if maxArgs >= 0 && len(checks) != maxArgs {
				return nil, fmt.Errorf("%s argument check table %s has %d entries, want %d", match[1], match[4], len(checks), maxArgs)
			}
		}
		functions = append(functions, builtin{Name: match[1], MinArgs: minArgs, MaxArgs: maxArgs, ReturnType: returnType, ArgumentChecks: checks})
	}
	if len(functions) == 0 {
		return nil, fmt.Errorf("global_functions table is empty")
	}
	if len(usedArgumentChecks) != len(argumentChecks) {
		unused := make([]string, 0, len(argumentChecks)-len(usedArgumentChecks))
		for name := range argumentChecks {
			if !usedArgumentChecks[name] {
				unused = append(unused, name)
			}
		}
		sort.Strings(unused)
		return nil, fmt.Errorf("unused argument check tables: %s", strings.Join(unused, ", "))
	}
	sort.Slice(functions, func(i, j int) bool { return functions[i].Name < functions[j].Name })
	for i := 1; i < len(functions); i++ {
		if functions[i-1].Name == functions[i].Name {
			return nil, fmt.Errorf("duplicate builtin function %q", functions[i].Name)
		}
	}
	return functions, nil
}

func parseArgumentChecks(source []byte) (map[string][]string, error) {
	start := bytes.Index(source, []byte("Lists of functions that check the argument types"))
	if start < 0 {
		return nil, fmt.Errorf("argument check table section not found")
	}
	arraysStart := bytes.Index(source[start:], []byte("static argcheck_T"))
	if arraysStart < 0 {
		return nil, fmt.Errorf("argument check tables not found")
	}
	start += arraysStart
	end := bytes.Index(source[start:], []byte("static garray_T *current_type_gap"))
	if end < 0 {
		return nil, fmt.Errorf("argument check table section terminator not found")
	}
	section := cComment.ReplaceAllString(string(source[start:start+end]), "")
	checks := make(map[string][]string)
	for _, match := range argumentArray.FindAllStringSubmatch(section, -1) {
		name := match[1]
		body := match[2]
		var entries []string
		for _, entry := range strings.Split(body, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			if !cIdentifier.MatchString(entry) {
				return nil, fmt.Errorf("%s has unsupported argument checker %q", name, entry)
			}
			entries = append(entries, entry)
		}
		for index, entry := range entries {
			if entry == "NULL" && index != len(entries)-1 {
				return nil, fmt.Errorf("%s has non-terminal NULL argument checker", name)
			}
		}
		if len(entries) > 0 && entries[len(entries)-1] == "NULL" {
			entries = entries[:len(entries)-1]
		}
		if len(entries) == 0 {
			return nil, fmt.Errorf("%s argument check table is empty", name)
		}
		if _, duplicate := checks[name]; duplicate {
			return nil, fmt.Errorf("duplicate argument check table %s", name)
		}
		checks[name] = entries
	}
	if len(checks) == 0 {
		return nil, fmt.Errorf("argument check tables not found")
	}
	return checks, nil
}

func returnTypeName(helper string) string {
	switch helper {
	case "ret_any":
		return "ReturnAny"
	case "ret_void":
		return "ReturnVoid"
	case "ret_bool":
		return "ReturnBool"
	case "ret_number":
		return "ReturnNumber"
	case "ret_float":
		return "ReturnFloat"
	case "ret_string":
		return "ReturnString"
	case "ret_blob":
		return "ReturnBlob"
	case "ret_list_any", "ret_list_number", "ret_list_string", "ret_list_dict_any", "ret_list_items", "ret_list_string_items", "ret_list_regionpos":
		return "ReturnList"
	case "ret_dict_any", "ret_dict_number", "ret_dict_string":
		return "ReturnDict"
	case "ret_number_bool":
		return "ReturnNumberOrBool"
	case "ret_channel":
		return "ReturnChannel"
	case "ret_job":
		return "ReturnJob"
	case "ret_tuple_any":
		return "ReturnTuple"
	case "ret_func_any", "ret_func_unknown":
		return "ReturnFunction"
	default:
		return "ReturnUnknown"
	}
}

func writeOutput(path string, functions []builtin) error {
	var generated bytes.Buffer
	fmt.Fprintf(&generated, "// Code generated by tools/genbuiltins from Vim %s (%s); DO NOT EDIT.\n", vimTag, vimCommit)
	fmt.Fprintln(&generated, "package vimdata")
	fmt.Fprintln(&generated)
	fmt.Fprintf(&generated, "const (\n\tBuiltinVimTag = %q\n\tBuiltinVimCommit = %q\n)\n\n", vimTag, vimCommit)
	fmt.Fprintln(&generated, "var builtinFunctions = [...]BuiltinFunction{")
	for _, function := range functions {
		fmt.Fprintf(&generated, "\t{Name: %q, MinArgs: %d, MaxArgs: %d, ReturnType: %s", function.Name, function.MinArgs, function.MaxArgs, function.ReturnType)
		if len(function.ArgumentChecks) > 0 {
			fmt.Fprint(&generated, ", ArgumentChecks: []string{")
			for index, check := range function.ArgumentChecks {
				if index > 0 {
					fmt.Fprint(&generated, ", ")
				}
				fmt.Fprintf(&generated, "%q", check)
			}
			fmt.Fprint(&generated, "}")
		}
		fmt.Fprintln(&generated, "},")
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
	fmt.Fprintf(os.Stderr, "genbuiltins: "+format+"\n", arguments...)
	os.Exit(1)
}
