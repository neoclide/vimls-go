package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchConfigFilePattern(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		pattern  string
		path     string
		roots    []string
		expected bool
	}{
		{
			name:     "empty pattern",
			pattern:  "",
			path:     "/path/to/vimrc",
			expected: false,
		},
		{
			name:     "relative pattern star is ignored",
			pattern:  "*",
			path:     "/path/to/any_file.vim",
			expected: false,
		},
		{
			name:     "relative pattern filename extension is ignored",
			pattern:  "*.vim",
			path:     "/path/to/options.vim",
			expected: false,
		},
		{
			name:     "relative pattern vimrc is ignored",
			pattern:  "*vimrc*",
			path:     "/path/to/.vimrc",
			expected: false,
		},
		{
			name:     "relative path wildcard is ignored",
			pattern:  "rc/*.vim",
			path:     "/home/user/.vim/rc/options.vim",
			expected: false,
		},
		{
			name:     "relative recursive wildcard is ignored",
			pattern:  "**/settings/**/*.vim",
			path:     "/home/user/dotfiles/vim/settings/layers/git.vim",
			expected: false,
		},
		{
			name:     "tilde expansion absolute pattern",
			pattern:  "~/.vimrc",
			path:     filepath.Join(home, ".vimrc"),
			expected: true,
		},
		{
			name:     "tilde expansion with wildcard",
			pattern:  "~/.config/nvim/*.vim",
			path:     filepath.Join(home, ".config", "nvim", "init.vim"),
			expected: true,
		},
		{
			name:     "tilde expansion with recursive wildcard",
			pattern:  "~/settings/**/*.vim",
			path:     filepath.Join(home, "settings", "layers", "git.vim"),
			expected: true,
		},
		{
			name:     "absolute path single-level wildcard",
			pattern:  filepath.Join(home, ".vim", "rc", "*.vim"),
			path:     filepath.Join(home, ".vim", "rc", "options.vim"),
			expected: true,
		},
		{
			name:     "absolute path single-level wildcard mismatch nested",
			pattern:  filepath.Join(home, ".vim", "rc", "*.vim"),
			path:     filepath.Join(home, ".vim", "rc", "sub", "options.vim"),
			expected: false,
		},
		{
			name:     "absolute path recursive wildcard",
			pattern:  filepath.Join(home, "dotfiles", "**", "*.vim"),
			path:     filepath.Join(home, "dotfiles", "vim", "settings", "git.vim"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchConfigFilePattern(tt.pattern, tt.path, tt.roots...)
			if got != tt.expected {
				t.Fatalf("MatchConfigFilePattern(%q, %q, %v) = %v; want %v", tt.pattern, tt.path, tt.roots, got, tt.expected)
			}
		})
	}
}

func TestIsConfigFile(t *testing.T) {
	root := "/workspace/dotfiles"
	runtimeRoot := "/usr/local/share/vim/vim92"

	tests := []struct {
		name           string
		path           string
		patterns       []string
		workspaceRoots []string
		runtimeRoots   []string
		expected       bool
	}{
		{
			name:     "standard .vimrc is config file",
			path:     "/home/user/.vimrc",
			expected: true,
		},
		{
			name:           "standard vimrc in workspace is config file",
			path:           filepath.Join(root, "vimrc"),
			workspaceRoots: []string{root},
			expected:       true,
		},
		{
			name:     "standard init.vim is config file",
			path:     "/home/user/.config/nvim/init.vim",
			expected: true,
		},
		{
			name:           "file in non-runtime directory in workspace is config file",
			path:           filepath.Join(root, "rc", "options.vim"),
			workspaceRoots: []string{root},
			expected:       true,
		},
		{
			name:     "standalone options.vim is config file",
			path:     "/home/user/.vim/options.vim",
			expected: true,
		},
		{
			name:           "plugin directory file in workspace is not config file by default",
			path:           filepath.Join(root, "plugin", "my_plugin.vim"),
			workspaceRoots: []string{root},
			expected:       false,
		},
		{
			name:           "autoload directory file in workspace is not config file by default",
			path:           filepath.Join(root, "autoload", "helper.vim"),
			workspaceRoots: []string{root},
			expected:       false,
		},
		{
			name:           "ftplugin directory file in workspace is not config file by default",
			path:           filepath.Join(root, "ftplugin", "python.vim"),
			workspaceRoots: []string{root},
			expected:       false,
		},
		{
			name:           "syntax directory file in workspace is not config file by default",
			path:           filepath.Join(root, "syntax", "c.vim"),
			workspaceRoots: []string{root},
			expected:       false,
		},
		{
			name:           "after plugin directory file in workspace is not config file by default",
			path:           filepath.Join(root, "after", "plugin", "override.vim"),
			workspaceRoots: []string{root},
			expected:       false,
		},
		{
			name:           "plugin file can be designated as config file via absolute patterns",
			path:           filepath.Join(root, "plugin", "my_plugin.vim"),
			patterns:       []string{filepath.Join(root, "plugin", "my_plugin.vim")},
			workspaceRoots: []string{root},
			expected:       true,
		},
		{
			name:           "wildcard absolute pattern designates plugin files as config files",
			path:           filepath.Join(root, "plugin", "anything.vim"),
			patterns:       []string{filepath.Join(root, "plugin", "*.vim")},
			workspaceRoots: []string{root},
			expected:       true,
		},
		{
			name:           "relative patterns are ignored and do not match",
			path:           filepath.Join(root, "plugin", "my_plugin.vim"),
			patterns:       []string{"plugin/my_plugin.vim", "*"},
			workspaceRoots: []string{root},
			expected:       false,
		},
		{
			name:           "case handling for runtime directories on Windows",
			path:           filepath.Join(root, "Plugin", "foo.vim"),
			workspaceRoots: []string{root},
			expected:       !caseInsensitiveFS,
		},
		{
			name:           "case handling for config names on Windows",
			path:           filepath.Join(root, "plugin", "INIT.VIM"),
			workspaceRoots: []string{root},
			expected:       caseInsensitiveFS,
		},
		{
			name:         "case handling for VIMRC in runtime root on Windows",
			path:         filepath.Join(runtimeRoot, "VIMRC"),
			runtimeRoots: []string{runtimeRoot},
			expected:     caseInsensitiveFS,
		},
		{
			name:         "runtime root defaults.vim is not user config file",
			path:         filepath.Join(runtimeRoot, "defaults.vim"),
			runtimeRoots: []string{runtimeRoot},
			expected:     false,
		},
		{
			name:         "runtime root filetype.vim is not user config file",
			path:         filepath.Join(runtimeRoot, "filetype.vim"),
			runtimeRoots: []string{runtimeRoot},
			expected:     false,
		},
		{
			name:         "runtime root menu.vim is not user config file",
			path:         filepath.Join(runtimeRoot, "menu.vim"),
			runtimeRoots: []string{runtimeRoot},
			expected:     false,
		},
		{
			name:         "runtime root known config name like init.vim is config file",
			path:         filepath.Join(runtimeRoot, "init.vim"),
			runtimeRoots: []string{runtimeRoot},
			expected:     true,
		},
		{
			name:     "ancestor directory named plugin outside roots is treated as config file",
			path:     "/home/plugin/config.vim",
			expected: true,
		},
		{
			name:     "ancestor directory named autoload outside roots is treated as config file",
			path:     "/var/autoload/custom_settings.vim",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsConfigFile(tt.path, tt.patterns, tt.workspaceRoots, tt.runtimeRoots)
			if got != tt.expected {
				t.Fatalf("IsConfigFile(%q, %v, %v, %v) = %v; want %v", tt.path, tt.patterns, tt.workspaceRoots, tt.runtimeRoots, got, tt.expected)
			}
		})
	}
}

func TestIsConfigFileSymlinkDotfiles(t *testing.T) {
	tempDir := t.TempDir()
	dotfiles := filepath.Join(tempDir, "dotfiles", "nvim")
	if err := os.MkdirAll(filepath.Join(dotfiles, "plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	realFile := filepath.Join(dotfiles, "plugin", "settings.vim")
	if err := os.WriteFile(realFile, []byte("echo 1"), 0o644); err != nil {
		t.Fatal(err)
	}

	configDir := filepath.Join(tempDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkNvim := filepath.Join(configDir, "nvim")
	if err := os.Symlink(dotfiles, symlinkNvim); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	symlinkFile := filepath.Join(symlinkNvim, "plugin", "settings.vim")

	// Pattern using the symlinked path (e.g. ~/.config/nvim/**/*.vim)
	pattern := filepath.Join(symlinkNvim, "**", "*.vim")
	if !IsConfigFile(symlinkFile, []string{pattern}, nil, nil) {
		t.Fatalf("expected %s to match pattern %s via symlink path", symlinkFile, pattern)
	}

	// Pattern using the real dotfiles path should also match when accessed via symlink
	realPattern := filepath.Join(dotfiles, "**", "*.vim")
	if !IsConfigFile(symlinkFile, []string{realPattern}, nil, nil) {
		t.Fatalf("expected %s to match real pattern %s", symlinkFile, realPattern)
	}
}
