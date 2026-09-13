package vimdata

import (
	"testing"
)

func TestFeatureHistoryCounts(t *testing.T) {
	if len(optionHistory) != 39 {
		t.Fatalf("expected 39 options in history, got %d", len(optionHistory))
	}
	if len(commandHistory) != 9 {
		t.Fatalf("expected 9 commands in history, got %d", len(commandHistory))
	}
	if len(functionHistory) != 48 {
		t.Fatalf("expected 48 functions in history, got %d", len(functionHistory))
	}
	if len(autocmdEventHistory) != 12 {
		t.Fatalf("expected 12 autocmd events in history, got %d", len(autocmdEventHistory))
	}
}

func TestOptionHistoryMetadata(t *testing.T) {
	for name, item := range optionHistory {
		if _, ok := LookupOption(name); !ok {
			t.Errorf("option %q not found in builtinOptions", name)
		}
		if item.Since() != "Since Vim "+item.Version {
			t.Errorf("option %q Since() = %q, want %q", name, item.Since(), "Since Vim "+item.Version)
		}
	}
	// Test lookup with prefix and short name
	if item, ok := LookupOptionHistory("&smoothscroll"); !ok || item.Version != "9.0.0640" {
		t.Fatalf("&smoothscroll lookup failed: %#v", item)
	}
	if item, ok := LookupOptionHistory("sms"); !ok || item.Version != "9.0.0640" {
		t.Fatalf("sms lookup failed: %#v", item)
	}
	if item, ok := LookupOptionHistory("&l:sms"); !ok || item.Version != "9.0.0640" {
		t.Fatalf("&l:sms lookup failed: %#v", item)
	}
}

func TestCommandHistoryMetadata(t *testing.T) {
	for name, item := range commandHistory {
		if _, ok := Lookup(":" + name); !ok {
			t.Errorf("command %q not found in commands", name)
		}
		if item.Since() != "Since Vim "+item.Version {
			t.Errorf("command %q Since() = %q, want %q", name, item.Since(), "Since Vim "+item.Version)
		}
	}
	if item, ok := LookupCommandHistory(":defer"); !ok || item.Version != "9.0.0370" {
		t.Fatalf(":defer lookup failed: %#v", item)
	}
	if item, ok := LookupCommandHistory("defer"); !ok || item.Version != "9.0.0370" {
		t.Fatalf("defer lookup failed: %#v", item)
	}
}

func TestFunctionHistoryMetadata(t *testing.T) {
	for name, item := range functionHistory {
		if _, ok := LookupFunction(name); !ok {
			t.Errorf("function %q not found in builtinFunctions", name)
		}
		if item.Since() != "Since Vim "+item.Version {
			t.Errorf("function %q Since() = %q, want %q", name, item.Since(), "Since Vim "+item.Version)
		}
	}
	if item, ok := LookupFunctionHistory("indexof"); !ok || item.Version != "9.0.0196" {
		t.Fatalf("indexof lookup failed: %#v", item)
	}
	if item, ok := LookupFunctionHistory("g:indexof"); !ok || item.Version != "9.0.0196" {
		t.Fatalf("g:indexof lookup failed: %#v", item)
	}
}

func TestAutocmdEventHistoryMetadata(t *testing.T) {
	for name, item := range autocmdEventHistory {
		if _, ok := LookupAutocmdEvent(name); !ok {
			t.Errorf("autocmd event %q not found in autocmdEvents", name)
		}
		if item.Since() != "Since Vim "+item.Version {
			t.Errorf("autocmd event %q Since() = %q, want %q", name, item.Since(), "Since Vim "+item.Version)
		}
	}
	if item, ok := LookupAutocmdEventHistory("WinResized"); !ok || item.Version != "9.0.0917" {
		t.Fatalf("WinResized lookup failed: %#v", item)
	}
	if item, ok := LookupAutocmdEventHistory("winresized"); !ok || item.Version != "9.0.0917" {
		t.Fatalf("winresized case-insensitive lookup failed: %#v", item)
	}
}

func TestVimVersionParsingAndComparison(t *testing.T) {
	v1, ok := ParseVimVersion("9.0.0500")
	if !ok || v1.Major != 9 || v1.Minor != 0 || v1.Patch != 500 {
		t.Fatalf("ParseVimVersion(9.0.0500) = %#v", v1)
	}
	v2, ok := ParseVimVersion("v9.0.0640")
	if !ok || v2.Major != 9 || v2.Minor != 0 || v2.Patch != 640 {
		t.Fatalf("ParseVimVersion(v9.0.0640) = %#v", v2)
	}
	if v1.Compare(v2) >= 0 {
		t.Fatalf("expected v1 < v2, got %d", v1.Compare(v2))
	}
	if v2.Compare(v1) <= 0 {
		t.Fatalf("expected v2 > v1, got %d", v2.Compare(v1))
	}
	if v1.Compare(v1) != 0 {
		t.Fatalf("expected v1 == v1, got %d", v1.Compare(v1))
	}
	v3, ok := ParseVimVersion("9.1")
	if !ok || v3.Major != 9 || v3.Minor != 1 || v3.Patch != 0 {
		t.Fatalf("ParseVimVersion(9.1) = %#v", v3)
	}
	if v3.Compare(v2) <= 0 {
		t.Fatalf("expected 9.1 > 9.0.0640, got %d", v3.Compare(v2))
	}
	if _, ok := ParseVimVersion(""); ok {
		t.Fatal("expected empty string to fail")
	}
	if _, ok := ParseVimVersion("invalid"); ok {
		t.Fatal("expected invalid to fail")
	}
}
