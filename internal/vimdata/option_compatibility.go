package vimdata

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// OptionCompatibility contains the reviewed, static parts of both editors'
// option contracts. An empty Vim.Name denotes a variant-only option. These
// overlays do not change the pinned Vim completion/hover metadata.
type OptionCompatibility struct {
	Vim, Variant Option
	Feature      string
}

// LookupOptionCompatibility resolves canonical names, short names and &g:/&l:
// selectors. Evidence: Vim v9.2.1015 runtime/doc/options.txt and Neovim
// 73923b0dd85bb936ba2f63ee916dabaa0603340d runtime/doc/options.txt (option tags)
// and runtime/doc/vim_diff.txt (Options). Later extensions need manual review.
// ValidationNone deliberately retains conservative handling of runtime values.
func LookupOptionCompatibility(name string) (OptionCompatibility, bool) {
	vim, found := LookupOption(name)
	if mac, ok := lookupMacVimCompatOption(optionLookupName(name)); ok {
		return OptionCompatibility{Variant: mac, Feature: "gui_macvim"}, true
	}
	if nvim, ok := lookupNeovimCompatOption(optionLookupName(name)); ok {
		// 'pb' means pumborder in Vim and pumblend in Neovim. Resolve
		// each editor independently, including the Vim value grammar.
		if vim.Name == "pumborder" {
			vim.Validation = OptionValidation{Kind: ValidationPumBorder}
		}
		return OptionCompatibility{Vim: vim, Variant: nvim, Feature: "nvim"}, true
	}
	if !found {
		return OptionCompatibility{}, false
	}
	nvim := cloneOption(vim)
	switch vim.Name {
	case "signcolumn":
		nvim.Validation = neovimSignColumnValidation
	case "foldcolumn":
		vim.Validation = compatNumberRange(0, 12)
		nvim.Type = OptionString
		nvim.Validation = compatExact("0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
			"auto", "auto:1", "auto:2", "auto:3", "auto:4", "auto:5", "auto:6", "auto:7", "auto:8", "auto:9")
	case "cmdheight":
		nvim.Validation.Min = 0
	case "laststatus":
		// Vim accepts numbers outside its documented 0..2 values. Keep its
		// permissive validator; 3 has Neovim-specific global-statusline meaning.
		nvim.Validation = compatNumberRange(0, 3)
	case "completeopt":
		vim.Validation = compatCommaList(vim.CompletionValues...)
		nvim.Validation = compatCommaList("menu", "menuone", "longest", "preview", "popup", "noinsert", "noselect", "fuzzy", "nosort", "preinsert", "nearest", "preselect")
	case "fillchars":
		nvim.Validation.Values = slices.DeleteFunc(nvim.Validation.Values, func(value string) bool { return value == "tpl_vert" })
		nvim.Validation.Values = append(nvim.Validation.Values, "wbr", "horiz", "horizup", "horizdown", "vertleft", "vertright", "verthoriz", "msgsep")
	case "jumpoptions":
		vim.Validation = compatCommaList("stack")
		nvim.Validation = compatCommaList("stack", "view", "clean")
	case "cpoptions":
		nvim.Validation.Values = nil
		for _, flag := range "aAbBcCdDeEfFiIJKlLmMnoOPqrRstSuvWxXyZ!$%+>;~_" {
			nvim.Validation.Values = append(nvim.Validation.Values, string(flag))
		}
	default:
		return OptionCompatibility{}, false
	}
	// These specific option grammars have been reviewed independently of Vim
	// build features; do not globally enable checks for feature-gated options.
	vim.AvailableWhen, nvim.AvailableWhen = "1", "1"
	return OptionCompatibility{Vim: vim, Variant: cloneOption(nvim), Feature: "nvim"}, true
}

func compatExact(values ...string) OptionValidation {
	return OptionValidation{Kind: ValidationExact, Values: values, ErrorCode: "E474"}
}

func compatCommaList(values ...string) OptionValidation {
	return OptionValidation{Kind: ValidationCommaList, Values: values, AllowEmpty: true, AllowDuplicates: true, Separator: ",", ErrorCode: "E474"}
}

func compatNumberRange(min, max int64) OptionValidation {
	return OptionValidation{Kind: ValidationNumberRange, HasMin: true, Min: min, MinErrorCode: "E474", HasMax: true, Max: max, MaxErrorCode: "E474"}
}

var neovimSignColumnValidation = func() OptionValidation {
	values := []string{"auto", "no", "yes", "number"}
	for digit := byte('1'); digit <= '9'; digit++ {
		values = append(values, "auto:"+string(digit), "yes:"+string(digit))
		for max := digit + 1; max <= '9'; max++ {
			values = append(values, "auto:"+string(digit)+"-"+string(max))
		}
	}
	return compatExact(values...)
}()

func validateCompatBorder(kind ValidationKind, value string) (OptionValueError, bool) {
	invalid := func() (OptionValueError, bool) { return optionValueError("E474", 0, len(value)), true }
	if kind == ValidationWinBorder {
		if slices.Contains([]string{"", "bold", "double", "none", "rounded", "shadow", "single", "solid"}, value) {
			return OptionValueError{}, false
		}
		if !compatBorderCharacters(value, ",", true) {
			return invalid()
		}
	} else if value != "" {
		style := ""
		for field := range strings.SplitSeq(value, ",") {
			if field == "margin" || field == "shadow" {
				continue
			}
			if characters, custom := strings.CutPrefix(field, "custom:"); custom {
				if !compatBorderCharacters(characters, ";", false) {
					return invalid()
				}
			} else if !slices.Contains([]string{"single", "double", "round", "ascii"}, field) {
				return invalid()
			}
			if style != "" && style != field {
				return invalid()
			}
			style = field
		}
	}
	return OptionValueError{}, false
}

func compatBorderCharacters(value, separator string, allowEmpty bool) bool {
	fields := strings.Split(value, separator)
	if len(fields) != 8 {
		return false
	}
	for _, field := range fields {
		// Keep encoded characters and combining sequences conservative; their
		// decoded cell width is not a static option grammar property.
		if strings.ContainsRune(field, '\\') || strings.ContainsFunc(field, func(r rune) bool { return unicode.IsMark(r) }) {
			continue
		}
		if utf8.RuneCountInString(field) != 1 && !(allowEmpty && field == "") {
			return false
		}
	}
	return true
}

func validateCompatMouseScroll(value string) (OptionValueError, bool) {
	if value == "" {
		return OptionValueError{}, false
	}
	seen := make(map[string]bool)
	for field := range strings.SplitSeq(value, ",") {
		direction, count, ok := strings.Cut(field, ":")
		_, err := strconv.ParseUint(count, 10, 64)
		if !ok || direction != "hor" && direction != "ver" || seen[direction] || err != nil || strings.HasPrefix(count, "+") {
			return optionValueError("E474", 0, len(value)), true
		}
		seen[direction] = true
	}
	return OptionValueError{}, false
}
