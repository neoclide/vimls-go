# Understanding diagnostics

vimls-go checks your script while you edit. It reports syntax errors, missing
names, invalid calls and Vim9 type errors that can be determined from source.
It also offers suggestions for maintaining plugins and configuration files.

It does not run the script. A clean diagnostics list cannot prove that code
depending on editor state, dynamic names or loading order will work at runtime.

## Errors, warnings and hints

- **Errors** usually mean the source is invalid, such as an unfinished block or
  a known type mismatch.
- **Warnings** include names the server cannot find and code whose behavior
  may depend on other scripts.
- **Hints** cover unused variables, deprecated references and style suggestions.
- **Information** messages can appear while an expression is incomplete or
  exceeds an analysis limit.

These are defaults. Your settings and the file's
[configuration role](userconfig.md) can change a message's severity.

Vim errors use codes such as `vim/E117`. Server-specific suggestions use names
such as `vimls/unused-variable`. Use the complete code when configuring them.

## Common problems

| Code | What to check |
| --- | --- |
| `vim/E117` | The function name could not be found. Check spelling, script scope and runtimepath. |
| `vim/E121`, `vim/E1001`, `vim/E1089` | A Vim9 variable or assignment target is unknown. Check its declaration and scope. |
| `vim/E118`, `vim/E119` | A function received too many or too few arguments. |
| `vim/E1012` | The value's type does not match the expected type. |
| `vim/E46`, `vim/E1018`, `vim/E741`, `vim/E742` | A read-only binding or locked value is being changed. The exact code depends on the context. |
| `vim/E113`, `vim/E518` | The option name is unknown. |
| `vim/E474`, `vim/E487`, `vim/E539` | A supported option-value check found an invalid value, number or flag. Dynamic values are not fully checked. |
| `vim/E488` | There is unexpected text after a command or expression. |
| `vim/E492` | The command is invalid or its name is not known. Unknown uppercase user commands are warnings. |

For Vim's explanation of an error, run `:help E117` with the relevant number.
vimls-go follows **Vim v9.2.1015**; help from another version can describe
different rules.

For example, this Vim9 assignment has a known type mismatch:

```vim
vim9script
var count: number = 'three'
```

Unresolved names are less certain. Functions and variables can be supplied by
plugins or created dynamically, so `E117`, `E121`, `E1001` and `E1089` are
reported as warnings. Missing-function checks wait for workspace and runtimepath
source indexing. Unknown uppercase commands also wait for runtime help, whose
command tags can establish a known name.

## Adjusting the messages

Use `vim.diagnostic.disabled` to hide a code, or
`vim.diagnostic.override` to change its severity. For example, in coc.nvim:

```json
{
  "vim.diagnostic.disabled": ["vimls/explicit-local-scope"],
  "vim.diagnostic.override": {
    "vimls/unused-variable": "information"
  }
}
```

Disabling a code wins over an override. Use `error`, `warning`,
`information` or `hint` as the severity; `off` is not a severity.
These settings apply while the server is running. Other clients use the same
values in their `vim` settings section; see [settings](configuration.md#settings).

A file normally shows at most 1,000 diagnostics, including any truncation notice.
You can change this with `vim.diagnostic.maxNumber`. If a message seems wrong,
a bug report with its full code and a small reproducing script is more useful
than a screenshot alone.

## Server-specific codes

The following are default severities. Configuration files suppress or lower
some plugin-oriented suggestions, as described in [the vimrc guide](userconfig.md).

### Plugins, mappings and configuration

| Code | Default | Meaning |
| --- | --- | --- |
| `vimls/autocmd-group-not-cleared` | Warning | Reloading may add another copy of an autocommand. |
| `vimls/autocmd-outside-augroup` | Warning | An autocommand has no group to manage or clear it. |
| `vimls/catch-error-message` | Warning | The catch pattern depends on error prose. Vim error codes and catch-all patterns are exempt. |
| `vimls/complex-autocmd` | Hint | Consider moving a long autocommand body into a function. |
| `vimls/complex-command` | Hint | Consider moving a long user-command body into a function. |
| `vimls/config-loaded-guard` | Hint | A loaded guard may skip your changes when the configuration is sourced again. |
| `vimls/config-mapleader-order` | Warning | Define the leader before mappings that use it. |
| `vimls/configuration-overwrite` | Warning | An unconditional assignment may replace a user's setting. |
| `vimls/direct-user-keymap` | Hint | A plugin can expose a `<Plug>` mapping so users can choose their own keys. |
| `vimls/duplicate-mapping` | Warning | A later mapping replaces an earlier mapping for the same key. |
| `vimls/echoerr` | Hint | This command deliberately raises an error. |
| `vimls/explicit-local-scope` | Hint | An explicit local scope would make this variable's role clearer. |
| `vimls/function-without-abort` | Hint | The Legacy function does not use `abort`. |
| `vimls/global-internal-state` | Hint | A short global variable looks like internal plugin state. |
| `vimls/implicit-pattern-case` | Hint | Pattern matching depends on the user's `ignorecase` option. |
| `vimls/implicit-regex-magic` | Hint | Pattern interpretation depends on the user's `magic` option. |
| `vimls/implicit-string-case` | Hint | String comparison depends on the user's `ignorecase` option. |
| `vimls/mapping-script-local-reference` | Warning | A script-local name may not be available when the mapping runs. |
| `vimls/mapping-without-unique` | Hint | The mapping may replace an existing mapping. |
| `vimls/match-command` | Hint | `:match` uses shared slots; plugin code may prefer `matchadd()`. |
| `vimls/missing-option-value` | Warning | In configuration files, a bare `:set option` displays the current value of a number or string option; it does not assign a new one. |
| `vimls/macvim-only-option` | Hint | This option setting is specific to MacVim. Protect it with `has('gui_macvim')`; definite invalid values still produce errors. See [MacVim compatibility](language-support.md#macvim-option-compatibility). |
| `vimls/neovim-only-option` | Hint | This option setting is specific to Neovim. Protect it with `has('nvim')`; settings rejected by both editors still produce errors. See [Neovim option compatibility](neovim.md#option-compatibility). |
| `vimls/normal-without-bang` | Warning | `:normal` may invoke user mappings; `:normal!` avoids that. |
| `vimls/recursive-map` | Warning | The mapping may expand other mappings. |
| `vimls/set-vs-setlocal` | Warning | The option assignment may change a global default. |

### Names and unused code

| Code | Default | Meaning |
| --- | --- | --- |
| `vimls/autoload-function-not-found` | Warning | The autoload function was not found in indexed runtime files. |
| `vimls/global-function-not-indexed` | Hint | The global function was not found in the workspace index. |
| `vimls/unknown-autocmd-event` | Hint | The event name is not recognized. Dynamic groups and `User` events need special care. |
| `vimls/unused-variable` | Hint | A Vim9 variable is declared but not used. |
| `vimls/deprecated` | Hint | The referenced symbol is marked deprecated. |

### Incomplete expressions

These Information messages may disappear as you finish typing.

| Code | Meaning |
| --- | --- |
| `vimls/invalid-atom` | The parser cannot read this expression part. |
| `vimls/invalid-member-tail` | Extra characters follow a member name. |
| `vimls/invalid-parenthesized-expression` | The parentheses need one expression. |
| `vimls/missing-call-comma` | A comma is missing between arguments. |
| `vimls/missing-delimiter` | A closing delimiter is missing. |
| `vimls/missing-expression` | An expression is missing. |
| `vimls/missing-interpolation-end` | An interpolated expression is missing `}`. |
| `vimls/missing-list-end` | A list is not closed. |
| `vimls/missing-member` | A member name is missing. |
| `vimls/missing-method-call` | A callable is missing its argument list. |
| `vimls/missing-ternary-colon` | A conditional expression is missing `:`. |
| `vimls/missing-type` | A Vim9 type is missing. |
| `vimls/trailing-expression` | Extra text follows an expression. |
| `vimls/trailing-type` | Extra text follows a type. |
| `vimls/unexpected-token` | This token is not expected here. |

### Analysis limits

| Code | Default | Meaning |
| --- | --- | --- |
| `vimls/diagnostics-truncated` | Warning | Some diagnostics were omitted to stay within the configured limit. |
| `vimls/file-too-large` | Warning | The file exceeds the 4 MiB analysis limit. |
| `vimls/embedded-command-depth` | Information | Embedded commands are nested too deeply to analyze. |
| `vimls/expression-too-deep` | Information | The expression exceeds the parser's nesting limit. |
| `vimls/type-too-deep` | Information | The type exceeds the parser's nesting limit. |

If ordinary code reaches an analysis limit, include that input in a bug report.
Hiding the message does not remove the limit.
