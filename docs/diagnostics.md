# Static diagnostics

This document records diagnostic semantics that are clearer in Vim's tests and
implementation than in its user documentation. The language server reports a
diagnostic only when the relevant source facts are statically known. Dynamic
function names, externally supplied test functions, and unresolved callable
values remain `unknown`.

The evidence below is pinned to Vim 9.2.1015.

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
