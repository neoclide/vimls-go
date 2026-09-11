package vimdata

import "strings"

// AutocmdEventDocumentation holds collected event help. Editor and Revision
// identify the source; Line is the one-based help tag definition line.
// Documentation is Markdown ready for hover or completion consumers.
type AutocmdEventDocumentation struct {
	Name, AliasOf    string
	Editor, Revision string
	Source, Tag      string
	Line             int
	Documentation    string
}

// AutocmdEventDocumentations returns a caller-owned list. Vim wins name
// collisions; Neovim-only events supplement the pinned Vim documentation.
func AutocmdEventDocumentations() []AutocmdEventDocumentation {
	return append([]AutocmdEventDocumentation(nil), autocmdEventDocumentation...)
}

// LookupAutocmdEventDocumentation matches event names case-insensitively.
// This documentation inventory does not change accepted syntax or completion.
func LookupAutocmdEventDocumentation(name string) (AutocmdEventDocumentation, bool) {
	i, ok := autocmdDocumentationIndex[strings.ToLower(name)]
	if !ok {
		return AutocmdEventDocumentation{}, false
	}
	return autocmdEventDocumentation[i], true
}

var autocmdDocumentationIndex = func() map[string]int {
	m := make(map[string]int, len(autocmdEventDocumentation))
	for i, e := range autocmdEventDocumentation {
		m[strings.ToLower(e.Name)] = i
	}
	return m
}()
