# Vim9 error codes below E1000: research appendix

Vim9 reuses many error codes that predate Vim9 script. A code below E1000 is
therefore not evidence that a source form follows Legacy Vim script semantics.
This appendix retains the release-specific evidence that is not already
represented in the static diagnostic reference or the official syntax
migration ledger.

The evidence is pinned to Vim v9.2.1015, commit
`5ab969f719bb09555e90e8dff8c94fc37bcbf2ae`. This is a research record, not a
pending-support count. Some entries describe runtime or mutable editor state
that the language server must keep as `unknown`; others identify narrow rules
that can become static diagnostics when all required facts are known.

## Evidence conventions

- An **official assertion** is an expectation in Vim's test suite. Arrays
  passed to `CheckDefAndScriptFailure` or `CheckDefExecAndScriptFailure` are
  ordered `[def, script]`. `CheckLegacyAndVim9Failure` covers Legacy, compiled
  `def`, and Vim9 script, in that order when it receives separate messages.
- A **clean-oracle observation** was reproduced with the pinned `src/vim`
  executable using a curated, side-effect-free input. It is weaker than an
  official Vim9 assertion and is labeled explicitly.
- **Static relevance** states which facts would be required to report the
  error without executing user Vim script. Runtime registries, lock state,
  dynamic containers, and unresolved namespaces must not be guessed.

The remaining evidence contains 14 assertion-backed reachable codes, 11
source or clean-oracle observations, and three narrowed negative findings.

## Assertion-backed Vim9 reachability

### E122: Function redefinition

Message: `Function {name} already exists, add ! to replace it`.

Vim9 can emit E122 when a nested `def` defines the same global function more
than once without `!`. In the official case, `Outer()` defines `g:Inner()` and
is executed twice; the second execution observes the existing function.

Static relevance: this depends on the mutable function namespace. A same-file
sequence may be provable, but an unresolved global definition remains
`unknown`.

Evidence: `src/testdir/test_vim9_func.vim:1212-1223`; the Legacy function
redefinition cases are in `src/testdir/test_functions.vim:3017-3029`.

### E123: Undefined function query

Message: `Undefined function: {name}`.

The `:function`/`:def` query form uses E123 when the requested function does
not exist. Vim's compiled case executes `func DoesNotExist`; a Vim9 script
case queries a missing `<SNR>` function.

Static relevance: the result depends on the complete function namespace.
Only a name whose lookup domain is completely known can be proved missing.

Evidence: `src/testdir/test_vim9_func.vim:208-214` and
`src/testdir/test_vim9_script.vim:4350`; the Legacy query is covered by
`src/testdir/test_user_func.vim:261`.

### E684: List index out of range

Message: `List index out of range: {index}`.

Legacy Vim script, compiled `def`, and Vim9 script all retain E684 for invalid
List indexes and slices. Vim also uses it for invalid `insert()` positions.

Static relevance: a literal List or Tuple length and a literal index or slice
bound are sufficient. Dynamic containers or indexes require runtime state.

Evidence: `src/testdir/test_listdict.vim:133-143,1533-1534`,
`src/testdir/test_vim9_assign.vim:289-293`, and
`src/testdir/test_vim9_expr.vim:2597`.

### E687: Fewer targets than List items

Message: `Less targets than List items`.

Legacy and Vim9 script evaluation use E687 when destructuring supplies more
items than targets. A compiled `def` uses E1093 for the corresponding literal
cardinality mismatch.

Static relevance: literal List or Tuple cardinality is provable, but the
context-specific E687/E1093 split must be retained. Dynamic values remain
runtime-only.

Evidence: `src/testdir/test_listdict.vim:157-169` and
`src/testdir/test_vim9_assign.vim:1021-1028`.

### E688: More targets than List items

Message: `More targets than List items`.

Legacy and Vim9 script evaluation use E688 when destructuring supplies fewer
items than targets. A compiled `def` again uses E1093 for a statically known
literal mismatch.

Static relevance: the same literal-cardinality and context requirements as
E687 apply.

Evidence: `src/testdir/test_listdict.vim:171-183` and
`src/testdir/test_vim9_assign.vim:1011-1019`.

### E695: Indexing a Funcref

Message: `Cannot index a Funcref`.

Vim retains E695 in Legacy, compiled `def`, and Vim9 script for an expression
such as `function('min')[0]`.

Static relevance: the error is provable when the receiver's Funcref type is
known. An unresolved receiver remains `unknown`.

Evidence: `src/testdir/test_listdict.vim:1516-1522` and
`runtime/doc/eval.txt:191-205`.

### E705: Variable name conflicts with a function

Message: `Variable name conflicts with existing function: {name}`.

Vim9 uses E705 when an assignment through a namespace dictionary would create
a variable whose name conflicts with an existing function. The official cases
include a script-local function and a global function.

Static relevance: this requires a complete view of the relevant variable and
function namespaces. Partial runtimepath or global state is insufficient.

Evidence: `src/testdir/test_vim9_assign.vim:3903-3912` and
`src/testdir/test_vim9_func.vim:1350-1360`.

### E710: Runtime destructuring has extra values

Message: `List value has more items than targets`.

A compiled `def` emits E710 while executing a `for [v1, v2]` loop when an
iterated List has more than two items. This is distinct from the direct
assignment E687/E1093 split.

Static relevance: Vim performs this check at execution. A language server may
prove a fully literal iterable, but must not infer the size of a dynamic value.

Evidence: `src/testdir/test_vim9_script.vim:3294-3299` and the Legacy List
slice-assignment contrast in `src/testdir/test_let.vim:352`.

### E711: Runtime destructuring has missing values

Message: `List value does not have enough items`.

A compiled `def` emits E711 while executing a destructuring `for` loop when an
iterated List has fewer items than targets.

Static relevance: the same runtime-cardinality restriction as E710 applies.

Evidence: `src/testdir/test_vim9_script.vim:3301-3306` and the Legacy List
slice-assignment contrast in `src/testdir/test_let.vim:354`.

### E719: Dictionary slicing

Message: `Cannot slice a Dictionary`.

Legacy Vim script, compiled `def`, and Vim9 script all retain E719 for a slice
whose receiver is a Dictionary.

Static relevance: a known Dictionary receiver and a slice operation are
sufficient; an unresolved receiver is not.

Evidence: `src/testdir/test_listdict.vim:1516-1522`,
`src/testdir/test_vim9_expr.vim:3485`, and `runtime/doc/eval.txt:3165-3168`.

### E741: Locked-value mutation

Messages: `Value is locked` and `Value is locked: {context}`.

Vim9 uses E741 when an operation mutates a locked value or the contents of an
immutable container. It can arise while executing a compiled function against
runtime lock state as well as while sourcing Vim9 script.

Static relevance: report it only when the exact target and its lock or nested
immutability are known from the analyzed snapshot. Editor-created locks and
aliased mutable values remain runtime state.

Evidence: `src/testdir/test_vim9_assign.vim:2241-2278`,
`src/testdir/test_vim9_cmd.vim:1663`,
`src/testdir/test_vim9_class.vim:3983`, and
`src/testdir/test_blob.vim:427`; the two messages are defined by
`src/errors.h:1884-1886`.

### E742: Fixed-value mutation

Messages: `Cannot change value` and `Cannot change value of {name}`.

The official Vim9 case passes the special `v:` dictionary into a compiled
function and attempts `unlet dict.count`. Vim recognizes the fixed provenance
at execution and emits E742.

Static relevance: the receiver's exact fixed or special provenance must be
known. A plain `dict<any>` parameter is not enough.

Evidence: `src/testdir/test_vim9_assign.vim:2845-2851`; Legacy fixed-argument
examples appear in `src/testdir/test_listdict.vim:886-898`; the two messages
are defined by `src/errors.h:1888-1890`.

### E909: Indexing a special variable

Message: `Cannot index a special variable`.

Vim retains E909 in Legacy, compiled `def`, and Vim9 script for direct indexes
such as `v:true[0]` and `v:null[0]`.

Static relevance: a resolved Special value is sufficient. Values whose type
or provenance is unknown must not be guessed.

Evidence: `src/testdir/test_listdict.vim:1518-1520` and
`runtime/doc/eval.txt:1542-1545`.

### E985: Dot assignment in script version 2 or later

Message: `.= is not supported with script version >= 2`.

Vim9 rejects `.=` and requires `..=`. The official case compiles a `def`
containing `d.k .= ''`; Legacy script version 1 accepts the historical form.

Static relevance: the effective script version and assignment operator are
lexical facts, so this rule is fully static.

Evidence: `src/testdir/test_vim9_cmd.vim:80-88`,
`src/vim9compile.c:2013`, and `src/evalvars.c:1099`.

## Source and clean-oracle reachability

The following entries have a shared implementation path, Legacy test evidence,
or a clean Vim9 reproduction, but no equivalent direct Vim9 `v9.Check*`
assertion was found in the pinned corpus. They must not be presented as
official Vim9 test coverage.

### E124: Missing opening parenthesis

Message: `Missing '(': {text}`.

The shared function-definition parser emits E124 when text after a `def` or
`function` name does not start with `(`. A clean Vim9 source containing
`def Foo abc ()` reproduced `E124: Missing '(': Foo abc ()`.

Static relevance: this is definition syntax and is statically decidable.

Evidence: `src/userfunc.c:5213-5224` and the Legacy assertion in
`src/testdir/test_user_func.vim:463`.

### E133: Top-level return

Message: `:return not inside a function`.

A clean Vim9 source containing a top-level `return` reproduced E133. The
shared `ex_return()` implementation checks whether a function call frame
exists.

Static relevance: lexical function context proves the ordinary source form,
although dynamically executed command strings require runtime context.

Evidence: `src/userfunc.c:6439-6442` and the Legacy assertion in
`src/testdir/test_user_func.vim:552-554`.

### E174: Duplicate user command

Message: `Command already exists: add ! to replace it: {definition}`.

Defining the same user command twice without `!` reproduced E174 in a clean
Vim9 source. Vim includes the second definition in the message.

Static relevance: the mutable user-command registry is authoritative. A
same-source duplicate may be provable, but runtimepath and editor-created
commands otherwise keep the result `unknown`.

Evidence: `src/usercmd.c:1265` and
`src/testdir/test_usercommands.vim:269-280,346-354`.

### E183: Lowercase user-command name

Message: `User defined commands must start with an uppercase letter`.

A clean Vim9 `command foo ...` definition reproduced E183. Unlike E174, this
validation depends only on the declared command name.

Static relevance: a complete literal `:command` definition is sufficient.

Evidence: `src/usercmd.c:1429`,
`src/testdir/test_usercommands.vim:299-300,352-354`, and
`src/testdir/test_vimscript.vim:6937-6939`.

### E707: Autoload function conflicts with a variable

Message: `Function name conflicts with variable: {name}`.

The verified Vim9 path is specifically an autoload file. An
`autoload/dup5func.vim` script declares `var Func` and then `export def Func()`;
the expanded `dup5func#Func` name conflicts and produces E707. The equivalent
plain sourced Vim9 file uses E1041 instead.

Static relevance: this requires the autoload path-derived name and complete
same-module declarations. It must not be generalized to every variable/function
collision.

Evidence: `src/testdir/test_vim9_import.vim:2755-2764` and
`src/userfunc.c:5486-5500`.

### E740: Runtime maximum function arguments

Message: `Too many arguments for function {call}`.

A clean Vim9 call to variadic `min()` with 21 arguments reproduced E740. The
variadic builtin declaration can bypass compiled arity checking before the
runtime argument-array ceiling is applied.

Static relevance: a direct resolved call with a literal argument count can be
proved only when the analyzer models Vim's runtime ceiling. Dynamic calls stay
runtime-only.

Evidence: `src/userfunc.c:2188-2200` and the Legacy assertion in
`src/testdir/test_user_func.vim:561-563`.

### E853: Duplicate function parameter

Message: `Duplicate argument name: {name}`.

The shared argument parser reproduced E853 for a Vim9 definition such as
`def Foo(a: number, a: number)`. The pinned direct assertion is a Legacy lambda
case rather than a Vim9 test helper.

Static relevance: parameter names and scopes are syntax facts, so the rule is
fully static.

Evidence: `src/userfunc.c:124-132` and
`src/testdir/test_lambda.vim:106-110`.

### E932: Top-level closure function

Message: `Closure function should not be at top level: {name}`.

A clean top-level Vim9 `func Foo() closure` reproduced E932. The shared
function-definition implementation rejects the `closure` attribute when no
outer function exists.

Static relevance: the enclosing function context and literal attribute are
sufficient.

Evidence: `src/userfunc.c:5360-5369` and
`src/testdir/test_lambda.vim:375-379`.

### E963: Wrong type assigned to a writable v: variable

Message: `Setting v:{name} to value with wrong type`.

A clean Vim9 assignment `v:errors = ''` reproduced E963. `v:errors` is
writable, so this is a value-type guard rather than E46. The older Legacy test
references do not directly assert E963; the clean observation and source path
are the retained evidence.

Static relevance: both the predefined variable's declared type and the value
type must be known. Dynamic values remain runtime checks.

Evidence: `src/evalvars.c:4296-4304` and `src/errors.h:2529-2530`.

### E989: Required parameter after a default

Message: `Non-default argument follows default argument`.

The shared argument parser reproduced E989 for
`def Foo(a: number = 1, b: number)`. The direct pinned assertion is a Legacy
function definition.

Static relevance: parameter order and default presence are syntax facts, so
the rule is fully static.

Evidence: `src/userfunc.c:360-364,422-469` and
`src/testdir/test_user_func.vim:147-150`.

### E995: Replacing an existing variable with const or final

Message: `Cannot modify existing variable`.

A clean Vim9 script that declares `const i = 1` and then repeats
`const i = 2` reproduced E995. This is the sequential script-level declaration
path; duplicate locals inside a compiled `def` use E1017.

Static relevance: same-snapshot declarations can be proved, but reload state
and externally existing variables require runtime knowledge.

Evidence: `src/evalvars.c:4222-4236`, `src/eval.c:2401-2406`, and the Legacy
const cases in `src/testdir/test_const.vim:154-199,259-274,299-301`.

## Narrowed negative findings

These findings mean that no direct Vim9-semantics path was found in the pinned
corpus or clean reproductions. They do not prove that a Vim9-root file can
never reach the historical code through `legacy`, dynamically executed text,
or another Legacy runtime API.

### E120: SID outside script context

Message: `Using <SID> not in a script context: {name}`.

Direct Vim9 spellings were intercepted by newer diagnostics: an expression
using `<SID>` produced E1010, while `s:` function or variable definitions used
E1268. The located E120 call sites serve Legacy name and expansion paths; no
direct Vim9 assertion was found.

Evidence: `src/userfunc.c:3877-3885,4668-4691,4814`,
`src/ex_docmd.c:10148`, and `src/term.c:7082`.

### E128: Legacy lowercase function-name rule

Message: `Function name must start with a capital or "s:": {name}`.

The shared function parser selects E1267 for ordinary lowercase Vim9 `def`,
`function`, and `delfunction` operations. E128 remains the Legacy wording; no
direct Vim9 E128 assertion was found.

Evidence: `src/userfunc.c:4700-4713`, the Vim9 E1267 cases in
`src/testdir/test_vim9_class.vim:4783,9123`, and the Legacy E128 case in
`src/testdir/test_user_func.vim:465`.

### E891: Legacy Funcref-to-Float conversion

Message: `Using a Funcref as a Float`.

Direct Vim9 expressions are rejected before the Legacy float-conversion path:
the verified forms used E703, E1219, or E1012 according to context. E891 is
asserted by the Legacy Vim script suite, and no direct Vim9 E891 assertion or
clean reproduction was found.

Evidence: `src/typval.c:362-424` and
`src/testdir/test_vimscript.vim:7418-7422`.
