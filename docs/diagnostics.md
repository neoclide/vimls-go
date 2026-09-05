# Static diagnostics

This document records diagnostic semantics that are clearer in Vim's tests and
implementation than in its user documentation. The language server reports a
diagnostic only when the relevant source facts are statically known. Dynamic
function names, externally supplied test functions, and unresolved callable
values remain `unknown`.

The evidence below is pinned to Vim 9.2.1015.

## LSP transport

Clients advertising `textDocument.diagnostic` receive document pull diagnostics
with full/unchanged opaque result IDs. The server advertises
`interFileDependencies: true` and `workspaceDiagnostics: true`; pull clients
do not receive push publications. Older clients retain the existing push
transport. `workspace/diagnostic` reports open and closed Vim files below
workspace roots, uses `version: null` for closed files, accepts per-URI previous
result IDs, and streams deterministic partial-result batches when requested.
External runtimepath-only files are not workspace diagnostic targets. Both pull
requests wait up to one second for active workspace indexing; an incomplete
bounded index is rejected rather than reported as complete. Related documents
and work-done progress are not implemented. When supported, the server requests
a diagnostic refresh after a diagnostic-setting or completed workspace-index
change.

## vimls-owned diagnostic codes

The default severity can be changed for publication with the workspace
setting `vim.diagnostic.override`, whose exact-code values are `error`,
`warning`, `information`, or `hint`. `vim.diagnostic.disabled` suppresses
exact matching codes, and takes precedence over any severity override. These
settings affect only LSP output; parser and analysis diagnostics retain their
original definitions and severities. For example:

```json
{
  "vim": {
    "diagnostic": {
      "disabled": ["vim/E117"],
      "override": {"vimls/deprecated": "warning"}
    }
  }
}
```

Both settings are dynamic workspace configuration. Valid changes reanalyze
open documents; omitted fields in a complete snapshot reset to defaults, and
malformed fields retain their previous values with a client warning. A
missing or null `diagnostic` section is not an error and leaves the settings
at their defaults.

Vim's own diagnostics retain their native `vim/E<number>` identifiers and are
listed in [Vim error code support](vim-error-code-support.md). vimls uses them
whenever Vim 9.2.1015 reports an error for the same source. The following
complete, lexically sorted table is reserved for non-Vim outcomes: analysis
limits, target compatibility, incomplete-input recovery, and conservative
style checks. These are never published as LSP errors; an occurrence can make
the default message more specific without changing its code.

| Code | Default severity | Default message |
| --- | --- | --- |
| `vimls/autocmd-group-not-cleared` | Warning | augroup does not clear existing autocommands |
| `vimls/autocmd-outside-augroup` | Warning | autocommand is not contained in an augroup |
| `vimls/autoload-function-not-found` | Warning | autoload function not found in current runtimepath |
| `vimls/catch-error-message` | Warning | catching human-readable error text is fragile; catch-all patterns and Vim error codes are exempt |
| `vimls/complex-autocmd` | Hint | complex autocommand body; consider delegating to a function |
| `vimls/complex-command` | Hint | complex user command body; consider delegating to a function |
| `vimls/config-loaded-guard` | Hint | a loaded guard skips the rest of the file on a later `:source`; edits below may not take effect |
| `vimls/config-mapleader-order` | Warning | mapping is defined before the leader assignment; `<Leader>` is expanded when the mapping is defined |
| `vimls/configuration-overwrite` | Warning | unconditional configuration assignment may overwrite a user value |
| `vimls/deprecated` | Hint | symbol is deprecated |
| `vimls/diagnostics-truncated` | Warning | additional diagnostics were omitted |
| `vimls/direct-user-keymap` | Hint | user key mapping reduces configurability; consider exposing a `<Plug>` mapping |
| `vimls/duplicate-mapping` | Warning | mapping for the same key is defined more than once; the later definition overwrites the earlier one |
| `vimls/echoerr` | Hint | echoerr always raises an error; use it only for intended failures |
| `vimls/embedded-command-depth` | Information | embedded command nesting exceeds parser limit |
| `vimls/explicit-local-scope` | Hint | use an explicit local scope for this variable |
| `vimls/expression-too-deep` | Information | expression nesting exceeds parser limit |
| `vimls/file-too-large` | Warning | file exceeds the 4 MiB analysis limit |
| `vimls/function-without-abort` | Hint | function does not use abort |
| `vimls/global-function-not-indexed` | Hint | global function not found in workspace index |
| `vimls/global-internal-state` | Hint | short global variable appears to be plugin-internal state |
| `vimls/implicit-pattern-case` | Hint | pattern match depends on 'ignorecase' |
| `vimls/implicit-regex-magic` | Hint | pattern relies on Vim's magic setting |
| `vimls/implicit-string-case` | Hint | string comparison depends on 'ignorecase' |
| `vimls/invalid-atom` | Information | invalid atom |
| `vimls/invalid-member-tail` | Information | member name has trailing characters |
| `vimls/invalid-parenthesized-expression` | Information | parenthesized expression requires one value |
| `vimls/mapping-script-local-reference` | Warning | mapping references a script-local name that may not be available |
| `vimls/mapping-without-unique` | Hint | mapping may overwrite an existing mapping; consider `<unique>` |
| `vimls/match-command` | Hint | :match uses shared match slots; prefer matchadd() in plugin code |
| `vimls/missing-call-comma` | Information | missing comma before call argument |
| `vimls/missing-delimiter` | Information | expected a closing delimiter |
| `vimls/missing-expression` | Information | expected expression |
| `vimls/missing-interpolation-end` | Information | expected } in interpolated string |
| `vimls/missing-list-end` | Information | missing end of list |
| `vimls/missing-member` | Information | expected member name |
| `vimls/missing-method-call` | Information | expected argument list after callable |
| `vimls/missing-option-value` | Warning | option requires a value; :set without an operator displays the current value |
| `vimls/missing-ternary-colon` | Information | expected : in ternary expression |
| `vimls/missing-type` | Information | expected Vim9 type |
| `vimls/normal-without-bang` | Warning | :normal may invoke user-defined mappings; prefer :normal! |
| `vimls/recursive-map` | Warning | mapping may recursively expand user mappings |
| `vimls/set-vs-setlocal` | Warning | :set may modify a global option; consider :setlocal |
| `vimls/trailing-expression` | Information | unexpected text after expression |
| `vimls/trailing-type` | Information | unexpected text after type |
| `vimls/type-too-deep` | Information | type nesting exceeds parser limit |
| `vimls/unexpected-token` | Information | unexpected token in expression |
| `vimls/unknown-autocmd-event` | Hint | unknown autocommand event |
| `vimls/unused-variable` | Hint | variable is declared but never used |

The source-of-truth table is `internal/syntax/vimls_diagnostics.go`. New
vimls-owned diagnostics must be added there before they are emitted.

## Read-only variables: E46

E46 is Vim's historical read-only-binding error. The analyzer reports
`Cannot change read-only variable "{name}"` for a direct assignment to a
known read-only `v:` value in Legacy Vim script, Vim9 script, or a compiled
`def`. It also reports E46 for direct `=` rebinding of a lexically resolved
top-level Vim9 `const` or `final`, and for direct assignment to a Legacy
function argument such as `a:value` or `a:000`.

Immutability does not imply E46 in every context. Direct rebinding of a local
`const` or `final` inside a compiled `def` uses E1018. Mutating a locked value
or a fixed container can use E741 or E742, and member, index, compound, or
dynamically resolved assignments remain on their own diagnostic paths. The
analyzer therefore requires both a statically resolved binding and the exact
assignment context before emitting E46.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:2545-2549` uses E46 for assignments to
  `v:true`, `v:false`, `v:null`, and `v:none` in both a `def` and Vim9 script.
- `src/testdir/test_vim9_script.vim:1814` uses E46 when a top-level Vim9
  `const` is rebound.
- `src/testdir/test_vim9_script.vim:3090-3094` distinguishes E1018 in a
  compiled `def` from E46 at Vim9 script level.
- `src/testdir/test_listdict.vim:974-977` distinguishes rebinding the Legacy
  variadic argument list (`a:000`, E46) from mutating its fixed contents
  (E742).
- `src/errors.h:124-126` defines the unnamed and named E46 messages.

## Vim9 parameter rebinding: E1090

Vim reports `E1090: Cannot assign to argument {name}` for direct `=` and
compound reassignment of a `def` or Vim9 lambda parameter. The analyzer emits
E1090 only for a resolved direct identifier target. Index and member mutation
of a List or Dictionary argument is allowed, while Legacy `a:` argument
rebinding retains its E46 diagnostic.

Representative source evidence, pinned to Vim 9.2.1015:

- `src/vim9compile.c:2360-2380` checks direct argument assignment after
  parsing the assignment left-hand side.
- `src/testdir/test_vim9_func.vim:2143-2152` permits List/Dictionary contents
  to change and reports E1090 for direct parameter rebinding.
- `runtime/doc/userfunc.txt:240-245` describes fixed argument bindings and
  mutable composite contents.

## Nested Vim9 redirection: E1092

Vim reports `E1092: Cannot nest :redir` when a compiled `def` encounters any
second `:redir` command while variable redirection is open. Only `redir END`
closes that state. Syntax analysis tracks it per enclosing `def`, reports the
nested command name, and retains the original open state so a later END still
recovers normally.

The rule starts from the Vim9 `redir => target` and `redir =>> target` forms.
An ordinary file or register redirection without an open compiled variable
redirection, and Legacy-dialect redirection, remain outside this diagnostic.

Representative source evidence, pinned to Vim 9.2.1015:

- `src/vim9cmds.c:2571-2615` accepts END for an open compiled redirection and
  otherwise emits E1092 before parsing the nested command's arguments.
- `src/testdir/test_vim9_cmd.vim:1999-2007` opens variable redirection, nests
  `redir > Xnopfile`, and expects E1092.
- `runtime/doc/various.txt:610-620` documents variable redirection and the
  E1092 marker.

## Function-local import: E1094

Vim reports `E1094: Import can only be used in a script` when `:import` appears
inside a `def` or `function`. Syntax analysis selects the command name and
retains the Import AST for recovery and editor features, but does not cascade
path, alias, or expression diagnostics from a command Vim rejects before
parsing its import arguments.

Top-level imports continue through the normal Vim9 path and alias rules. A
Legacy-root file is not rejected solely for retaining an import command; the
placement diagnostic applies only when the command is nested in a callable
body.

Representative source evidence, pinned to Vim 9.2.1015:

- `src/vim9compile.c:4638-4645` emits E1094 immediately for CMD_import while
  compiling a `def`.
- `src/vim9script.c:646-656` applies the same error when `:import` executes
  outside script sourcing.
- `src/testdir/test_vim9_import.vim:499-506,549-556` covers misplaced imports
  followed by assignment and call uses.
- `runtime/doc/vim9.txt:3465-3472` documents the `function` and `def` scope
  restriction.

## Unreachable Vim9 code: E1095

Vim reports `E1095: Unreachable code after :return` or `E1095: Unreachable
code after :throw` when compiled Vim9 code continues after a command that
cannot fall through. Analysis reports the first unreachable command in each
Vim9 script, `def`, or Vim9 lambda command sequence. Legacy `function` bodies
remain outside this diagnostic.

The same check applies after a complete `if`/`else` whose branches all
terminate and after the statically provable `try`/catch forms already used by
the missing-return analysis. Structural branch and closing commands are not
reported as unreachable. Incomplete blocks, syntax-error spans, loops, and
dynamic control flow remain conservative instead of inventing reachability.

Representative source evidence, pinned to Vim 9.2.1015:

- `src/vim9compile.c:4561-4585` rejects the next non-structural command after
  the compiler records a return or throw and preserves throw state across an
  `if`/`else` ending.
- `src/testdir/test_vim9_func.vim:534-541` expects E1095 when all branches
  return before another `:return`.
- `src/testdir/test_vim9_script.vim:1101-1109,1198-1210` covers code after a
  throw inside `try` and after an all-return `try`/catch.
- `runtime/doc/userfunc.txt:198-210` documents the Vim9-only unreachable-code
  rule and its lambda example.

## Unknown options: E113 and E518

Vim uses different native error codes for an unknown option according to the
syntax that performs the lookup.

| Code | Static context | Message |
| --- | --- | --- |
| `E113` | An option expression or assignment such as `&missing`, `&g:missing`, or `&l:missing` | `Unknown option: {name}` |
| `E518` | An operand of `:set`, `:setlocal`, or `:setglobal` | `Unknown option: {name}` |

Both codes predate Vim9 script and remain in use there. The analyzer resolves
documented long and short option names from the pinned Vim option table. An
unknown `t_` terminal option remains conservative `unknown`, because terminal
codes can be created or removed at runtime and vary by build and terminal.

The previous internal `vimls/unknown-option` warning is not used for these
cases. Proven option failures use Vim's native error code and error severity.

Representative source evidence:

- `src/errors.h:274-277` defines E113; `src/testdir/test_let.vim:279-283`
  covers legacy option expressions.
- `src/testdir/test_vim9_assign.vim:188,1589` and
  `src/testdir/test_vim9_expr.vim:4173` show that Vim9 retains E113.
- `src/errors.h:1313-1316` defines E518; `src/testdir/test_options.vim:885-905`
  covers unknown operands for all three `:set` variants.

## Unresolved functions and variables

Vim uses E117 for a direct function call whose name cannot be found. The
language server checks unscoped lowercase calls and explicit script-local calls
(`s:Name()` and `<SID>Name()`) against builtins, lexical callables and same-file
function declarations. Script-local declarations may appear after the call.
The same check applies to parsed mapping command bodies and `<expr>` mappings.
For `nmap <leader>sr :call <SID>SessionReload()<CR>`, a missing declaration
produces `Unknown function: s:SessionReload` on `SessionReload`; `<SID>` is a
script namespace prefix, not part of the function identifier. Other undecoded
mapping key notation remains conservative and may suppress body diagnostics.
Globally scoped calls such as `g:Dynamic()`, autoload calls such as
`plugin#Dynamic()`, and dynamic member calls retain their separate conservative
rules because their target may be supplied dynamically.

The server defers E117, global-function-not-indexed hints and autoload-function-not-found
warnings until both workspace and runtimepath source indexes are complete.
Workspace installation alone is insufficient while runtimepath scanning is pending.
Index installation recomputes open-document diagnostics; unrelated local diagnostics
remain available during indexing.

A direct function name of 200 bytes or more has a context-specific result.
Compiling it inside a Vim9 `def` produces E1011, while the same unresolved call
at Vim9 script level produces E117. The analyzer preserves this distinction.
Functions whose availability depends on Vim build features also remain outside
the E117 rule unless the server has matching feature information.

E121 is the Vim9 script-level error for an undefined variable read. A missing
unscoped variable or unknown `v:` variable produces
`E121: Undefined variable: {name}` when evaluated at script level; compiling
the same read in a `def` or lambda produces E1001 instead. The unsupported
`a:`, `l:`, and `x:` namespaces follow the same contextual split: Vim9 script
uses E121, while a compiled expression uses E1075. Ordinary undeclared
assignment targets have their separate E1089 rule.

Legacy Vim script and externally scoped Vim9 names such as `g:`, `s:`, `b:`,
`w:`, and `t:` remain conservative `unknown`, because another script or the
editor can create them dynamically. For a member or index expression, only an
unresolved unscoped root or index expression is diagnosed.

Unresolved-symbol diagnostics E117, E121, E1001, and E1089 are always reported
as LSP warnings. Their native Vim codes and messages stay unchanged.

Representative source evidence:

- `runtime/doc/map.txt`, `script-local`, defines `<SID>` in mappings as the
  script-local identity used by `s:` function declarations.
- `src/errors.h:284-285` defines E117.
- `src/errors.h:292-295` defines E121.
- `src/testdir/test_vim9_expr.vim:3885-3897` shows that an unscoped call does
  not resolve a `g:` function.
- `src/testdir/test_vim9_expr.vim:4179-4181` distinguishes E1075 in a `def`
  from E121 at Vim9 script level for unsupported namespaces.
- `src/testdir/test_vim9_expr.vim:4491-4493` distinguishes E1001 in a `def`
  from E121 at script level for unknown `v:` variables.
- `src/testdir/test_vim9_expr.vim:4498-4499` distinguishes the long-name
  E1011/E117 pair and covers an ordinary missing function.

## Non-callable values: E1085

Vim reports `E1085: Not a callable type: {name}` when compiling a Vim9 call
whose callee has a statically known non-`func` type. The analyzer reports this
for the callee span and stops call diagnostics for that expression. Unknown
and `any` callees remain conservative `unknown`; known `func` values continue
through the existing argument-count and argument-type diagnostics.

Representative source evidence, pinned to Vim 9.2.1015:

- `src/vim9instr.c` `generate_PCALL` selects E1085 for a non-callable type.
- `src/testdir/test_vim9_script.vim:210-211` covers number and string variable
  calls compiled in a `def`.

## Script cannot import itself: E1088

Vim reports `E1088: Script cannot import itself` after a static import resolves
to the importing script. The language server compares the canonical realpath
identities already retained by the workspace import graph, so equivalent path
spellings and symbolic links cannot hide a self-import. The diagnostic selects
the import path expression.

Dynamic or unresolved import paths remain conservative `unknown`; analysis
does not execute an expression to guess its target. E1088 takes precedence over
ordinary missing-import diagnostics once the target identity is known.

Representative source evidence, pinned to Vim 9.2.1015 (`5ab969f`):

- `src/vim9script.c:528-538` rejects an imported script ID equal to the current
  script ID after resolving the file.
- `src/testdir/test_vim9_import.vim:574-582` writes and sources a script that
  imports itself and asserts E1088.
- `runtime/doc/vim9.txt:3577-3584` documents the self-import failure.

## Runtime sign lookup: E155

E155 means `Unknown sign: {name}`. The language server does not emit it from
source alone because the result depends on Vim's mutable sign-definition
registry. For example, Vim9 script converts the number in
`sign_undefine([1])` to the sign name `"1"`; the call succeeds if that sign was
defined earlier and reports E155 only when it is absent. Static analysis cannot
choose between those states.

A compiled `def` is different: its typed call rejects `list<number>` before
consulting the registry and uses E1013. The analyzer retains that compile-time
type diagnostic while leaving the script-level call unknown.

Representative source evidence:

- `src/errors.h:365-366` defines E155.
- `src/sign.c:1129-1143` shows that E155 depends on `sign_find()` and the
  current registry.
- `src/testdir/test_vim9_builtin.vim:4299-4301` distinguishes E1013 in a `def`
  from the registry-dependent E155 at script level.

## Function argument count: E118 and E119

E118 and E119 predate Vim9 script, but Vim still uses them in both legacy Vim
script and selected Vim9 contexts.

| Code | Vim message | Static meaning |
| --- | --- | --- |
| `E118` | `Too many arguments for function: {name}` | A statically resolved call supplies more arguments than the callable can accept. |
| `E119` | `Not enough arguments for function: {name}` | A statically resolved call supplies fewer arguments than the callable requires. |

For a user-defined function, the minimum argument count stops at the first
default parameter or variadic parameter. A legacy `...` parameter and a Vim9
`...name: list<T>` parameter remove the upper bound, but they do not satisfy
required parameters that precede them. A non-variadic function's maximum is
the number of declared parameters. Supplying `v:none` for a Vim9 default
parameter still counts as supplying that argument; Vim then uses the default
value.

Method-call syntax counts the receiver as an argument. For example, the
receiver in `value->Callback(extra)` is the first argument passed to
`Callback`. Built-in functions that invoke callbacks also have their own
implicit callback arguments. `indexof()` passes an index and a value, so a
callback that accepts only one argument receives E118 when this mismatch is
reported from a Vim9 script.

Vim9 does not use E118/E119 for every argument-count failure. Context-specific
diagnostics take precedence. In particular, the same invalid `indexof()`
callback is E176 while compiling a `def`, but E118 at Vim9 script level. The
analyzer must preserve that distinction instead of replacing every callable
signature mismatch with E118 or E119.

Representative source evidence:

- `src/testdir/test_user_func.vim:113-137` covers legacy default parameters and
  `...` arguments.
- `src/testdir/test_method.vim:106-116` proves that a method receiver
  participates in E118/E119 argument counting.
- `src/testdir/test_vim9_func.vim:784-801` covers Vim9 default parameters.
- `src/testdir/test_vim9_func.vim:985-986` covers statically resolved nested
  `def` calls.
- `src/testdir/test_vim9_func.vim:1593-1664` covers direct and method-style
  lambda calls.
- `src/testdir/test_vim9_func.vim:1923-1952` covers required, default, and
  variadic Vim9 parameters together.
- `src/testdir/test_vim9_builtin.vim:2343-2351` distinguishes E176 in a `def`
  from E118 at Vim9 script level for an `indexof()` callback.

## Invalid argument count: E176

Vim's message is `Invalid number of arguments`. E176 predates Vim9 script and
has more than one static use, so the code alone does not identify a callable
invocation error.

A legacy or Vim9 `:command` definition produces E176 when its `-nargs` value is
not one of `0`, `1`, `*`, `?`, `+`, or `_`. This validates the argument-count
specification itself; it does not count arguments in a later invocation of the
user command.

Vim also uses E176 while compiling a `def` when `map()`, `filter()`,
`foreach()`, or `indexof()` receives a statically known callback with an
incompatible number of declared argument slots. These callbacks are invoked
with an index or key and a value. A callback with exactly two slots is
accepted. A single variadic slot is also accepted, because it can receive both
values. More than two declared slots are rejected even when the last slot is
variadic. Argument-type and return-type mismatches use their own diagnostics
and are not E176.

The equivalent callback mismatch does not unconditionally use E176 outside a
compiled `def`. Depending on direction and context, Vim9 script uses E118,
E1106, or E1190. Static analysis therefore applies E176 only to the exact
contexts above.

Representative source evidence:

- `src/usercmd.c:1060-1094` defines the accepted `-nargs` values and emits E176
  for every other value; `src/testdir/test_usercommands.vim:300-316` covers the
  legacy failure.
- `src/evalfunc.c:746-778` checks the callback argument slots used by
  `map()`/`filter()`-style functions.
- `src/testdir/test_vim9_builtin.vim:2799-2808` distinguishes E176 in a `def`
  from E1190 at Vim9 script level.
- `src/testdir/test_vim9_func.vim:4397-4406` distinguishes E176 in a `def` from
  E1106 at Vim9 script level.

## Invalid argument: E475

E475 is Vim's historical general-purpose `Invalid argument: {value}` error,
not a single Vim9 type rule. The language server emits it only for
command-specific forms whose invalid value is present directly in the source.

The official compile fixture includes a top-level Vim9 declaration such as
`var $VAR: number`: an environment variable cannot have a type-only
declaration without a value, so Vim9 script reports E475. The equivalent
declaration in a `def` uses the context-specific E1016 instead.

At Vim9 script root, a simple literal flags argument to `searchpair()` or
`searchpairpos()` is also statically checkable. These functions accept `b`,
`c`, `m`, `n`, `r`, `s`, `w`, `W`, and `z`; `n` and `s` cannot be combined.
Unsupported flags, including the generic search flags `e` and `p`, produce
E475 on the literal. Dynamic strings remain unknown. A skip expression string
is compiled only inside a `def`; its unresolved names use E1001 and do not turn
into script-level E475 diagnostics.

Representative source evidence:

- `src/testdir/test_vim9_assign.vim:217` distinguishes E1016 in a `def` from
  E475 at Vim9 script level for the environment-variable declaration.
- `src/evalfunc.c:10672-10716` parses search flags, and
  `src/evalfunc.c:11116-11127` rejects flags that `searchpair()` cannot use.
- `src/testdir/test_vim9_builtin.vim:3924` distinguishes E1001 while compiling
  the skip expression in a `def` from E475 for the invalid script-level flags.
- `src/testdir/test_search.vim:438-453` covers invalid literal flags for both
  `searchpair()` and `searchpairpos()`.

## Invalid command: E476

E476 means `Invalid command: {command}`. Vim uses it while compiling a `def`
when text at command position cannot be interpreted as a valid Vim9 command.
The parser reports it only when that conclusion is independent of Vim's
mutable user-command registry. Supported forms include a complete typed
assignment that omitted `var`, a lowercase name that is neither a builtin nor
a possible user command, and an invalid form of a known builtin command.

The same source text does not always use E476 at script level. An invalid Ex
command uses E492 there, while `call Name (` uses E1068 for the whitespace
before the argument list. The parser keeps that context distinction instead
of treating E476 as a generic unknown-command code.

The builtin Ex command table is complete for the pinned Vim release. The
user-command table comes from the immutable workspace snapshot and the
statically indexed external runtime sources (`plugin`, `autoload`, and
`import`). Runtime help Ex-command tags also contribute known names. Capitalized
command candidates remain opaque syntax; after the source index and help
catalog are ready, unresolved user commands receive an E492 warning. Builtin
commands such as `Print` are not affected.

Representative source evidence:

- `src/testdir/test_vim9_assign.vim:3110-3133` distinguishes E476 in a `def`
  from E492 at script level for `MyVar: string = 'abc'` without `var`.
- `src/testdir/test_vim9_cmd.vim:2076` distinguishes E476 in a `def` from E492
  at script level for declaration-like `name:replacement` text.
- `src/testdir/test_vim9_func.vim:781` distinguishes E476 in a `def` from E1068
  at script level for `call Name (`.
- `src/testdir/test_vim9_script.vim:4836-4841,4881` covers lowercase and known
  builtin command-position forms and their E476/E492 context split.

## Trailing characters: E488

E488 means `Trailing characters: {text}`. It is used when Vim has already
recognized an expression or command but finds source text that cannot belong
to it. This differs from E476 and E492, which reject the command itself.

Most supported E488 cases are diagnosed directly by the Vim9 parser, including
extra declaration text, invalid condition tails, unmatched delimiters, and
text attached to a command without the required separation. Analysis adds two
cases whose validity depends on expression meaning. A literal `\=` replacement
passed to `substitute()` is parsed as a Vim9 expression and reports E488 for a
trailing token. At script level, member syntax on a statically known String
also reports E488; compiling the corresponding expression in a `def` uses the
type-specific E1229 instead.

Dynamic replacement strings and receivers with unknown types remain unknown.

Representative source evidence:

- `src/testdir/test_vim9_builtin.vim:4641` covers trailing text inside a
  `substitute()` replacement expression in both a `def` and Vim9 script.
- `src/testdir/test_vim9_expr.vim:4188` distinguishes E1229 in a `def` from
  E488 at script level for member syntax on a String.
- `src/testdir/test_vim9_assign.vim:1202` and
  `src/testdir/test_vim9_cmd.vim:472` cover parser-level expression and command
  tails.

## Not an editor command: E492

E492 means `Not an editor command: {command}`. Vim reports it only after text
at command position fails to resolve to either a builtin Ex command or a user
command. In a compiled `def`, the corresponding statically invalid command
usually uses E476 instead.

The parser reports E492 only for source shapes that do not require the mutable
user-command registry. Current Vim9 support covers a complete typed assignment
that omitted `var`, the lowercase declaration-like `notexist:repl` form, and
the pinned builtin-command forms `ka`, `:1ka`, and `mode 4`. Arbitrary unknown
legacy commands and dynamically executed strings remain opaque. Capitalized
user-command calls are checked separately after the workspace/runtime source
indexes are both complete and runtime help collection has finished. An exact name from
an explicit `:command` definition or an Ex-command help tag (such as
`*:CocRestart*`) is accepted; known abbreviations retain the E464 warning.
Otherwise E492 is reported as a **warning** on the command name, because
dynamic command creation may still make it available at runtime. Help completion
refreshes diagnostics, and removing a help root removes its known commands.
This warning can be disabled with `diagnostic.disabled: ["vim/E492"]`; the
same setting also disables parser E492 errors. Parser E492 occurrences retain
error severity by default.

Representative source evidence:

- `src/testdir/test_vim9_assign.vim:3110-3112` reports E492 for the script-level
  `MyVar: string = 'abc'` form that omitted `var`.
- `src/testdir/test_vim9_cmd.vim:2073-2076` distinguishes script-level E492
  from compiled E476 for `notexist:repl`.
- `src/testdir/test_vim9_script.vim:4832-4841,4878-4881` covers the pinned
  invalid command forms and their E476/E492 context split.
- `src/testdir/test_usercommands.vim`, `Test_CmdUndefined`, verifies E492 for
  missing commands and successful command creation by `CmdUndefined`.
- `runtime/doc/message.txt:797-801` defines E492 after builtin and user-command
  lookup fails.

## Using a Special as a Number: E611

E611 means `Using a Special as a Number`. It is the historical numeric
conversion error used by Legacy Vim script and by the Vim9 script evaluator.
A compiled Vim9 `def` applies its stricter type rules first and reports E1051
for the same invalid `+` expression.

Analysis reports E611 when a numeric arithmetic expression directly contains
a statically known Special value such as `v:none` or `v:null`. The diagnostic
is attached to that operand. Values whose type cannot be proven remain
unknown, so dynamic expressions do not acquire speculative conversion errors.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:2056-2057` distinguishes E1051 in a compiled
  `def` from E611 in Vim9 script for `v:none` and `v:null` operands.
- `src/typval.c:257` emits E611 when Legacy-compatible numeric conversion sees
  a Special value.
- `src/errors.h:1564-1565` defines the exact English message.

## Index not allowed after a value: E689

E689 means `Index not allowed after a {type}: {assignment}`. Legacy Vim script
and the Vim9 script evaluator use it when an assignment target applies an index
or slice to a value that cannot be mutated through indexing. A compiled Vim9
`def` uses the context-specific E1141 for a String instead.

Analysis currently reports E689 only for assignments through an index or slice
whose receiver is statically known to be a String. Reading a String index is
not an assignment error, and receivers with unknown types remain unknown. The
diagnostic span selects the indexed target while the message retains the full
assignment text, matching Vim's context.

Representative source evidence:

- `src/testdir/test_vim9_assign.vim:1845-1856` distinguishes E1141 in a
  compiled `def` from E689 in Vim9 script for `+=` and `..=` String-index
  assignments.
- `src/testdir/test_let.vim:320-326` covers the Legacy String-index assignment.
- `src/eval.c:1819-1840` rejects assignment indexes after unsupported value
  types, and `src/errors.h:1779-1780` defines the exact E689 message.

## Invalid type for len(): E701

E701 means `Invalid type for len()`. Legacy Vim script and the Vim9 script
evaluator use this native error when the argument's value category is not
accepted by `len()`. A compiled Vim9 `def` performs strict argument checking
first and reports E1013 for the same source.

Analysis reuses the pinned `arg_len1` builtin checker. It reports E701 only
when the argument type is statically known and invalid; unknown arguments stay
unknown. Strings, Numbers, Blobs, Lists, Tuples, Dictionaries, and Objects
remain accepted, including method-call syntax.

Representative source evidence:

- `src/testdir/test_vim9_builtin.vim:2620-2627` distinguishes E1013 in a
  compiled `def` from E701 in Vim9 script for `len(true)` and lists accepted
  value categories.
- `src/testdir/test_functions.vim:123-131` covers Legacy E701 for Special and
  Funcref arguments.
- `src/evalfunc.c:8881-8925` implements the accepted runtime categories, and
  `src/errors.h:1803-1804` defines the exact message.

## Using a Funcref as a Number: E703

E703 means `Using a Funcref as a Number`. Legacy Vim script and the Vim9
script evaluator use this historical conversion error when a Funcref is used
where a Number is required. A compiled Vim9 `def` performs strict type checking
first: the corresponding index error is E1012, while arithmetic `+` uses
E1051.

Analysis reports E703 when an arithmetic operand or List, Tuple, Blob, or
String index is statically known to be a Funcref. Unknown values remain
unknown, and compiled `def` expressions retain their Vim9-specific diagnostic.
The diagnostic span selects the Funcref operand or index rather than the whole
expression.

Representative source evidence:

- `src/testdir/test_listdict.vim:1524-1525` distinguishes E1012 in a compiled
  `def` from E703 in Vim9 script for a lambda used as a List index.
- `src/testdir/test_float_func.vim:18-20` covers Legacy conversion of a
  Funcref to a Number.
- `src/typval.c:214-230` maps both `VAR_FUNC` and `VAR_PARTIAL` numeric
  conversion to E703, and `src/errors.h:1807-1808` defines the exact message.

## Invalid Funcref variable name: E704

E704 means `Funcref variable name must start with a capital: {name}`. A plain
variable or parameter that holds a Funcref must start with an ASCII capital.
The `w:`, `b:`, and `t:` namespaces are exempt, as is `s:` in Legacy script;
`g:` still requires a capital after the prefix. Autoload names containing `#`
and direct class or interface members do not use the ordinary Funcref-variable
rule.

Analysis reports E704 when a variable, constant, function parameter, or lambda
parameter has a statically known `func` or `partial` type and its name violates
that rule. This covers explicit function types and types inferred from lambdas
or known Funcref-producing expressions. Dynamic Dictionary updates, foreign
language assignments, and values whose type is unknown remain unknown.

Representative source evidence:

- `src/testdir/test_vim9_assign.vim:68-80` and `2967-2990` cover inferred
  Funcref declarations in compiled `def` and Vim9 script contexts.
- `src/testdir/test_vim9_func.vim:2875-2878` covers an explicit `func()` local,
  while `src/testdir/test_functions.vim:4256-4290` covers function and lambda
  parameters beginning with an underscore.
- `src/evalvars.c:4484-4503` implements the namespace, capital, and autoload
  rules. `src/vim9compile.c:2190-2195` and `src/userfunc.c:579-589` apply them
  to declarations and parameters; `src/errors.h:1809-1810` defines the exact
  message.

## Missing Dictionary key: E716

E716 means `Key not present in Dictionary: "{key}"`. Legacy Vim script and
Vim9 script report it while evaluating a missing Dictionary member or index.
Compiled `def` code normally performs the same lookup at execution time; an
invalid dot-member tail can instead be rejected earlier as E488.

Analysis reports E716 only when the complete key set is statically known. It
supports direct Dictionary literals and a freshly declared literal or
default-empty Vim9 Dictionary when no intervening command can have exposed or
changed it. Dot members and literal String or Number indexes are checked.
Existing keys remain valid. Dynamic indexes, Dictionary parameters, aliases,
function results, and values used by an intervening command remain unknown.
Plain member assignment may create a key and is not treated as a missing-key
read.

For the recovered `dict.a#b` and `dict.a:b` forms, Vim9 script looks up the
valid prefix `a`; analysis therefore reports E716 only when that prefix is
provably absent. A compiled `def` retains Vim's E488 for the invalid tail.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:3140-3162` covers missing Dictionary members,
  literal indexes, and the different `a#b`/`a:b` script and `def` outcomes.
- `src/testdir/test_let.vim:345-359` covers Legacy member access during a
  compound assignment.
- `src/eval.c:1405-1410` reports a missing read, while `src/eval.c:2487-2492`
  distinguishes missing compound-assignment keys from keys created by plain
  assignment. `src/errors.h:1833-1834` defines the exact message.

## Duplicate Dictionary key: E721

E721 means `Duplicate key in Dictionary: "{key}"`. Legacy Vim script and Vim9
script use it when two statically evaluable entries in the same Dictionary
literal produce the same case-sensitive key. Analysis selects the later key as
the diagnostic span.

Vim9 bare keys and quoted keys are literal strings. A bracketed String or
Number key is evaluated before comparison, so `{[001]: 1, '1': 2}` contains a
duplicate. An unbracketed Vim9 Number retains its source spelling, so
`{001: 1, '1': 2}` does not. Legacy quoted and Number keys are also compared,
but a Legacy bare identifier is an expression whose value may change at
runtime and therefore remains unknown.

Dynamic computed keys remain unknown. Malformed entries retain their structural
Dictionary diagnostic instead of receiving a cascading E721, and keys in
nested Dictionary literals are compared only within their own literal.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:3134-3144` covers a direct duplicate and a
  runtime duplicate produced by a computed key.
- `src/testdir/test_listdict.vim:320-334` covers Legacy duplicate keys.
- `src/vim9expr.c:1943-1992` checks Vim9 literal and constant computed keys;
  `src/dict.c:1068-1076` checks evaluated Legacy keys; and
  `src/vim9execute.c:293-309` defers dynamic compiled keys to execution.
- `src/errors.h:1843-1844` defines the exact message.

## Using a Dictionary as a Number: E728

E728 means `Using a Dictionary as a Number`. Legacy Vim script and the Vim9
script evaluator use this historical conversion error when a Dictionary is an
operand of numeric arithmetic.

Analysis reports E728 for a statically known Dictionary operand of binary
`+`, `-`, `*`, `/`, or `%` outside a compiled `def`. The diagnostic selects the
Dictionary operand, checking the left operand before the right. Unknown values
remain unknown. A compiled Vim9 `def` keeps the operator-specific type error:
E1036 for `-`, `*`, or `/`, E1035 for `%`, and E1051 for `+`.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:1875-1879` distinguishes E1036 in a
  compiled `def` from E728 at Vim9 script level for subtraction.
- `src/testdir/test_vim9_expr.vim:2277-2281` covers Dictionary multiplication,
  division, and remainder and their E1036/E1035 compiled counterparts.
- `src/typval.c:225-249` selects E728 when numeric conversion receives a
  Dictionary, and `src/errors.h:1857-1858` defines the exact message.

## Using a Funcref as a String: E729

E729 means `Using a Funcref as a String`. Legacy Vim script and the Vim9
script evaluator use it when concatenation tries to convert a Funcref or
Partial to a String.

Analysis reports E729 for a statically known Funcref or Partial operand of
Legacy `.` or Vim9 `..` concatenation. The diagnostic selects the invalid
operand, checking the left operand before the right. Unknown values remain
unknown, and explicit operations such as `string()` are not treated as
implicit concatenation conversions. A compiled Vim9 `def` does not receive
E729; Vim uses E1105 for its operator type error, which is handled as a
separate diagnostic.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:1964-1968` distinguishes E1105 in a compiled
  `def` from E729 at Vim9 script level for a Funcref operand.
- `src/testdir/test_vim9_expr.vim:2048-2054` covers both Funcref and Partial
  concatenation.
- `src/typval.c:1219-1224` maps Funcref and Partial string conversion to E729,
  and `src/errors.h:1859-1860` defines the exact message.

## Using a List as a String: E730

E730 means `Using a List as a String`. Legacy Vim script and the Vim9 script
evaluator use it when an operation implicitly requires a String but receives a
List.

Analysis reports E730 for statically known Lists in Legacy `.` or Vim9 `..`
concatenation, Vim9 computed Dictionary keys, Vim9 string-option assignments,
and the String-or-Funcref argument of `search()` and `searchpos()`. It selects
the List expression and leaves dynamically typed values unknown. A compiled
Vim9 `def` does not receive E730: Vim uses E1105 for concatenation and computed
Dictionary keys, E1012 for option assignment, and E1013 for the builtin
argument mismatch.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:1944-1948` and `2048-2049` distinguish E1105
  in a compiled `def` from E730 at Vim9 script level for concatenation.
- `src/testdir/test_vim9_expr.vim:3152-3154` covers a List-valued computed
  Dictionary key, while `src/testdir/test_vim9_expr.vim:4172-4174` covers a
  string-option assignment.
- `src/testdir/test_vim9_builtin.vim:3831-3833` and `3984-3986` distinguish
  E1013 in a compiled `def` from E730 for `search()` and `searchpos()`.
- `src/typval.c:1223-1224` maps List string conversion to E730, and
  `src/errors.h:1861-1862` defines the exact message.

## Using a Dictionary as a String: E731

E731 means `Using a Dictionary as a String`. Legacy Vim script and the Vim9
script evaluator use it when concatenation tries to convert a Dictionary to a
String.

Analysis reports E731 for a statically known Dictionary operand of Legacy `.`
or Vim9 `..` concatenation. The diagnostic selects the invalid operand,
checking the left operand before the right, while dynamically typed values
remain unknown. A compiled Vim9 `def` does not receive E731; Vim uses E1105 for
its operator type error.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:1944-1953` and `2048-2050` distinguish E1105
  in a compiled `def` from E731 at Vim9 script level.
- `src/typval.c:1226-1229` maps Dictionary string conversion to E731, and
  `src/errors.h:1863-1864` defines the exact message.

## Wrong variable type for compound assignment: E734

E734 means `Wrong variable type for {operator}=`. Vim reports it when a
compound assignment cannot operate on the statically known target and value
types.

Analysis reports E734 for Legacy `.=` or Vim9 `..=` when a statically known
String variable receives a List or Dictionary outside a compiled `def`. The
message uses Vim's normalized `.=` spelling. The same source in a compiled
`def` uses E1105 instead. Analysis also reports E734 for numeric compound
assignment to a statically known Dictionary target in both Vim9 script and a
compiled `def`. Diagnostics select the compound operator; unknown target or
value types remain unknown.

Representative source evidence:

- `src/testdir/test_vim9_assign.vim:268-287` distinguishes E1105 in a compiled
  `def` from E734 at Vim9 script level for List and Dictionary concatenation.
- `src/testdir/test_vim9_assign.vim:3510-3513` covers `+=`, `-=`, `*=`, `/=`,
  and `%=` on a Dictionary target in both contexts.
- `src/vim9compile.c:3488-3506` rejects compound assignment on a Dictionary
  type, while `src/eval.c:2749-2796` applies the runtime type matrix.
- `src/errors.h:1869-1870` defines the exact message template.

## Using a List as a Number: E745

E745 means `Using a List as a Number`. Legacy Vim script and the top-level
Vim9 script evaluator use it when an evaluated operand requires numeric or
Boolean conversion from a List.

Analysis reports E745 for a statically known List operand of numeric binary
operators outside a compiled `def`. List-plus-List remains valid
concatenation. For a right operand, analysis requires a statically valid
numeric left operand so that an earlier Blob or otherwise unsupported operand
does not receive the wrong error. Analysis also reports E745 for a List used by
`&&` or `||`, including a right operand only when a Boolean literal left
operand proves that short-circuit evaluation reaches it. Unknown values remain
unknown.

Compiled Vim9 uses its type-checking errors instead: the three List-valued
logical forms below use E1012 in a `def` and E745 at top-level `vim9script`.
They do not use E1013. Arithmetic forms use E1035, E1036, or E1051 in a
compiled `def`, depending on the operator and other operand.

Representative source evidence:

- `src/testdir/util/vim9.vim:121-137` defines paired errors as the compiled
  `def` error followed by the top-level Vim9 script error.
- `src/testdir/test_vim9_expr.vim:686-697` and `861` distinguish E1012 from
  E745 for `[] || false`, an evaluated `false || []`, and an evaluated
  `true && []`, including interpolated expressions.
- `src/testdir/test_vim9_expr.vim:1881-1884`, `2041-2045`, and `2271-2277`
  distinguish compiled arithmetic type errors from E745 at script level.
- `src/typval.c:241-242` maps List numeric conversion to E745, and
  `src/errors.h:1899-1900` defines the exact message.

## Cannot use percent with Float: E804

E804 means `Cannot use '%' with Float`. Legacy Vim script and the top-level
Vim9 script evaluator report it when both operands are numeric and either
operand is a Float, because modulo is defined only for Numbers.

Analysis reports E804 on the `%` operator when both operand types are
statically known as Number or Float and at least one is Float. Operand
conversion errors take precedence: for example, `1.0 % []` reports E745 on the
List instead of E804. Unknown and otherwise unsupported operand types remain
unknown so that a later error code is not guessed. A compiled Vim9 `def` uses
E1035 for the same Float modulo expression.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:2293-2295` distinguishes E1035 in a compiled
  `def` from E804 at top-level Vim9 script.
- `src/eval.c:4803-4881` converts both operands before rejecting Float modulo,
  establishing conversion-error precedence.
- `src/errors.h:2071-2073` defines the exact message.

## Using a Float as a Number: E805

E805 means `Using a Float as a Number`. Vim uses it when a runtime operation
requires a Number but receives a Float.

Analysis reports E805 for a statically known Float ternary condition in
Legacy, top-level Vim9, and a compiled `def`. Unlike logical `&&` and `||`, the
compiled ternary form retains E805. Analysis also reports E805 for a Float
List, Blob, String, or Tuple index outside a compiled `def`, and for a
top-level Vim9 builtin argument whose generated checker requires exactly a
Number. The corresponding compiled contexts remain E1012 for an index and
E1013 for a builtin argument. Ordinary Float arithmetic is valid, while Float
modulo remains E804; Boolean conversion through `!` does not receive E805.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:193-206` uses E805 for a Float ternary
  condition in both a compiled `def` and top-level Vim9 script.
- `src/testdir/test_vim9_builtin.vim:1344-1350` distinguishes E1013 in a
  compiled `def` from E805 at script level for `extendnew()`'s Number
  argument.
- `src/testdir/test_listdict.vim:1537-1540`, together with
  `src/testdir/util/vim9.vim:249-265`, distinguishes Legacy and script-level
  E805 from compiled E1012 for Float List indexes and slice bounds.
- `src/typval.c:214-226` maps Float-to-Number conversion to E805, and
  `src/errors.h:2074-2075` defines the exact message.

## Using a Float as a String: E806

E806 means `Using a Float as a String`. For statically parsed expressions, Vim
uses it when a Float value itself is indexed or sliced and would therefore
need to act as a String.

Analysis reports E806 on a statically known Float receiver of `[...]` outside
a compiled Vim9 context. Both literal and resolved variable receivers are
supported in Legacy and top-level Vim9 script. A Float used as the index of a
valid container is E805 instead. Float concatenation is valid conversion and
does not receive E806. A compiled Vim9 `def` uses E1107 for the Float-receiver
indexing form.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:2283-2284` distinguishes E1107 in a compiled
  `def` from E806 at top-level Vim9 script.
- `src/testdir/test_vimscript.vim:7419` covers the same Float receiver in
  Legacy Vim script.
- `src/eval.c:6079-6095` maps a Float index receiver to E806, while
  `src/eval.c:4545-4565` shows that Float concatenation is accepted.
- `src/errors.h:2076-2077` defines the exact message.

## Invalid extend container arguments: E896

E896 means `Argument of {function} must be a List, Dictionary or Blob`. Vim
uses it when the first two arguments of `extend()` or `extendnew()` do not form
a same-kind List, Dictionary, or Blob pair at runtime.

Analysis reports E896 when both argument types are statically known and either
the first argument is not an accepted container or the second argument has a
different outer type. The diagnostic selects the first argument when it is
invalid, otherwise the second, and includes the exact builtin name. Legacy and
top-level Vim9 script use E896; a compiled Vim9 `def` retains E1013. A
same-kind container pair with incompatible element types also remains E1013,
as required by Vim's script-level static type check. Unknown argument types
remain unknown.

Representative source evidence:

- `src/testdir/test_vim9_builtin.vim:1190-1198` distinguishes runtime E896
  from compiled E1013 for `extend()`, and keeps same-kind List element
  mismatches on E1013 in both Vim9 contexts.
- `src/testdir/test_vim9_builtin.vim:1344-1349` covers Dictionary, List, and
  Blob mismatches for `extendnew()` with the same def/script distinction.
- `src/list.c:3154-3182` accepts only same-kind List, Dictionary, or Blob
  pairs and emits E896 otherwise.
- `src/errors.h:2331-2333` defines the exact message template.

## Invalid value used as a String: E908

E908 means `Using an invalid value as a String: {type}`. Top-level Vim9
script uses it when concatenation tries to convert a Job or Channel operand,
or a right-hand Void operand, to String.

Analysis reports E908 for statically known Job and Channel operands of Vim9
`..` outside a compiled context. It also reports a right-hand Void, Job, or
Channel only when the left operand is statically known to complete its own
conversion first. This preserves left-to-right error precedence: a left Void
or Blob does not cause a speculative E908 on the right. Legacy conversion is
left untouched, and a compiled Vim9 `def` retains E1105 for these operator type
errors.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:1954-1962` and `1986-1997` distinguish
  compiled E1105 from script-level E908 for Void, Job, and Channel operands.
- `src/testdir/test_vim9_expr.vim:2049-2068` repeats the same distinctions in
  declaration expressions.
- `src/eval.c:4545-4567` checks a right-hand Void, Job, or Channel before
  concatenation, while `src/typval.c:1269-1296` defines the left-side value
  conversions and their precedence.
- `src/errors.h:2370-2371` defines the exact message template.

## Using a Blob as a Number: E974

E974 means `Using a Blob as a Number`. Vim uses it when a Blob reaches a
runtime operation that requires numeric conversion.

Analysis reports E974 for a statically known Blob operand of numeric binary
operators outside a compiled Vim9 context. Blob-plus-Blob remains valid
concatenation, and right-side E974 is reported only after a statically numeric
left operand so that an earlier List, String, or unsupported value keeps its
own error. Unary `+` or `-` and a Blob ternary condition use E974 in Legacy,
top-level Vim9, and a compiled `def`. Compiled binary arithmetic instead keeps
E1051, E1035, or E1036 according to the operator.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:193-196` uses E974 for a Blob ternary
  condition in both compiled and script contexts.
- `src/testdir/test_vim9_expr.vim:1881-1889`, `2041-2046`, and `2271-2273`
  distinguish compiled binary operator errors from script-level E974 and
  establish left-to-right operand precedence.
- `src/testdir/test_vim9_expr.vim:4152-4160` uses E974 for unary minus in both
  compiled and script contexts.
- `src/typval.c:250-273` maps Blob numeric conversion to E974, and
  `src/errors.h:2561-2562` defines the exact message.

## Using a Blob as a String: E976

E976 means `Using a Blob as a String`. Legacy and top-level Vim9 script use it
when concatenation requires String conversion from a Blob.

Analysis reports E976 for a statically known Blob operand of Legacy `.` or
Vim9 `..` concatenation outside a compiled context. The invalid operand is
selected left-to-right. Numeric Blob operations remain E974, and Blob-plus-
Blob remains valid container concatenation. A compiled Vim9 `def` uses E1105
for the concatenation type error.

Representative source evidence:

- `src/testdir/test_vim9_expr.vim:1954-1967` and `2049-2054` distinguish E1105
  in a compiled `def` from E976 at top-level Vim9 script.
- `src/typval.c:1206-1250` rejects Blob-to-String conversion in both strict and
  non-strict paths.
- `src/errors.h:2565-2566` defines the exact message.

## Cannot lock an option: E996

E996 is shared by several `const` and `final` targets. The option form means
`Cannot lock an option`.

Syntax analysis reports E996 when Legacy `const`, Vim9 `const`, or Vim9
`final` targets an option such as `&filetype`. The option span is selected. A
Vim9 `final &option` without an initializer keeps E996 priority instead of the
generic E1125 `Final requires a value`; ordinary final declarations without a
value remain E1125. Other E996 target messages are not inferred from this
option rule.

Representative source evidence:

- `src/testdir/test_vim9_script.vim:267-272` distinguishes an ordinary final
  declaration without a value from `final &option` in a compiled `def`.
- `src/testdir/test_const.vim:277-282` covers Legacy `const` assignments to
  environment, register, and scoped option targets.
- `src/vim9compile.c:1498-1521` rejects `const` and `final` option destinations
  before compiling an assignment, while `src/evalvars.c:1745-1764` implements
  the runtime option path.
- `src/errors.h:2617-2626` defines the E996 message family.

## Missing return value: E1003

Analysis reports E1003 for a bare `return` inside a `def` whose declared
return type is known and is not `void`. This applies whether the file has a
Legacy or Vim9 root, because the `def` body uses compiled Vim9 return rules.
The `return` command is selected.

An omitted return type is treated as `void`, while a malformed return type
remains unknown; neither triggers this rule. A non-void function with no
`return` statement is the separate E1027 case, while returning a value from a
void or untyped function is E1096.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_func.vim:2434-2439` is the official E1003 compile
  failure: `def Func(): number` contains a bare `return`.
- `src/vim9cmds.c:2754-2760` emits E1003 when a bare return is compiled and
  the function return type is neither `void` nor unknown.
- `runtime/doc/vim9.txt:1388-1408` distinguishes E1003 from E1027 and E1096.
- `src/errors.h:2644-2645` defines the exact message.

## Value returned without a return type: E1096

Analysis reports E1096 when an ordinary Vim9 `def` with no declared return
type, or with the return type `void`, uses `:return` with a value. The
diagnostic selects the `return` command. This rule also applies to a `def` in
a Legacy-root file because its body is compiled as Vim9.

A bare `:return` continues to use E1003 when the function requires a value.
Non-void and malformed return types, Legacy `function` bodies, Vim9 lambda
command blocks, and top-level script commands remain outside E1096.
Constructor-like `new*` and `_new*` definitions in class, interface, and enum
aggregates are also excluded: valid constructors receive an implicit object
return type, while invalid aggregate forms keep their earlier structural
diagnostic instead of cascading E1096.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/vim9cmds.c:2699-2709` emits E1096 before compiling the return
  expression when the ordinary function return type is void, while explicitly
  excluding lambdas.
- `src/testdir/test_vim9_func.vim:478-490,2419-2431` covers omitted and
  explicit `void` return types.
- `src/vim9class.c:2470-2487,2564-2569` identifies `new*` and `_new*` methods
  as constructors, and `src/vim9compile.c:4919-4936` assigns the constructed
  object return type.
- `runtime/doc/vim9.txt:1388-1409` distinguishes E1096 from missing return
  value and missing return statement diagnostics.

## Script-variable declaration in a function: E1101

Syntax analysis reports E1101 when a compiled `def` in a Legacy-root file
tries to declare an `s:` script variable with `var`, `final`, or `const`. The
diagnostic selects the full `s:name`. An ordinary assignment to an existing or
dynamic script variable remains valid E1101-wise; only declaration syntax is
rejected.

The rule also preserves Vim's recovery for an omitted declaration command,
such as `s:name: type`: inside a `def` this declaration-shaped input receives
E1101 instead of a generic trailing-character or invalid-command diagnostic.
A Vim9-root file uses its separate E1268 rule for `s:`, while Legacy
`function` bodies and script-level commands remain outside E1101.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/vim9compile.c:1866-1903` separates the `s:` namespace, reports E1101
  for a declaration, and otherwise retains script-variable lookup.
- `src/vim9compile.c:2031-2040,4605-4628` applies the check to compiled
  `var`, `final`, and `const` assignments.
- `src/testdir/test_vim9_assign.vim:214-226` distinguishes Legacy-root E1101
  declarations from E1268 in a Vim9-root `def`.
- `src/testdir/test_vim9_cmd.vim:2068-2071` covers the declaration-shaped
  `s:notexist:repl` recovery form.
- `runtime/doc/vim9.txt:1495-1510` documents when `s:` is optional, required,
  or rejected according to script and function context.

## Invalid compiled string conversion: E1105

Analysis reports E1105 when a statically known value cannot undergo Vim's
strict implicit String conversion in a compiled Vim9 `def` or lambda. This
includes List, Tuple, Dictionary, Void, Blob, Funcref, Partial, Job, Channel,
Class, Object, and Typealias values. The diagnostic selects the invalid value;
binary `.` and `..` check the left operand before the right and report only the
first failed conversion. A String-target `.=`, or its Vim9 `..=` spelling, also
checks the right-hand value. Unknown and `any` values remain conservative.

Computed Vim9 Dictionary keys use the same strict conversion, while ordinary
identifier keys remain literal key names. Interpolated strings use Vim's
separate interpolation conversion: List, Tuple, and Dictionary values are
accepted there, but the other invalid types still receive E1105. In particular,
a Typealias directly interpolated inside a compiled function receives E1105
rather than the general E1407 value diagnostic.

Top-level Vim9 script and Legacy function expressions retain their established
E729, E730, E731, E734, E908, and E976 paths. A `def` in a Legacy-root file is
still compiled as Vim9 and therefore uses E1105.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/vim9instr.c:185-245` defines strict `ISN_2STRING` conversion and the
  `TOSTRING_INTERPOLATE` List, Tuple, and Dictionary exception.
- `src/vim9expr.c:1943-1964,3406-3418` applies strict conversion to computed
  Dictionary keys and concatenation operands in evaluation order.
- `src/vim9compile.c:1240-1249,3515-3536` applies interpolation and compound-
  assignment conversion rules.
- `src/testdir/test_vim9_expr.vim:1943-1967,2048-2068,3152-3154` covers the
  official invalid operand types and computed Dictionary keys.
- `src/testdir/test_vim9_assign.vim:278-287` distinguishes compiled E1105 from
  script-level E734 for compound concatenation.
- `src/testdir/test_vim9_typealias.vim:274-286` covers the Typealias
  interpolation precedence case.

## Too many callback arguments: E1106

Analysis reports E1106 at Vim9 script level when `map()`, `filter()`, or
`foreach()` receives a direct Vim9 lambda that cannot accept both callback
arguments supplied by the builtin: the index or key and the item value. A
zero-slot lambda reports `2 arguments too many`; a one-slot lambda reports
`One argument too many`. The diagnostic selects the complete lambda.

This rule is deliberately limited to direct lambda arguments with statically
known, non-variadic parameter lists. A two-slot lambda and a one-slot lambda
with variadic rest are accepted. Stored or dynamically typed callbacks remain
outside this direct-lambda rule: stored callbacks keep their existing general
type-mismatch path, while dynamically typed callbacks remain unknown. A lambda
declaring more than two slots is the opposite mismatch, not E1106.

Context-specific errors remain separate. The same callback shapes in a
compiled `def` use E176, `indexof()` retains its script-level E118 path, and
Legacy-root expressions do not receive E1106.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_func.vim:4397-4406` expects E176 in a `def`, then the
  singular and plural E1106 messages for the equivalent Vim9 script lambdas.
- `src/evalfunc.c:740-778` defines the two callback slots used by
  `map()`/`filter()`-style builtins and their compiled argument-count check.
- `src/vim9execute.c:585-612,6725-6742` emits the pluralized too-many and
  too-few argument diagnostics when invoking compiled Vim9 callables.
- `runtime/doc/builtin.txt:3683-3689,7114-7119` documents the two callback
  arguments and the strict Vim9-lambda E1106 behavior.

## Invalid compiled index receiver: E1107

Analysis reports E1107 when a compiled Vim9 `def` or lambda indexes or slices
a statically known Number or Float. The message is Vim's historical
`String, List, Dict or Blob required`, and the diagnostic selects the complete
receiver expression. A `def` retained in a Legacy-root file still compiles as
Vim9 and follows this rule.

String, List, Tuple, Dictionary, and Blob receivers remain valid. Unknown and
`any` receivers stay conservative. Funcref, Partial, Bool, Special, Job,
Channel, Class, Object, Typealias, and Void values belong to other compiler
error categories and are not mapped to E1107; a numeric Typealias is likewise
not treated as its underlying Number or Float value. Incomplete bracket input
keeps its recovery diagnostic without an E1107 cascade.

At top-level Vim9 script, Number indexing remains E1062 and Float indexing
remains E806. Legacy function and script expressions retain their existing
behavior.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/vim9expr.c:75-318` implements compiled member access, admits the valid
  receiver categories, and sends Number and Float to E1107 while preserving
  the separate Funcref and special-variable errors.
- `src/testdir/test_vim9_expr.vim:2283-2284,2589` distinguishes compiled
  E1107 from script-level E1062 and E806 for hexadecimal Number, decimal
  Number, and Float receivers.
- `src/errors.h:2867-2868` defines the exact E1107 message.

## Bang on a nested function: E1117

Syntax analysis reports E1117 when a `def!` or function-definition
`function!` header is nested anywhere inside an enclosing compiled `def`. The
diagnostic selects the bang and uses Vim's exact `Cannot use ! with nested
:def` or `Cannot use ! with nested :function` message. Intervening control
blocks do not change the enclosing compile context, and a `def` retained in a
Legacy-root file follows the same rule.

The bang is rejected before nested-function name parsing, so it cannot be used
as a redeclaration mechanism. A repeated nested declaration without bang keeps
E1073 instead. Top-level `def!` does not receive E1117; top-level Vim9
non-global `function!` keeps E477, and Legacy `function!` remains valid. A bang
on an `enddef` or `endfunction` closer is not a nested-function header and does
not receive E1117.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/vim9compile.c:1035-1045` rejects `forceit` before parsing a nested
  function name and chooses `:def` or `:function` for the message.
- `src/testdir/test_vim9_func.vim:994-1027` distinguishes E1073 for a duplicate
  nested declaration without bang from E1117 for both nested header forms.
- `src/errors.h:2886-2887` defines the exact E1117 message.

## String used as a Boolean: E1135

Analysis reports E1135 when a statically known String is consumed as a Boolean
in Vim9 script. Covered contexts are `if`, `elseif`, and `while` conditions,
ternary conditions, the operands of `&&` and `||` that are guaranteed to be
evaluated, and String-returning predicate callbacks passed to `filter()` or
`indexof()`. The diagnostic selects the condition, operand, or callback. A
simple direct or parenthesized string literal includes its known value in the
message; a value known only by type uses `Using a String as a Bool` without
inventing a runtime value.

Compiled `def` and lambda contexts keep Vim's constant-folding distinction. A
literal String in a ternary, `if`, or `elseif` condition reports E1135, while
logical operands and `while` conditions stay on the compile-time E1012 path.
Nonliteral String conditions are not remapped to E1135. This also applies to a
`def` retained in a Legacy-root file. Ordinary Legacy conditions do not receive
E1135.

Short-circuiting is respected: the right operand is diagnosed only when the
left operand proves it will run. Unknown and `any` values remain conservative,
and permissive `!value` conversion is unchanged. `map()` and `foreach()` do not
consume callback results as predicates. Callback argument-count and parameter
type errors also keep their existing E1106, E118, E176, or E1013 precedence.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/typval.c:197-244` rejects a String passed through Vim9's checked Boolean
  conversion and emits E1135 with the runtime value.
- `src/vim9expr.c:45-57,3666-3810,3905-3972` distinguishes strict logical
  operand type checks from checked constant conversion in a ternary.
- `src/testdir/test_vim9_expr.vim:136-193,688-859` covers literal ternaries,
  logical operands, short-circuit behavior, and the E1012/E1135 context split.
- `src/testdir/test_vim9_cmd.vim:438-510` distinguishes `if` and `elseif`
  constant folding from compiled `while` behavior and Vim9 script evaluation.
- `src/testdir/test_vim9_builtin.vim:1567-1574,2361-2369` expects E1135 for
  String-returning `filter()` and `indexof()` predicates at script level while
  compiled callers retain E1013.
- `src/errors.h:2922-2923` defines the exact E1135 message.

## Bool used as a Number: E1138

Analysis reports E1138 when a Vim9 script numeric binary or compound operator
consumes a statically known Bool as a Number. The diagnostic selects the first
offending operand and uses Vim's exact `Using a Bool as a Number` message. The
four Bool spellings
`true`, `false`, `v:true`, and `v:false`, parenthesized values, and identifiers
with a known Bool type follow the same rule.

The same script-level diagnostic applies when a `sort()` comparison callback
has the correct parameter signature but a statically known Bool return type;
the callback expression is selected. Parameter-count and parameter-type
mismatches retain their existing errors. Compiled `def` and lambda arithmetic
or callbacks remain strict compile-time type mismatches such as E1051, E1036,
or E1013. Ordinary Legacy arithmetic, unknown values, and valid Boolean
contexts do not receive E1138.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/typval.c:197-263` rejects Bool-to-Number conversion in Vim9 while
  retaining Legacy conversion behavior.
- `src/list.c:2207-2253` passes a `sort()` callback result through checked
  Number conversion.
- `src/testdir/test_vim9_expr.vim:2058-2061` distinguishes compiled E1051 from
  script-level E1138 for all four Bool spellings.
- `src/testdir/test_vim9_builtin.vim:4427-4434` distinguishes a compiled
  callback return mismatch from script-level E1138.
- `src/errors.h:2929-2930` defines the exact E1138 message.

## Non-indexable assignment receiver: E1141

Analysis reports E1141 when a compiled Vim9 `def` or lambda writes through a
member, index, or slice whose statically known receiver is not a mutable
indexable destination. This covers direct and compound assignments as well as
`redir =>` and `redir =>>` targets. The diagnostic uses Vim's exact
`Indexable type required` message and selects the complete invalid receiver.
A `def` retained in a Legacy-root file follows the same compiled rule.

List, Dictionary, Blob, Class, Object, and known local class or enum-object
receivers remain valid destinations. Unknown and `any` receivers stay
conservative. Tuple writes keep their more specific E1532 or E1533 immutability
diagnostics, and direct Class or Typealias declarations are not reinterpreted
through an underlying primitive type.

The rule applies only to writes. Reading a String index is valid, while
ordinary Vim9 script String-index writes keep their runtime E689 path. Legacy
assignments and incomplete targets do not receive E1141. Nested recovering
targets produce at most one E1141 on the first statically known invalid
receiver.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/vim9compile.c:2730-2800` admits mutable List, Dictionary, Blob, Class,
  Object, and `any` destinations, reserves Tuple for its immutability error,
  and sends other known receiver types to E1141.
- `src/testdir/test_listdict.vim:458-474` distinguishes script-level numeric
  member assignment from compiled E1141.
- `src/testdir/test_vim9_assign.vim:673-680,1844-1857` covers String index
  assignment and both compound-assignment forms.
- `src/testdir/test_vim9_cmd.vim:1990-2000` expects E1141 for a compiled
  indexed `redir` target.
- `src/errors.h:2935-2936` defines the exact E1141 message.

## Invalid object comparison: E1153

Analysis reports E1153 when both operands of a Vim9 comparison are statically
known Object values and the operator is `>`, `>=`, `<`, `<=`, `=~`, or `!~`.
The diagnostic uses Vim's exact `Invalid operation for object` message and
selects the comparison operator. It applies at script level and in compiled
`def` and lambda bodies, including a `def` retained in a Legacy-root file.

Object equality and identity comparisons with `==`, `!=`, `is`, and `isnot`
remain valid. Mixed Object and non-Object comparisons keep their existing type
mismatch diagnostics, while direct Class, Enum, and Typealias declarations are
not treated as Object values. Unknown and incomplete operands remain
conservative. Invalid comparisons of Bool, Special, List, and Blob values keep
their existing E1072 behavior.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/vim9instr.c:489-577` selects the Object comparison instruction and
  rejects every operation except equality, inequality, and identity.
- `src/testdir/test_vim9_class.vim:1580-1608` accepts Object equality and
  identity at script and compiled scope, then expects E1153 for all six invalid
  operators in both contexts.
- `src/errors.h:2959-2960` defines the exact E1153 message template.

## Forbidden flatten call: E1158

Analysis reports E1158 when a Vim9 command calls the builtin `flatten()`
directly or through method syntax. The diagnostic uses Vim's exact `Cannot use
flatten() in Vim9 script, use flattennew()` message and selects the builtin
name. It applies at script level and in compiled `def` and lambda bodies,
including `vim9cmd` and a `def` retained in a Legacy-root file.

E1158 owns the call before ordinary builtin arity and argument-type checks, so
an otherwise invalid `flatten()` argument list still receives the prohibition
diagnostic. Ordinary Legacy commands, a one-command `legacy` override,
`flattennew()`, scoped or member names, dynamic calls, and incomplete calls do
not receive E1158.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/vim9expr.c:1380-1413` emits E1158 after parsing arguments and before
  normal builtin dispatch and checking.
- `src/list.c:1104-1113` rejects `flatten()` while executing Vim9 script and
  otherwise retains the Legacy implementation.
- `src/testdir/test_vim9_builtin.vim:1416-1421` expects E1158 at both script
  and compiled `def` scope.
- `runtime/doc/vim9.txt:2688-2695` requires `flattennew()` and demonstrates the
  prohibition with `vim9cmd` method syntax.
- `src/errors.h:2971-2972` defines the exact E1158 message.

## Variadic parameter default: E1160

Syntax analysis reports E1160 when a Vim9 `def` variadic parameter has a
default value. The diagnostic uses Vim's exact `Cannot use a default for
variable arguments` message and selects the `=` through the retained default
expression. Typed, inferred, multiline, and Legacy-root `def` signatures use
the same rule.

The parameter remains variadic and retains its name, optional type, default
span, and parsed default expression so the function body, terminator, and
following commands remain available during recovery. A valid variadic
parameter without a default and an ordinary optional parameter remain valid.
Legacy `function` arguments and Vim9 lambda defaults keep their separate
grammar and diagnostic ownership.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/userfunc.c:279-317` recognizes a Vim9 variadic parameter and rejects an
  immediately following default before continuing ordinary argument parsing.
- `src/testdir/test_vim9_func.vim:2027-2034` expects E1160 for the official
  multiline variadic-default signature.
- `runtime/doc/vim9.txt:300-314` defines variable arguments as the final named
  parameter with a List type and documents optional arguments separately.
- `src/errors.h:2977-2978` defines the exact E1160 message.

## Destructuring element type mismatch: E1163

Analysis reports E1163 for the first statically provable element type mismatch
in a fixed-target Vim9 List or Tuple destructuring assignment. The diagnostic
uses Vim's exact `Variable N: type mismatch, expected T but got U` form and
counts ignored `_` targets in the one-based variable index. A concrete literal
selects the mismatching element; a known typed List or Tuple expression selects
the complete right-hand side.

Typed destructuring declarations use the same rule and do not also receive the
ordinary E1012 assignment diagnostic. The check applies at script level and in
compiled `def` and lambda bodies, including a `def` retained in a Legacy-root
file. Cardinality, container-kind, void-value, invalid-target, and incomplete
input diagnostics keep their existing ownership. Unknown and `any` values,
inferred declaration targets, Legacy destructuring, and final rest targets
remain conservative.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/vim9compile.c:3289-3315,3416-3430` associates each unpacked assignment
  item with its one-based variable index before checking the target type.
- `src/testdir/test_vim9_assign.vim:542-566` expects E1163 for fixed-target
  assignments and identifies the first mismatching variable.
- `src/testdir/test_vim9_assign.vim:1045-1055` covers typed destructuring
  declarations and both literal and named List inputs.
- `src/vim9type.c:1107-1138` formats variable-index type mismatches.
- `src/errors.h:2983-2986` defines the exact E1163 message templates.

## Missing vim9cmd command: E1164

Syntax analysis reports E1164 when `vim9cmd`, including an accepted
abbreviation, is the final command modifier and is not followed by a command.
The diagnostic uses Vim's exact `vim9cmd must be followed by a command` message
and selects the `vim9cmd` modifier.

End of line, a command separator, and a comment in the file's root dialect all
terminate the missing command. The scanner retains the empty command, modifier,
comment or separator token, and following command for recovery. Other missing
Vim9 command modifiers continue to use E1082, and a later modifier or valid
command prevents E1164.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/ex_docmd.c:3237-3250` requires a command immediately after `vim9cmd` and
  emits E1164 before enabling its one-command Vim9 context.
- `src/ex_docmd.c:5945-5974` recognizes command termination with the comment
  delimiter selected by the current file dialect.
- `src/testdir/test_vim9_cmd.vim:5-17` expects E1164 for bare `vim9cmd` and
  accepts its command-followed abbreviations.
- `runtime/doc/vim9.txt:108-109` states that `vim9cmd` cannot stand alone.
- `src/errors.h:2988-2989` defines the exact E1164 message.

## Invalid range assignment: E1165

Analysis reports E1165 for a direct `=` assignment to a slice when the receiver
is statically known to be invalid in a compiled Vim9 context. The diagnostic
uses Vim's exact `Cannot use a range with an assignment: {assignment}` form,
retains the complete assignment text in the message, and selects the slice
target.

The check applies in `def` and block-lambda bodies, including a `def` retained
under a Legacy root. List and Blob slices remain valid, Tuple slices retain
E1533, and `any`, unresolved receivers, incomplete input, compound assignments,
ordinary Vim9 script execution, and Legacy assignments remain outside E1165.
E1165 also suppresses lower-priority indexability and assignment-type cascades
for the same target.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/vim9compile.c:2664-2686` rejects a range for direct assignment when the
  compiled receiver is neither a List, Blob, nor `any`, while preserving the
  Tuple-specific diagnostic.
- `src/testdir/test_vim9_assign.vim:1745` expects E1165 for the official
  inferred-Dictionary assignment in a compiled `def`.
- `runtime/doc/eval.txt:3165-3175` documents range assignment and associates it
  with E1165.
- `src/errors.h:2991-2992` defines the exact E1165 message template.

## Dictionary range unlet: E1166

Analysis reports E1166 when a compiled Vim9 `unlet` targets a range on a
statically known Dictionary variable. The diagnostic uses Vim's exact `Cannot
use a range with a dictionary` message and selects the complete slice target.
Only the first provable invalid target in one command is reported.

The check applies in `def` and block-lambda bodies, including `def` under a
Legacy root and the bang form of `unlet`. It remains conservative for dynamic
receivers, `any`, unresolved names, incomplete input, and non-Dictionary
receivers. Ordinary Vim9 script execution and Legacy commands are not treated
as compile-time E1166. List and Blob ranges and Dictionary item/member targets
remain valid. Earlier parser or semantic failures inside the range retain
precedence over E1166.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/vim9compile.c:2664-2704` compiles an `unlet` range first and then emits
  E1166 only when the statically known destination is a Dictionary.
- `src/testdir/test_vim9_assign.vim:2701-2704` expects E1166 for an inferred
  local Dictionary range in a compiled `def`.
- `runtime/doc/eval.txt:3165-3175` documents range operations and associates
  them with E1166.
- `src/errors.h:2993-2994` defines the exact E1166 message.

## Compiled argument shadowing: E1167

Analysis reports E1167 when a Vim9 `def` or lambda argument shadows a
previously declared local variable, constant, or argument in an enclosing
compiled lexical scope. The diagnostic uses Vim's exact `Argument name shadows
existing variable: {name}` form and selects the new argument name.

This includes nested functions, nested lambdas, control-block locals, and
`def` under a Legacy root. The special `_` argument is exempt. Later and
sibling-scope declarations do not count, and Legacy functions and lambdas are
unchanged. Root script items and aggregate members retain their more specific
E1168 and E1340 ownership instead of cascading to E1167. An overlapping parser
diagnostic also retains precedence.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/vim9compile.c:440-495` gives script and class conflicts priority, exempts
  `_`, and emits E1167 for an existing compiled local or argument.
- `src/vim9compile.c:3881-3908` checks nested compiled-function arguments
  against their enclosing compilation context.
- `src/userfunc.c:55-109` applies the same defined-name check while parsing
  Vim9 lambda arguments.
- `src/testdir/test_vim9_func.vim:1613-1619,1638-1653` expects E1167 for the
  three supported local/argument shadowing shapes.
- `runtime/doc/vim9.txt:527-532` documents the no-shadowing rule.
- `src/errors.h:2995-2996` defines the exact E1167 message template.

## Script argument collision: E1168

Analysis reports E1168 when a Vim9 `def` or lambda argument conflicts with a
visible script variable, constant, type alias, class, interface, or enum. The
diagnostic selects the argument name and preserves Vim's remaining signature
text in the message: for example, `Argument already declared in the script: A:
number)`.

Root script items and items in an ancestor script control block are visible;
items from sibling or finished blocks are not. Deferred `def` compilation sees
later root declarations, while a lambda evaluated directly in script sees only
earlier declarations. A lambda compiled inside a `def` follows the deferred
rule. `_`, Legacy arguments, scoped global assignments, imports, functions,
local variables, and aggregate members are not E1168 script-item matches.
E1168 has priority over E1167 for the same argument.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/vim9compile.c:440-470` checks script items before compiled locals and
  emits E1168 with the unmodified argument tail.
- `src/testdir/test_vim9_func.vim:1422-1502` covers root, same-block,
  sibling-block, and later-root visibility during deferred compilation.
- `src/testdir/test_vim9_func.vim:1613-1620` distinguishes a def-local E1167
  conflict from a script-level E1168 conflict for the same lambda shape.
- `src/testdir/test_vim9_typealias.vim:144-150` requires the exact
  `A: number)` message tail for a type-alias collision.
- `runtime/doc/vim9.txt:527-532` documents the script-wide no-shadowing rule.
- `src/errors.h:2997-2998` defines the exact E1168 message template.

## Unsupported for-loop iterable: E1177

Analysis reports E1177 when a Vim9 `for` loop iterates a value whose type is
statically known not to be iterable. The diagnostic uses Vim's exact `For loop
on {type} not supported` form and selects the iterable expression. Local class
instances use Vim's runtime type name `object` rather than their class name.

Lists, Tuples, Strings, and Blobs remain valid iterables. Unknown and `any`
values stay conservative because their runtime type decides whether the loop
is valid. Legacy commands, including an explicit `legacy for` under a Vim9
root, retain their historical iterable diagnostics instead of receiving
E1177. When E1177 owns an invalid iterable, the loop does not also receive a
binding E1012 diagnostic; malformed iterable syntax and earlier expression
diagnostics keep precedence.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_script.vim:3079-3086` expects the exact Dictionary
  message for both a compiled literal and a dynamically typed runtime value.
- `src/vim9cmds.c:1010-1075` accepts only List, Tuple, String, Blob, `any`, and
  unknown compile-time iterable types and emits E1177 for every other known
  type.
- `src/vim9execute.c:2990-3040` applies the corresponding runtime check.
- `runtime/doc/eval.txt:3637-3644` documents the supported Vim9 iterable
  values and the E1177 failure.
- `src/vim9type.c:2532-2561` defines the lower-case runtime type names used in
  the message.
- `src/errors.h:3015-3016` defines the exact E1177 message template.

## Cannot lock or unlock a local variable: E1178

Analysis reports E1178 when a compiled Vim9 `lockvar` or `unlockvar` command
directly targets a bare local variable or constant. The diagnostic uses Vim's
exact `Cannot lock or unlock a local variable` message and selects the first
invalid target in the command.

The check applies in `def` and block-lambda bodies, including captured locals
and a `def` retained under a Legacy root. A function argument may be locked,
as may bare `this`, a local value's member or indexed item, a script variable,
an aggregate member, and explicitly scoped state. Top-level Vim9 script and
Legacy commands retain their runtime behavior. Incomplete targets and earlier
diagnostics keep precedence.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_cmd.vim:1813-1824` expects E1178 for both `lockvar`
  and `unlockvar` on an inferred local List in a compiled `def`.
- `src/testdir/test_vim9_class.vim:4062-4095` accepts bare `this` but rejects
  ordinary four- and five-character method locals.
- `src/testdir/test_tuple.vim:1349-1361` distinguishes the compiled local
  failure from the corresponding Legacy and Vim9-script runtime behavior.
- `src/vim9cmds.c:176-345` resolves the root target, rejects only a bare local,
  and routes parameters, `this`, members, indexes, class members, and script
  variables through their supported paths.
- `runtime/doc/vim9.txt:523-525` directs local bindings to `const` and `final`
  instead of `lockvar`.
- `src/errors.h:3017-3018` defines the exact E1178 message.

## Ignored underscore used as a variable: E1181

Analysis reports E1181 when the Vim9 ignored-argument spelling `_` is used as
an ordinary declaration, assignment target, or value. The diagnostic uses
Vim's exact `Cannot use an underscore here` message and selects the bare
underscore. A direct invalid declaration owns its command, and an underscore
call does not also receive an unknown-function diagnostic.

The underscore remains valid as a repeated `def` or lambda parameter, a `for`
binding, and an item in a List or Tuple destructuring target. Dictionary keys,
names that merely begin with `_`, scoped `g:_`, Legacy commands, and an
explicit `legacy` command retain their ordinary meaning. The Vim9 rule also
applies to a `def` retained under a Legacy root and to an explicit `vim9cmd`.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_func.vim:4385-4395` expects E1181 for `var _ = 1`
  and `var x = _` in both compiled and Vim9-script contexts.
- `src/testdir/test_vim9_script.vim:2807-2815` accepts `_` as a loop binding
  and then expects E1181 when code tries to read it.
- `runtime/doc/vim9.txt:316-324` documents repeated ignored arguments and
  states that they need no type.
- `src/vim9compile.c:3658-3683` accepts underscore destructuring slots but
  rejects a standalone assignment target.
- `src/vim9expr.c:3137-3152` and `src/eval.c:5326-5340` reject compiled and
  evaluated underscore reads before ordinary name resolution.
- `src/evalvars.c:4164-4178` applies the corresponding rule to a standalone
  Vim9 script assignment.
- `src/errors.h:3027-3028` defines the exact E1181 message.

## Dictionary function in Vim9 script: E1182

Syntax analysis reports E1182 when a `def` or `function` header in Vim9
context uses a Dictionary-member name such as `Object.Method`. The diagnostic
uses Vim's `Cannot define a dict function in Vim9 script: {name}` message and
selects the complete function name.

The Vim9 rule applies at script level, inside a compiled `def`, to a `def`
retained under a Legacy root, and after an explicit `vim9cmd`. It owns the
invalid dotted header before capital-name or nested-namespace checks. A
top-level Legacy Dictionary function and an explicit `legacy function` retain
Legacy behavior, while ordinary class, interface, and enum methods are not
Dictionary functions.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_func.vim:111-168` expects E1182 for `function` and
  `def` Dictionary-member headers at Vim9 script level and inside a compiled
  `def`, including script-local and global receivers.
- `src/userfunc.c:5122-5134` rejects a dotted `:function` name in Vim9 script
  context before the Legacy Dictionary-function name is resolved.
- `src/vim9compile.c:1063-1072` applies the same rule while compiling a nested
  function definition.
- `runtime/doc/vim9.txt:272-286` states that `:def` cannot define a Dictionary
  function and recommends a Vim9 class or an explicit Dictionary parameter.
- `src/errors.h:3029-3030` defines the exact E1182 message.

## Range with assignment operator: E1183

Analysis reports E1183 when compiled Vim9 code uses a compound assignment
operator with a slice target, such as `items[1 : 2] += other`. The diagnostic
uses Vim's `Cannot use a range with an assignment operator: {expression}`
message, includes the complete assignment expression, and selects the slice
target.

The rule depends on the assignment shape, not the receiver's inferred type,
because Vim rejects the range while loading the compound-assignment target.
It therefore applies in a `def` or compiled lambda to List, Blob, Dictionary,
String, tuple, `any`, and unresolved receivers. Plain `=` slice assignments
retain E1165 and the supported List/Blob rules; direct-index compound
assignments are not ranges. Top-level Vim9 evaluation, Legacy commands, and an
explicit `legacy` command inside a `def` do not receive this compile-only code.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_blob.vim:135-139` expects E1183 only for the compiled-`def`
  variant of `b[1 : 1] ..= 0z55`; Legacy and Vim9-script evaluation both use
  E734 instead.
- `src/vim9compile.c:2557-2649` loads a compound-assignment target, parses its
  index, and emits E1183 as soon as that index is a range.
- `src/vim9compile.c:3396-3404` takes this target-loading path only for a
  compound operator, before compiling the right-hand expression.
- `src/errors.h:3031-3032` defines the exact E1183 message.

## Echo expression without a value: E1186

Analysis reports E1186 when an echo-family command must display or consume an
expression whose static type is `void`. The diagnostic uses Vim's `Expression
does not result in a value: {expression}` message and selects the void
expression item.

`echo` and `echon` check their evaluated result in every dialect, so the rule
also applies at Vim9 script level, to a Legacy command that calls a known
`def`, and to an explicit `legacy echo`. Vim's compiled multi-expression path
applies the same check to `echomsg`, `echoerr`, `echoconsole`, `echowindow`,
and `execute` inside a `def` or compiled lambda. Their evaluated top-level and
explicit-`legacy` forms do not use E1186. Each whitespace-separated expression
is checked independently; unknown return types remain conservative.

This is distinct from E1031: inferred initializers and destructuring
assignments that consume a void value keep that code, while an effect-only
standalone call remains valid.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_cmd.vim:2017-2037` expects E1186 for a no-return `def`
  called by `echo` at Vim9 script level and inside a compiled `def`.
- `src/eval.c:8010-8044` evaluates each `echo` or `echon` expression and emits
  E1186 when its result is `VAR_VOID`.
- `src/vim9cmds.c:2145-2179` performs the corresponding per-expression void
  check while compiling echo-family commands and `execute`.
- `src/vim9compile.c:4712-4732` routes those compiled commands through the
  shared multi-expression checker.
- `src/errors.h:3039-3040` defines the exact E1186 message.

## Legacy flow-control command in a compiled body: E1189

Syntax analysis reports E1189 when `:legacy` is applied to a flow-control
command in a compiled Vim9 body. The forbidden commands are `if`, `elseif`,
`else`, `endif`, `for`, `endfor`, `continue`, `break`, `while`, `endwhile`,
`try`, `catch`, `finally`, and `endtry`. The diagnostic retains the original
command spelling and arguments in Vim's `Cannot use :legacy with this command:
{command}` message.

An invalid command does not open, branch, or close a syntax block, so recovery
continues at the next command without adding missing-end or mismatched-block
diagnostics. The rule applies inside a `def`, including one declared from a
Legacy-root file, and inside a compiled block lambda. It does not apply at
Vim9 script level, inside a nested Legacy `function`, or to allowed commands
such as `legacy call`. When both dialect modifiers occur, only the final
effective modifier state controls this diagnostic.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_func.vim:3568-3575` checks all fourteen forbidden
  commands with `legacy {command} expr` inside a compiled `def`.
- `src/vim9compile.c:4510-4534` checks the final `CMOD_LEGACY` state, rejects
  exactly those command indexes, and returns before compiling the command.
- `src/errors.h:3048-3050` defines the exact E1189 message.

## Too few callback arguments: E1190

Analysis reports E1190 at Vim9 script level when `map()`, `filter()`, or
`foreach()` receives a direct Vim9 lambda that requires more arguments than
the two values supplied by the builtin: the index or key and the item value. A
lambda with three required parameters reports `One argument too few`; four
required parameters report `2 arguments too few`. The diagnostic selects the
complete lambda.

The calculation uses required parameters, so an accepted variadic rest after
two required parameters does not cause E1190. Stored function values,
dynamically typed callbacks or containers, Legacy expressions, and ordinary
direct function calls remain on their existing conservative or general arity
paths. The opposite callback mismatch keeps E1106, while a compiled `def`
keeps E176 and compiled block-lambda scopes do not use this script-level code.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:2790-2808` distinguishes compiled E176
  from the singular and plural E1190 messages for direct script lambdas.
- `src/evalfunc.c:737-815` defines the two callback slots shared by `map()`,
  `filter()`, and `foreach()` and performs the compiled signature check.
- `src/vim9execute.c:585-612,6725-6742` computes missing required arguments
  when invoking a compiled Vim9 callable and emits the pluralized error.
- `src/errors.h:3052-3053` defines the exact E1190 messages.

## Expression without an effect: E1207

Analysis reports E1207 for a Vim9 expression command whose complete value is
only a register, environment variable, known option, known literal, predefined
variable, or lexically visible variable, constant, or parameter. It also
reports the code for a string literal passed directly to `:eval`. The
diagnostic selects that complete expression and preserves its source text in
Vim's `Expression without an effect: {expression}` message.

The rule applies in a Vim9 script, a compiled `def` from either root dialect,
a compiled block lambda, and an explicit `:vim9cmd`. An effective `:legacy`
command remains exempt. Calls, assignments, method and index expressions,
parenthesized and compound values, unresolved names and options, forward
declarations, incomplete atoms, and expressions with trailing text remain on
their existing paths. A bare Ex command is covered only when an earlier
visible variable-like declaration shadows that spelling, as in `var undo = 1`
followed by `undo`.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_cmd.vim:744-821` covers registers, local and global
  options, an environment variable, and single- and double-quoted `:eval`
  arguments in both a compiled `def` and Vim9 script evaluation.
- `src/testdir/test_vim9_script.vim:1847-1860` distinguishes assignment to a
  variable named `undo` from evaluating the same name without an effect.
- `src/ex_eval.c:944-1018` recognizes the narrow name-only shapes and emits
  E1207 only after successful Vim9 evaluation.
- `src/vim9cmds.c:1998-2025` performs the corresponding compiled-expression
  check before dropping an otherwise useful result.
- `src/errors.h:3101-3102` defines the exact E1207 message.

## Number required for builtin argument: E1210

Analysis reports E1210 at Vim9 script level when a builtin argument with a
known static type reaches Vim's strict Number check with a non-Number value.
The message uses the normalized one-based argument position, including method
calls, and the diagnostic selects the complete offending argument.

The direct rule follows Vim's `arg_number` metadata. The same code also applies
to the value inserted into a Blob and to a List or Blob index passed to
`remove()`, because those container-dependent runtime paths explicitly use the
Number check. A compiled `def` or block lambda retains the stricter E1013 type
mismatch. Float-to-Number conversion retains E805, while Legacy calls,
dynamically typed arguments, List element-type mismatches, and the
container-dependent third argument of `extend()` remain on their existing
paths.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:206-208` distinguishes E1013 in a
  compiled `def` from E1210 for the first and second Number arguments during
  Vim9 script evaluation.
- `src/testdir/test_vim9_builtin.vim:2321-2327` covers the Blob item and later
  direct Number arguments of `index()`.
- `src/testdir/test_vim9_builtin.vim:3670-3676` covers List and Blob indexes
  passed to `remove()`.
- `src/testdir/test_blob.vim:404-408` covers a non-Number value added to a Blob.
- `src/typval.c:487-499` implements the strict runtime Number check and its
  one-based argument number.
- `src/list.c:2964-2976,3252-3289` applies that check to Blob insertion and
  List or Blob removal.
- `src/errors.h:3109-3110` defines the exact E1210 message.

## List required for builtin argument: E1211

Analysis reports E1211 at Vim9 script level when a builtin's outer argument
must be a List but its known static type is not a List. The diagnostic selects
the complete argument and uses the normalized one-based position for both
ordinary and method calls.

The rule covers the general List checker and the outer-type portion of
`list<number>` and `list<string>` checkers. A value that is already a List but
has an incompatible known element type remains on E1013 instead. `slice()` also
uses E1211 for an unsupported container because Vim deliberately routes that
runtime failure through its List check. Other unions, including `reverse()`,
keep their own diagnostics. Compiled `def` and block-lambda scopes retain
E1013; Legacy and dynamically typed calls remain conservative.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:811-824` covers a later general List
  argument and the outer type of a `list<string>` argument.
- `src/testdir/test_vim9_builtin.vim:2385-2388` distinguishes a non-List
  E1211 failure from a List element-type E1013 failure.
- `src/testdir/test_vim9_builtin.vim:2664-2678` makes the same distinction for
  `list<number>` arguments.
- `src/testdir/test_vim9_builtin.vim:4321-4328` covers the special `slice()`
  container path.
- `src/typval.c:606-618` implements the strict runtime List check.
- `src/list.c:3496-3510` routes unsupported `slice()` containers through that
  check before validating its Number arguments.
- `src/errors.h:3111-3112` defines the exact E1211 message.

## Bool required for builtin argument: E1212

Analysis reports E1212 at Vim9 script level when an `arg_bool` builtin
argument has a known incompatible value. The diagnostic selects the complete
argument and uses its normalized one-based position, including method calls.

Vim's Bool argument contract also accepts Number values zero and one. Static
zero and one expressions, including parenthesized and unary-plus forms, are
therefore accepted in both script and compiled contexts. A different static
Number reports E1212 during script evaluation and E1013 in a compiled `def` or
block lambda. A script-level Number whose value is not statically known stays
conservative because it may be zero or one at runtime; the same Number type in
a compiled scope retains E1013. Bool-or-Number and Bool-or-Dictionary unions,
Legacy calls, and dynamically typed arguments do not use E1212.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:370-373` distinguishes compiled E1013
  from script-level E1212 for a first Bool argument.
- `src/testdir/test_vim9_builtin.vim:470-472` covers a later Bool argument.
- `src/testdir/test_vim9_builtin.vim:733-736` accepts Number one and rejects
  Number two for the same Bool position.
- `src/evalfunc.c:392-399` defines the compile-time `arg_bool` checker.
- `src/typval.c:525-539` implements the runtime Bool check and its Number
  zero-or-one exception.
- `src/errors.h:3113-3114` defines the exact E1212 message.

## Imported item redefinition: E1213

Analysis reports E1213 when a valid root-level Vim9 import alias is followed
by a script-level variable, constant, destructuring binding, `def`, or legacy
function with the same name. The diagnostic selects the later declaration and
is emitted once for the imported alias.

Declarations inside a function, lambda, class, interface, or enum do not use
E1213. Neither do assignments, reads, type aliases, aggregate declarations,
Legacy-root imports, or an import alias already rejected because an earlier
script item owns the name.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_import.vim:335-341` covers a variable declaration
  that redefines an imported alias.
- `src/testdir/test_vim9_import.vim:2221-2228` covers a root-level `def` with
  the imported name.
- `src/testdir/test_vim9_import.vim:2230-2240` distinguishes a nested use of
  the imported name, which belongs to E1236 rather than E1213.
- `src/evalvars.c:4179-4195` applies E1213 to script-local declarations while
  routing non-declaration use through E1236.
- `src/userfunc.c:1959-1988` applies the same distinction to functions.
- `src/errors.h:3115-3116` defines the exact E1213 message.

## Invalid digraph list structure: E1216

Analysis reports E1216 for a Vim9 `digraph_setlist()` argument whose outer
List has a statically provable invalid item: a direct inner List with other
than two items, a known non-List inner value, or `null_list` as an inner item.
It also reports E1216 for a known non-List outer argument at script level.
The diagnostic selects the complete outer argument, including a normalized
method receiver.

A known outer List suppresses the generated `list<string>` element check,
because the builtin actually requires a List of two-item Lists and validates
their contents separately. Empty and null outer Lists, valid direct pairs,
and dynamically shaped Lists are accepted conservatively. A compiled
non-List outer argument retains E1013, and E1214/E1215 string-content checks
remain outside this rule.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_digraph.vim:580-592` covers valid pairs, invalid inner
  lengths, non-List items, a null inner List, and a null outer List.
- `src/testdir/test_vim9_builtin.vim:1002-1003` distinguishes compiled E1013
  from script-level E1216 for known non-List outer arguments.
- `src/digraph.c:2163-2213` requires an outer List and exactly two values in
  every non-null inner List before delegating content validation.
- `src/errors.h:3123-3124` defines the exact E1216 message.

## Channel or Job required for builtin argument: E1217

Analysis reports E1217 at Vim9 script level when an `arg_chan_or_job`
builtin argument has a known incompatible type. The diagnostic selects the
complete normalized argument and uses its one-based position, including a
method receiver or a later optional argument.

Channel, Job, and dynamically typed values are accepted. A mismatch in a
compiled `def` or block lambda retains E1013, Legacy calls are not diagnosed,
and the stricter Job-only checker remains separate from this rule.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:558-725` distinguishes compiled E1013
  from script-level E1217 across first and later channel-handle arguments.
- `src/evalfunc.c:991-1001` accepts Channel, Job, and unknown types for the
  `arg_chan_or_job` checker.
- `runtime/doc/vim9.txt:2756-2767` documents the method-call diagnostic.
- `src/errors.h:3127-3129` defines the exact E1217 message.

## Job required for builtin argument: E1218

Analysis reports E1218 at Vim9 script level when an `arg_job` builtin
argument has a known non-Job type. The diagnostic selects the complete
normalized argument and uses its one-based position, including method calls.

Job and dynamically typed values are accepted, while a Channel remains
incompatible with this Job-only checker. A mismatch in a compiled `def` or
block lambda retains E1013, Legacy calls are not diagnosed, and the broader
Channel-or-Job checker remains on E1217.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:2541-2585` distinguishes compiled E1013
  from script-level E1218 for the Job builtins and covers a Channel mismatch.
- `src/evalfunc.c:978-988` defines the strict `arg_job` checker.
- `runtime/doc/vim9.txt:2756-2768` documents the method-call diagnostic.
- `src/errors.h:3130-3131` defines the exact E1218 message.

## Float or Number required for builtin argument: E1219

Analysis reports E1219 at Vim9 script level when an `arg_float_or_nr`
builtin argument has a known incompatible type. The diagnostic selects the
complete normalized argument and uses its one-based position, including
method receivers and later arguments.

Float, Number, and dynamically typed values are accepted. A mismatch in a
compiled `def` or block lambda retains E1013, Legacy calls are not diagnosed,
and strict Number-only checkers remain on their own diagnostic path.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:65-69` distinguishes compiled E1013 from
  script-level E1219 for `abs()`.
- `src/testdir/test_vim9_builtin.vim:1441-1517` covers first and later
  Float-or-Number arguments across the math builtins.
- `src/evalfunc.c:281-293` defines the `arg_float_or_nr` checker.
- `runtime/doc/vim9.txt:2756-2769` documents the method-call diagnostic.
- `src/errors.h:3133-3134` defines the exact E1219 message.

## String or Number required for builtin argument: E1220

Analysis reports E1220 at Vim9 script level for known mismatches in the
String-or-Number, buffer, and line-number builtin checkers. It also reports
E1220 for an invalid Dictionary key passed as the second argument of
`remove()`. The diagnostic selects the complete normalized argument and uses
its one-based position, including method calls and later arguments.

String, Number, and dynamically typed values are accepted. `remove()` keeps
E1210 when its first argument is a List or Blob, and stays conservative when
the container type is unknown. A mismatch in a compiled `def` or block lambda
retains E1013, Legacy calls are not diagnosed, and buffer-or-Dictionary unions
remain outside this rule.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:223-249` distinguishes compiled E1013
  from script-level E1220 for line-number and buffer arguments.
- `src/testdir/test_vim9_builtin.vim:2584-2585` covers a later
  String-or-Number argument.
- `src/testdir/test_vim9_builtin.vim:3671-3674` covers the Dictionary branch
  of `remove()`.
- `src/evalfunc.c:493-555` defines the String-or-Number, buffer, and
  line-number checkers.
- `src/evalfunc.c:1142-1164` selects the second `remove()` argument checker
  from the first argument's container type.
- `src/errors.h:3135-3136` defines the exact E1220 message.

## String or Blob required for builtin argument: E1221

Analysis reports E1221 at Vim9 script level when an `arg_string_or_blob`
builtin argument has a known incompatible type. The diagnostic selects the
complete normalized argument and uses its one-based position, including
method receivers and later arguments.

String, Blob, and dynamically typed values are accepted. A mismatch in a
compiled `def` or block lambda retains E1013, Legacy calls are not diagnosed,
and String-or-Number checkers remain on E1220.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:593-598` distinguishes compiled E1013
  from script-level E1221 for a later raw-channel argument.
- `src/testdir/test_vim9_builtin.vim:4239-4242` covers the first argument of
  `sha256()`.
- `src/evalfunc.c:612-627` defines the `arg_string_or_blob` checker.
- `src/errors.h:3137-3138` defines the exact E1221 message.

## String or List required for builtin argument: E1222

Analysis reports E1222 at Vim9 script level when a String-or-List builtin
argument has a known incompatible outer type. The diagnostic selects the
complete normalized argument and uses its one-based position, including
method receivers and later arguments.

String, List, and dynamically typed values are accepted. For a
`list<string>` checker, a List with a known incompatible element type remains
on its existing type/content path rather than E1222. A mismatch in a compiled
`def` or block lambda retains E1013, Legacy calls are not diagnosed, and the
cursor checker's distinct String-or-Number-or-List contract remains separate.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:289-292` covers a later String-or-List
  argument.
- `src/testdir/test_vim9_builtin.vim:1038-1045` distinguishes an outer-type
  E1222 failure from a compiled List element-type E1013 failure.
- `src/testdir/test_vim9_builtin.vim:4297-4301` covers both outer-type and
  List element behavior for `sign_undefine()`.
- `src/evalfunc.c:555-593` defines the `list<string>` and List-of-any checker
  variants.
- `src/errors.h:3139-3140` defines the exact E1222 message.

## String or Dictionary required for builtin argument: E1223

Analysis reports E1223 at Vim9 script level when either ordering of the
String-or-Dictionary builtin checker receives a known incompatible type. The
diagnostic selects the complete normalized argument and uses its one-based
position, including method receivers.

String, Dictionary, and dynamically typed values are accepted. A mismatch in
a compiled `def` or block lambda retains E1013, Legacy calls are not
diagnosed, and the wider buffer-or-Dictionary union remains outside this
rule.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:814-817` distinguishes compiled E1013
  from script-level E1223 for `complete_add()`.
- `src/testdir/test_vim9_builtin.vim:2948-2951` covers the opposite checker
  spelling used by `mapset()`.
- `src/evalfunc.c:595-610` and `src/evalfunc.c:1070-1085` define the two
  checker orderings.
- `src/errors.h:3141-3142` defines the exact E1223 message.

## String, Number or List required for builtin argument: E1224

Analysis reports E1224 at Vim9 script level when either the general
String-or-Number-or-List checker or the cursor argument checker receives a
known incompatible type. The diagnostic selects the complete normalized
argument and uses its one-based position, including method receivers and later
arguments.

String, Number, List, and dynamically typed values are accepted. A mismatch
in a compiled `def` or block lambda retains E1013, Legacy calls are not
diagnosed, and wider unions such as buffer-or-Dictionary remain outside this
rule.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:249-252` distinguishes compiled E1013
  from script-level E1224 for a later `setbufline()` argument.
- `src/testdir/test_vim9_builtin.vim:917-920` covers the cursor-specific
  checker.
- `src/testdir/test_vim9_builtin.vim:4711-4720` covers the later input
  argument of `system()` and `systemlist()`.
- `src/evalfunc.c:1052-1068` defines the general checker, and
  `src/evalfunc.c:1218-1238` defines the cursor variant.
- `src/errors.h:3143-3144` defines the exact E1224 message.

## String, List, Tuple or Dictionary required for builtin argument: E1225

Analysis reports E1225 when the first argument of `count()` has a known type
other than String, List, Tuple, or Dictionary. The diagnostic selects the
complete normalized argument, including a method receiver, and uses argument
position one.

Unlike most neighboring builtin checker diagnostics, E1225 is emitted during
both Vim9 script evaluation and `def` or block-lambda compilation. String,
List, Tuple, Dictionary, and dynamically typed values are accepted. Legacy
calls and the separate five-kind checker that also accepts Blob remain outside
this rule.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:887-890` requires E1225 in both compiled
  and script contexts for `count()`.
- `src/evalfunc.c:1197-1216` directly emits E1225 from the
  `arg_string_list_tuple_or_dict` checker instead of using the generic type
  mismatch helper.
- `src/errors.h:3145-3146` defines the exact E1225 message.

## List or Blob required for builtin argument: E1226

Analysis reports E1226 at Vim9 script level when a normalized
`arg_list_or_blob` builtin argument has a known incompatible type. This covers
both ordinary and modifiable checker spellings. The diagnostic selects the
complete normalized argument and uses its one-based position, including a
method receiver.

List, Blob, and dynamically typed values are accepted. A mismatch in a
compiled `def` or block lambda retains E1013, Legacy calls are not diagnosed,
and the wider List-or-Dictionary-or-Blob checker remains separate.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:72-75` distinguishes compiled E1013 from
  script-level E1226 for `add()`.
- `src/testdir/test_vim9_builtin.vim:2434-2437` covers `insert()`.
- `src/evalfunc.c:429-455` defines the ordinary and modifiable checker
  variants through the generic compile-time type mismatch helper.
- `src/errors.h:3147-3148` defines the exact E1226 message.

## List, Dictionary or Blob required for builtin argument: E1228

Analysis reports E1228 at Vim9 script level when a normalized
`arg_list_or_dict_or_blob` builtin argument has a known incompatible type.
This covers both ordinary and modifiable checker spellings. The diagnostic
selects the complete normalized argument and uses its one-based position,
including a method receiver.

List, Dictionary, Blob, and dynamically typed values are accepted. A mismatch
in a compiled `def` or block lambda retains E1013, Legacy calls are not
diagnosed, and the wider variant that also accepts String remains outside this
rule.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:3668-3671` distinguishes compiled E1013
  from script-level E1228 for `remove()`.
- `src/evalfunc.c:647-677` defines the ordinary and modifiable checker
  variants through the generic compile-time type mismatch helper.
- `src/errors.h:3149-3152` distinguishes the unused List-or-Dictionary E1227
  message from the exact E1228 message.

## Dictionary or Object required for member access: E1229

Analysis reports E1229 in a compiled `def` or block lambda when dot-key
member access has a statically known receiver that is neither a Dictionary nor
an Object. The diagnostic selects the complete member expression and names
both the requested key and the inferred receiver type.

Dictionary, generic Object, local class, interface, enum-object, and dynamic
receivers are accepted. Static aggregate selectors, arrow-method syntax,
incomplete expressions, and Legacy functions remain on their existing paths.
At top-level Vim9 script, the same String member spelling retains E488. For a
nested invalid chain, only the first invalid member is reported.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_expr.vim:4183-4188` distinguishes compiled E1229 from
  top-level script E488 for a String receiver.
- `src/vim9instr.c:2282-2318` accepts Dictionary, Object, any, and unknown
  receiver types and directly emits E1229 for other compiled types.
- `src/errors.h:3153-3154` defines the exact E1229 message.

## Bar separator in a collected command block: E1231

Syntax analysis reports E1231 when a command inside a collected `:command {}`
or `:autocmd {}` body recognizes a top-level `|` itself after the block reader
has already selected the following physical line as the next command. The
diagnostic selects the conflicting bar, keeps its same-line tail opaque, and
resumes at the next physical line.

The check is limited to command consumers that call Vim's `set_nextcmd()`,
including variable deletion and locking, substitute and syntax commands,
find-pattern commands, `wincmd`, imports, function deletion, unambiguous
function definitions, and the `final` and `throw` expression consumers. The
generated command flags remain authoritative: commands with `EX_TRLBAR` or
`EX_EXPR_ARG` are exempt, so `echo 'hello' | echo 'there'` remains valid in a
collected block. A one-command `legacy` modifier does not leave the collected
block context.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `runtime/doc/map.txt:1875-1889` documents E1231 and the collected user-command
  block restriction.
- `src/ex_docmd.c:2337-2361` records the following physical line for a
  non-`EX_EXPR_ARG` command inside a block, while `src/ex_docmd.c:6005-6027`
  emits E1231 when that command later finds another separator.
- `src/evalvars.c:2109-2113`, `src/vim9script.c:657-662`, and
  `src/ex_cmds.c:4322-4331` are representative command-local
  `set_nextcmd()` consumers.
- `src/errors.h:3160-3161` defines the exact E1231 message.

## Literal argument required for `exists_compiled()`: E1232

Analysis reports E1232 for `exists_compiled()` calls compiled inside a `def`
or block lambda unless the parentheses contain exactly one direct single- or
double-quoted string literal. Number and identifier arguments, concatenations,
parenthesized strings, missing or extra arguments, and method forms retain the
compiler's E1232 path. The diagnostic selects the invalid argument, the extra
argument for an overfull call, or the function name when no direct argument is
present.

This special compile-time check owns the call before ordinary builtin arity and
argument-type checks, so it does not cascade to E118, E119, or E1013. Top-level
and one-command `legacy` calls are excluded because they reach the runtime
E1233 path instead.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:1071-1074` expects E1232 for Number and
  identifier arguments compiled in a `def`, while the same top-level forms use
  E1233.
- `src/vim9expr.c:1310-1357` handles `exists_compiled()` before ordinary call
  compilation and accepts only a directly parsed quoted string followed by the
  closing parenthesis.
- `runtime/doc/builtin.txt:2972-2984` requires a literal string and limits the
  builtin to `:def` functions.
- `src/errors.h:3163-3164` defines the exact E1232 message.

## `exists_compiled()` outside compiled Vim9 code: E1233

Analysis reports E1233 when a statically named `exists_compiled()` call with
valid builtin arity reaches Vim's runtime implementation. This includes
top-level Vim9 script, Legacy script and functions, Vim9-root `function`
bodies, a one-command `legacy` inside a `def`, and method calls outside compiled
Vim9 code. The diagnostic selects the function name.

Compiled Vim9 `def` and block-lambda calls retain the E1232 literal-string
check. Calls with missing or extra effective arguments retain E119 or E118,
because Vim checks builtin arity before invoking the runtime implementation.
The E1233 path owns the otherwise valid call and suppresses ordinary argument
type diagnostics such as E1013.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:1071-1074` expects E1233 for Number and
  identifier arguments at Vim9 script level, while compiled `def` forms use
  E1232.
- `src/evalfunc.c:4785-4788` implements the runtime builtin solely by emitting
  E1233.
- `runtime/doc/builtin.txt:2972-2984` limits `exists_compiled()` to `:def`
  functions.
- `src/errors.h:3165-3166` defines the exact E1233 message.

## Bare `legacy` command modifier: E1234

Syntax analysis reports E1234 when `legacy`, including an accepted abbreviation
such as `leg`, is immediately followed by the end of a command, a bar, or a
comment in the enclosing command dialect. The diagnostic selects the written
modifier and preserves the existing empty-command recovery so the following
command is still parsed.

The check runs before optional range parsing and only when `legacy` is the last
modifier. Thus `legacy 3delete` remains valid, `legacy vim9cmd` with no final
command retains E1164, and `vim9cmd legacy` with no final command uses E1234.
For comments, Vim9 context recognizes `#` and Legacy context recognizes `"`;
the one-command dialect switch does not change that decision.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_cmd.vim:14-15` expects E1234 for bare `legacy`.
- `src/ex_docmd.c:3144-3161` accepts `legacy` abbreviations down to three
  characters and emits E1234 immediately when `ends_excmd2()` sees a command
  terminator.
- `src/ex_docmd.c:5959-5970` defines the enclosing-dialect-aware terminator and
  comment rules used by that check.
- `runtime/doc/vim9.txt:146-147` states that `:legacy` cannot stand alone.
- `src/errors.h:3168-3169` defines the exact E1234 message.

## Bool or Number builtin argument: E1235

Analysis reports E1235 at top-level Vim9 script when argument one of
`getchar()` or `getcharstr()` has a statically known type that is neither Bool
nor Number. The diagnostic selects the invalid argument, includes its one-based
argument index, and owns the mismatch instead of also reporting E1013.

Compiled `def` and block-lambda calls retain E1013. Bool and Number values are
type-valid for this rule, so out-of-domain numeric constants such as `2` remain
on the existing E1023 value-check path. Unknown values, Legacy calls, and the
optional Dictionary argument keep their existing behavior.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:1883` and `:1902` distinguish compiled
  E1013 from top-level E1235 for String arguments to `getchar()` and
  `getcharstr()`.
- `src/typval.c:540-552` implements `arg_bool_or_nr` by accepting only Bool and
  Number values and emitting E1235 otherwise.
- `runtime/doc/vim9.txt:2758-2777` documents the top-level Vim9 builtin type
  check and its `getcharstr('9')` example.
- `src/errors.h:3171-3172` defines the exact E1235 message.

## Using an import namespace itself: E1236

Analysis reports E1236 when a resolved Vim9 import alias is used as the target
of a direct assignment or as a function callee. Calls inside compiled `def` and
block-lambda scopes follow the same rule. A nested `def` declaration also
reports E1236 when its name resolves to a visible root import alias. The
diagnostic selects the alias use and includes its spelling.

Contiguous `Alias.Member` reads, calls, and assignments remain valid namespace
access. Other bare namespace reads, compound assignments, indexing, and method
arrows retain E1060, while script-scope declarations that redefine an imported
name retain E1213.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_import.vim:501-507` and `:551-557` distinguish the
  top-level E1236 assignment and call cases from their separately invalid
  compiled-wrapper forms.
- `src/testdir/test_vim9_import.vim:2234-2240` expects E1236 when a nested
  `def` reuses an imported alias.
- `src/vim9expr.c:1298-1309` rejects an imported namespace name before
  compiling a direct call.
- `src/evalvars.c:4182-4200` rejects assignment to an imported namespace
  itself, and `src/userfunc.c:1974-1992` distinguishes imported-name function
  use from a new script-scope redefinition.
- `runtime/doc/vim9.txt:3586-3592` documents direct call use of an import
  namespace as E1236.
- `src/errors.h:3173-3174` defines the exact E1236 message.

## Blob builtin argument: E1238

Analysis reports E1238 at top-level Vim9 script when an `arg_blob` builtin
argument has a statically known non-Blob type. This covers `base64_encode()`,
`blob2list()`, and argument one of `blob2str()`, including method receivers.
The diagnostic selects the invalid effective argument, uses its one-based
index, and owns the mismatch instead of also reporting E1013.

Compiled `def` and block-lambda calls retain E1013. Valid Blob and unknown
values, Legacy calls, ordinary arity, and `blob2str()`'s optional Dictionary
argument keep their existing behavior.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:356` and `:366` distinguish compiled
  E1013 from top-level E1238 for `blob2list(10)` and `blob2str("ab")`.
- `src/typval.c:590-602` implements `arg_blob` and emits E1238 for non-Blob
  values.
- `runtime/doc/vim9.txt:2758-2778` documents the top-level Vim9 builtin type
  check and its method example.
- `src/errors.h:3179-3180` defines the exact E1238 message.

## Line-address arithmetic overflow: E1247

Syntax analysis reports E1247 when an accepted Ex range contains statically
provable 64-bit line-address overflow. This includes a relative decimal whose
magnitude reaches `LONG_MAX`, regardless of sign, and a positive explicit or
implicit offset that makes a known nonnegative base reach `LONG_MAX`. The
current-line address `.` has a guaranteed minimum of one, covering Vim's
`.9223372036854775806` regression shape. The diagnostic selects the offending
decimal token.

The check is deliberately conservative and independent of the Go host word
size. A huge standalone absolute address is not diagnosed solely from its
spelling, because Vim's checked E1247 paths occur during subsequent relative
address parsing. Digits inside search patterns, command arguments, Vim9
expressions without an explicit range colon, and non-overflowing negative
offsets remain untouched. The full range token and following command or bar
recovery are preserved.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_excmd.vim:721-734` covers long dot-relative addresses,
  the `LONG_MAX - 1` addition boundary, command recovery, and unchanged buffer
  contents after failure.
- `src/ex_docmd.c:4758-4805` emits E1247 when a parsed relative magnitude is
  `MAXLNUM` or positive addition reaches `LONG_MAX`.
- `src/charset.c:2254-2270` defines the decimal parsing used by that path, and
  `src/vim.h:1898-1919` defines `linenr_T` and the non-MVS `MAXLNUM` bound.
- `runtime/doc/cmdline.txt:781-790` documents absolute line-number addresses
  and E1247.
- `src/errors.h:3206-3207` defines the exact E1247 message.

## Container builtin argument: E1251

Analysis reports E1251 when a Vim9 builtin that accepts a List, Tuple,
Dictionary, Blob, or String receives a statically known value outside that
union. This covers `foreach()` and `items()` in both script and compiled
contexts, and top-level script calls to `filter()`, `map()`, and `mapnew()`.
The diagnostic selects the invalid effective argument and includes its
one-based index.

Compiled `filter()`, `map()`, and `mapnew()` calls retain the stricter E1013
from their generic compile-time checker. Tuple values pass this type check;
the separate restriction on mutating a Tuple is left to E1524. Valid or
unknown containers, Legacy calls, callback diagnostics, and arity diagnostics
keep their existing behavior.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:1563,1686,2523,2712-2714,2945-2947`
  distinguishes compiled E1013 from top-level E1251 for `filter()`, `map()`,
  and `mapnew()`, and covers E1251 from `foreach()` and `items()`.
- `src/evalfunc.c:683-733` defines the generic and native argument checkers;
  only the native checker includes Tuple and directly emits E1251.
- `src/list.c:2810-2840` applies the runtime union check before the separate
  Tuple restriction for the mutating functions.
- `runtime/doc/vim9.txt:2758-2779` documents the Vim9 builtin argument checks
  and an E1251 `filter()` example.
- `src/errors.h:3217-3218` defines the exact E1251 message.

## Sequence builtin argument: E1253

Analysis reports E1253 at top-level Vim9 script when the first argument to
`reduce()` or `reverse()` has a statically known type other than String, List,
Tuple, or Blob. The diagnostic selects that argument and includes its
one-based index.

Both builtins use a generic compile-time checker, so compiled `def` and
block-lambda calls retain E1013. Valid sequence values, unknown values, Legacy
calls, later `reduce()` argument diagnostics, and ordinary arity diagnostics
keep their existing behavior.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:3514,3736` distinguishes compiled E1013
  from top-level E1253 for `reduce()` and `reverse()`.
- `src/evalfunc.c:951-980` defines the two generic compile-time checkers and
  routes their rejected types through the ordinary argument mismatch.
- `src/typval.c:860-880` defines the runtime String, List, Tuple, or Blob
  union check and emits E1253.
- `src/list.c:3338-3342,3467-3473` applies that check in `reverse()` and
  `reduce()`.
- `runtime/doc/vim9.txt:2758-2780` documents the builtin argument checks and
  an E1253 `reverse()` example.
- `src/errors.h:3221-3222` defines the exact E1253 message.

## Script variable as a compiled loop binding: E1254

Analysis reports E1254 when a compiled Vim9 `def` or block lambda uses an
`s:` script variable as a `for` loop binding. The diagnostic selects the full
binding name and checks each destructured binding independently without
suppressing iterable diagnostics.

Top-level Vim9 loops may use `s:name`, with or without the prefix, and remain
outside this rule. Legacy loops, an explicit `legacy for`, ordinary local and
underscore bindings, and allowed scoped targets such as `g:name` also keep
their existing behavior.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_script.vim:3214-3237` rejects `for s:var` in a
  compiled `def` while explicitly permitting it at script level.
- `src/vim9cmds.c:1153-1175` rejects the `s:` spelling after classifying the
  compiled loop destination, while other non-local destinations use their
  normal store path.
- `runtime/doc/vim9.txt:1054-1064` documents the Vim9 loop-variable rules and
  allowed global-variable target.
- `src/errors.h:3223-3224` defines the exact E1254 message.

## String or function callback argument: E1256

Analysis reports E1256 when a compiled Vim9 callback checker receives a
statically known value that is neither a String nor a function or Partial.
This covers `filter()`, `map()`, `foreach()`, `sort()`, and `uniq()` in a
`def` or block lambda. Top-level Vim9 `sort()` and `uniq()` use the same code,
as does `indexof()` because its runtime implementation checks the callback
type directly. The diagnostic selects the invalid effective argument and
includes its one-based index.

Top-level `filter()` and `map()` still report E1024 when a Number must undergo
strict String conversion. A callable with an incompatible signature retains
its callback arity or type diagnostic rather than E1256. Valid String,
function, Partial, and unknown values, ordinary arity failures, and Legacy
calls keep their existing behavior.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_listdict.vim:1022-1027` expects E1256 for `sort()` with
  numeric callback selectors in both compiled and script contexts.
- `src/testdir/test_vim9_builtin.vim:1563-1564,2713-2715` distinguishes
  compiled E1256 from top-level E1024 for numeric `filter()` and `map()`
  callback arguments.
- `src/evalfunc.c:821-910` implements the native filter, map, foreach, and
  sort callback checkers and emits E1256 only for a non-String, non-callable
  value.
- `src/list.c:2529-2541` applies the direct runtime check for `sort()` and
  `uniq()`; `src/evalfunc.c:8435-8445` does the same for `indexof()`.
- `runtime/doc/vim9.txt:2758-2782` documents the builtin argument check and
  an E1256 `call()` example.
- `src/errors.h:3229-3230` defines the exact E1256 message.

## Assignment to an imported namespace: E1258

Analysis reports E1258 when a compiled Vim9 `def` or block lambda assigns
directly to an imported namespace alias instead of one of its members. The
diagnostic selects the alias and includes the trimmed assignment tail. It
applies to plain and compound assignment and remains available for an
incomplete right-hand side so following commands can still be analyzed.

At top-level Vim9 script, direct assignment retains E1236 and compound
assignment retains E1060. A compiled bare read also retains E1060, while a
compiled direct call retains E1236. Valid namespace-member assignments and
Legacy commands remain outside E1258.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_import.vim:342-356` creates an imported namespace and
  expects E1258 when a compiled `def` assigns directly to it.
- `src/vim9compile.c:1804-1823` emits E1258 when the imported assignment LHS
  has no member dot.
- `src/vim9compile.c:1340-1360` defines the compiled assignment operators that
  reach this LHS path.
- `runtime/doc/vim9.txt:3601-3611` documents E1258 with an incomplete
  `i_cc =` assignment.
- `src/errors.h:3233-3234` defines the exact E1258 message.

## Missing imported member name: E1259

Analysis reports E1259 when a compiled Vim9 `def` or block-lambda assignment
uses an imported namespace dot that is not followed by a valid member-name
start. The diagnostic selects the alias, includes the trimmed assignment tail,
and suppresses a provisional missing-member syntax diagnostic that describes
the same malformed target. Recovery continues through an incomplete
right-hand side.

Whitespace after the namespace dot retains E1074 precedence. Valid member
assignments, a missing-dot E1258 target, non-assignment reads and calls,
top-level Vim9 evaluation, Legacy commands, and non-import receivers remain
outside E1259.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_import.vim:357-364` expects E1259 for the compiled
  assignment `expo.99 = 9`.
- `src/vim9compile.c:1817-1831` skips whitespace after the import dot and
  emits E1259 when no valid name is consumed.
- `runtime/doc/vim9.txt:3611-3621` documents E1259 with `i_cc.8 = 0`.
- `src/errors.h:3235-3236` defines the exact E1259 message.

## Unlet of an imported item: E1260

Analysis reports E1260 when `unlet` or `unlet!` directly targets a member of
an imported namespace. This applies at top-level Vim9 script, in a compiled
`def` or block lambda, and in a Legacy function inside a Vim9-root script. The
diagnostic selects the member name and includes it in the message.

Removing an item inside an exported List or Dictionary is different from
removing the exported item itself. Nested member, index, and slice targets
therefore remain outside E1260 and continue to their own type, range,
mutability, or key checks. Bare aliases, malformed members, non-import
receivers, lock commands, and Legacy-root files also keep their existing
behavior.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_assign.vim:2858-2892` covers E1260 in a compiled
  `def`, at script level, and in a Legacy function inside a Vim9 script.
- `src/vim9cmds.c:124-154` emits E1260 for a compiled imported member only
  when no further index remains.
- `src/evalvars.c:2125-2161` applies the corresponding runtime rule.
- `runtime/doc/vim9.txt:3621-3630` documents the direct imported-item removal
  and member-only message.
- `src/errors.h:3237-3238` defines the exact E1260 message.

## Duplicate resolved import: E1262

Workspace analysis reports E1262 on the second import that resolves to the
same script, even when the source spellings or `as` aliases differ. The
comparison uses the import graph's canonical filesystem target, including
resolved symbolic links, rather than comparing raw path strings. The
diagnostic selects the second import path and includes its evaluated static
spelling.

Only resolved targets participate. Dynamic or unresolved imports remain
unknown instead of being guessed from similar text, and a self-import retains
the earlier E1088 precedence. Distinct targets and the first import of a
target remain quiet.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_import.vim:564-574` imports one script under two
  aliases and expects E1262 on the duplicate.
- `src/testdir/test_vim9_import.vim:3096-3111` covers duplicate autoload
  targets whose path spelling differs only by case on a case-insensitive
  filesystem.
- `src/vim9script.c:536-557` compares resolved script IDs, preserves reload
  handling, and emits E1262 for the later import.
- `runtime/doc/vim9.txt:3643-3651` documents duplicate imports with different
  aliases.
- `src/errors.h:3241-3242` defines the exact E1262 message.

## Autoload-style function name in Vim9: E1263

Syntax analysis reports E1263 when a non-exported Vim9 `def` or `function`
definition uses a valid name containing `#`. The diagnostic selects the full
function name. Such names bypass the ordinary E1267 capital-name check so the
autoload-specific rule owns the failure.

An exported definition remains outside E1263, as does a Legacy-root autoload
definition. A trailing `#` has no final function-name component and remains on
Vim's separate missing-name path. Hash characters in parameters, bodies, and
comments do not affect the definition name.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_import.vim:2791-2807` distinguishes a Legacy-root
  autoload definition from E1263 in a Vim9 script.
- `src/userfunc.c:5165-5182` handles exported definitions first, then emits
  E1263 for a parsed Vim9 function name containing the autoload separator.
- `runtime/doc/userfunc.txt:565-568` directs Vim9 scripts to use a name without
  `#` and export it instead.
- `src/errors.h:3243-3244` defines the exact E1263 message.

## Missing relative or absolute autoload import: E1264

Workspace analysis reports E1264 when a static `import autoload` using a
relative or absolute path cannot resolve to a readable regular file. The
diagnostic selects the import path and includes its decoded spelling. Valid
relative and absolute autoload imports remain supported and produce no error.

Runtime-path autoload names are distinct: a missing name below `autoload/`
retains E1053. Dynamic path expressions and paths that cannot be checked
safely remain unknown. Vim first reports its file-open error for a missing or
non-file direct path and then reaches E1264; the language server reports the
specific import error because it does not duplicate Vim's filesystem errors.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_import.vim:2993-3016` distinguishes missing relative
  and absolute autoload paths from a missing runtime-path autoload name.
- `src/vim9script.c:356-403` validates a direct autoload path without loading
  the target and preserves the unresolved script-ID sentinel on failure.
- `src/vim9script.c:471-531` separates relative and absolute paths from the
  `autoload/` runtime search and emits E1264 after a direct-path failure.
- `runtime/doc/vim9.txt:3745-3764` documents valid relative and absolute
  autoload imports, so E1264 is not applied to paths that resolve.
- `src/errors.h:3245-3246` defines the exact E1264 message.

## Script namespace in Vim9 script: E1268

Syntax analysis reports E1268 for a non-empty `s:name` used with Vim9 command
semantics in a Vim9-root file. This covers function and variable definitions,
reads, calls, assignments, loop bindings, expression mappings, and nested
expressions. The diagnostic selects the complete scoped name and is emitted
once even when one syntax node is retained by multiple owners.

The root dialect remains significant. A Legacy-root `def`, an explicitly
`legacy` command, and an ordinary Legacy `function` body in a Vim9 file retain
their legacy namespace rules. A bare `s:` namespace dictionary is not a named
item. Vim's special compiled `unlet s:name` path retains E1081, and a nested
definition already rejected as an unsupported namespace retains E1075.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_script.vim:214-255` covers `s:` function and variable
  definitions, calls, and reads at Vim9 script level.
- `src/testdir/test_vim9_assign.vim:219-226` and `3054-3083` cover assignment
  and read failures inside a Vim9 script `def`.
- `src/testdir/test_vim9_assign.vim:2802-2841` distinguishes script-level
  E1268 from the compiled `unlet` E1081 behavior and a Legacy function body.
- `runtime/doc/vim9.txt:1495-1538` defines the root-script, `def`, and legacy
  function namespace matrix, including explicit `legacy` commands.
- `src/vim9compile.c:1870-1894`, `src/userfunc.c:5108-5130`, and
  `src/eval.c:5328-5350` reject named `s:` uses in the applicable contexts.
- `src/errors.h:3257-3258` defines the exact E1268 message.

## Name expected: E1015

Syntax analysis reports E1015 when a compiled `def` tuple starts with a
missing item, such as `var value = (, 'a', 'b')`. The diagnostic selects the
leading comma and includes the remaining expression text in Vim's message.
The missing first child is retained so later tuple items and following
commands remain available after recovery.

The same expression uses E15 in Legacy script and at top-level Vim9 script.
This distinction follows the compiled-expression context, not the file's root
dialect: a `def` retained in a Legacy-root file still uses E1015.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_tuple.vim:156-162` expects E15, E1015, and E15 for the
  Legacy, compiled `def`, and top-level Vim9 variants respectively.
- `src/vim9expr.c:3134-3143` reports E1015 when a compiled expression expects
  a name but sees another non-ending token; an empty expression instead uses
  E1143.
- `src/errors.h:2672-2673` defines the exact message template.

## Using a Number as a Bool: E1023

Vim9 permits the Numbers `0` and `1` where a Boolean is expected, but rejects
every other Number with E1023. Analysis reports the error when the numeric
value is statically known in an `if`, `elseif`, or `while` condition, a ternary
condition, or an evaluated top-level Vim9 `&&` or `||` operand. The invalid
numeric expression is selected and the message contains its decimal value.

Logical operands are checked left-to-right and the right operand is checked
only when a statically known left operand evaluates it. Inside a compiled
`def`, invalid Number operands of `&&` and `||` remain the compile-time E1012
type mismatch; a direct condition such as `if 3` uses E1023 in both a `def` and
top-level Vim9 script. Legacy numeric truthiness remains unchanged. Dynamic
values stay unknown rather than being guessed.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_expr.vim:683-697` distinguishes E1012 in compiled
  logical expressions from E1023 in script evaluation and uses E1023 for
  `if 3` in both contexts.
- `src/testdir/test_vim9_expr.vim:829-846` covers left-to-right `&&`
  conversion and its compiled/script error-code split.
- `runtime/doc/vim9.txt:2501-2514` states that only 0 and 1 are accepted as
  Numbers used as Booleans.
- `src/typval.c:205-223` implements the Vim9 value check, and
  `src/errors.h:2690-2693` defines the exact E1023 and E1024 messages.

## Using a Number as a String: E1024

At top-level Vim9 script, `filter()` and `map()` evaluate their second
argument as either a String expression or a function. When a statically known
valid List, Dictionary, Blob, or String container is followed by a Number,
analysis reports E1024 on that Number. Method-call syntax follows the same
rule.

Legacy script still converts the Number to a String and does not receive this
diagnostic. A compiled `def` or Vim9 lambda instead applies strict callback
typing and uses E1256 for the same Number argument. If the first argument is
already an invalid container, its diagnostic takes priority and analysis does
not add E1024.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:1563-1564` distinguishes E1256 in a
  compiled `def` from E1024 at Vim9 script level for `filter()`.
- `src/testdir/test_vim9_builtin.vim:2713-2715` makes the same distinction for
  `map()`.
- `runtime/doc/vim9.txt:2501-2519` contrasts the accepted Legacy conversion
  with the Vim9 E1024 behavior.
- `src/typval.c:1206-1218` emits E1024 for strict Number-to-String conversion,
  and `src/errors.h:2692-2693` defines the exact message.

## Missing return statement: E1027

Analysis reports E1027 when an explicitly non-void `def` or block lambda can
reach its closing `enddef` or `}` without returning a value or throwing. The
diagnostic selects that closing token. A `def` retained in a Legacy-root file
still follows this compiled Vim9 rule.

A direct `return` or `throw` terminates the current path. An `if` terminates
only when it has an `else` and every `if`, `elseif`, and `else` branch
terminates. A return inside a `for` or `while` is not guaranteed because the
loop may execute zero times. For `try`, a `finally` that ends in `return` is
sufficient; without one, the try and catch paths must all end in `return` and
the catches must end with a catch-all. Pattern-only catches do not cover the
compiler's fallthrough case. Unlike a direct `throw` or one merged by `endif`,
a `throw` immediately before `endtry` is cleared by Vim's compiler state and
does not satisfy this rule.

An omitted or `void` return type does not require a return statement. A bare
`return` in a non-void function uses E1003 and still terminates the path, so it
does not also acquire E1027. Incomplete functions and control blocks retain
their syntax diagnostics instead of receiving a speculative missing-return
diagnostic.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_func.vim:519-533` contains the two official E1027
  compile cases, each with one non-returning `if` branch.
- `src/testdir/test_vim9_func.vim:544-595` accepts a final `throw` and complete
  `if`/`elseif`/`else` paths containing returns or throws.
- `src/testdir/test_vim9_script.vim:1212-1238` distinguishes a pattern-only
  catch that still produces E1027 from a `finally` that returns.
- `src/testdir/test_vim9_script.vim:5552-5561` applies E1027 to an explicitly
  non-void block lambda.
- `src/vim9compile.c:4561-4702` and `src/vim9cmds.c:600-868,1726-1960`
  maintain and merge the compiler's return and throw state.
- `src/vim9compile.c:4912-4938` emits the missing-return error at function
  completion, and `src/errors.h:2700-2701` defines its exact message.

## Using a String as a Number: E1030

E1030 means `Using a String as a Number: "{value}"`. Analysis reports it when
Vim9 runtime expression semantics require a statically known String to be a
Number. This includes arithmetic at script level, String-valued indexes and
slice bounds on Strings, Lists, Tuples, or Blobs at script level, and unary
`+` or `-` in both script and compiled `def` contexts. A simple literal
contributes its value to the message; a known String whose runtime value is
unavailable retains the native message prefix without inventing a value.

The diagnostic follows Vim's left-to-right conversion order. For example,
`'text' + 0z1122` reports E1030 on the String before considering the Blob.
Legacy expressions retain their permissive String-to-Number coercion. A
compiled `def` performs static checking first for binary arithmetic and
indexes: the corresponding errors remain E1051 for `+`, E1036 for `-`, `*`,
and `/`, E1035 for `%`, and E1012 for an index or slice bound.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_expr.vim:2046` distinguishes script-level E1030 from
  compiled E1051 for String plus Blob.
- `src/testdir/test_vim9_expr.vim:2267-2269` distinguishes script-level E1030
  from compiled E1036 or E1035 for String multiplication, division, and
  remainder.
- `src/testdir/test_vim9_expr.vim:4156-4157` requires E1030 for unary `-` and
  `+` in both compiled and script contexts.
- `src/testdir/test_vim9_expr.vim:4334-4346` distinguishes script-level E1030
  from compiled E1012 for String indexes and slice bounds.
- `src/vim9execute.c:8233-8253` rejects a runtime String used as a Number, and
  `src/errors.h:2706-2707` defines the exact message.

## Invalid identity comparison: E1037

E1037 means `Cannot use "{operator}" with {type}`. In Vim9 expressions,
analysis reports it when `is` or `isnot` compares two statically known values
of the same scalar type: Bool, Special, Number, or Float. The diagnostic
selects the identity operator. It applies in a compiled `def`, at script level,
and after `vim9cmd`; Legacy identity comparisons retain their historical
behavior.

Strings, Blobs, Lists, Dictionaries, Functions, and Objects support identity
comparison and do not receive E1037. Mixed Number and Float operands, mixed
Special-and-other-type comparisons, and unknown or `any` values also remain
outside this rule. The suffixed operators `is#`, `is?`, `isnot#`, and
`isnot?` are invalid Vim9 syntax and retain E15 instead of being mapped to
E1037.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_expr.vim:1645-1648` rejects Number identity in both
  compiled and script contexts.
- `src/testdir/test_vim9_expr.vim:1736-1744` covers `is` and `isnot` for Bool,
  Special, Number, and Float, and distinguishes the invalid operator suffixes.
- `src/testdir/test_vim9_disassemble.vim:2302-2323` retains valid identity
  comparisons for reference-like and String values.
- `src/vim9instr.c:495-585` selects comparison instructions and emits E1037
  only for these four same-type scalar comparisons.
- `src/errors.h:2721-2722` defines the exact message.

## Function nesting too deep: E1058

E1058 means `Function nesting too deep`. Analysis reports it on the 51st
nested named function definition, which is the point where Vim's function-body
collector would exceed its 50-entry stack. Legacy `function` definitions and
Vim9 `def` definitions use the same native error. Other control blocks do not
contribute to this depth.

The parser retains the over-deep block after reporting E1058 so later commands
and closing tokens can still be recovered. Cross-dialect definitions retain
their existing loose recovery, and the static rule does not claim inline block
functions that are not represented as named syntax blocks.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vimscript.vim:7367-7403` dynamically enters 51 nested
  Legacy `function` definitions and asserts E1058.
- `runtime/doc/vim9.txt:1428-1431` documents the corresponding nested `def`
  limit.
- `src/userfunc.c:982-987,1203-1231` defines the 50-entry stack and rejects
  the next nested named definition.
- `src/userfunc.c:1254-1272` applies the same native limit while collecting
  inline block functions.
- `src/errors.h:2765-2766` defines the exact message.

## Import alias requires a dot: E1060

E1060 means `Expected dot after name: {text}`. A Vim9 import alias names a
namespace rather than a value, so analysis reports E1060 when a resolved alias
reference does not reach its namespace dot. The diagnostic selects the alias.
Expression uses preserve the source tail that Vim includes in the message,
while a compound assignment target retains the alias alone. This covers bare
values, arithmetic, compound assignment targets and operands, and a line break
before the dot. Script evaluation also rejects a space before the dot. Direct
assignment uses its own E1094 or E1236 rule instead of E1060.

A contiguous member access such as `module.Export` does not receive E1060 and
does not require resolving the imported file first. The compiled `def` lookup
also skips same-line horizontal space before the dot; script evaluation does
not. Once the applicable lookup recognizes a dot, whitespace or a line break
after it uses E1074 instead. Legacy-dialect recovery remains conservative,
while an explicit `vim9cmd` command uses Vim9 import namespace semantics.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_import.vim:151-159` reports E1060 for a line break
  before the member dot.
- `src/testdir/test_vim9_import.vim:191-200` reports E1060 from a compiled
  `def` that uses an import alias in arithmetic.
- `src/testdir/test_vim9_import.vim:223-229` preserves the exact
  `Export exported` message tail.
- `src/testdir/test_vim9_import.vim:507-525` covers a bare alias and compound
  assignment on both sides.
- `runtime/doc/vim9.txt:3567-3576` documents the namespace-dot form.
- `src/vim9expr.c:650-696`, `src/eval.c:7660-7675`, and
  `src/evalvars.c:3180-3205` implement the compiled and evaluated checks.
- `src/errors.h:2769-2770` defines the exact message.

## Cannot index a Number: E1062

E1062 means `Cannot index a Number`. Analysis reports it when a Vim9
script-level expression indexes or slices a receiver that is statically known
to be a Number. The diagnostic selects the receiver. Literal and resolved
Number values are supported, while `any` and otherwise unknown receivers stay
unknown.

Legacy expressions retain their historical Number indexing behavior. A
compiled `def` rejects the same statically known receiver earlier with E1107,
so it does not also receive E1062. A dynamically typed value can still produce
E1062 while a `def` executes, but static analysis does not guess that runtime
value. Float receivers remain E806 at script level rather than being folded
into this rule.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_expr.vim:2283-2284` distinguishes script-level E1062
  for a Number receiver from E806 for a Float and compiled E1107 for both.
- `src/testdir/test_vim9_expr.vim:2589-2590` covers a Number literal and the
  runtime-only dynamically typed `def` case.
- A clean Vim v9.2.1015 source containing `var x = 1234[1 : 2]` reports E1062,
  confirming that the shared runtime check also covers slices.
- `src/eval.c:6083-6131` emits E1062 for a Number receiver only under Vim9
  evaluation semantics.
- `src/vim9expr.c:137-320` routes the statically compiled form to E1107.
- `src/errors.h:2773-2774` defines the exact message.

## Invalid import string: E1071

E1071 means `Invalid string for :import: {text}`. Vim evaluates the import
path expression before resolving a file and requires a non-null, non-empty
String. Syntax analysis reports E1071 when that failure is statically certain:
an empty string literal, a literal value whose type is not String, or Vim's
zero-argument `test_null_string()` builtin. The diagnostic selects the path
expression, while its message retains the remaining import text exactly as Vim
does.

Dynamic expressions remain unknown because normal language-server analysis
does not execute Vim script. Legacy-root recovery also stays quiet; retaining
an `import` command there does not apply Vim9 import evaluation semantics.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_import.vim:610-616` asserts the exact E1071 message
  for an empty String, an empty List, and the null String returned by
  `test_null_string()`.
- `src/vim9script.c:399-443` evaluates the path, then rejects a non-String,
  null String, or empty String before attempting file resolution.
- `runtime/doc/testing.txt:362-365` defines `test_null_string()` as the
  zero-argument test builtin that returns a null String.
- `src/errors.h:2791-2792` defines the exact message.

## Incompatible comparison types: E1072

E1072 means `Cannot compare {type} with {type}`. Analysis reports it for a
Vim9 comparison when both operand types are statically known and Vim cannot
select a valid comparison. This includes incompatible types, ordering or
pattern operations on Bool, Special, List, or Blob values, and comparisons of
`v:none` with a non-String value. The diagnostic selects the operator and uses
Vim's lower-case type names in operand order.

Number and Float remain mutually comparable. A String may be compared with
`v:none`, and other null Special comparisons retain Vim's separate semantics.
An operand without a concrete type fact stays unknown because its runtime value
decides whether E1072 occurs. Legacy commands retain their historical
conversion and container-specific errors instead of receiving this Vim9 code.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_expr.vim:1293-1313` covers List, Number, and Blob
  values compared with `v:none` in both compiled and script evaluation.
- `src/testdir/test_vim9_expr.vim:1316-1343` covers runtime-dependent `any`
  values as well as the statically known Special/Bool and Bool/Bool failures.
- `src/testdir/test_vim9_expr.vim:1634-1643` covers String/Number mismatch and
  ordering two Bool values.
- `src/vim9instr.c:495-589` selects compiled comparison instructions, permits
  Number/Float and dynamic `any`, and emits E1072 for the rejected type pairs.
- `src/typval.c:1619-1629,2016-2023` applies the corresponding Vim9 runtime
  checks.
- `src/errors.h:2793-2794` defines the exact message.

## Import member whitespace after dot: E1074

E1074 means `No white space allowed after dot`. After resolving a Vim9 import
alias and recognizing its namespace dot, Vim requires the exported member name
to start immediately. Analysis reports E1074 for a same-line space or a
continued line break after that dot and selects the intervening whitespace.

This is not the generic member-access E1202 rule. Name resolution determines
whether the receiver is an import alias, and diagnostic composition removes
the provisional generic spacing or missing-member diagnostic before exposing
E1074. If script evaluation sees whitespace before the dot, it reports E1060
instead. A compiled `def` skips same-line horizontal whitespace before the dot,
so `Export . exported` reaches E1074. A contiguous `Export.` with no member
uses E1048, and Legacy-dialect member access remains unchanged.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_import.vim:151-168` distinguishes E1060 for a line
  break before the dot from E1074 for a line break after it.
- `src/testdir/test_vim9_import.vim:202-211` reports E1074 from a compiled
  `def` whose import dot has whitespace on both sides.
- `src/testdir/test_vim9_import.vim:231-250` distinguishes script-level E1074
  after the dot from E1048 when the member name is absent.
- A clean Vim v9.2.1015 oracle confirms that script evaluation rejects
  `Export .Member` with E1060, while a compiled `def` accepts it and reports
  E1074 only when whitespace also follows the dot.
- `src/vim9expr.c:665-696` implements the compiled import-member lookup.
- `src/eval.c:7656-7676` implements the corresponding evaluated lookup.
- `src/errors.h:2797-2798` defines the exact message.

## Cannot unlet a Vim9 variable: E1081

E1081 means `Cannot unlet {name}`. A Vim9 `:unlet` command may directly remove
only variables in the `g:`, `w:`, `t:`, or `b:` namespaces and environment
variables. It cannot remove a local, script-local, argument, predefined, or
otherwise unqualified variable, and `:unlet!` does not relax this rule. The
diagnostic selects the direct target name.

A direct `s:name` at Vim9 script level is rejected earlier as E1268. E1081
applies when that target reaches compilation inside a Vim9 `def`; a Legacy-root
script keeps the historical script-local behavior.

Removing an item is different from removing its containing variable. List and
Dictionary targets such as `items[0]`, `dict.key`, and `s:dict.key` remain
valid E1081-wise and continue to their own index, key, mutability, or imported
item checks. Legacy `:unlet` retains its historical behavior; an explicit
`vim9cmd unlet` uses the Vim9 restriction.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_assign.vim:2636-2658` covers `unlet` and `unlet!` for
  script-local and local variables, while allowing Dictionary item removal.
- `src/testdir/test_vim9_assign.vim:2802-2843` covers script-level variables,
  closed-over script variables, Legacy functions, and `vim9cmd` inside a def.
- `src/vim9cmds.c:83-102` permits only direct `g:`, `w:`, `t:`, and `b:` names
  under Vim9 semantics.
- `src/vim9cmds.c:109-169` routes environment and indexed targets separately
  before applying the direct-name restriction.
- `src/errors.h:2812-2813` defines the exact message.

## Non-number bitshift operand: E1282

E1282 means `Bitshift operands must be numbers`. Analysis reports it on the
`<<` or `>>` operator when a statically known operand is not a Number. The
left operand is checked first, matching Vim's evaluation order. Unknown and
`any` operands stay quiet because their runtime values decide the result.

Legacy and script-level Vim9 expressions apply the runtime rule directly. In
a compiled `def` or expression lambda, Vim instead reports E1012 for a typed
non-Number variable. E1282 remains the compiled result for primitive constants
that Vim precomputes, including String, Float, Blob, Bool, Special, and typed
null literals. Parentheses around such a literal do not change that result.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_expr.vim:1043-1050` distinguishes literal E1282 failures,
  compiled variable E1012 failures, and the context-dependent `![]` cases.
- `src/vim9expr.c:3428-3541` checks precomputed operands by runtime value type
  while using compiled stack type checks for other expressions.
- `src/eval.c:4417-4448,4490-4497` implements the evaluated operand checks and
  bitshift operation.
- `runtime/doc/eval.txt:1422-1432` documents Number operands and shift amounts.
- `src/errors.h:3295-3296` defines the exact message.

## Negative bitshift amount: E1283

E1283 means `Bitshift amount must be a positive number`. Vim's implementation
allows zero and rejects only a negative right operand. Analysis reports E1283
on that right operand for `<<` and `>>` when its Number value is statically
known.

This covers signed Number literals and a simple same-scope binding whose
static Number initializer remains unchanged before the shift. Reassigned,
cross-scope, dynamic, `any`, and otherwise unresolved values stay quiet. A
non-Number operand continues to use E1282 or the compiled E1012 rule first.
For a left-associated chain, analysis exposes the first failing shift in Vim's
evaluation order.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_expr.vim:1041-1042` covers a direct negative amount and a
  negative amount loaded from a variable in Legacy, compiled, and script
  contexts.
- `src/vim9expr.c:3502-3530` rejects a negative precomputed right operand while
  folding a compiled shift.
- `src/vim9execute.c:5520-5539` rejects a negative amount loaded by compiled
  bytecode at execution time.
- `src/eval.c:4417-4448` implements the same check for evaluated expressions.
- `runtime/doc/eval.txt:1422-1432` documents bitshift operands and amounts.
- `src/errors.h:3297-3298` defines the exact message.

## Non-repeatable builtin argument: E1301

E1301 means `String, Number, List, Tuple or Blob required for argument 1`.
Analysis reports it for a Vim9 script-level `repeat()` call when the first
argument has a statically known type outside those five repeatable kinds. The
diagnostic selects that argument, including a method-call receiver.

A compiled `def` or expression lambda retains Vim's E1013 static signature
error for the same mismatch. Unknown and `any` values remain unresolved, valid
repeatable values stay quiet, and Legacy calls retain their conversion
behavior. Ordinary arity diagnostics continue to own malformed calls.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:3713-3721` distinguishes compiled E1013
  from script-level E1301 and exercises the supported repeatable values.
- `src/evalfunc.c:1160-1176` defines the compiled first-argument checker and
  permits String, Number, Blob, List, Tuple, `any`, and unknown types.
- `src/evalfunc.c:10633-10654` applies the runtime Vim9 checks before selecting
  the repeat implementation.
- `src/typval.c:937-953` emits E1301 for a non-repeatable runtime value.
- `runtime/doc/builtin.txt:9336-9350` documents `repeat()` and method use.
- `src/errors.h:3348-3352` defines the exact message.

## Compiled loop nesting limit: E1306

E1306 means `Loop nesting too deep`. A compiled Vim function supports at most
ten simultaneously enclosing `for` and `while` loops. Analysis reports E1306
on the command name that opens the eleventh loop; both loop forms contribute
to the same depth.

The count belongs to one compiled `def` or lambda. A nested lambda starts a new
count instead of inheriting enclosing loops. Top-level Vim9 execution and
Legacy `function` blocks do not use this compiler limit. Other block kinds do
not contribute to the count, and deeper loops in the same already-invalid
chain do not repeat the diagnostic.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_script.vim:3158-3213` covers ten successful mixed
  loops and failures for an eleventh mixed or `for` loop.
- `src/structs.h:2501` defines `MAX_LOOP_DEPTH` as 10.
- `src/vim9cmds.c:1002-1007` applies the shared depth limit while compiling
  `for`.
- `src/vim9cmds.c:1315-1320` applies the same limit while compiling `while`.
- `src/errors.h:3365-3366` defines the exact message.

## Const value passed to a modifying builtin: E1307

E1307 means `Argument {n}: Trying to modify a const {type}`. In a compiled
`def` or lambda, analysis reports it when a modifying builtin receives a
direct reference to a `const` binding. The diagnostic selects the argument or
method receiver and preserves Vim's inferred container type in the message.

The modifying checker set covers `add()`, `extend()`, `filter()`, `map()`,
`remove()`, `reverse()`, `sort()`, and `uniq()`. `final` bindings and loop
items still permit mutations of their contained value. Non-modifying variants
such as `extendnew()` and `mapnew()` do not use E1307, and analysis does not
propagate constness through aliases. A type error on the modified argument
precedes E1307; once E1307 is selected, later argument and callback diagnostics
for that call are suppressed.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_builtin.vim:179-204,1294-1337` distinguishes `const`
  List, Dictionary, and Blob failures from successful `final` and loop-item
  mutation for `add()` and `extend()`.
- `src/testdir/test_vim9_builtin.vim:1637-1650,2775-2788,3649-3760` covers
  `filter()`, `map()`, `remove()`, and `reverse()`.
- `src/testdir/test_vim9_builtin.vim:4399-4406,5077-5084` covers `sort()` and
  `uniq()`.
- `src/evalfunc.c:238-267` first checks the argument type, then rejects its
  `TTFLAG_CONST` flag and formats the concrete type name.
- `src/evalfunc.c:1280,1348-1383` assigns the modifying checkers to these
  builtin argument tables while keeping their non-modifying variants separate.
- `src/errors.h:3367-3368` defines the exact message.

## Lowercase class name: E1314

E1314 means `Class name must start with an uppercase letter: {argument}`. In a
Vim9 class or abstract class declaration, the first name byte must be an ASCII
uppercase letter. Analysis selects the parsed class name while retaining Vim's
full trimmed declaration argument in the message.

The uppercase check happens before the malformed-name E1315 check. A lowercase,
numeric, underscore, or non-ASCII first byte therefore owns the diagnostic even
when invalid punctuation follows the name. The recovering aggregate and block
remain available, and parsing continues after `endclass`. A Legacy class is
owned by E1316 instead.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:5-27` distinguishes the Vim9-only class
  requirement, lowercase-name E1314, and malformed-name E1315.
- `src/vim9class.c:1945-1963` routes both `class` and `abstract class` through
  the same declaration logic.
- `src/vim9class.c:1965-1993` checks the Vim9-script gate, then ASCII uppercase,
  then whitespace after the name in that order.
- `src/errors.h:3385-3388` defines E1314 and the following E1315 message.

## Missing whitespace after a type name: E1315

E1315 means `White space required after name: {remainder}`. Vim9 aggregate and
type-alias declarations require whitespace after each declared or referenced
type name before punctuation, an assignment, or another grammar element.
Analysis reports E1315 on the complete offending remainder while retaining the
partially parsed declaration for recovery.

The rule covers class, interface, and enum names; names after `extends` and
`implements`; comma-separated interface lists; and type-alias names before `=`
or another separator. A lowercase first byte is rejected earlier by the
aggregate-specific uppercase diagnostic. Once E1315 is selected, later
trailing-text, missing-assignment, and assignment-spacing diagnostics are
suppressed. Legacy aggregate declarations retain their Vim9-script gate.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:21-27,2477-2485` covers punctuation in a
  class name and after an extended class name.
- `src/testdir/test_vim9_interface.vim:300-361,525-535,1088-1098` covers
  extended and implemented names plus comma separation.
- `src/testdir/test_vim9_enum.vim:22-36` distinguishes uppercase-name and
  missing-whitespace failures for enums.
- `src/testdir/test_vim9_typealias.vim:78-118` distinguishes whitespace around
  an alias name and assignment from the neighboring E1069 and E1394 rules.
- `src/vim9class.c:1978-1993,2907-2917` applies uppercase checks before the
  shared whitespace-after-name error for aggregates and type aliases.
- `src/errors.h:3387-3388` defines the exact message.

## Invalid command in a class body: E1318

E1318 means `Not a valid command in a class: {command}`. A Vim9 class body
accepts member declarations and method definitions directly; other commands
are rejected at that level. Analysis selects the complete offending command,
including any class modifier, and uses the same text in the diagnostic message.

The restriction applies only to the direct class body. Commands inside a valid
method remain method-body syntax, while interfaces and enums use their own
aggregate-specific diagnostics. A bare `def` or `static def` is also E1318 and
enters method recovery so its `enddef` cannot close the class accidentally. An
abstract method signature cannot acquire a body: its first body command is
E1318. Diagnostics produced only while parsing an invalid class-body command
are suppressed, and parsing resumes at the class closer and following top-level
declarations.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:142-240` covers obsolete `this.` member
  spellings, `that`, and `variable` as invalid direct class-body commands.
- `src/testdir/test_vim9_class.vim:491-509` covers bare `def` and `static def`.
- `src/testdir/test_vim9_class.vim:2124-2134` rejects an arbitrary command after
  a valid class method, and lines 5671-5681 reject a body command after an
  abstract method signature.
- `src/vim9class.c:2473-2502,2578-2586` distinguishes a missing method name and
  the aggregate-specific fallback for invalid body commands.
- `src/errors.h:3393-3394` defines the exact message.

## Method not found on a class: E1325

E1325 means `Method "{method}" not found in class "{class}"`. For a complete
Vim9 dot call whose receiver resolves to a same-file class or enum, analysis
selects the method name after proving that neither the applicable method table
nor a function-typed member contains it. Unknown values and cross-file import
namespaces remain conservative.

Class receivers use only class methods declared on that aggregate; class
methods are not inherited. Object receivers use object methods and object
members through the class hierarchy. Calling a method through the wrong kind
of receiver remains owned by E1385 or E1386, and protected access remains owned
by E1366. `super` searches inherited object methods but reports the current
class name when none exists. A local class type alias resolves to its underlying
class for this message.

A concrete class without an explicit `new` or `_new` has Vim's generated
`new()` constructor. Abstract classes do not receive that default, an exact
`_new` suppresses the public default, and enum constructor methods whose names
start with `new` or `_new` are removed after the enum is created. Ordinary
function-typed class and object members remain callable.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:2818-2834` shows that `super` does not expose
  a parent class method, and lines 2938-2948 reject the missing constructor of
  an abstract class.
- `src/testdir/test_vim9_class.vim:4980-4998,10958-10968` covers non-inherited
  protected class methods and `_new` suppressing the default `new`.
- `src/testdir/test_vim9_enum.vim:1332-1375` rejects default and named enum
  constructors at script and compiled-function level.
- `src/testdir/test_vim9_generics.vim:1365-1377` applies E1325 to a missing
  generic object method.
- `src/vim9expr.c:420-475` chooses the class or object method table, handles
  `super`, and accepts a function-typed member before reporting a missing
  method.
- `src/vim9class.c:1467-1520,1529-1608,2709-2749` defines the default
  constructor, inherits only object methods, and removes enum constructors.
- `src/vim9class.c:4080-4106` selects E1325 after ruling out a wrong receiver
  kind; `src/errors.h:3404-3405` defines the exact message.

## Variable not found on an object: E1326

E1326 means `Variable "{variable}" not found in object "{class}"`. Analysis
reports it for a complete Vim9 member read, write, or method reference whose
receiver has a known same-file class or enum type and whose object-variable and
object-method tables contain no matching name. Ordinary calls remain owned by
E1325, while unknown values and cross-file import namespaces remain
conservative.

Class object variables and object methods are inherited. Class variables and
class methods are not: accessing an inherited static name through a child
object is E1326, while accessing a static name declared directly on that
object's class remains owned by E1375 or E1385. Protected and non-writable
object variables remain reserved for E1333 and E1335 instead of being
downgraded to E1326. `this` is resolved in object methods and constructors, and
`super` searches the parent object table but names the current class in the
diagnostic. Enum objects expose `name`, `ordinal`, declared object variables,
and object methods.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:242-252,1275-1280,1931-1939` covers a
  missing `this` field, assignment target, and object read.
- `src/testdir/test_vim9_class.vim:11140-11163` distinguishes a valid inherited
  object variable through `super` from a non-inherited static variable and
  reports the current class name.
- `src/testdir/test_vim9_class.vim:826-836,10201-10245` reaches E1326 through
  runtime `any` values; analysis deliberately leaves those flows unknown.
- `src/vim9class.c:4113-4126` gives a directly declared class variable the
  higher-priority wrong-receiver diagnostic before selecting E1326.
- `src/vim9execute.c:3360-3410` accepts variables and bound method references
  before choosing E1326 for a missing object member; `src/errors.h:3406-3407`
  defines the exact message.

## Invalid constructor shorthand default: E1328

E1328 means `Constructor default value must be v:none: {tail}`. Inside a Vim9
class-like aggregate, a `def` method whose name starts with `new` may use
`this.member` constructor shorthand parameters. If such a parameter has a
default, its value must start with `v:none`; analysis reports the original text
from the end of the member name through the default value so the message and
span preserve the user's equals-sign spacing.

The rule does not apply to ordinary constructor parameters, top-level
functions, other method names, `_new`, typed `this.member` recovery, or a
parameter without a default. Vim checks only the first six nonblank bytes of
the default at this stage. A tail beginning with `v:none` therefore belongs to
later expression or separator validation rather than E1328.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:1054-1066` reports the exact message for
  `def new(this.val = 'a')`.
- `runtime/doc/vim9class.txt:816-850` documents `v:none` as the sole constructor
  shorthand default and explains that it leaves the member initializer in
  control.
- `src/userfunc.c:320-347` restricts `this.member` parameters to methods whose
  names start with `new`, checks the six-byte `v:none` prefix, and passes the
  untouched text beginning after the member name to the diagnostic.
- `src/errors.h:3410-3411` defines the exact message.

## Invalid value-declaration type: E1330

E1330 means `Invalid type used in variable declaration: void`. The `void` type
describes the absence of a returned value and therefore cannot be used where a
Vim9 value must exist. Analysis reports the first invalid member of each type
at the inner `void` span.

Value positions include script, local, class-like member, loop-binding,
function-parameter, and lambda-parameter declarations; container and function
argument types; and generic call type arguments. A direct function or lambda
return type may be `void`, but a value type nested inside that return type may
not be. The same recursive rule applies while parsing a type alias: a direct
`type Empty = void` is valid, whereas `type Items = list<void>` is not.

When a value declaration uses a same-file alias whose expansion is invalid,
analysis reports `void` in the message and selects the alias use as the source
span. Alias cycles and unresolved or imported aliases remain conservative.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_assign.vim:2344-2435` covers locals, function and
  lambda parameters, containers, class members, loop bindings, generic type
  arguments, function types, and type-alias uses.
- `src/testdir/test_vim9_class.vim:1242-1253` covers a `void` object variable.
- `src/vim9type.c:1001-1016` defines the value-declaration predicate: `void`
  and special null/none types are rejected.
- `src/vim9type.c:1620-1651,1695-1721,1830-1853` applies the predicate to
  container, function-argument, and tuple member types while leaving function
  return types separate.
- `src/userfunc.c:550-578`, `src/vim9class.c:75-101`, and
  `src/vim9generics.c:296-311` apply it to parameters, aggregate members, and
  generic arguments.
- `src/errors.h:3414-3415` defines the exact message.

## Public variable beginning with underscore: E1332

E1332 means `public variable name cannot start with underscore: {command}`.
Vim9 aggregate variables use a leading underscore to request protected access,
so an explicit `public` modifier on the same name is contradictory. Analysis
reports the complete direct member command, preserving its original modifiers,
type, initializer, and spacing in both the message and span.

The check applies to variable-like `var`, `final`, and `const` members,
including static members, but not to methods, aggregate-external declarations,
or an implicit protected variable without `public`. It runs as soon as the
member name is available, before type and initializer validation. The invalid
member does not participate in the later public/protected name-pair E1406
check.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:1284-1292` reports the exact complete command
  for `public var _val = 10`.
- `runtime/doc/vim9class.txt:145-170` defines underscore-prefixed variables as
  protected and associates E1332 with that rule.
- `src/vim9class.c:57-82` performs the public/underscore check immediately
  after finding the member name and before parsing its type or initializer.
- `src/errors.h:3418-3419` defines the exact message.

## Protected variable access: E1333

E1333 means `Cannot access protected variable "{variable}" in class
"{class}"`. A Vim9 aggregate variable whose name starts with an underscore is
protected unless it is explicitly public (which is itself invalid under
E1332). Analysis reports the member-name span for a complete read, write, or
function-typed member call whose receiver resolves to a same-file class or
enum.

An object variable is accessible inside its defining class and descendant
classes. An inherited variable therefore names the class that defined it in
the diagnostic. A static variable is accessible only inside its exact defining
class, not from a descendant. The same object rule applies to enum members.

Unknown values, imported aggregates, and incomplete member expressions remain
conservative. Public underscore declarations remain owned by E1332, protected
methods by E1366, and access through the wrong receiver kind by the applicable
class/object diagnostic.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:1270-1277` covers external protected object
  reads and writes, while lines 1502-1536 show that a child cannot access a
  protected static variable declared by its parent.
- `src/testdir/test_vim9_class.vim:1940-2052` covers inherited protected object
  variables and uses the defining class in the diagnostic.
- `src/testdir/test_vim9_class.vim:780-815` reaches the rule through runtime
  `any` values; analysis deliberately leaves those flows unknown.
- `src/vim9expr.c:530-620` applies the distinct object and static access rules
  to reads, method references, and calls.
- `src/vim9compile.c:1635-1665,2070-2105` applies the same rules to assignment
  targets, and `src/vim9class.c:3626-3637` defines descendant object access.
- `src/errors.h:3420-3421` defines the exact message.

## Variable is not writable: E1335

E1335 means `Variable "{variable}" in class "{class}" is not writable`.
Vim9 aggregate variables without `public` are readable outside their class but
cannot be written there. Analysis reports the member-name span for a complete
ordinary or compound assignment, or a `lockvar`/`unlockvar` target, whose
receiver resolves to a same-file class or enum.

An object variable is writable inside its defining class and descendant
classes. An inherited variable therefore names the class that defined it in
the diagnostic. A static variable is writable only inside its exact defining
class. Public variables bypass E1335, while an inaccessible underscore-prefixed
variable is E1333. If access is allowed, `final` and `const` assignment remains
owned by E1409; access denial takes priority and produces E1335 instead.

At script level, writes to built-in enum object variables such as `name` and
`ordinal` also produce E1335. The corresponding compiled-function and enum
constructor cases retain their more specific E1423, E1426, and E1427
diagnostics. Unknown values, imported aggregates, incomplete member targets,
and wrong receiver kinds remain conservative or owned by their existing
diagnostics.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:860-925` covers nested typed object members
  and distinguishes an explicitly public final member from a default one.
- `src/testdir/test_vim9_class.vim:1460-1500,1685-1720` covers descendant and
  external writes to static variables, compound assignment, and public access.
- `src/testdir/test_vim9_class.vim:1945-2010` covers inherited object variables
  and uses the defining class in the diagnostic.
- `src/testdir/test_vim9_class.vim:3685-3770` applies E1335 before the general
  object-member `lockvar` restriction when access itself is denied.
- `src/testdir/test_vim9_enum.vim:1200-1225,1280-1305` distinguishes
  script-level E1335 for `name` and `ordinal` from compiled-function E1423.
- `src/vim9compile.c:1627-1663,2051-2105` checks object and static write access
  before read-only flags and aggregate-specific mutation rules.
- `src/errors.h:3423-3424` defines the exact message.

## Class variable not found: E1337

E1337 means `Class variable "{variable}" not found in class "{class}"`.
For a complete Vim9 member read, write, or non-call reference whose receiver
resolves to a same-file class, analysis reports the member-name span after
proving that the class has no applicable static variable or class method.

Static variables and class methods are not inherited. An access through a
child class therefore searches only that child and uses the child name in the
diagnostic. Local class type aliases resolve to the underlying class. A dot
call remains owned by E1325, while an existing object variable or object method
used through a class remains owned by E1376, E1386, or protected-access
diagnostics. Enum receivers use E1422 or E1423 instead.

Unknown and imported receivers, incomplete member syntax, and Legacy commands
remain conservative. Existing static members retain the more specific E1333,
E1335, and E1409 write/access rules.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:1688-1720` covers both reading and assigning
  a missing static member.
- `src/vim9expr.c:605-628` accepts class variables and class method references
  before selecting the missing-member path.
- `src/vim9class.c:4113-4141` selects E1376 for an object variable and E1422
  for an enum receiver before falling back to E1337 for a class receiver.
- `src/errors.h:3429-3430` defines the exact message.

## Method argument shadows a class variable: E1340

E1340 means `Argument already declared in the class: {name}`. A direct Vim9
class or enum method parameter cannot reuse the name of a static variable
visible as a bare class member in that method. Analysis reports the
parameter-name span and considers the complete aggregate, so the static
declaration may appear before or after the method. Class methods also see
static variables from their class hierarchy.

The rule covers object methods, static methods, constructors, abstract methods,
defaulted parameters, and named variadic parameters. The exact discard name
`_` is exempt. Object variables, methods, top-level functions, and nested
lambdas do not participate.

A conflicting script item retains the earlier E1168 diagnostic. Malformed
parameters remain owned by syntax recovery, duplicate parameter lists do not
gain a secondary E1340, and Legacy functions are excluded.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:1798-1820` covers a method argument that
  shadows a static class variable.
- `runtime/doc/vim9class.txt:297-300` states that method argument and local
  variable names cannot shadow class members.
- `src/vim9class.c:979-1017,2649-2657` checks arguments from both class
  functions and object methods against the completed direct class-member list.
- `src/vim9compile.c:325-365,3878-3910` also checks method arguments against
  static variables found through the defining class hierarchy.
- `src/vim9compile.c:443-470` gives script conflicts priority before class
  conflicts and exempts the exact `_` argument.
- `src/errors.h:3434-3436` defines the exact message.

## Local declaration shadows a class variable: E1341

E1341 means `Variable already declared in the class: {name}`. A Vim9 local
declaration compiled directly in a class or enum method cannot reuse the name
of a static variable visible as a bare class member. Analysis reports each
conflicting declaration-name span.

The rule covers `var`, `final`, and `const` bindings, destructuring and loop
bindings, declarations in nested control-flow blocks, and a nested `def` name
introduced by the direct method body. Static variables from the defining class
hierarchy participate. Declarations inside that nested function or a lambda
have their own compilation context and are excluded, as are object variables,
methods, top-level declarations, and Legacy functions. The exact discard name
`_` is exempt.

A visible script item has priority over E1341. When E1341 applies, it owns the
declaration instead of the generic E1006 or E1017 redeclaration diagnostics.
Malformed declarations remain owned by syntax recovery.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:1822-1836` covers a direct method local that
  shadows a static member.
- `runtime/doc/vim9class.txt:297-300` forbids local-variable shadowing of class
  members.
- `src/vim9compile.c:325-365,440-470` searches the defining class hierarchy and
  gives script-variable conflicts priority.
- `src/vim9compile.c:1935-1946,2018-2030` checks new local declarations against
  visible class variables.
- `src/vim9compile.c:1085-1100` applies the same check to a nested `def` name in
  the outer method compilation context.
- `src/errors.h:3437-3439` defines the exact message.

## Lowercase interface name: E1343

E1343 means `Interface name must start with an uppercase letter: {argument}`.
In a Vim9 interface declaration, the first name byte must be an ASCII uppercase
letter. Analysis selects the parsed interface name while retaining Vim's full
trimmed declaration argument in the message.

The Vim9-script gate runs first, so Legacy and one-shot `legacy` declarations
remain E1342. The uppercase check then runs before malformed-name E1315. A
lowercase, numeric, underscore, or non-ASCII first byte therefore owns the
diagnostic even when invalid punctuation follows. The recovering interface
aggregate and block remain available, and parsing continues after
`endinterface`.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_interface.vim:5-55` distinguishes the Vim9-only
  interface rule, lowercase-name E1343, and following declarations.
- `src/vim9class.c:1965-1993` checks the Vim9-script gate, ASCII-uppercase
  requirement, and whitespace after the name in that order.
- `src/errors.h:3440-3442` defines the exact message.

## Interface name not found: E1346

E1346 means `Interface name not found: {name}`. For a Vim9 class with an
`implements` clause, analysis reports the closing `endclass` when the first
interface name cannot be resolved at the point of the class declaration.
Names declared later in the script are therefore not visible.

A resolved same-file interface is valid. A resolved class, variable, or other
script item is not E1346 and remains available for the more specific E1347
check. A qualified name whose prefix resolves to an imported script remains
conservative because same-file analysis cannot prove whether that script
exports the requested interface.

Class-header syntax errors take priority and prevent interface validation, as
they do in Vim. Legacy declarations and interface or enum aggregates are not
covered by this rule. A diagnostic elsewhere in the class body does not hide
an independently provable missing interface name.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_interface.vim:324-344` distinguishes a missing
  interface name from a resolved regular class and places the failure on the
  closing `endclass`.
- `src/vim9class.c:835-864` resolves `implements` entries in source order,
  emits E1346 only when name lookup fails, and selects E1347 for a resolved
  value that is not an interface.
- `src/errors.h:3446-3449` defines the exact E1346 and E1347 messages.

## Not a valid interface: E1347

E1347 means `Not a valid interface: {name}`. For a Vim9 class with an
`implements` clause, analysis reports the closing `endclass` when the first
resolved name is provably not an interface class value. Same-file classes,
enums, type aliases, functions, import aliases, and variables with a known
value type participate. A variable typed as an interface contains an object,
not the interface declaration itself, so it is also invalid here.

Validation follows source order. A resolved interface permits the next entry
to be checked; an unresolved entry remains E1346, and either failure stops the
scan. A variable whose value type remains `any` or unknown, and a qualified
member of an imported script, remain conservative because same-file analysis
cannot prove their runtime values. Later entries are not diagnosed after such
an unknown result.

Class-header syntax errors take priority. Legacy declarations and interface or
enum aggregates are excluded, while a class naming itself in `implements` is a
resolved class value and therefore receives E1347.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_interface.vim:334-351` covers a resolved regular class
  and a number variable, both reported at the closing `endclass`.
- `src/testdir/test_vim9_class.vim:9706-9715` covers a class attempting to
  implement itself, and `src/testdir/test_vim9_enum.vim:350-359` covers an
  enum used as an interface.
- `src/vim9class.c:835-864` selects E1346 only when lookup fails and E1347 when
  the resolved value is not an interface.
- `src/errors.h:3446-3449` defines the exact message.

## Interface variable is not implemented: E1348

E1348 means `Variable "{variable}" of interface "{interface}" is not
implemented`. For a Vim9 class whose same-file `implements` entries resolve to
interfaces, analysis reports the closing `endclass` when the first required
object variable is absent from the class and its extended-class hierarchy.

Inherited interface variables are checked before variables declared directly
in the child interface. Direct `implements` entries retain source order, and
the diagnostic names that direct interface even when the required variable is
inherited from one of its parents. A static variable or method with the same
name does not implement an interface object variable.

An exact object-variable match with a different access level or type remains
owned by E1367 or E1382 and stops validation without a secondary E1348. After
one interface's variables are valid, its methods must also be present and
compatible before validation reaches the next interface. Unresolved, invalid,
or imported interface names and incomplete headers remain conservative.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_interface.vim:239-254` covers the exact missing
  variable message and places it on `endclass`.
- `src/testdir/test_vim9_interface.vim:1043-1056` reaches a missing variable in
  the second directly implemented interface after the first interface's method
  is implemented.
- `src/testdir/test_vim9_interface.vim:1238-1271` shows that variables inherited
  from the class hierarchy satisfy an interface and selects the first remaining
  missing variable.
- `src/vim9class.c:217-244,647-732` flattens inherited members parent-first and
  checks exact names, access, and type across the class hierarchy.
- `src/vim9class.c:799-890` checks variables before methods for each interface
  and stops on the first failure.
- `src/errors.h:3450-3453` defines the exact E1348 and E1349 messages.

## Interface method is not implemented: E1349

E1349 means `Method "{method}" of interface "{interface}" is not
implemented`. After all required variables of a same-file interface are
present and compatible, analysis reports the closing `endclass` when the first
required object method is absent from the class and its extended-class
hierarchy.

Inherited interface methods are checked before methods declared directly in
the child interface. Direct `implements` entries retain source order, and the
diagnostic names that direct interface even when the missing method comes from
an interface parent. A static method or variable with the same name does not
implement an interface object method.

An exact method with an incompatible signature remains owned by E1383 and
stops validation without a secondary E1349. A missing variable remains E1348
and takes priority over methods in the same interface. Invalid static or
protected interface methods, unresolved or imported interface names, and
incomplete headers do not create cascading E1349 diagnostics.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_interface.vim:256-271` covers the exact missing-method
  message and places it on `endclass`.
- `src/testdir/test_vim9_interface.vim:1031-1056` shows direct interface order:
  a missing method in the first interface stops validation before the second,
  while a valid method permits the second interface's variable check.
- `src/testdir/test_vim9_interface.vim:1170-1211` covers methods inherited from
  the class hierarchy and selects the first remaining missing method.
- `src/vim9class.c:735-825` checks exact object-method names and signatures
  across the class hierarchy.
- `src/vim9class.c:878-887` checks variables before methods for each interface.
- `src/errors.h:3452-3455` defines the exact E1349 and E1350 messages.

## Duplicate implements clause: E1350

E1350 means `Duplicate "implements"`. A Vim9 class or enum declaration may
contain only one `implements` clause. The parser reports the second keyword
itself before attempting to parse another interface name, so the same error
also owns a repeated clause with a missing name.

The first clause's interface list remains in the recovering aggregate, and the
block plus following declarations remain available. Repeating an interface
inside one comma-separated list is instead E1351. An interface declaration
using `implements` is E1381, while Legacy declarations retain their dialect
diagnostics.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_interface.vim:212-224` covers the exact duplicate
  clause and reports it on the class header.
- `src/vim9class.c:2038-2051` rejects a second `implements` keyword before
  scanning its following name.
- `runtime/doc/vim9class.txt:695-699` states that `implements` can appear only
  once and distinguishes duplicate entries in one list.
- `src/errors.h:3454-3457` defines the exact E1350 and E1351 messages.

## Duplicate interface in implements list: E1351

E1351 means `Duplicate interface after "implements": {name}`. Within one
comma-separated `implements` list, a Vim9 class or enum may name each interface
only once. The parser reports the second exact name and retains its full
qualified spelling in the message.

The duplicate is not appended to the recovering aggregate; earlier distinct
names remain in source order, and parsing continues after the aggregate block.
Name comparison is case-sensitive. A second `implements` clause is E1350,
while missing required whitespace is E1315 and takes priority before duplicate
comparison. Legacy declarations retain their dialect diagnostics.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_interface.vim:226-237` covers the exact duplicate name
  and reports it on the class header.
- `src/vim9class.c:2054-2084` compares each parsed name with earlier entries
  before appending it to the interface list.
- `runtime/doc/vim9class.txt:695-699` requires each interface name to appear
  only once.
- `src/errors.h:3454-3459` defines the exact E1350, E1351, and E1352 messages.

## Duplicate extends clause: E1352

E1352 means `Duplicate "extends"`. A Vim9 class or interface declaration may
contain only one `extends` clause. The parser reports the second keyword before
attempting to parse another parent name, including when that name is missing.

The first parent remains the sole entry in the recovering aggregate, and the
block plus following declarations remain available. An enum's first `extends`
clause is instead E1416. Malformed whitespace after the first parent remains
E1315, while Legacy declarations retain their dialect diagnostics.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:2377-2386` covers the exact duplicate clause
  and reports it on the class header.
- `src/vim9class.c:2012-2021` rejects a second `extends` keyword before scanning
  its following parent name.
- `runtime/doc/vim9class.txt:656-664` states that a class can extend one class.
- `src/errors.h:3458-3463` defines the exact E1352, E1353, and E1354 messages.

## Class name not found: E1353

E1353 means `Class name not found: {name}`. A complete Vim9 class or interface
reports it when its `extends` name cannot be resolved at that source position.
The diagnostic belongs to the aggregate terminator, matching Vim's deferred
validation. Imported qualified names remain unknown until workspace import
analysis can prove their members.

E1353 also owns a well-formed `object<T>` type when `T` is proven not to be an
object type. Its message and span retain the angle-bracket suffix, for example
`<number>`. `any`, prior local classes, interfaces, enums, and aliases ending
in those types are accepted. Unresolved names and malformed types remain with
their existing conservative or syntax diagnostics instead of gaining a
cascade.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:2388-2394` covers an unresolved base class
  and reports E1353 on `endclass`.
- `src/testdir/test_vim9_class.vim:11744-11749` covers the exact
  `object<number>` message in script and compiled-function contexts.
- `src/vim9class.c:315-348` distinguishes an unresolved `extends` name from a
  resolved value that cannot be extended.
- `src/vim9type.c:1920-1977` accepts `object<any>` and object-valued inner
  types, and reports E1353 for a parsed non-object inner type.
- `src/errors.h:3460-3463` defines the exact E1353 and E1354 messages.

## Cannot extend: E1354

E1354 means `Cannot extend {name}`. After an `extends` name resolves, a Vim9
class requires a class target and a Vim9 interface requires an interface
target. A class also cannot extend itself, an interface, or an enum. The same
rules apply through local type-alias chains.

The diagnostic belongs to the aggregate terminator. Unknown inferred values,
qualified imports, and aliases whose imported target is unavailable remain
conservative. An unresolved unqualified name remains E1353, while malformed or
incomplete headers retain their syntax or recovery diagnostics.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:2396-2403` covers extending a resolved
  scalar variable and reports E1354 on `endclass`.
- `src/testdir/test_vim9_class.vim:9699-9707` covers a class extending itself.
- `src/testdir/test_vim9_interface.vim:1058-1076` covers both class/interface
  category mismatches.
- `src/testdir/test_vim9_enum.vim:340-349` covers a class extending an enum.
- `src/vim9class.c:315-348` validates self-extension, resolution, and the
  required class/interface category in that order.
- `src/errors.h:3462-3465` defines the exact E1354 and E1355 messages.

## Duplicate function: E1355

E1355 means `Duplicate function: {name}`. Within one Vim9 class, interface, or
enum, method names are unique across object methods, static methods, abstract
declarations, and constructors. A single leading underscore marks protected
access and does not create a separate name, so `_Foo` conflicts with `Foo`.

Comparison is case-sensitive and does not cross aggregate boundaries or class
inheritance. A body method is considered defined only after its complete
`enddef`; interface and abstract declarations are complete on their header.
The diagnostic retains the later spelling and points at the location where
that later definition becomes complete. Duplicate-method ownership suppresses
generic, access, interface, and override cascades for the same aggregate.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:2407-2417` covers two methods with the same
  public spelling and reports the later definition.
- `src/testdir/test_vim9_class.vim:4648-4686` covers protected/public spelling
  collisions in both orders.
- `src/testdir/test_vim9_class.vim:6108-6165` covers collisions between static
  and object methods.
- `src/vim9class.c:1101-1126` compares normalized names across both method
  collections.
- `src/vim9class.c:2538-2564` performs the duplicate check after a method has
  been defined and before adding it to either collection.
- `runtime/doc/vim9class.txt:638-642` forbids method overloading by argument
  type.
- `src/errors.h:3464-3467` defines the exact E1355 and E1356 messages.

## Super must be followed by a dot: E1356

E1356 means `"super" must be followed by a dot`. In an object method, static
method, constructor, or a Lambda nested in one of those methods, `super` is a
special receiver and must be immediately followed by `.`. Bare uses and other
postfix forms such as `super[0]` report the keyword; whitespace before the dot
is a syntax error on that whitespace.

A direct `super.member` receiver is accepted here and remains subject to the
context and parent-class checks owned by E1357 and E1358. Outside a class
method, a dotted use is likewise reserved for E1357. Legacy expressions and
the separate `this` keyword do not gain E1356.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:11064-11074` covers whitespace between
  `super` and `.` with the exact message.
- `src/testdir/test_vim9_cmd.vim:2144-2150` retains a bare-`super` crash
  regression that includes E1356.
- `src/vim9compile.c:39-62` requires `.` after `super` in object methods and
  constructors and then checks for a parent class.
- `src/vim9expr.c:851-864` identifies static class-method context while
  preserving object-method context through nested closures.
- `src/vim9expr.c:990-1002` requires `.` after `super` in that static context.
- `src/errors.h:3466-3469` defines the exact E1356 and E1357 messages.

## Super outside a class method: E1357

E1357 means `Using "super" not in a class method`. A dotted `super.member`
receiver is valid only while compiling a class or enum method, constructor, or
a Lambda nested in one of those methods. Script-level expressions, ordinary
top-level defs and Lambdas, and class member initializers report the `super`
keyword.

A method context without a parent aggregate is not E1357; it proceeds to the
E1358 check. Bare `super` remains owned by the missing-dot and ordinary name
rules, while whitespace before `.` remains E1356. Legacy member expressions do
not gain this Vim9 semantic diagnostic.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:11076-11091` covers `super.member` in a
  top-level def with the exact E1357 message.
- `src/testdir/test_vim9_class.vim:11015-11037` confirms that a Lambda nested
  in an object method retains its method context.
- `src/vim9expr.c:358-372` emits E1357 when a `super` member lookup has no
  current class method.
- `src/vim9compile.c:39-62` handles object methods and constructors as class
  contexts before parent validation.
- `src/errors.h:3468-3471` defines the exact E1357 and E1358 messages.

## Super outside a child class: E1358

E1358 means `Using "super" not in a child class`. An object method,
constructor, or a Lambda nested in one of them may use `super.member` only
when its class has an `extends` clause. Enum object methods have no parent and
therefore have the same error.

Static methods retain their separate member-lookup behavior. A valid child
class proceeds to normal parent-member checks. An unresolved or invalid parent
remains owned by E1353 or E1354, and incomplete aggregates do not gain a
cascade. Outside a method remains E1357, while a missing immediate dot remains
E1356.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:11093-11104` covers an object method in a
  class without a parent and the exact E1358 message.
- `src/testdir/test_vim9_class.vim:11015-11037` confirms that object-method
  context is retained through a nested Lambda.
- `src/vim9compile.c:39-62` checks the immediate dot before requiring a parent
  for object methods and constructors.
- `runtime/doc/vim9class.txt:667-678` defines `super.` as access to an object
  method of the base class and distinguishes static method calls.
- `src/errors.h:3470-3473` defines the exact E1358 and E1359 messages.

## Constructor in an abstract class: E1359

E1359 means `Cannot define a "new" method in an abstract class`. An abstract
Vim9 class cannot define `new`, a named `new*` constructor, or a protected
`_new*` constructor. This includes concrete bodies and abstract declarations.

A body constructor reports where its complete definition ends; an abstract
declaration reports its method name. Abstract-class ownership takes priority
over the static-constructor and constructor-return-type rules. Incomplete
methods are retained without E1359, and concrete classes, enums, ordinary
methods, and case-distinct `New` methods are unaffected.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:2976-2982` covers a concrete `new()` body in
  an abstract class with the exact E1359 message.
- `src/vim9class.c:1130-1155` rejects abstract-class constructors before the
  static modifier and return-type checks.
- `src/vim9class.c:2528-2550` defines the method before applying constructor
  validation, so a body diagnostic is observed at its `enddef`.
- `runtime/doc/vim9class.txt:495-506` states that an abstract class has no
  `new()` method.
- `src/errors.h:3472-3475` defines the exact E1359 and E1360 messages.

## Statically known null object use: E1360

E1360 means `Using a null object`. Analysis reports it for a member read,
member write, method call, or method reference whose receiver is provably
`null_object`. The proof is deliberately narrow: it covers the literal itself,
a variable initialized directly from that literal, and a non-aggregate Vim9
variable with an object, class, interface, enum, or matching type-alias type
but no initializer.

If the variable is assigned anywhere else in the file, analysis keeps its
runtime value unknown and does not report E1360. Class and interface members,
function arguments, values passed through calls, and control-flow-dependent
values are also left to runtime analysis. This prevents an LSP diagnostic from
assuming when a function is called or which branch executed. The diagnostic
selects the receiver that is known to be null, and malformed member expressions
retain their syntax recovery without an E1360 cascade.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:583-665` covers uninitialized typed objects,
  explicit `null_object`, captured script variables, and dynamically typed
  member writes.
- `src/testdir/test_vim9_class.vim:1844-1897` covers member reads and writes in
  both compiled functions and script context.
- `src/testdir/test_vim9_class.vim:6261-6380` covers method calls and method
  references on null objects.
- `src/eval.c:1733-1743` rejects a null object before resolving a member for an
  evaluated lvalue.
- `src/vim9execute.c:613-623,3339-3350,6186-6202` rejects null receivers for
  compiled object method calls and member access.
- `runtime/doc/vim9class.txt:798-803` documents the default null value of an
  uninitialized object variable and the E1360 behavior.
- `src/errors.h:3474-3475` defines the exact E1360 message.

## Incomplete null-class type: E1363

E1363 means `Incomplete type`. Analysis reports it when Vim9 script-level code
uses `null_class` as the receiver of a complete member read, write, or call.
The receiver has no class identity, so the member type cannot be resolved.

Compiled `def` and Lambda bodies deliberately do not receive E1363: Vim checks
that form at runtime and reports E1395 instead. A standalone `null_class`, a
real class receiver, legacy expressions, and incomplete member syntax are also
left unchanged. The diagnostic selects the `null_class` receiver.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_class.vim:551-555` expects E1363 in script context but
  E1395 for the same expression in a compiled function.
- `src/vim9expr.c:373-405` emits E1363 when compile-time member lookup has no
  class identity and checks the missing-name case separately.
- `runtime/doc/vim9.txt:2357-2368` documents the script-level E1363 and
  compiled E1395 distinction.
- `src/errors.h:3482-3483` defines the exact E1363 message.

## Missing function opening parenthesis: E124

E124 means `Missing '(': {text}`. Syntax analysis reports it when a parsed
`:function` or `:def` name is followed by non-comment text that does not begin
an argument list. The diagnostic message retains Vim's complete function
header argument, while its span selects the invalid tail after the name.

A complete signature remains valid, whitespace immediately before `(` keeps
its more specific Vim9 whitespace diagnostic, and a name-only `:function` or
`:def` query is not treated as a malformed definition. The incomplete header
and following commands remain in the syntax tree for editor recovery.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_user_func.vim:461-465` expects E124 for
  `func Xfunc abc ()` and distinguishes an unclosed argument list as E125.
- `src/userfunc.c:5213-5224` emits E124 when the byte after the parsed function
  name is not an opening parenthesis.
- `src/errors.h:300-303` defines the exact E124 and E125 messages.

## Illegal function argument: E125

E125 means `Illegal argument: {text}`. Syntax analysis reports it for an
argument whose name is not an ASCII identifier, the reserved Legacy argument
names `firstline` and `lastline`, an immediately attached Vim9 `#` comment at
the start of an argument list, and the official multiline form where a default
expression is still missing when the closing `)` is reached.

The Legacy `:function Name(` form with no argument text also receives E125 at
the missing-argument position. A nonempty unclosed parameter remains an editor
recovery diagnostic, and a same-line Vim9 default such as `arg: number = )`
keeps E15. Valid typed, ignored `_`, variadic, and constructor arguments are
retained without E125. Complete invalid headers still preserve their function
and following-command syntax data.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_func.vim:940-955` expects E125 for an attached comment
  and for a multiline missing default expression.
- `src/testdir/test_user_func.vim:461-465` expects E125 for an empty unclosed
  Legacy argument list.
- `src/userfunc.c:77-100` validates argument identifiers and reserved Legacy
  names, and `src/userfunc.c:245-545` handles multiline argument recovery.
- `src/errors.h:302-303` defines the exact E125 message.

## Missing function terminator: E126

E126 means `Missing :endfunction`. Syntax analysis reports it when a parsed
Legacy `:function` definition remains open at end of file, including a
`:function` embedded in a Vim9-root script. The diagnostic selects the opening
command and retains the incomplete function block and its body.

A closed function and a name-only `:function` query remain valid. A malformed
header keeps its earlier signature diagnostic without an E126 cascade, while
an unclosed `:def` continues to use E1057.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_func.vim:401-416` distinguishes an unclosed `def`
  (E1057) from an unclosed `func` (E126) in a Vim9 script.
- `src/userfunc.c:1054-1061` selects E126 when the open function is not a
  `:def` and no valid terminator is found.
- `src/errors.h:304-305` defines the exact E126 message.

## Lowercase Legacy function name: E128

E128 means `Function name must start with a capital or "s:": {name}`. Syntax
analysis reports it for a Legacy `:function` whose global name starts with an
ASCII lowercase letter, including an explicit lowercase `g:` name. The
diagnostic selects the retained function name.

Script-local `s:` functions, autoload names containing `#`, Dictionary
functions, other scoped names, and capitalized globals remain outside E128.
Vim9 `:def` and `:function` headers use their separate E1267 rule. The bang on
`:function!` does not change the naming rule, and following commands remain
available after the invalid definition.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_user_func.vim:461-465` expects E128 for `func xfunc()`.
- `src/testdir/test_vimscript.vim:7428-7461` covers lowercase plain and
  explicit-global Legacy definitions, including `:function!`.
- `src/userfunc.c:4670-4713` distinguishes script-local names, lowercase
  builtin-shaped global names, class methods, and Vim9's E1267 wording.
- `src/errors.h:308-309` defines the exact E128 message.

## Missing Vim9 function name: E129

E129 means `Function name required`. Syntax analysis reports it when a Vim9
function header contains only the `g:` namespace, when a Vim9-style function
name ends in `#` immediately before its argument list, or when a Vim9
`:function` query starts with a Legacy double-quote comment. The diagnostic
selects the incomplete name or query argument and retains following commands.

A complete `g:Name` and an autoload name with a non-empty final component remain
outside E129. A hash comment after a Vim9 `:function` query is valid, while a
Legacy `:function` definition ending in `#` retains Legacy behavior.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_vim9_func.vim:3734-3743` distinguishes an empty `g:` name
  (E129) from the `<SID>:` (E884) and missing-parenthesis (E488) cases.
- `src/testdir/test_vim9_import.vim:2788-2799` expects E129 for a Vim9 autoload
  definition whose name has no component after its final `#`.
- `src/testdir/test_vim9_script.vim:3898-3910` distinguishes `#` from `"` after
  a Vim9 `:function` query.
- `src/userfunc.c:4476-4482` rejects an empty name and a Vim9 autoload name that
  ends at `#`; `src/errors.h:310-311` defines the exact E129 message.

## Return outside a function: E133

E133 means `:return not inside a function`. Analysis reports a direct `:return`
whose lexical command-block ancestry contains neither a Legacy `:function` nor
a Vim9 `:def`. This includes returns nested in script-level conditionals,
loops, try blocks, and augroups. The diagnostic selects the command name.

Returns in function and inline-function bodies remain valid. Commands stored as
autocommand bodies or user-command replacements are not diagnosed because
their eventual invocation context is dynamic. Aggregate bodies keep their
more specific invalid-command diagnostics instead of adding E133.

Representative source evidence for Vim v9.2.1015 (`5ab969f`):

- `src/testdir/test_user_func.vim:551-554` sources a script containing a
  top-level `return 10` and expects E133.
- `src/userfunc.c:6428-6442` checks for a current function call before parsing
  the return expression.
- `src/errors.h:317-318` defines the exact E133 message.
