<p align="center">
  <img src="assets/vimls-go-logo.svg" width="180" alt="vimls-go logo">
</p>

<h1 align="center">vimls-go</h1>

<p align="center">
  <a href="https://github.com/neoclide/vimls-go/actions/workflows/ci.yml"><img src="https://github.com/neoclide/vimls-go/actions/workflows/ci.yml/badge.svg" alt="CI status"></a>
</p>

`vimls-go` is a fast, lightweight, and safe Language Server Protocol (LSP) server for **Legacy Vim script** and **Vim9 script**, written in Go.

It analyzes Vim scripts entirely through static analysis without executing user code, starting a Vim instance, or requiring Vim to be installed at runtime.

Its grammar and metadata ceiling supports Vim syntax through **v9.2.1015**, covering both modern Vim9 script language features (classes, interfaces, types, enums) and classic Legacy Vim script idioms with backwards compatibility.

---

## Key Highlights

- **Dual-Dialect Support**: Independent root parsers for Legacy Vim script and Vim9 script with contextual dialect switching (`vim9script`, `legacy`, `vim9cmd`, `scriptversion`).
- **Safe Static Analysis**: Untrusted workspace files and scripts are analyzed safely without sourcing or evaluating code.
- **Fast & Lightweight**: Single standalone binary with low memory footprint, incremental document synchronization, and background analysis.
- **Runtimepath & Workspace Aware**: Indexes workspace Vim files and recursive `plugin`, `autoload`, and `import` scripts from external runtime roots. External `colors/*.vim` files provide names/paths only; other external runtime subtrees are not scanned.

---

## Implemented Features

| Feature Category | Capabilities & Supported Behaviors |
| --- | --- |
| **Diagnostics** | • Syntax and structural error detection with resilient error recovery.<br>• Unresolved identifier detection (`E117`, `E121`, `E1001`, `E1089`).<br>• Statically provable Vim9 type errors and immutable variable re-assignment checks.<br>• Unused Vim9 variables and deprecated reference hints (`unnecessary`, `deprecated` tags). |
| **Code Completion** | Context-aware completion with detail and documentation for:<br>• Ex commands and user commands<br>• Built-in and user-defined functions<br>• Scope variables (`g:`, `b:`, `w:`, `t:`, `s:`, `v:`, local/Vim9 variables)<br>• Options (`:set`, `&opt`)<br>• Autocommand events and groups (`:autocmd`)<br>• Key mappings (`:map`, `<silent>`, `<expr>`, keycodes like `<CR>`, `<Leader>`)<br>• Syntax and highlight groups<br>• Imports, exported members, and object/class members<br>• Autoload functions and color schemes |
| **Hover & Docs** | Shows symbol kinds, inferred types, signatures, doc comments, and embedded official Vim help tags and documentation. |
| **Signature Help** | Parameter lists, active parameter highlighting, and documentation for built-in functions, user-defined functions, imported callables, methods, and class constructors. |
| **Navigation** | • **Go to Definition** & **Declaration** across local scopes, imports, autoload functions, and workspace files.<br>• **Find References** across open buffers and indexed workspace files.<br>• **Document Highlights** (read/write occurrences within the current file).<br>• **Document Links** for imported file targets. |
| **Type & Call Hierarchy** | • **Type Hierarchy**: Class/interface inheritance and implementation relationships (`supertypes` / `subtypes`).<br>• **Go to Implementation**: Resolves interfaces and abstract class members to concrete implementations.<br>• **Call Hierarchy**: Incoming and outgoing call hierarchies for statically resolved named callables. |
| **Code Lens** | Reference counts for named functions, methods, and constructors. Implementation counts are limited to Vim9 abstract-class and interface methods. Clickable results require client support for `editor.action.showReferences`. |
| **Symbols & Outline** | • **Document Symbols**: File outline (functions, classes, interfaces, enums, variables, commands).<br>• **Workspace Symbols**: Fuzzy symbol search across the entire project.<br>• **Folding Ranges**: Folding blocks for functions, classes, conditionals, loops, heredocs, and comments.<br>• **Selection Ranges**: Semantic selection expansion and shrinking. |
| **Refactoring & Editing** | • **Rename**: Safe symbol rename across references with pre-check validation (`prepareRename`).<br>• **Semantic Tokens**: Full semantic syntax highlighting for types, functions, variables, parameters, and modifiers.<br>• **Inlay Hints**: Inferred variable and return type hints for Vim9 script.<br>• **Quick Fixes**: Automated code actions for unambiguous syntax repairs. |
| **Formatting** | Source-preserving document and range indentation formatting (only proven leading indentation whitespace is modified; expressions and bodies are never destructively mangled). |

---

## Installation & Releases

### Option 1: Download Pre-built Binaries (Recommended)

Published builds, when available, appear on the [GitHub Releases](https://github.com/neoclide/vimls-go/releases) page. `v1.0.0-rc.1` is currently a local candidate, not a published release; see [candidate evidence](docs/release-candidate.md) and [support limitations](docs/language-support.md).

The archive naming contract is:

| Operating System | Architecture | Archive Name |
| --- | --- | --- |
| **macOS** | Apple Silicon (`arm64`) | `vimls-vX.Y.Z-darwin-arm64.tar.gz` |
| **macOS** | Intel (`amd64`) | `vimls-vX.Y.Z-darwin-amd64.tar.gz` |
| **Linux** | 64-bit (`x86_64` / `amd64`) | `vimls-vX.Y.Z-linux-amd64.tar.gz` |
| **Linux** | ARM64 (`aarch64` / `arm64`) | `vimls-vX.Y.Z-linux-arm64.tar.gz` |
| **Linux** | ARMv7 (`armv7`) | `vimls-vX.Y.Z-linux-armv7.tar.gz` |
| **Windows** | 64-bit (`x86_64` / `amd64`) | `vimls-vX.Y.Z-windows-amd64.zip` |
| **Windows** | ARM64 (`arm64`) | `vimls-vX.Y.Z-windows-arm64.zip` |
| **FreeBSD** | 64-bit (`amd64`) | `vimls-vX.Y.Z-freebsd-amd64.tar.gz` |

#### Linux / macOS Quick Install Example:

```sh
# Select an actually published tag and the archive matching your platform:
release_tag=vX.Y.Z
curl -fsSL -o vimls.tar.gz "https://github.com/neoclide/vimls-go/releases/download/${release_tag}/vimls-${release_tag}-linux-amd64.tar.gz"

# Extract the binary:
tar -xzf vimls.tar.gz

# Move to a directory in your PATH (e.g., ~/.local/bin or /usr/local/bin):
mv vimls ~/.local/bin/
chmod +x ~/.local/bin/vimls
```

### Option 2: Install via `go install`

If you have Go 1.26 or newer installed:

```sh
go install github.com/neoclide/vimls-go/cmd/vimls@latest
```

### Option 3: Build from Source

```sh
git clone https://github.com/neoclide/vimls-go.git
cd vimls-go
make build
# Binaries are generated in ./bin/ (./bin/vimls and ./bin/vimparse)
```

`vimparse <file>` prints the parsed syntax tree as JSON. It selects the Vim9
root parser when the file's first effective command is `vim9script`; otherwise
it uses the legacy root parser.

---

## Editor Configuration

`vimls` uses standard **stdio** communication by default (an optional `--listen <addr>` flag is available for TCP debugging).

### 1. coc.nvim

Add to your `coc-settings.json` (open with `:CocConfig`):

```json
{
  "languageserver": {
    "vimls": {
      "command": "vimls",
      "filetypes": ["vim"]
    }
  }
}
```

### 2. Neovim (nvim-lspconfig / Built-in LSP)

With Neovim's built-in LSP client:

```lua
vim.api.nvim_create_autocmd("FileType", {
  pattern = "vim",
  callback = function(args)
    vim.lsp.start({
      name = "vimls",
      cmd = { "vimls" },
      root_dir = vim.fs.root(args.buf, { ".git", ".vim" }) or vim.fs.dirname(vim.api.nvim_buf_get_name(args.buf)),
    })
  end,
})
```

### 3. vim-lsp

Add to your `~/.vimrc`:

```vim
if executable('vimls')
  augroup vimls_lsp
    autocmd!
    autocmd User lsp_setup call lsp#register_server({
        \ 'name': 'vimls',
        \ 'cmd': {server_info -> ['vimls']},
        \ 'allowlist': ['vim'],
        \ })
  augroup END
endif
```

---

## Initialization Options

`runtimepath` is the only supported `initializationOptions` field.

| Setting | Type | Default | Description |
| --- | --- | --- | --- |
| `runtimepath` | `string[]` | *Auto-discovered* | Custom array of ordered runtime paths. An explicit empty array `[]` disables runtime indexing. |

Runtimepath roots outside the current workspace folders are intentionally
scanned narrowly: `plugin/**/*.vim`, `autoload/**/*.vim`, and
`import/**/*.vim` are parsed for public APIs and documentation. Direct
`colors/*.vim` children contribute only their colorscheme name and canonical
path for completion. Other runtime files, including `after/plugin`, are not
indexed unless they are inside a workspace folder; dynamically sourced files
may therefore remain undiscovered.

## Workspace Settings

Clients that advertise `textDocument.diagnostic` use document pull diagnostics;
older clients retain `textDocument/publishDiagnostics`. Pull diagnostics are
advertised with `workspaceDiagnostics: true`: workspace pull covers open and
closed Vim files below workspace roots, supports previous result IDs and
partial-result progress, and excludes external runtimepath-only files.

User settings are supplied through LSP `workspace/configuration`.

| Setting | Type | Default | Description |
| --- | --- | --- | --- |
| `workspace.rebuildDebounce` | `number` | `100` | Milliseconds to wait after the latest workspace rebuild trigger; `0` rebuilds immediately. |
| `suggest.excludeRuntimePath` | `boolean` | `false` | Omit completion items sourced from runtimepath files outside current workspace roots. |
| `diagnostic.disabled` | `string[]` | `[]` | Exact diagnostic codes to omit from published LSP diagnostics; accepts native, `vimls/`, and future codes. |
| `diagnostic.override` | `object` | `{}` | Exact diagnostic code to LSP severity (`error`, `warning`, `information`, or `hint`); disabled codes take precedence. |
| `diagnostic.maxNumber` | `number` | `1000` | Maximum diagnostics per document, including the truncation marker. |

Settings are nested objects inside the workspace configuration: `diagnostic`
carries `disabled`, `override`, and `maxNumber`, `workspace` carries `rebuildDebounce`, and
`suggest` carries `excludeRuntimePath`. They are read under the `vim` workspace
configuration section and are dynamic; changed values reanalyze open documents.
Every setting is optional: a missing, empty, or `null` setting is not an error
and keeps the documented default. They do not belong in
`initializationOptions`.

`diagnostic.disabled` takes precedence over `diagnostic.override`, and
when diagnostics exceed `diagnostic.maxNumber`, the server retains errors,
warnings, information, then hints in that order. `diagnostic.maxNumber` must be
a positive integer. The limit includes the final truncation marker.
`workspace.rebuildDebounce` keeps its previous value when omitted or invalid.
`suggest.excludeRuntimePath` is a boolean workspace setting: empty, missing, or
`null` values mean `false`; when enabled, workspace-root files remain eligible
even if they are inside a runtimepath root. `workspace/symbol` independently
excludes files outside workspace roots at all times.

Clients dynamically replace runtimepath with this custom request:

```json
{"jsonrpc":"2.0","id":1,"method":"vimls/didChangeRuntimepath","params":{"runtimepath":["/path/to/vim/runtime"]}}
```

It returns JSON `null`; the notification form remains compatible. See
[Client configuration](docs/configuration.md#custom-request-vimlsdidchangeruntimepath)
for its delta-indexing and error-handling contract.

---

## Documentation

- [Language Server Features](docs/language-support.md)
- [Client Configuration Guide](docs/configuration.md)
- [Architecture & Design](docs/architecture.md)
- [Diagnostic Reference](docs/diagnostics.md)
- [Project Roadmap](docs/roadmap.md)
- [Test Strategy & Oracle](docs/testing.md)

---

## LICENSE

This project is licensed under the **MIT License**. See [LICENSES/MIT.txt](LICENSES/MIT.txt) for full details.

Vim syntax definitions, documentation excerpts, and test metadata derived from the official Vim codebase are subject to the **Vim License**. See [LICENSES/VIM.txt](LICENSES/VIM.txt).
