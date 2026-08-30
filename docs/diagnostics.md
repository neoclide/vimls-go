# Static diagnostics

This document records diagnostic semantics that are clearer in Vim's tests and
implementation than in its user documentation. The language server reports a
diagnostic only when the relevant source facts are statically known. Dynamic
function names, externally supplied test functions, and unresolved callable
values remain `unknown`.

The evidence below is pinned to Vim 9.2.1015.

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

The official compile corpus includes a top-level Vim9 declaration such as
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

The builtin Ex command table is complete for the pinned Vim release. A complete
user-command table, however, requires parsing every Vim file on runtimepath and
publishing the result as an immutable workspace snapshot. Until that exists,
capitalized candidates such as `Print` and `CallMe (` remain opaque and do not
produce E476 or E492.

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
legacy commands, dynamically executed strings, and capitalized command
candidates remain opaque. They can be classified only after every Vim file on
runtimepath has contributed to an immutable user-command snapshot.

Representative source evidence:

- `src/testdir/test_vim9_assign.vim:3110-3112` reports E492 for the script-level
  `MyVar: string = 'abc'` form that omitted `var`.
- `src/testdir/test_vim9_cmd.vim:2073-2076` distinguishes script-level E492
  from compiled E476 for `notexist:repl`.
- `src/testdir/test_vim9_script.vim:4832-4841,4878-4881` covers the pinned
  invalid command forms and their E476/E492 context split.
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
