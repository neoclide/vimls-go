package workspace

import "testing"

func TestStaticImportAndRuntimePathClassification(t *testing.T) {
	for _, test := range []struct {
		raw, path   string
		runtime, ok bool
	}{
		{"'pkg/api.vim'", "pkg/api.vim", true, true}, {"'./api.vim'", "./api.vim", false, true}, {"'/tmp/api.vim'", "/tmp/api.vim", false, true}, {"$'api'", "", false, false},
	} {
		path, ok := StaticImportPath(test.raw)
		if path != test.path || ok != test.ok || RuntimeImport(test.raw) != test.runtime {
			t.Errorf("%q = %q, %v, runtime=%v", test.raw, path, ok, RuntimeImport(test.raw))
		}
	}
	for _, test := range []struct {
		prefix string
		want   bool
	}{{"pkg", true}, {"", true}, {"./pkg", false}, {"/pkg", false}, {`C:/pkg`, false}} {
		if got := RuntimeImportCompletionPrefix(test.prefix); got != test.want {
			t.Errorf("prefix %q = %v", test.prefix, got)
		}
	}
}
