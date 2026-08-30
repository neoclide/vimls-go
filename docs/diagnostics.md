# Static diagnostics

This document records diagnostic semantics that are clearer in Vim's tests and
implementation than in its user documentation. The language server reports a
diagnostic only when the relevant source facts are statically known. Dynamic
function names, externally supplied test functions, and unresolved callable
values remain `unknown`.

The evidence below is pinned to Vim 9.2.1015.

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
language server reports `E117: Unknown function: {name}` only for an unscoped
Vim9 call when the name does not resolve to a built-in function, a lexical
callable, or a same-file function declaration. Scoped calls such as
`g:Dynamic()`, autoload calls such as `plugin#Dynamic()`, member calls, and
legacy Vim script calls remain `unknown` because their target may be supplied
dynamically.

A direct function name of 200 bytes or more has a context-specific result.
Compiling it inside a Vim9 `def` produces E1011, while the same unresolved call
at Vim9 script level produces E117. The analyzer preserves this distinction.
Functions whose availability depends on Vim build features also remain outside
the E117 rule unless the server has matching feature information.

Unresolved-symbol diagnostics are warnings by default. Clients can set
`vimls.unresolvedSeverity` to `error`, `warning`, `information`, or `hint`.
The same value applies to E117 and the unknown-variable codes E121, E1001, and
E1089. It changes only the LSP severity; the native Vim code and message stay
unchanged. The workspace setting can be returned as
`{"unresolvedSeverity":"error"}` for the requested `vimls` section, or nested
under `vimls` in `workspace/didChangeConfiguration`. The same field is accepted
in `initializationOptions` as the initial value.

Representative source evidence:

- `src/errors.h:284-285` defines E117.
- `src/testdir/test_vim9_expr.vim:3885-3897` shows that an unscoped call does
  not resolve a `g:` function.
- `src/testdir/test_vim9_expr.vim:4498-4499` distinguishes the long-name
  E1011/E117 pair and covers an ordinary missing function.

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
