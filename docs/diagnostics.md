# Understanding diagnostics

vimls-go checks your script while you edit. It reports syntax errors, missing
names, invalid calls and Vim9 type errors that can be determined from source.
It also offers suggestions for maintaining plugins and configuration files.

During typing, analysis yields to completion and waits until 150 ms after the
latest accepted edit before resuming. Scope/reference/type traversals include
batched pause and cancellation checks. Obsolete open-document semantic work is
abandoned after edits or close; same-content work can resume. Diagnostic requests and background work
have independent cancellation; results still require current document and
workspace snapshots before publication.

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

`vim/E174` checks statically repeated user-command definitions without `!`.
A single definition is safe to reload from the same script and receives no hint.

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
| `vim/E521`, `vim/E928`, `vim/E734` | An option assignment requires a number, requires a string, or uses an incompatible assignment operator. Known RHS types also receive the corresponding Vim conversion error; unknown and `any` RHS types are skipped. See [Option assignment errors](#option-assignment-errors) below. |
| `vim/E488` | There is unexpected text after a command or expression. |
| `vim/E492` | The command is invalid or its name is not known. Unknown uppercase user commands are warnings. |

For Vim's explanation of an error, run `:help E117` with the relevant number.
vimls-go follows the current [Vim baseline](language-support.md). Help from
another version can describe different rules.

Vim9 environment variables, registers and writable string `v:` variables require
string values for plain and concatenating assignments. Known incompatible types
report E1012, including in destructuring assignments; `@# = bufnr()` is allowed.
For `[@%] = [1]`, Vim9 scripts and `vim9cmd` report E1012 before checking register
writability, while `def` reports E354. A string value such as `[@%] = ['ok']`
reports E354 in all three contexts.
Legacy assignments retain their conversions. Invalid Vim9 command names such
as `ch_log` report E492 at script level and E476 inside `def`.

Command and autocommand bodies under literal false conditions (including
`if 0` and `while 0`) do not produce expression/type errors from their skipped
text. Unknown conditions retain diagnostics. Missing closing delimiters and
following statements remain checked. In user-command templates, multiline
`<q-args>` and other placeholders remain unknown until expansion.

Builtin return-value inference also uses the corrections recorded in Vim
v9.2.1104, checked against v9.2.1132, for functions already in the pinned metadata.
For example, assigning `match()` to a string produces E1012, while
`submatch(0, true)` is accepted as `list<string>`. A default value alone does not
establish the type of `get()`: unknown elements or a different default type keep
the result unknown and avoid an unsupported type-mismatch diagnostic.

Type checks also use statically known exported variable and constant types from
ordinary and autoload imports once their targets are indexed, including values
re-exported through other files. Named types are distinguished by their defining
file, with known inheritance, interface and type-alias relationships respected.
Autoload imports receive static type errors without waiting for Vim to load the
script. Missing or ambiguous targets and circular inference remain conservative.

In `def` functions and Vim9 lambdas, a `type(value) == v:t_string` guard does
not change an `any` value's compiled type. A variable initialized from that
value remains `any`, so assigning a number to the copy does not cause E1012.
Interpreted Vim9 script and Legacy guard inference retain their runtime-value
rules; a script-level copy of a string can still receive E1012 on reassignment.

Builtin calls in default parameter values and enum constructor arguments receive
the same argument type checks as other calls in their Vim9 context.

Null-receiver checks (`vim/E1360`) cover literal null objects and variables
known to hold null objects without reassignment. For variables with visible
writes, a limited source-order check also catches reads after their null
declaration and before a write or an operation that could change their value.
Later assignments and merely defining a function, lambda or user command do not
erase an earlier null read. Assignment checks its reads before invalidating the
target's null state.

This source-order check does not follow calls or merge branch states. Calls,
loops, exception handling and commands with unmodeled effects discard its
temporary facts. Deferred bodies start without the enclosing sequence's
temporary facts; a captured variable with any visible write remains unknown
there. Assignments do not establish new null or non-null facts, and these checks
do not change inferred types.

In Vim9, a direct `if` or `elseif` condition can suppress E1360 for the same
variable inside its guarded branch. The supported non-null conditions are
`x != null_object`, `x isnot null_object`, `x != null`, and `instanceof(x, C)`
with exactly one statically known local class `C`. An `else` paired with an
`if` using `x == null_object`, `x is null_object`, or `x == null` also protects
the variable, provided there is no `elseif`. Parentheses, reversed comparisons
and logical negation are supported. `x is null`, `x isnot null`, and
`instanceof` with `null_class`, multiple classes, aliases, interfaces or imported
classes do not establish a guard.
Malformed conditional headers and unclosed `if` blocks do not establish guards.

Guards follow declaration identity, apply only inside the branch, and do not
carry into deferred function, lambda, user-command, autocommand or mapping
bodies. There is no inference from short-circuit expressions, loops or early
returns, and no tracking of dynamic rebinding through `execute` or unresolved
calls. This only suppresses null-receiver diagnostics: declared types, member
lookup, and compiled E1325/E1013 checks remain unchanged. Completion separately
uses branch-local type facts as described in [language support](language-support.md).

## Option assignment errors

These diagnostics follow the current [Vim baseline](language-support.md) for
direct option assignments, except for the simplified boolean
compound-assignment policy below.
Known options are assumed to exist regardless of build-feature conditions.
Unknown RHS types and Vim9 `any` skip type checks; value-dependent errors require
statically known values.

| Code | Meaning / example |
| --- | --- |
| E474 | Invalid option-setting syntax, e.g. `set hidden=abc`. |
| E521 | Number required, e.g. `set history=abc` or `let &hidden = 'abc'`; also invalid boolean option compound assignment in a Vim9 `def`. |
| E928 | String required, e.g. `let &titlestring = v:true`. |
| E734 | Assignment operator incompatible with the option, e.g. `let &titlestring += 1`. |
| E703 / E729 | Using a Funcref as a Number / String. |
| E745 / E730 | Using a List as a Number / String. |
| E728 / E731 | Using a Dictionary as a Number / String. |
| E974 / E976 | Using a Blob as a Number / String. |
| E805 | Using a Float as a Number. |
| E611 | Using a Special as a Number. |
| E1138 | Using a Bool as a Number. |
| E1023 | Using a Number other than 0 or 1 as a Bool. |
| E1012 | Type mismatch in a Vim9 compiled assignment. |
| E1019 | Concatenating to a non-string option in Vim9 compiled code. |
| E1051 / E1035 / E1036 | Invalid operands for compiled arithmetic assignment. |

The error code depends on Legacy, Vim9 script or `def` context. As a deliberate
simplification, invalid boolean option compound assignments in a Vim9 `def`
uniformly report E521 instead of Vim's RHS-dependent type and operator codes.
Unknown and `any` RHS types still skip this check.

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
| `vimls/abbreviated-option` | Hint | An abbreviated option name was used instead of the canonical full name. |
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
| `vimls/encoding-after-scriptencoding` | Warning | Set 'encoding' before ':scriptencoding'; setting 'encoding' after ':scriptencoding' may corrupt character conversion. |
| `vimls/explicit-local-scope` | Hint | An explicit local scope would make this variable's role clearer. |
| `vimls/function-without-abort` | Hint | The Legacy function does not use `abort`. |
| `vimls/global-empty-pattern` | Warning | An empty pattern in `:global` or `:vglobal` reuses the previous search pattern from interactive history. |
| `vimls/global-internal-state` | Hint | A short global variable looks like internal plugin state. |
| `vimls/implicit-pattern-case` | Hint | Pattern matching depends on the user's `ignorecase` option; Vim9 `=~` and `!~` comparisons are exempt. |
| `vimls/implicit-regex-magic` | Hint | Pattern interpretation depends on the user's `magic` option. |
| `vimls/implicit-string-case` | Hint | String comparison depends on the user's `ignorecase` option. |
| `vimls/mapping-script-local-reference` | Warning | An `s:name` identifier in a mapping may not be available when it runs; literal substrings such as `align-items:` are ignored. |
| `vimls/mapping-without-unique` | Hint | The mapping may replace an existing mapping. |
| `vimls/match-command` | Hint | `:match` uses shared slots; plugin code may prefer `matchadd()`. |
| `vimls/missing-option-value` | Warning | In configuration files, a bare `:set option` displays the current value of a number or string option; it does not assign a new one. |
| `vimls/macvim-only-option` | Hint | This option setting is specific to MacVim. Protect it with `has('gui_macvim')`; definite invalid values still produce errors. See [MacVim compatibility](language-support.md#macvim-option-compatibility). |
| `vimls/neovim-only-option` | Hint | This option setting is specific to Neovim. Protect it with `has('nvim')`; settings rejected by both editors still produce errors. See [Neovim option compatibility](neovim.md#option-compatibility). |
| `vimls/normal-without-bang` | Warning | `:normal` may invoke user mappings; `:normal!` avoids that. |
| `vimls/recursive-map` | Warning | The mapping may expand other mappings. |
| `vimls/set-nomagic` | Warning | Disabling 'magic' breaks plugins because most patterns assume 'magic' is on; use `\M` instead. |
| `vimls/set-vs-setlocal` | Warning | The option assignment may change a global default. |
| `vimls/substitute-empty-pattern` | Warning | An empty pattern in `:substitute` reuses the previous search pattern from interactive history. |
| `vimls/substitute-gdefault` | Hint | The `:substitute` command may behave unexpectedly if the user has configured `gdefault`. |

### Names and unused code

| Code | Default | Meaning |
| --- | --- | --- |
| `vimls/autoload-function-not-found` | Warning | The autoload function was not found in indexed runtime files. |
| `vimls/global-function-not-indexed` | Hint | The global function was not found in the workspace index. |
| `vimls/unknown-autocmd-event` | Hint | The event name is not recognized. Dynamic groups and `User` events need special care. |
| `vimls/unused-variable` | Hint | A Vim9 variable is declared but not used. |
| `vimls/variable-type-change` | Warning | Consecutive simple Legacy assignments change a variable's known basic type. |
| `vimls/deprecated` | Hint | The referenced symbol is marked deprecated. |

`vimls/variable-type-change` marks the later assignment and links to the previous
one. It compares basic types, so changing List or Dictionary element types does
not warn. Either `unknown` or `any` clears the previous type: a sequence such as
`string → unknown → number` does not warn. Tracking is limited to runs of simple
Legacy `let` assignments in the same scope. Calls, other commands and control-flow
boundaries clear the history; deferred command payloads are excluded. Vim9 keeps
its existing type diagnostics without this additional warning.

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

## Target Vim version diagnostics

When a target Vim version is configured via startup options (e.g. `vimVersion`
in `initializationOptions`), vimls-go checks referenced language features
introduced after Vim v9.0.0000 against that target version. If a referenced
feature is newer than the target version, vimls-go emits a standard Vim error
diagnostic that explicitly states in which Vim version it was added:

| Feature kind | Code | Message format / example |
| --- | --- | --- |
| Ex command | `vim/E492` | `Not an editor command: defer (added in Vim 9.0.0370)` |
| Option in `:set` | `vim/E518` | `Unknown option: smoothscroll (added in Vim 9.0.0640)` |
| Option in expressions (`&opt`) | `vim/E113` | `Unknown option: smoothscroll (added in Vim 9.0.0640)` |
| Builtin function | `vim/E117` | `Unknown function: indexof (added in Vim 9.0.0196)` |
| Autocommand event | `vim/E216` | `No such group or event: WinResized (added in Vim 9.0.0917)` |

These diagnostics use precomputed semantic references and command indexes without
redundant AST expression traversals. User-declared variables, functions, and
custom commands are disambiguated and never confused with newer built-in features.
