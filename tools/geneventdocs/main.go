// Command geneventdocs collects pinned Vim and Neovim event help into Go data.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/neoclide/vimls-go/internal/vimhelp"
)

const vimRevision = "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae"
const neovimRevision = "73923b0dd85bb936ba2f63ee916dabaa0603340d"

type entry struct {
	name, alias, editor, revision string
	documentation, source, tag    string
	line                          int
}

var vimEventRE = regexp.MustCompile(`KEYVALUE_ENTRY\(\s*-?EVENT_([A-Z0-9_]+)\s*,\s*"([A-Za-z][A-Za-z0-9]*)"\s*\)`)
var luaEventRE = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9]*)\s*=\s*(true|false),`)
var luaAliasRE = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9]*)\s*=\s*(?:'([A-Za-z][A-Za-z0-9]*)'|"([A-Za-z][A-Za-z0-9]*)"),`)
var helpTagRE = regexp.MustCompile(`\*[^*[:space:]]+\*`)
var exampleRE = regexp.MustCompile(`(?:^| )>[A-Za-z0-9_-]*$`)

func main() {
	vimRoot := flag.String("vim-root", "", "read-only Vim Git checkout containing v9.2.1015")
	nvimRoot := flag.String("neovim-root", "", "read-only Neovim Git checkout containing the pinned snapshot")
	output := flag.String("output", "internal/vimdata/autocmd_docs_generated.go", "generated Go file")
	flag.Parse()
	if *vimRoot == "" || *nvimRoot == "" || flag.NArg() != 0 {
		fatal(fmt.Errorf("require -vim-root and -neovim-root"))
	}
	vim, err := collect(*vimRoot, "Vim", vimRevision)
	if err != nil {
		fatal(err)
	}
	nvim, err := collect(*nvimRoot, "Neovim", neovimRevision)
	if err != nil {
		fatal(err)
	}
	merged := merge(vim, nvim)
	for _, e := range merged {
		if e.documentation == "" {
			fatal(fmt.Errorf("missing documentation for merged event %s", e.name))
		}
	}
	data, err := render(merged)
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "Vim: %d events; Neovim: %d events; merged: %d (Vim wins duplicates)\n", len(vim), len(nvim), len(merged))
}

func gitFile(root, revision, path string) ([]byte, error) {
	data, err := exec.Command("git", "-C", root, "show", revision+":"+path).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) != 0 {
			return nil, fmt.Errorf("read %s at %s: %w: %s", path, revision, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("read %s at %s: %w", path, revision, err)
	}
	return data, nil
}

func inventory(source []byte, editor string) ([]entry, error) {
	var entries []entry
	if editor == "Vim" {
		matches := vimEventRE.FindAllSubmatch(source, -1)
		canonical := map[string]string{}
		for _, m := range matches {
			if strings.EqualFold(string(m[1]), string(m[2])) {
				canonical[string(m[1])] = string(m[2])
			}
		}
		for _, m := range matches {
			e := entry{name: string(m[2])}
			if c := canonical[string(m[1])]; c != "" && c != e.name {
				e.alias = c
			}
			entries = append(entries, e)
		}
	} else {
		section := ""
		for _, line := range strings.Split(string(source), "\n") {
			switch strings.TrimSpace(line) {
			case "events = {":
				section = "events"
				continue
			case "aliases = {":
				section = "aliases"
				continue
			case "},":
				section = ""
				continue
			}
			if section == "events" {
				if m := luaEventRE.FindStringSubmatch(line); m != nil {
					entries = append(entries, entry{name: m[1]})
				}
			} else if section == "aliases" {
				if m := luaAliasRE.FindStringSubmatch(line); m != nil {
					alias := m[2]
					if alias == "" {
						alias = m[3]
					}
					entries = append(entries, entry{name: m[1], alias: alias})
				}
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("empty %s event inventory", editor)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.name] {
			return nil, fmt.Errorf("duplicate event %s", e.name)
		}
		seen[e.name] = true
	}
	for _, e := range entries {
		if e.alias != "" && !seen[e.alias] {
			return nil, fmt.Errorf("missing alias target %s for %s", e.alias, e.name)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries, nil
}

func tags(line string) []string {
	var result []string
	for _, span := range helpTagRE.FindAllStringIndex(line, -1) {
		if span[0] > 0 && line[span[0]-1] != ' ' && line[span[0]-1] != '\t' {
			continue
		}
		if span[1] < len(line) && line[span[1]] != ' ' && line[span[1]] != '\t' {
			continue
		}
		result = append(result, line[span[0]+1:span[1]-1])
	}
	return result
}

func extract(source []byte, path string, entries []entry) map[string]entry {
	lines := strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n")
	lineTags := make([][]string, len(lines))
	inExample := false
	for i, line := range lines {
		if inExample {
			if strings.TrimSpace(line) == "<" || (line != "" && strings.TrimLeft(line, " \t") == line) {
				inExample = false
			} else {
				continue
			}
		}
		lineTags[i] = tags(line)
		if exampleRE.MatchString(strings.TrimRight(line, " \t")) {
			inExample = true
		}
	}
	wanted := map[string]bool{}
	for _, e := range entries {
		wanted[e.name] = true
	}
	result := map[string]entry{}
	for i, ts := range lineTags {
		for _, tag := range ts {
			if !wanted[tag] {
				continue
			}
			start := i
			groupEnd := i
			// Bullet-defined events may share the following explanation/examples
			// (notably PackChangedPre and PackChanged).
			if strings.HasPrefix(strings.TrimSpace(lines[i]), "• ") {
				for start > 0 && len(lineTags[start-1]) > 0 && strings.HasPrefix(strings.TrimSpace(lines[start-1]), "• ") {
					start--
				}
				for groupEnd+1 < len(lines) && len(lineTags[groupEnd+1]) > 0 && strings.HasPrefix(strings.TrimSpace(lines[groupEnd+1]), "• ") {
					groupEnd++
				}
			}
			// Adjacent tag-only headings share a body; retain each tag's own line.
			for start < len(lines) && len(lineTags[start]) != 0 && strings.TrimSpace(helpTagRE.ReplaceAllString(lines[start], "")) == "" {
				start++
			}
			end := start
			for end < len(lines) {
				trimmed := strings.TrimSpace(lines[end])
				if end > start && end > groupEnd && len(lineTags[end]) > 0 {
					break
				}
				if len(trimmed) >= 12 && (strings.Trim(trimmed, "=") == "" || strings.Trim(trimmed, "-") == "") {
					break
				}
				if end > start && trimmed != "" && lines[end] == trimmed && strings.Trim(trimmed, "ABCDEFGHIJKLMNOPQRSTUVWXYZ ") == "" {
					break
				}
				end++
			}
			body := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
			result[tag] = entry{name: tag, source: path, tag: tag, line: i + 1, documentation: body}
		}
	}
	return result
}

func collect(root, editor, revision string) ([]entry, error) {
	path := "src/autocmd.c"
	helpPaths := []string{"runtime/doc/autocmd.txt"}
	if editor == "Neovim" {
		path = "src/nvim/auevents.lua"
		helpPaths = append(helpPaths, "runtime/doc/diagnostic.txt", "runtime/doc/lsp.txt", "runtime/doc/pack.txt", "runtime/doc/deprecated.txt")
	}
	source, err := gitFile(root, revision, path)
	if err != nil {
		return nil, err
	}
	entries, err := inventory(source, editor)
	if err != nil {
		return nil, err
	}
	docs := map[string]entry{}
	for _, help := range helpPaths {
		data, err := gitFile(root, revision, help)
		if err != nil {
			return nil, err
		}
		for name, doc := range extract(data, help, entries) {
			if _, exists := docs[name]; !exists {
				docs[name] = doc
			}
		}
	}
	for i := range entries {
		e := &entries[i]
		doc, ok := docs[e.name]
		if !ok && e.alias != "" {
			doc, ok = docs[e.alias]
		}
		if ok {
			e.documentation, e.source, e.tag, e.line = doc.documentation, doc.source, doc.tag, doc.line
		}
		e.editor, e.revision = editor, revision
		if e.documentation == "" {
			fmt.Fprintf(os.Stderr, "%s event %s has no help block\n", editor, e.name)
		}
	}
	return entries, nil
}

func merge(vim, nvim []entry) []entry {
	byName := map[string]entry{}
	for _, e := range nvim {
		byName[strings.ToLower(e.name)] = e
	}
	for _, e := range vim {
		byName[strings.ToLower(e.name)] = e
	}
	result := make([]entry, 0, len(byName))
	for _, e := range byName {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].name < result[j].name })
	return result
}

func render(entries []entry) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintln(&b, "// Code generated by tools/geneventdocs; DO NOT EDIT.")
	fmt.Fprintf(&b, "// Vim v9.2.1015: %s\n// Neovim snapshot: %s\n", vimRevision, neovimRevision)
	fmt.Fprintln(&b, "// Modified manual excerpts (OPL-1.0+); attribution and modification details: LICENSES/VIM-DOC.txt.")
	fmt.Fprintln(&b, "package vimdata\n\nvar autocmdEventDocumentation = []AutocmdEventDocumentation{")
	for _, e := range entries {
		fmt.Fprintf(&b, "{Name:%q, AliasOf:%q, Editor:%q, Revision:%q, Source:%q, Tag:%q, Line:%d, Documentation:%q},\n", e.name, e.alias, e.editor, e.revision, e.source, e.tag, e.line, markdown(e.documentation))
	}
	fmt.Fprintln(&b, "}")
	return format.Source(b.Bytes())
}

func markdown(source string) string {
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "• ") {
			lines[i] = helpTagRE.ReplaceAllStringFunc(line, func(tag string) string { return "`" + strings.Trim(tag, "*") + "`" })
		}
	}
	return vimhelp.ToMarkdown(strings.Join(lines, "\n"))
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "geneventdocs:", err); os.Exit(1) }
