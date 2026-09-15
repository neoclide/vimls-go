package workspace

import (
	"path/filepath"
	"strings"
)

// RewriteImportPath returns the literal import path that must replace raw after
// either the file raw resolves to moves from oldTarget to newTarget, or the
// importing script moves to from. The original spelling form is preserved: a
// relative import stays relative to from's directory, an absolute import stays
// absolute, and a runtimepath import stays relative to the same lookup
// directory. from is the importer's new location, which is its current location
// when only the target moves.
//
// The result may be byte-identical to the decoded raw when neither move changes
// how Vim resolves the import; callers detect that by comparing the requoted
// text rather than by inspecting the returned value.
//
// ok is false whenever the new location cannot be expressed in the original
// form. Callers must then leave the import alone instead of guessing a path
// that would change how Vim resolves it.
func RewriteImportPath(from, raw, oldTarget, newTarget string) (string, bool) {
	spec, ok := decodeStaticPath(raw)
	if !ok || spec == "" || oldTarget == "" || newTarget == "" {
		return "", false
	}
	newPath := filepath.FromSlash(newTarget)
	if strings.HasPrefix(spec, ".") {
		if from == "" {
			return "", false
		}
		relative, err := filepath.Rel(filepath.Dir(filepath.FromSlash(from)), newPath)
		if err != nil || relative == "" || relative == "." {
			return "", false
		}
		// A relative import must keep its leading dot; without it Vim would
		// search 'runtimepath' instead of the importing script's directory.
		if !strings.HasPrefix(relative, ".") {
			relative = "." + string(filepath.Separator) + relative
		}
		return filepath.ToSlash(relative), true
	}
	// Absolute and runtimepath imports are both resolved from a fixed
	// directory, so only the target's displacement inside it is needed. Using
	// the recorded spelling's directory rather than the resolved one keeps a
	// caller's own path spelling intact.
	displacement, err := filepath.Rel(filepath.Dir(filepath.FromSlash(oldTarget)), newPath)
	if err != nil || displacement == "" {
		return "", false
	}
	if displacement == "." {
		// The target stayed beside the same lookup directory, so neither the
		// importer's location nor the target's name affects this spelling.
		return spec, true
	}
	value := filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(spec)), displacement))
	if value == "." || value == ".." || strings.HasPrefix(value, ".."+string(filepath.Separator)) {
		return "", false
	}
	value = filepath.ToSlash(value)
	if isAbsolutePath(spec) {
		if !isAbsolutePath(value) {
			return "", false
		}
		return value, true
	}
	if !RuntimeImportCompletionPrefix(value) {
		return "", false
	}
	return value, true
}

// ImportPathName returns the namespace a literal import path introduces: its
// filename without the .vim extension. A non-literal or extension-less path has
// no statically known name.
func ImportPathName(raw string) (string, bool) {
	name := defaultImportName(raw)
	return name, name != ""
}
