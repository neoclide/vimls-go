package vimhelp

import (
	"strings"
	"testing"
)

func TestExtractSymbolsBoundariesAliasesAndExamples(t *testing.T) {
	source := "Introduction *plugin*\r\nIntro text.\r\n" +
		"*g:plugin_enabled*\r\nEnable the plugin; see |PluginRun()|.\r\n" +
		"*PluginRun()* *plugin#run()*\r\nRun example: >vim\r\n" +
		"  echo '*g:fake*'\r\n  echo 'a|b'\r\n  <meta> *g:also_fake*\r\n<\r\n" +
		"*:PluginCommand*\r\nUnrelated command documentation.\r\n" +
		"*plugin#inner*\r\n*plugin#outer*\r\nShared description.\r\n" +
		"*<Plug>(plugin-run)*\r\nRun the mapping.\r\n" +
		"====================\r\nUnrelated footer.\r\n"
	docs := ExtractSymbols("/plugin/doc/test.txt", []byte(source))
	if len(docs) != 7 {
		t.Fatalf("entries = %#v", docs)
	}
	if docs[0].Name != "g:plugin_enabled" || docs[0].Line != 3 || docs[0].Kind != "global variable" || !strings.Contains(docs[0].Markdown, "`PluginRun()`") {
		t.Fatalf("variable = %#v", docs[0])
	}
	if docs[1].Kind != "global function" || docs[2].Kind != "autoload function" || docs[1].Markdown != docs[2].Markdown {
		t.Fatalf("functions = %#v", docs[1:3])
	}
	if !strings.Contains(docs[1].Markdown, "```vim\n  echo '*g:fake*'\n  echo 'a|b'\n  <meta> *g:also_fake*\n```") {
		t.Fatalf("example not preserved: %q", docs[1].Markdown)
	}
	if docs[3].Name != ":PluginCommand" || docs[3].Kind != "Ex command" || docs[3].Markdown != "Unrelated command documentation." {
		t.Fatalf("command = %#v", docs[3])
	}
	if docs[4].Markdown != "Shared description." || docs[5].Markdown != docs[4].Markdown {
		t.Fatalf("bare aliases = %#v", docs[4:])
	}
	if docs[6].Name != "<Plug>(plugin-run)" || docs[6].Kind != "plug mapping" || docs[6].Markdown != "Run the mapping." {
		t.Fatalf("plug mapping = %#v", docs[6])
	}
	for _, doc := range docs {
		if (doc.Name != ":PluginCommand" && strings.Contains(doc.Markdown, "Unrelated")) || doc.Source != "/plugin/doc/test.txt" {
			t.Fatalf("section/source = %#v", doc)
		}
	}
}

func TestExtractSymbolsClassificationAndRecovery(t *testing.T) {
	source := "*b:local*\nLocal.\n*s:Private()*\nPrivate.\n*EventName*\nEvent.\n" +
		"*<Plug>()*\nEmpty mapping.\n*<Plug>(bad name)*\nBad mapping.\n" +
		"*g:pattern_{name}*\nPattern.\n*len()*\nBuilt-in.\n" +
		"*g:Explicit()*\nExplicit global.\n*foo#bar()*\nFirst definition.\n" +
		"*foo#bar()*\nSecond definition.\n*Empty()*\n================\n" +
		"*Final()*\nExample: >\n  echo 1\n"
	docs := ExtractSymbols("test.txt", []byte(source))
	if len(docs) != 6 || docs[0].Name != "len" || docs[1].Kind != "global function" || docs[4].Markdown != "" {
		t.Fatalf("entries = %#v", docs)
	}
	if docs[2].Markdown != "First definition." || docs[3].Markdown != "Second definition." || !strings.HasSuffix(docs[5].Markdown, "```") {
		t.Fatalf("duplicate/empty/incomplete entries = %#v", docs)
	}
}
