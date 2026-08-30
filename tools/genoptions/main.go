// Command genoptions generates Vim's option metadata.
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
	"strings"

	"github.com/chemzqm/vimls-go/tools/internal/vimhelp"
)

const (
	vimTag    = "v9.2.1015"
	vimCommit = "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae"
)

type option struct {
	Name                string
	ShortName           string
	Type                string
	Scope               string
	Documentation       string
	DocumentationSource string
}

var optionPattern = regexp.MustCompile(`(?m)^[ \t]*\{"([^"]+)"\s*,\s*(?:NULL|"([^"]*)")\s*,\s*([^,]+),`)
var pvDefinitionPattern = regexp.MustCompile(`(?m)^[ \t]*#[ \t]*define[ \t]+(PV_[A-Z0-9_]+)[ \t]+([^\r\n]+)`)
var pvTokenPattern = regexp.MustCompile(`\bPV_[A-Z0-9_]+\b`)
var termPattern = regexp.MustCompile(`(?m)^[ \t]*p_term\("([^"]+)"\s*,`)

func main() {
	root := flag.String("vim-root", os.Getenv("VIM_SOURCE"), "path to the official Vim Git checkout")
	output := flag.String("output", "internal/vimdata/options_generated.go", "generated Go file")
	flag.Parse()
	if *root == "" {
		fatalf("set -vim-root or VIM_SOURCE")
	}
	if err := verifyRevision(*root); err != nil {
		fatalf("%v", err)
	}
	source, err := readRevisionFile(*root, "src/optiondefs.h")
	if err != nil {
		fatalf("%v", err)
	}
	options, err := parseSource(source)
	if err != nil {
		fatalf("parse optiondefs.h: %v", err)
	}
	if len(options) != 469 {
		fatalf("found %d ordinary options, want 469", len(options))
	}
	terms, err := parseTerms(source)
	if err != nil {
		fatalf("parse terminal options: %v", err)
	}
	if len(terms) != 93 {
		fatalf("found %d p_term options, want 93", len(terms))
	}
	options = append(options, terms...)
	if err := validateOptions(options); err != nil {
		fatalf("validate options: %v", err)
	}
	sort.Slice(options, func(i, j int) bool { return options[i].Name < options[j].Name })
	if err := addDocumentation(*root, options); err != nil {
		fatalf("read option documentation: %v", err)
	}
	if err := writeOutput(*output, options); err != nil {
		fatalf("write output: %v", err)
	}
}

func verifyRevision(root string) error {
	out, err := exec.Command("git", "-C", root, "rev-list", "-n", "1", vimTag).Output()
	if err != nil || strings.TrimSpace(string(out)) != vimCommit {
		return fmt.Errorf("%s in %s does not resolve to pinned commit %s", vimTag, root, vimCommit)
	}
	return nil
}

func readRevisionFile(root, path string) ([]byte, error) {
	out, err := exec.Command("git", "-C", root, "show", vimTag+":"+path).Output()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}

func parseSource(source []byte) ([]option, error) {
	marker := bytes.Index(source, []byte("static struct vimoption options[]"))
	if marker < 0 {
		return nil, fmt.Errorf("options table not found")
	}
	tableEnd := bytes.Index(source[marker:], []byte("\n};"))
	if tableEnd < 0 {
		return nil, fmt.Errorf("options table terminator not found")
	}
	table := source[marker : marker+tableEnd]
	matches := optionPattern.FindAllSubmatchIndex(table, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("options table is empty")
	}
	pvDefinitions := make(map[string]string)
	for _, match := range pvDefinitionPattern.FindAllSubmatch(source[:marker], -1) {
		pvDefinitions[string(match[1])] = string(match[2])
	}
	result := make([]option, 0, len(matches))
	for index, match := range matches {
		name := string(table[match[2]:match[3]])
		short := ""
		if match[4] >= 0 {
			short = string(table[match[4]:match[5]])
		}
		typ, err := optionType(string(table[match[6]:match[7]]))
		if err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		entryEnd := len(table)
		if index+1 < len(matches) {
			entryEnd = matches[index+1][0]
		}
		scope, err := optionScope(table[match[0]:entryEnd], pvDefinitions)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		result = append(result, option{Name: name, ShortName: short, Type: typ, Scope: scope})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func optionType(flags string) (string, error) {
	result := ""
	for _, f := range strings.Split(flags, "|") {
		switch strings.TrimSpace(f) {
		case "P_BOOL":
			result = "OptionBool"
		case "P_NUM":
			result = "OptionNumber"
		case "P_STRING":
			result = "OptionString"
		}
	}
	if result == "" {
		return "", fmt.Errorf("missing option type in %s", flags)
	}
	return result, nil
}

func optionScope(entry []byte, definitions map[string]string) (string, error) {
	scopes := make(map[string]bool)
	for _, token := range pvTokenPattern.FindAll(entry, -1) {
		name := string(token)
		if name == "PV_NONE" {
			continue
		}
		body, ok := definitions[name]
		if !ok {
			return "", fmt.Errorf("scope macro %s is not defined", name)
		}
		scope := ""
		switch {
		case strings.Contains(body, "OPT_BOTH"):
			scope = "OptionGlobalLocal"
		case strings.Contains(body, "OPT_WIN"):
			scope = "OptionWindow"
		case strings.Contains(body, "OPT_BUF"):
			scope = "OptionBuffer"
		default:
			return "", fmt.Errorf("scope macro %s has unsupported definition %q", name, body)
		}
		scopes[scope] = true
	}
	if len(scopes) == 0 {
		return "OptionGlobal", nil
	}
	if len(scopes) != 1 {
		return "", fmt.Errorf("conflicting scopes %v", scopes)
	}
	for scope := range scopes {
		return scope, nil
	}
	panic("unreachable")
}

func parseTerms(source []byte) ([]option, error) {
	matches := termPattern.FindAllStringSubmatch(string(source), -1)
	result := make([]option, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		if seen[m[1]] {
			return nil, fmt.Errorf("duplicate terminal option %s", m[1])
		}
		seen[m[1]] = true
		result = append(result, option{Name: m[1], Type: "OptionString", Scope: "OptionGlobal"})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func validateOptions(options []option) error {
	canonical := make(map[string]bool, len(options))
	abbreviations := make(map[string]string)
	for _, option := range options {
		if option.Name == "" {
			return fmt.Errorf("empty canonical option name")
		}
		if canonical[option.Name] {
			return fmt.Errorf("duplicate canonical option %s", option.Name)
		}
		canonical[option.Name] = true
		if option.ShortName != "" {
			if previous := abbreviations[option.ShortName]; previous != "" {
				return fmt.Errorf("duplicate abbreviation %s for %s and %s", option.ShortName, previous, option.Name)
			}
			abbreviations[option.ShortName] = option.Name
		}
	}
	for abbreviation, name := range abbreviations {
		if canonical[abbreviation] && abbreviation != name {
			return fmt.Errorf("abbreviation %s for %s conflicts with a canonical option", abbreviation, name)
		}
	}
	return nil
}

func addDocumentation(root string, options []option) error {
	tagsSource, err := readRevisionFile(root, "runtime/doc/tags")
	if err != nil {
		return err
	}
	tagFiles, err := vimhelp.ParseTags(tagsSource)
	if err != nil {
		return err
	}
	files := map[string][]option{}
	for i := range options {
		file, ok := tagFiles["'"+options[i].Name+"'"]
		if !ok {
			return fmt.Errorf("documentation tag for %s is missing", options[i].Name)
		}
		files[file] = append(files[file], options[i])
	}
	filenames := make([]string, 0, len(files))
	for filename := range files {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	for _, file := range filenames {
		list := files[file]
		source, err := readRevisionFile(root, "runtime/doc/"+file)
		if err != nil {
			return err
		}
		tags := make([]string, 0, len(list))
		for _, o := range list {
			tags = append(tags, "'"+o.Name+"'")
		}
		docs, err := vimhelp.Extract(file, source, tags)
		if err != nil {
			return err
		}
		for i := range options {
			if tagFiles["'"+options[i].Name+"'"] != file {
				continue
			}
			d, ok := docs["'"+options[i].Name+"'"]
			if !ok || d.Markdown == "" || d.Source == "" {
				return fmt.Errorf("documentation for %s is empty", options[i].Name)
			}
			options[i].Documentation = d.Markdown
			options[i].DocumentationSource = d.Source
		}
	}
	return nil
}

func writeOutput(path string, options []option) error {
	options = append([]option(nil), options...)
	sort.Slice(options, func(i, j int) bool { return options[i].Name < options[j].Name })
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by tools/genoptions from Vim %s (%s); DO NOT EDIT.\n", vimTag, vimCommit)
	fmt.Fprintln(&b, "// Documentation is derived from Vim runtime help; see Vim's LICENSE.")
	fmt.Fprintf(&b, "package vimdata\n\nconst (\n\tOptionVimTag = %q\n\tOptionVimCommit = %q\n)\n\nvar builtinOptions = [...]Option{\n", vimTag, vimCommit)
	for _, o := range options {
		fmt.Fprintf(&b, "\t{Name: %q, ShortName: %q, Type: %s, Scope: %s, Documentation: %q, DocumentationSource: %q},\n", o.Name, o.ShortName, o.Type, o.Scope, o.Documentation, o.DocumentationSource)
	}
	b.WriteString("}\n")
	out, err := format.Source(b.Bytes())
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "genoptions: "+format+"\n", args...)
	os.Exit(1)
}
