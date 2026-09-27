# What vimls-go supports

vimls-go supports Legacy Vim script and Vim9 script through **Vim v9.2.1015**,
including classes, interfaces, enums and imports. Both dialects can be used in
the same project. See the [changelog](../CHANGELOG.md) for release availability.

Generated command, function, option and `v:` variable metadata comes from
**Vim v9.2.1132**. The general grammar and oracle suite remain based on
v9.2.1015, with these targeted updates checked against v9.2.1132:

- Builtin return types follow patch 1104, including argument-dependent helpers
  and nested lists.
- Vim9 string-only assignment targets follow patch 1130: environment variables,
  registers and writable string `v:` variables require strings for `=` and
  `..=`, including destructuring. Plain `@# = number` remains valid. Legacy
  conversions are retained.
- Invalid Vim9 command names containing underscores follow patch 1094: E492
  at script level and E476 inside `def`.
- `source ++dryrun` (patch 1084) links the filename without including the
  option. Filename escapes and continuation spans are preserved.
- Stored command/autocommand bodies in branches proven false by literal
  conditions avoid spurious diagnostics (patch 1132). Unknown conditions stay
  conservative; missing block delimiters remain checked. User-command
  placeholders in multiline blocks retain unknown types until expansion.

Other metadata records its own source revision.

## Editing features

| Feature | Support |
| --- | --- |
| Completion | Commands, functions, variables, options, events, mappings, imports and class members. |
| Hover and signature help | Types, signatures, source comments, runtime help and feature introduction history. |
| Diagnostics | Syntax errors, unresolved names, invalid calls, Vim9 type errors and target Vim version checks. |
| Navigation | Definitions, references, implementations, call hierarchy and workspace symbols. |
| Editing | Rename, linked editing, selected quick fixes, folding and selection expansion. |
| Inlay hints and Code Lens | Inferred Vim9 types, reference counts and implementation counts. |
| Semantic highlighting | Types, functions, variables, parameters and modifiers. |
| Formatting | Indentation for files, selections and supported typing events. |

The parser tolerates unfinished code. Formatting preserves expressions, line
wrapping and embedded language bodies. Rename refuses ambiguous targets and
changes that require renaming autoload files or namespaces.

Renaming a member of an exported aggregate includes its statically resolved
references in importing files, whether started at the declaration or a use site.

Variable-name positions after `let`, `var`, `const` and `final` return no
completion candidates. Type annotations and initializer expressions retain
their respective completions.

Linked editing covers the declaration and its references in the current file.
It is offered only for symbols whose rename is a single-file edit: script-local
and local variables, function parameters, non-exported Vim9 symbols and
`<SID>`-free script-local Legacy names. It is withheld for exported, autoload
and global symbols, for class and interface members, and for symbols declared in
another file, because editing only the open document would leave the rest of the
workspace inconsistent. Explicit `g:`, `b:`, `w:`, `t:` and `v:` names are withheld
at every scope, including declarations inside functions: these scopes are shared
with other scripts. Non-exported class, interface, enum and type-alias names,
and explicit `as` import aliases, include their type annotation references.
Local type or explicit alias rename and linked editing are withheld when a
same-named reference remains unresolved, including a function body that uses
a class or import declared later.
Filename-derived import aliases require a file rename instead. A reference
spelled differently from the declaration, such as `<SID>name` beside `s:name`,
is withheld as well, because linked ranges must carry identical text.

Renaming a file through the client's file operation rewrites the Vim9 `:import`
statements that name it, and also the relative imports of the renamed files
themselves when their own spelling moves with them. Each import keeps its
original form: a relative path stays relative to the importing script, an
absolute path stays absolute, and a `runtimepath` import stays below the same
`import/` or `autoload/` directory. A `:import` without an `as` alias derives its
namespace from the filename, so the derived declaration and every bound
reference, including namespace qualifiers in type annotations, are rewritten
together for plain single- or double-quoted literals.
The proposed path is checked against the whole file rename batch and the
runtimepath search order; a path that would resolve to another script is
withheld. An import is left alone, and its document is skipped rather than partly
edited, when the new location cannot be expressed in the original form, when
the new filename is not a valid Vim identifier, when the
document's content cannot be verified against the indexed source, or when the
derived namespace has a textual occurrence that the analysis does not bind, such
as a comment or opaque code. A rename is never refused for these reasons.

A rename batch whose source or existing destination is itself a symbolic link
produces no import edits. Moving or replacing the link must not be treated as
moving or replacing its target, and its effect on other files in the batch
cannot be proven by canonical file identities alone. Regular files reached
through symbolic-link parent directories remain supported.

Types are inferred from known values and function return types in both dialects.
Builtin call inference supplements broad metadata with known return-value rules,
including the corrections recorded in Vim v9.2.1104 and checked against
v9.2.1132. Numeric and container results retain their known types. For `expand()`,
`glob()`, `globpath()` and `submatch()`, an omitted or literal list flag determines
whether the result is a string or `list<string>`; a dynamic flag remains unknown.
Bare `true` and `false` count as literal flags only in Vim9 command contexts,
including `def` bodies and `vim9cmd`. In Legacy commands they remain dynamic
names; `v:true`, `v:false`, `0` and `1` work in both dialects.
`get()` infers an element type only when it agrees with the default's type
(number when omitted). Unknown container elements or conflicting defaults,
including null defaults, keep the result unknown.

In `def` functions and Vim9 lambdas, `type()` guards preserve compiled types:
copying an `any` value inside the guarded branch still infers `any`.
Vim9 null comparisons and direct `instanceof` guards can suppress E1360 within
their non-null branch; they do not narrow the variable's declared or compiled type.

Completion additionally uses a more specific type for a direct variable receiver
inside a proven Vim9 guard branch. A `type(value)` comparison can specialize the
receiver's basic type and filter incompatible builtin `->` candidates;
`instanceof(value, LocalClass)` offers that class's members, including inherited
members. This applies inside `def` functions and Vim9 lambdas as well as at
script level. It does not change the variable's declared type or Vim's compiled
member and argument checks, so
accessing a derived-only member through a base-typed variable may still require
an explicit cast.

Supported branches are direct `if`/`elseif` conditions and the `else` of a
negated guard with no `elseif`. Parentheses, reversed `type()` comparisons and
logical negation are supported. A complete guard header can help completion
while its closing `endif` is still missing; malformed headers cannot.
These completion facts apply only within the guarded branch. Writes, calls,
loops and unknown command effects invalidate them; they do not carry into
deferred bodies or through copied variables. Compound guards, imported classes,
class aliases, interfaces, and multiple-class `instanceof` checks are not used
for completion narrowing. Unknown receiver types and argument checkers keep
their existing builtin candidates; custom function candidates remain available.

Uncertain dynamic behavior may leave types or references unresolved. See
[diagnostics](diagnostics.md) for supported guard conditions, diagnostic coverage
and warning policies.

LF, CRLF and CR line endings are supported. Formatting and rename preserve
existing line endings.

### Imports

`import 'libs.vim'` exposes exported members as `libs.Two`; use `as` to choose
a different namespace name.

Ordinary and autoload imports propagate statically known exported `var`, `const`,
and `final` types into local inference, completion details, and type checks,
including values re-exported through other files and container element types.
Named types retain their defining file, so aliases of the same type agree and
unrelated classes with the same name stay distinct. Known inheritance and
interface relationships are preserved.

Autoload values receive the same static checks without executing or loading
user scripts. Dynamic or unresolved imports remain unknown. Within an import
cycle, inference uses only independently known export types across the cycle's
internal edges.

Ordinary `import` completion shows full runtimepath file paths and offers nearby
files and directories with a `./` prefix. `import autoload` completes runtimepath
files and directories one level at a time. Typing `/` continues path completion.
The current file is excluded from import suggestions.

## Plugin files and help

Workspace Vim files are indexed for analysis and navigation. Outside the
workspace, runtimepath indexing covers:

| Location | Used for |
| --- | --- |
| `plugin/**/*.vim`, `autoload/**/*.vim`, `import/**/*.vim` | Symbols, completion and navigation. |
| `colors/*.vim` | Color-scheme names and paths. |
| `doc/*.txt` | Runtime help. |

Runtime help loads in the background. Built-in signatures, option data and
language rules remain tied to the supported Vim version. See
[configuration](configuration.md) for runtimepath and help settings.

## Mappings and configuration files

References are followed in supported autocommand bodies, expression mappings,
`<Cmd>` bodies and static function-name callback options. Generated code is
not executed or analyzed.

Colon-led mapping bodies (`:...<CR>` and `:<C-U>...<CR>`) in Normal, Visual,
Select and Operator-pending modes decode `\|` and case-insensitive `<Bar>`
using Vim's default `cpoptions` rules. This includes separators inside strings;
the decoded Ex parser decides whether a bar separates commands or is string
content. Navigation, rename, completion, hover, signature help and semantic
highlighting retain the original source positions, including continued lines.
Other key notation and command-line editing keys remain outside this decoded
path, except for the existing `<SID>` reference support. Custom `cpoptions`
and arbitrary key execution are not modeled. This does not extend the existing
`<Cmd>`, `<ScriptCmd>` or expression-register parsing.

Vimrc-style files receive different style suggestions from plugin files.
See [editing configuration files](userconfig.md).

## Limits to keep in mind

- Dynamic code and loading order may leave types or references unresolved.
- Mixed-dialect `def` and `function` bodies have incomplete analysis.
- Call hierarchy excludes lambdas and deferred command bodies.
- Embedded languages and syntax newer than Vim v9.2.1015 are not analyzed.
- Files larger than 4 MiB are synchronized but not analyzed.

## Neovim compatibility

Some Neovim names are recognized, but full Neovim API completion and type
checking are not provided. Neovim-only option settings receive a Hint unless
protected by `has('nvim')` or a version guard such as `has('nvim-0.7')`.
See [Neovim compatibility](neovim.md#option-compatibility).

## MacVim option compatibility

MacVim-specific options have semantic highlighting, hover documentation and
compatibility diagnostics. Protect their settings with `has('gui_macvim')`
to suppress compatibility Hints; definite invalid settings still report errors.
See the [MacVim option list](https://macvim.org/docs/gui_mac.txt.html#macvim-options).
