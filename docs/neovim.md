# Using vimls-go in Neovim

You can use either [coc-vimls](https://github.com/neoclide/coc-vimls) or Neovim's
built-in LSP client. Choose one so that you do not start duplicate servers.

## Built-in client

Install `vimls` as described in the [README](../README.md#install). With
Neovim 0.11 or newer, add this to your Lua configuration after your plugin
manager has set up the runtimepath:

```lua
vim.lsp.config('vimls', {
  cmd = { 'vimls' },
  filetypes = { 'vim' },
  root_markers = { '.git' },
  init_options = {
    runtimepath = vim.api.nvim_list_runtime_paths(),
  },
})
vim.lsp.enable('vimls')
```

Use an absolute path in `cmd` if the executable is not on Neovim's `PATH`.
Open a Vim script and run `:checkhealth vim.lsp` to check that the client
has attached. This setup does not require nvim-lspconfig.

The server settings go under `settings.vim`. For example, add this before
`vim.lsp.enable('vimls')` to hide one style suggestion:

```lua
vim.lsp.config('vimls', {
  settings = {
    vim = {
      diagnostic = {
        disabled = { 'vimls/explicit-local-scope' },
      },
    },
  },
})
```

The full list is in [settings](configuration.md#settings). Key mappings,
completion display and diagnostic appearance are controlled by Neovim; see its
[LSP documentation](https://neovim.io/doc/user/lsp.html).

## Plugins loaded later

The startup example captures runtimepath when it runs. If your plugin manager
adds directories later, send the updated list to the running server:

```lua
for _, client in ipairs(vim.lsp.get_clients({ name = 'vimls' })) do
  client:notify('vimls/didChangeRuntimepath', {
    runtimepath = vim.api.nvim_list_runtime_paths(),
  })
end
```

This is useful for newly loaded plugins. To reload changed help in a directory
that is already indexed, restart the server.

## Neovim-specific Vimscript

The main language target is Vim v9.2.1015. A compatibility list recognizes
some Neovim names, such as `nvim_buf_get_lines()`, `&shada` and `v:lua`, so
they do not receive unknown-name warnings. This is not full Neovim API support:
the list does not provide Neovim function signatures, type checking or a
complete set of API completion items.

Lua code, including `init.lua` and embedded Lua bodies, needs a Lua language
server. Editing Vim9 in Neovim also does not make Neovim able to execute it.

## Option compatibility

A setting accepted only by Neovim receives `vimls/neovim-only-option` (Hint)
unless the surrounding condition guarantees Neovim. For example:

```vim
if has('nvim')
  set signcolumn=auto:2
  set foldcolumn=auto:3
  set laststatus=3
else
  set signcolumn=yes
endif
```

The `else` branch of `if !has('nvim')` is also protected. Parentheses,
negation, comparisons with `0`/`1`, `&&`, `||`, nested branches and `elseif`
are tracked. `has('nvim') || other` does not guarantee Neovim. Functions,
lambdas and parsed embedded command bodies inherit their defining context;
Short-circuit operands
and ternary branches carry the same context for expression analysis.
A script guard such as `if !has('nvim') | finish | endif` also protects
subsequent settings. The same applies to `has('gui_macvim')` conditions.
Finish propagation through loops, try blocks or deferred bodies, variables
containing `has()` results, and `return` flow remain unsupported.
Contradictory guards do not suppress errors.

Reviewed value differences cover `signcolumn`, `foldcolumn`, `cmdheight`,
`laststatus`, `completeopt`, `fillchars`, `jumpoptions` and `cpoptions`.
Known Neovim-only options such as `inccommand`, `pumblend`, `winblend`,
`statuscolumn`, `winbar` and `shada` also receive the guard hint. Full names,
documented short names, `setlocal`/`setglobal` and `&g:`/`&l:` assignments are
recognized. The short name `pb` retains both meanings: Vim's `pumborder` and
Neovim's `pumblend`.

Vim-compatible values retain their existing behavior. Values rejected by both
reviewed grammars, such as `signcolumn=auto:10`, remain errors even inside a
Neovim guard. A guard does not hide unknown-option errors or unrelated
diagnostics. Static list/flag additions via `:set +=` and `^=` are checked;
removals and operations requiring the previous value remain conservative.
Dynamic expressions, escaped values and runtime-dependent formats are not
fully checked. A Neovim-only option name can still receive a Hint when its
assigned value is dynamic. Neovim-only options have semantic highlighting and
hover documentation, including short names. The ambiguous `pb` alias documents
both Vim and Neovim meanings. Completion candidates retain the pinned Vim list;
this does not enable Neovim function diagnostics or API completion.
`laststatus=3` also receives the Hint because its global-statusline meaning is
Neovim-specific, even though Vim accepts the number without an error. Other
numeric values of this permissive Vim option do not receive new range errors.

Compatibility rules use Vim v9.2.1015 and the documented option contracts in
[Neovim snapshot 73923b0dd8](https://github.com/neovim/neovim/blob/73923b0dd85bb936ba2f63ee916dabaa0603340d/runtime/doc/options.txt).
They are a reviewed subset, not a complete version-by-version Neovim model.
For example, the snapshot documents `completeopt=preselect`, while the locally
checked Neovim 0.12.4 executable does not accept it.
