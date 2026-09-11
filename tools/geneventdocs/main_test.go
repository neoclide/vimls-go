package main

import (
	"strings"
	"testing"
)

func TestInventoryCanonicalAliases(t *testing.T) {
	vim, err := inventory([]byte(`KEYVALUE_ENTRY(-EVENT_BUFREADPOST, "BufRead"),
KEYVALUE_ENTRY(-EVENT_BUFREADPOST, "BufReadPost"),`), "Vim")
	if err != nil || len(vim) != 2 || vim[0].alias != "BufReadPost" || vim[1].alias != "" {
		t.Fatalf("%+v %v", vim, err)
	}
	nvim, err := inventory([]byte(`events = {
  BufReadPost = true,
},
aliases = {
  BufRead = 'BufReadPost',
},
nvim_specific = {
  Other = true,
},`), "Neovim")
	if err != nil || len(nvim) != 2 || nvim[0].alias != "BufReadPost" {
		t.Fatalf("%+v %v", nvim, err)
	}
	if _, err := inventory(nil, "Vim"); err == nil {
		t.Fatal("accepted empty inventory")
	}
}

func TestExtractSharedInlineAndFinalBoundaries(t *testing.T) {
	source := []byte(" *Alpha* *Alias*\nShared body.\n *Beta*\nBeta body. >lua\n  local x = '*Fake*'\n<\nTail.\n==============\nUnrelated prose.\n• *Old* Never fired.\n\nKEYCODES\nmore prose\n")
	docs := extract(source, "help.txt", []entry{{name: "Alpha"}, {name: "Alias"}, {name: "Beta"}, {name: "Old"}})
	if docs["Alpha"].documentation != "Shared body." || docs["Alias"].documentation != docs["Alpha"].documentation || docs["Alpha"].line != 1 {
		t.Fatalf("%+v", docs)
	}
	if !strings.HasSuffix(docs["Beta"].documentation, "Tail.") || strings.Contains(docs["Beta"].documentation, "Unrelated") {
		t.Fatal(docs["Beta"])
	}
	if docs["Old"].documentation != "• *Old* Never fired." {
		t.Fatal(docs["Old"])
	}
}

func TestExtractAdjacentAliasHeadings(t *testing.T) {
	docs := extract([]byte("*Alpha*\n*Alias*\nShared body.\n*Next*\nNext body.\n"), "help.txt", []entry{{name: "Alpha"}, {name: "Alias"}, {name: "Next"}})
	if docs["Alpha"].documentation != "Shared body." || docs["Alias"].documentation != "Shared body." || docs["Alias"].line != 2 {
		t.Fatalf("%+v", docs)
	}
}

func TestMergeVimWinsIncludingAliases(t *testing.T) {
	vim := []entry{{name: "FileType", editor: "Vim", documentation: "vim"}, {name: "BufWrite", alias: "BufWritePre", editor: "Vim"}}
	nvim := []entry{{name: "FileType", editor: "Neovim", documentation: "nvim"}, {name: "LspAttach", editor: "Neovim"}, {name: "BufWrite", alias: "different"}}
	merged := merge(vim, nvim)
	if len(merged) != 3 || merged[0].alias != "BufWritePre" || merged[1].documentation != "vim" || merged[2].name != "LspAttach" {
		t.Fatalf("%+v", merged)
	}
}

func TestExtractBulletEventsShareExplanation(t *testing.T) {
	docs := extract([]byte("• *Before* - before\n• *After* - after\n\nShared data and examples.\n============\nUnrelated\n"), "pack.txt", []entry{{name: "Before"}, {name: "After"}})
	if docs["Before"].documentation != docs["After"].documentation || !strings.Contains(docs["Before"].documentation, "Shared data") {
		t.Fatalf("%+v", docs)
	}
	if !strings.Contains(markdown(docs["Before"].documentation), "`Before`") {
		t.Fatal("lost inline event name")
	}
}
