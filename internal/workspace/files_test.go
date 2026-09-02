package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestDiscoverFilesFindsVimConventionsAndOrdersResults(t *testing.T) {
	root := t.TempDir()
	canonicalRoot := mustResolverCanonical(t, root)
	writeDiscoveryFile(t, filepath.Join(root, "vimrc"), "set nocompatible\n")
	writeDiscoveryFile(t, filepath.Join(root, "gvimrc"), "set guioptions+=a\n")
	writeDiscoveryFile(t, filepath.Join(root, "exrc"), "set number\n")
	writeDiscoveryFile(t, filepath.Join(root, "plugin.vim"), "echo 'plugin'\n")
	writeDiscoveryFile(t, filepath.Join(root, "ordinary.txt"), "not Vim\n")
	for _, directory := range []string{"plugin", "autoload", "import", "after", "ftplugin", "indent", "compiler", "syntax", "colors", "keymap", "pack/pkg/start/demo/plugin"} {
		writeDiscoveryFile(t, filepath.Join(root, directory, "extensionless"), "echo 'runtime'\n")
	}
	writeDiscoveryFile(t, filepath.Join(root, "plugin", "README.md"), "not Vim\n")
	writeDiscoveryFile(t, filepath.Join(root, "syntax", "metadata.json"), "{}\n")

	files, truncated, err := DiscoverFiles(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("unlimited discovery was truncated")
	}
	if !sort.StringsAreSorted(files) {
		t.Fatalf("files are not sorted: %#v", files)
	}
	for _, path := range files {
		if !pathWithin(canonicalRoot, path) {
			t.Fatalf("file escaped root: %q", path)
		}
	}
	for _, name := range []string{"vimrc", "gvimrc", "exrc", "plugin.vim"} {
		if !containsPath(files, filepath.Join(root, name)) {
			t.Fatalf("missing Vim file %q in %#v", name, files)
		}
	}
	if containsPath(files, filepath.Join(root, "ordinary.txt")) {
		t.Fatalf("ordinary file was discovered: %#v", files)
	}
	for _, path := range []string{filepath.Join(root, "plugin", "README.md"), filepath.Join(root, "syntax", "metadata.json")} {
		if containsPath(files, path) {
			t.Fatalf("runtime non-script was discovered: %q", path)
		}
	}
	if got := countRuntimeFiles(files, canonicalRoot); got != 11 {
		t.Fatalf("runtime files = %d, want 11: %#v", got, files)
	}
}

func TestDiscoverFilesSkipsVCSAndUnsafeSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions and targets are not portable on Windows")
	}
	root := t.TempDir()
	canonicalRoot := mustResolverCanonical(t, root)
	outside := t.TempDir()
	inside := filepath.Join(root, "plugin", "inside-target")
	writeDiscoveryFile(t, inside, "echo 'inside'\n")
	outsideFile := filepath.Join(outside, "outside.vim")
	writeDiscoveryFile(t, outsideFile, "echo 'outside'\n")
	if err := os.Symlink(inside, filepath.Join(root, "plugin", "safe-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "plugin", "unsafe-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(root, "plugin", "broken-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "plugin", "unsafe-dir")); err != nil {
		t.Fatal(err)
	}
	writeDiscoveryFile(t, filepath.Join(root, ".git", "config"), "metadata\n")
	writeDiscoveryFile(t, filepath.Join(root, ".hg", "store"), "metadata\n")
	writeDiscoveryFile(t, filepath.Join(root, ".svn", "entries"), "metadata\n")
	writeDiscoveryFile(t, filepath.Join(root, "CVS", "Entries"), "metadata\n")
	writeDiscoveryFile(t, filepath.Join(root, ".gitignore"), "*.swp\n")
	writeDiscoveryFile(t, filepath.Join(root, "node_modules", "plugin", "dependency.vim"), "echo 'dependency'\n")
	writeDiscoveryFile(t, filepath.Join(root, "plugin", "real.vim"), "echo 'real'\n")

	files, truncated, err := DiscoverFiles(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("unlimited discovery was truncated")
	}
	if !containsPath(files, filepath.Join(root, "plugin", "safe-link")) {
		t.Fatalf("safe in-root symlink missing: %#v", files)
	}
	for _, path := range files {
		if strings.Contains(path, "unsafe-") || strings.Contains(path, string(filepath.Separator)+"node_modules"+string(filepath.Separator)) || strings.Contains(path, string(filepath.Separator)+".git"+string(filepath.Separator)) || strings.Contains(path, string(filepath.Separator)+".hg"+string(filepath.Separator)) || strings.Contains(path, string(filepath.Separator)+".svn"+string(filepath.Separator)) || strings.Contains(path, string(filepath.Separator)+"CVS"+string(filepath.Separator)) {
			t.Fatalf("unsafe/VCS path discovered: %q", path)
		}
		if !pathWithin(canonicalRoot, path) {
			t.Fatalf("file escaped root: %q", path)
		}
	}
}

func TestDiscoverFilesSkipsHomeAndSystemTempRoots(t *testing.T) {
	home, homeErr := os.UserHomeDir()
	roots := []string{os.TempDir()}
	if homeErr == nil {
		roots = append(roots, home)
	}
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		files, truncated, err := DiscoverFiles(root, 1)
		if err != nil || truncated || len(files) != 0 {
			t.Fatalf("DiscoverFiles(%q) = %#v, truncated = %v, error = %v", root, files, truncated, err)
		}
	}
}

func TestDiscoverFilesContextHonorsCancelledDeadline(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryFile(t, filepath.Join(root, "plugin", "cancel.vim"), "echo 'cancel'\n")
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	files, truncated, err := DiscoverFilesContext(ctx, root, 0)
	if !errors.Is(err, context.DeadlineExceeded) || files != nil || truncated {
		t.Fatalf("DiscoverFilesContext cancelled result = %#v, %t, %v", files, truncated, err)
	}
}

func TestDiscoverFilesRejectsSymlinkRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks are not portable on Windows")
	}
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	files, truncated, err := DiscoverFiles(link, 10)
	if err == nil || files != nil || truncated {
		t.Fatalf("DiscoverFiles(%q) = %#v, %v, %v", link, files, truncated, err)
	}
	if !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("error = %q", err)
	}
}

func TestDiscoverFilesEnforcesDeterministicLimit(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"z.vim", "a.vim", "m.vim"} {
		writeDiscoveryFile(t, filepath.Join(root, name), "let g:x = 1\n")
	}
	all, truncated, err := DiscoverFiles(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if truncated || len(all) != 3 {
		t.Fatalf("all = %#v, truncated = %v", all, truncated)
	}
	for limit := 1; limit <= len(all)+1; limit++ {
		got, wasTruncated, err := DiscoverFiles(root, limit)
		if err != nil {
			t.Fatal(err)
		}
		wantLength := min(limit, len(all))
		if len(got) != wantLength || !equalStrings(got, all[:wantLength]) {
			t.Fatalf("limit %d = %#v, want %#v", limit, got, all[:wantLength])
		}
		if wasTruncated != (limit < len(all)) {
			t.Fatalf("limit %d truncated = %v, want %v", limit, wasTruncated, limit < len(all))
		}
	}
}

func TestDiscoverFilesDeduplicatesCanonicalPathsBeforeLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks are not portable on Windows")
	}
	root := t.TempDir()
	target := filepath.Join(root, "z.vim")
	writeDiscoveryFile(t, target, "let g:z = 1\n")
	writeDiscoveryFile(t, filepath.Join(root, "m.vim"), "let g:m = 1\n")
	if err := os.Symlink(target, filepath.Join(root, "a.vim")); err != nil {
		t.Fatal(err)
	}

	all, truncated, err := DiscoverFiles(root, 0)
	if err != nil || truncated || len(all) != 2 {
		t.Fatalf("all = %#v, truncated = %v, error = %v", all, truncated, err)
	}
	limited, truncated, err := DiscoverFiles(root, 1)
	if err != nil || !truncated || len(limited) != 1 || limited[0] != all[0] {
		t.Fatalf("limited = %#v, truncated = %v, error = %v, want %#v", limited, truncated, err, all[:1])
	}
}

func TestDiscoverFilesRejectsInvalidRootsWithoutPartialResults(t *testing.T) {
	file := filepath.Join(t.TempDir(), "root.vim")
	writeDiscoveryFile(t, file, "echo 'not a root'\n")
	for _, root := range []string{filepath.Join(t.TempDir(), "missing"), file} {
		files, truncated, err := DiscoverFiles(root, 10)
		if err == nil {
			t.Fatalf("DiscoverFiles(%q) unexpectedly succeeded", root)
		}
		if files != nil || truncated {
			t.Fatalf("DiscoverFiles(%q) returned partial result: %#v, %v", root, files, truncated)
		}
		if !strings.Contains(err.Error(), "workspace root") {
			t.Fatalf("error = %q", err)
		}
	}
}

func TestDiscoverFilesSkipsPermissionErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission errors are not portable on Windows")
	}
	root := t.TempDir()
	writeDiscoveryFile(t, filepath.Join(root, "before.vim"), "echo 'before'\n")
	blocked := filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDiscoveryFile(t, filepath.Join(blocked, "hidden.vim"), "echo 'hidden'\n")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("filesystem does not enforce the permission restriction")
	}
	files, truncated, err := DiscoverFiles(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if truncated || !containsPath(files, filepath.Join(root, "before.vim")) || containsPath(files, filepath.Join(blocked, "hidden.vim")) {
		t.Fatalf("permission-filtered result = %#v, truncated = %v", files, truncated)
	}
}

func writeDiscoveryFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsPath(paths []string, want string) bool {
	if canonical, err := canonicalPathAllowMissing(want); err == nil {
		want = canonical
	} else {
		want = filepath.Clean(want)
	}
	return slices.Contains(paths, want)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func countRuntimeFiles(paths []string, root string) int {
	count := 0
	for _, path := range paths {
		relative, err := filepath.Rel(root, path)
		if err == nil && hasVimRuntimeDirectory(relative) {
			count++
		}
	}
	return count
}
