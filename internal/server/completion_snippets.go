package server

import (
	"github.com/neoclide/vimls-go/internal/syntax"
	"go.lsp.dev/protocol"
)

// completionSnippet describes a textDocument completion item whose insertText
// is an LSP snippet template.
type completionSnippet struct {
	label      string
	insertText string
}

// legacyCompletionSnippets mirrors the snippet completion templates shipped by
// vim-language-server (src/server/snippets.ts). Labels that exactly name a
// built-in Ex command (such as "if") are handled by commandBlockSnippet so the
// completion list does not contain duplicate labels.
func legacyCompletionSnippets(configFile bool) []completionSnippet {
	command := "command! ${1:attr} ${2:cmd} ${3:rep} $0"
	if configFile {
		command = "command ${1:attr} ${2:cmd} ${3:rep} $0"
	}
	return []completionSnippet{
		{label: "func", insertText: "function ${1:Name}(${2}) ${3:abort}\n\t$0\nendfunction"},
		{label: "tryc", insertText: "try\n\t${1}\ncatch /.*/\n\t$0\nendtry"},
		{label: "tryf", insertText: "try\n\t${1}\nfinally\n\t$0\nendtry"},
		{label: "trycf", insertText: "try\n\t${1}\ncatch /.*/\n\t${2}\nfinally\n\t$0\nendtry"},
		{label: "aug", insertText: "augroup ${1:Start}\n\tautocmd!\n\t$0\naugroup END"},
		{label: "aut", insertText: "autocmd ${1:group-event} ${2:pat} ${3:once} ${4:nested} ${5:cmd}"},
		{label: "cmd", insertText: command},
		{label: "hi", insertText: "highlight ${1:default} ${2:group-name} ${3:args} $0"},
	}
}

// vim9CompletionSnippets are additional block templates for Vim9 script.
func vim9CompletionSnippets() []completionSnippet {
	return []completionSnippet{
		{label: "class", insertText: "class ${1:Name}\n\t$0\nendclass"},
		{label: "interface", insertText: "interface ${1:Name}\n\t$0\nendinterface"},
		{label: "enum", insertText: "enum ${1:Name}\n${2:Value}\nendenum"},
	}
}

// completionSnippetItems converts snippet templates to completion items. They
// are only useful to snippet-capable clients; otherwise the labels are not
// real Ex commands and selecting them would not insert useful text.
func completionSnippetItems(dialect syntax.Dialect, enabled, docsMarkdown, configFile bool) []protocol.CompletionItem {
	if !enabled {
		return nil
	}
	templates := legacyCompletionSnippets(configFile)
	if dialect == syntax.Vim9 {
		templates = append(templates, vim9CompletionSnippets()...)
	}
	items := make([]protocol.CompletionItem, 0, len(templates))
	for _, template := range templates {
		items = append(items, protocol.CompletionItem{
			Label:            template.label,
			Kind:             protocol.CompletionItemKindSnippet,
			InsertText:       protocol.NewOptional(template.insertText),
			InsertTextFormat: protocol.InsertTextFormatSnippet,
			Documentation:    completionDocumentation(docsMarkdown, "```vim\n"+template.insertText+"\n```"),
		})
	}
	return items
}

// configMappingSkeleton is the §7 P1 mapping skeleton for user configuration
// files. It is offered at the mapping LHS position, deliberately without
// <unique>, and leaves the key and the command payload as placeholders the
// user confirms.
func configMappingSkeleton(docsMarkdown bool) protocol.CompletionItem {
	const insertText = "<Leader>${1:key} ${2:<Cmd>call ${3:Function}()<CR>}"
	return protocol.CompletionItem{
		Label:            "<Leader>",
		Kind:             protocol.CompletionItemKindSnippet,
		InsertText:       protocol.NewOptional(insertText),
		InsertTextFormat: protocol.InsertTextFormatSnippet,
		Documentation:    completionDocumentation(docsMarkdown, "```vim\n"+insertText+"\n```"),
	}
}

// commandBlockSnippet returns a block snippet for an Ex command when snippet
// expansion is enabled and the command starts a block that benefits from a
// source template. In user configuration files the :function block omits the
// "!" (Vim v9.2.1015 silently replaces same-script functions on re-source, so
// a bang is neither needed nor safer there).
func commandBlockSnippet(name string, dialect syntax.Dialect, enabled, configFile bool) (string, bool) {
	if !enabled {
		return "", false
	}
	switch name {
	case "if":
		return "if ${1:condition}\n\t$0\nendif", true
	case "for":
		return "for ${1:item} in ${2:list}\n\t$0\nendfor", true
	case "while":
		return "while ${1:condition}\n\t$0\nendwhile", true
	case "try":
		return "try\n\t$1\ncatch /.*/\n\t$0\nendtry", true
	case "function":
		if dialect == syntax.Legacy {
			if configFile {
				return "function ${1:Name}()\n\t$0\nendfunction", true
			}
			return "function! ${1:Name}()\n\t$0\nendfunction", true
		}
	case "def":
		if dialect == syntax.Vim9 {
			return "def ${1:Name}()\n\t$0\nenddef", true
		}
	case "class", "interface", "enum":
		if dialect == syntax.Vim9 {
			for _, snippet := range vim9CompletionSnippets() {
				if snippet.label == name {
					return snippet.insertText, true
				}
			}
		}
	}
	return "", false
}
