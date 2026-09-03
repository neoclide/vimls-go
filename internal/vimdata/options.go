package vimdata

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// OptionType is the Vim value category accepted by an option.
type OptionType uint8

const (
	OptionBool OptionType = iota
	OptionNumber
	OptionString
)

// OptionScope describes where Vim stores an option value.
type OptionScope uint8

const (
	OptionGlobal OptionScope = iota
	OptionWindow
	OptionBuffer
	OptionGlobalLocal
)

// ValidationKind identifies a pure, state-independent part of Vim's option
// validation callback. ValidationNone means vimls deliberately performs no
// value check; it is not a third validation result.
type ValidationKind uint8

const (
	ValidationNone ValidationKind = iota
	ValidationExact
	ValidationCommaList
	ValidationFlagList
	ValidationNumberRange
)

// OptionValidation is the statically reproducible part of an opt_did_set_cb.
// Callback provenance is retained even when Kind is ValidationNone, so a
// complex or stateful callback is visible without being executed or guessed.
type OptionValidation struct {
	Kind            ValidationKind
	Values          []string
	AllowEmpty      bool
	AllowDuplicates bool
	Separator       string
	ErrorCode       string
	HasMin          bool
	Min             int64
	MinErrorCode    string
	HasMax          bool
	Max             int64
	MaxErrorCode    string
	Callback        string
	Sources         []OptionCallbackSource
}

// OptionCallbackSource identifies one platform/build implementation of an
// option callback in the pinned Vim source tree.
type OptionCallbackSource struct {
	Source string
	Line   int
}

// OptionValueError is a statically proven failure from OptionValidation.
// Start and End are byte offsets within the decoded option value.
type OptionValueError struct {
	Code    string
	Message string
	Start   int
	End     int
}

// ValidateOptionValue applies only the pure rule migrated from Vim's callback.
// No rule means no error; this function never consults editor or process state.
func ValidateOptionValue(validation OptionValidation, value string) (OptionValueError, bool) {
	switch validation.Kind {
	case ValidationExact:
		if value == "" && validation.AllowEmpty || slices.Contains(validation.Values, value) {
			return OptionValueError{}, false
		}
		return optionValueError(validation.ErrorCode, 0, len(value)), true
	case ValidationCommaList:
		if commaListValid(validation, value) {
			return OptionValueError{}, false
		}
		return optionValueError(validation.ErrorCode, 0, len(value)), true
	case ValidationFlagList:
		if value == "" && validation.AllowEmpty {
			return OptionValueError{}, false
		}
		for offset := 0; offset < len(value); {
			r, size := utf8.DecodeRuneInString(value[offset:])
			character := value[offset : offset+size]
			if !slices.Contains(validation.Values, character) {
				message := "Illegal character <" + character + ">"
				if r > 127 || r == utf8.RuneError {
					message = fmt.Sprintf("Illegal character <<%02x>>", value[offset])
				}
				return OptionValueError{Code: validation.ErrorCode, Message: message, Start: offset, End: offset + size}, true
			}
			offset += size
		}
	case ValidationNumberRange:
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return OptionValueError{}, false
		}
		if validation.HasMin && number < validation.Min {
			return optionValueError(validation.MinErrorCode, 0, len(value)), true
		}
		if validation.HasMax && number > validation.Max {
			return optionValueError(validation.MaxErrorCode, 0, len(value)), true
		}
	}
	return OptionValueError{}, false
}

func commaListValid(validation OptionValidation, value string) bool {
	if value == "" {
		return validation.AllowEmpty
	}
	for offset := 0; offset < len(value); {
		matched := false
		for _, candidate := range validation.Values {
			if !strings.HasPrefix(value[offset:], candidate) {
				continue
			}
			end := offset + len(candidate)
			if end != len(value) && value[end] != ',' {
				continue
			}
			offset = end
			if offset < len(value) {
				offset++
			}
			matched = true
			break
		}
		if !matched {
			return false
		}
	}
	return true
}

func optionValueError(code string, start, end int) OptionValueError {
	message := "Invalid argument"
	if code == "E487" {
		message = "Argument must be positive"
	}
	return OptionValueError{Code: code, Message: message, Start: start, End: end}
}

// Option describes one option from Vim's pinned options[] table. AvailableWhen
// is its C preprocessor condition; "1" means every build and "0" means no
// build. Terminal t_XX options are included because Vim accepts them through
// &t_XX syntax.
type Option struct {
	Name                string
	ShortName           string
	Type                OptionType
	Scope               OptionScope
	Flags               []string
	Variants            []OptionVariant
	CompletionValues    []string
	Validation          OptionValidation
	AvailableWhen       string
	RequiredFeatures    []string
	DefinitionSource    string
	DefinitionLine      int
	Documentation       string
	DocumentationSource string
}

// OptionVariant preserves one conditional-compilation form of an option's
// storage, callbacks, and defaults in Vim's options[] initializer.
type OptionVariant struct {
	Condition      string
	Variable       string
	Indirect       string
	DidSetCallback string
	ExpandCallback string
	ViDefault      string
	VimDefault     string
}

// LookupOption resolves an exact canonical name or Vim's exact documented
// abbreviation. Vim does not accept arbitrary unique prefixes. The & sigil
// and an optional g: or l: selector are accepted for expression analysis.
func LookupOption(name string) (Option, bool) {
	name = optionLookupName(name)
	if name == "" {
		return Option{}, false
	}
	for _, option := range builtinOptions {
		if option.Name == name {
			return cloneOption(option), true
		}
	}
	for _, option := range builtinOptions {
		if option.ShortName == name {
			return cloneOption(option), true
		}
	}
	return Option{}, false
}

// IsTerminalOptionName reports the t_xx option namespace accepted by Vim.
// Terminal option names are runtime/build dependent, so an entry missing from
// the pinned table is still a valid terminal option rather than an unknown
// ordinary option.
func IsTerminalOptionName(name string) bool {
	name = optionLookupName(name)
	return len(name) >= 2 && name[:2] == "t_"
}

func optionLookupName(name string) string {
	if len(name) > 0 && name[0] == '&' {
		name = name[1:]
		if len(name) > 2 && (name[:2] == "g:" || name[:2] == "l:") {
			name = name[2:]
		}
	}
	if len(name) >= 4 && name[0] == '<' && name[len(name)-1] == '>' && name[1] == 't' && name[2] == '_' {
		name = name[1 : len(name)-1]
	}
	return name
}

// Options returns the pinned options[] table by canonical name.  Callers own
// the returned slice.
func Options() []Option {
	result := make([]Option, len(builtinOptions))
	for i, option := range builtinOptions {
		result[i] = cloneOption(option)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func cloneOption(option Option) Option {
	option.Flags = append([]string(nil), option.Flags...)
	option.Variants = append([]OptionVariant(nil), option.Variants...)
	option.CompletionValues = append([]string(nil), option.CompletionValues...)
	option.Validation.Values = append([]string(nil), option.Validation.Values...)
	option.Validation.Sources = append([]OptionCallbackSource(nil), option.Validation.Sources...)
	option.RequiredFeatures = append([]string(nil), option.RequiredFeatures...)
	return option
}

// OptionValues returns values extracted from Vim's fixed v9.2.1015 :set
// completion arrays and flag strings. Dynamic values such as encodings,
// paths, events and runtime names are deliberately excluded. Callers own the
// returned slice.
func OptionValues(name string) []string {
	option, ok := LookupOption(name)
	if !ok {
		return nil
	}
	return append([]string(nil), option.CompletionValues...)
}
