<p align="center">
  <img src="https://raw.githubusercontent.com/neoclide/vimls-go/main/assets/vimls-go-logo.svg" width="180" alt="vimls-go logo">
</p>

# vimls-go

A language server for Legacy Vim script and Vim9 script, written in Go.

It adds completion, error checking, navigation and refactoring to editors that
support LSP. You can use it with coc.nvim, vim-lsp or Neovim's built-in client.

## What it does

- Completes commands, functions, variables, options, mappings and class members.
- Finds syntax errors, unknown names and Vim9 type errors while you edit.
- Follows definitions and references across files, imports and autoload functions.
- Shows function signatures, inferred types and help from your Vim runtime.
- Finds callers, class relationships and interface implementations.
- Renames resolved symbols and formats indentation.

Both dialects can share a workspace. The supported syntax goes through
**Vim v9.2.1015**. See [language support](docs/language-support.md) for the
features and their limits.

The server reads your scripts without sourcing or executing them. It runs as a
single executable; core analysis needs neither Node.js nor an installed Vim.

## Install

With coc.nvim:

```vim
:CocInstall coc-vimls
```

For other clients, download an archive from
[GitHub Releases](https://github.com/neoclide/vimls-go/releases/latest).
Choose the operating system and CPU that match your machine:

| Machine | Archive suffix |
| --- | --- |
| macOS, Apple Silicon | `darwin-arm64.tar.gz` |
| macOS, Intel | `darwin-amd64.tar.gz` |
| Linux, x86-64 or ARM64 | `linux-amd64.tar.gz` or `linux-arm64.tar.gz` |
| Windows, x86-64 or ARM64 | `windows-amd64.zip` or `windows-arm64.zip` |
| Linux ARMv7 / FreeBSD x86-64 | `linux-armv7.tar.gz` / `freebsd-amd64.tar.gz` |

Unpack the archive and put `vimls` (`vimls.exe` on Windows) on your `PATH`.
Run `vimls --version` to check the installation. The server normally uses
standard input and output; no command-line arguments are needed.

With Go 1.26 or newer, you can install the published version directly:

```sh
go install github.com/neoclide/vimls-go/cmd/vimls@v0.1.0
```

Or build the current source:

```sh
git clone https://github.com/neoclide/vimls-go.git
cd vimls-go
make build
```

This creates `bin/vimls` and `bin/vimparse`. The latter prints a script's syntax
tree and is mainly useful when debugging the parser.

## Set up your editor

- [coc.nvim and vim-lsp setup, settings and troubleshooting](https://github.com/neoclide/vimls-go/blob/main/docs/configuration.md)
- [Neovim setup](https://github.com/neoclide/vimls-go/blob/main/docs/neovim.md)
- [Editing vimrc and other configuration files](https://github.com/neoclide/vimls-go/blob/main/docs/userconfig.md)
- [Understanding and adjusting diagnostics](https://github.com/neoclide/vimls-go/blob/main/docs/diagnostics.md)

For plugin completion and help, the client should send the editor's
`runtimepath`. Without usable paths, the server tries a clean Vim process to
find default runtime directories. It loads no user configuration or plugins.
If Vim is unavailable, core analysis still works.

## Contributing

Bug reports are most useful with a small Vim script, the server version, your
editor/client, and what you expected to happen.

See [development and tests](https://github.com/neoclide/vimls-go/blob/main/docs/testing.md)
to work on the server, [architecture](https://github.com/neoclide/vimls-go/blob/main/docs/architecture.md)
to find the relevant code, and the [roadmap](https://github.com/neoclide/vimls-go/blob/main/docs/roadmap.md)
for remaining work. These docs describe the current source; the
[changelog](CHANGELOG.md) separates unreleased changes from published versions.

## License

[MIT](LICENSES/MIT.txt). Copied Vim material keeps its
[Vim license](LICENSES/VIM.txt).
