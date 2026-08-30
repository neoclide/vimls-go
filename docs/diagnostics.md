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
