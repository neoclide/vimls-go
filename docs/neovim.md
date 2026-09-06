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
