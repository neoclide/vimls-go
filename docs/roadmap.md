# Roadmap

[Version 0.1.0](https://github.com/neoclide/vimls-go/releases/tag/v0.1.0)
is published. It provides the main editing features for Legacy Vim script and
Vim9; the [changelog](../CHANGELOG.md) lists changes made since that release.

The aim remains broad support for both languages through the pinned Vim
version. The [support guide](language-support.md) describes what works today.
The items below are remaining work, not promises for a particular release.

## Language support

Generated command, function, option and `v:` variable metadata now uses Vim
v9.2.1132. The reviewed `fillchars` helper fingerprint includes the v9.2.1119
default vertical separator change; static option validation rules are unchanged.
The general grammar and Vim oracle suite remain based on v9.2.1015. Targeted
v9.2.1132 compatibility now covers builtin return helpers, string-only
assignment targets and underscore command errors. Broader
grammar migration remains separate; see the [support guide](language-support.md).

Option diagnostics now include feature-gated options, treating known options as
available without inspecting the user's Vim build.
Compiled boolean option compound assignments now use E521 uniformly when the
RHS type is known, as a deliberate simplification of Vim's error-code choices.
Referencing a command, option, builtin function or autocommand event added after
the configured target Vim version reports the matching Vim error and names the
version that introduced it.

Import `as` completion uses the parsed path expression boundary, including
quotes inside filenames, and does not accept extra adjacent path expressions.
Import expressions omit namespace-only candidates. Type completion uses the
current text to insert required spacing, even when a client repeats a trigger.
Incomplete lambda parameter types recover without crashing the parser.
Variable declaration names return empty completion results, while type
annotations and initializer expressions retain their completion contexts.

Plain literal imports without `as` introduce the filename-derived namespace:
`import 'libs.vim'` makes exported members available as `libs.Two`. Scope
analysis, member completion and navigation use this namespace. Renaming a
filename-derived namespace is unsupported because it would change the import
path; explicit aliases remain available with `as`.

Runtimepath `import` completion lists full indexed file paths, including nested
files. `import autoload` lists only direct files and subdirectories; typing `/`
completes the next directory level. An empty ordinary
`import` path also offers sibling `.vim` files and subdirectories with a `./`
prefix; an empty `import autoload` path only offers runtimepath files and
subdirectories. Explicit
relative paths remain supported for both forms.
All import candidates exclude the importing file, including symlink aliases.
`/` triggers completion. Relative and absolute paths still complete one directory
at a time; completion does not recursively scan the filesystem.

User-command metadata now survives logical-line mapping, including continued
definitions and Vim9 attribute completion.

Documented command bang variants now have a complete hover range, including
the bang character, while retaining built-in documentation precedence.
Completion resolve now carries the existing command bang context and selects
the corresponding built-in help.

When Neovim's runtime help lacks a pinned Vim built-in function, its hover can
load Vim's clean `$VIMRUNTIME` help once from `vim` on `PATH`. This fallback
adds only missing non-Neovim function documentation to hover results.

Legacy variable type-change warnings now cover straight-line simple assignments.
They discard unknown types and stop at calls and control-flow boundaries; broader
flow-sensitive checking remains outside this warning's current scope.

Builtin return-value inference now covers the corrections recorded in Vim
v9.2.1104 and checked against v9.2.1132, while the generated metadata retains its
recorded source version. Fixed results and known numeric or container arguments
give more precise types. `expand()`, `glob()`, `globpath()` and `submatch()`
distinguish strings from lists using a static list flag in both dialects,
including method calls. Dynamic flags remain unknown. `get()` requires the
element and default types to agree; a default on an unknown dictionary, or a
conflicting null or scalar default, does not establish the result type.

Type inference preserves compiled types inside `type()` guards
in `def` functions and Vim9 lambdas. A copy of an `any` value remains `any`,
avoiding false E1012 diagnostics on later assignments. Interpreted script and
Legacy guard inference retain their existing behavior.

Vim9 completion queries use branch-local type facts for direct variable
receivers. Basic `type()` guards filter incompatible builtin method candidates,
and `instanceof(value, LocalClass)` enables that class's member candidates.
These facts stop at writes, calls and execution boundaries and do not alter
compiled diagnostics or shared declaration types. Completion still uses its
request-owned type query rather than waiting for full analysis. See
[language support](language-support.md) for the supported scope.

E1360 checks also recognize an initial null value before a later assignment or
an uncalled deferred writer. This is limited to source order within an execution
sequence: calls, loops and unmodeled effects discard temporary null facts, and
captured mutable values remain conservative. Assignment and general control-flow
type inference remain outside this check.

Linked editing reports the declaration and its references in the current file so
a client can rename the symbol by typing. It is offered only where a
single-document edit is provably complete, so exported, autoload and global
symbols, class and interface members, symbols declared in another file, and
references spelled differently from the declaration are all withheld.
Explicit `g:`, `b:`, `w:`, `t:` and `v:` names are withheld at every scope,
including declarations inside functions, because their scopes are shared with
other scripts.
Extending it to those symbols needs a client that applies edits across documents
while typing.
Non-exported class, interface, enum and type-alias names and explicit `as` import
aliases include their bound type annotation references. These cover variable,
parameter, return and nested types, casts, generic call arguments and aggregate
inheritance clauses. Filename-derived import aliases still require a file rename.
Local type or explicit alias rename and linked editing remain withheld when a
same-named reference is unresolved, such as an earlier function body referring
to a later class or import.

Renaming a member of an exported aggregate uses the same workspace reference
search from its declaration and from an importing use site. The search includes
closed importing files and direct calls to a statically resolved exported
factory function used as receivers. Members of unrelated aggregates with the
same spelling are left alone. Linked editing still withholds members because
its ranges cannot cover multiple documents.

A renamed file rewrites the `:import` statements that name it, using the import
graph's reverse edges to find the importing documents. Each import is rewritten
in its own form, and the relative imports of the renamed files are recomputed for
their new location. The derived namespace of an import without an `as` alias is
rewritten together with its bound references, and only when the namespace has no
textual occurrence outside the literal that the analysis does not bind. That
completeness rule still withholds a document whose namespace appears in a comment
or opaque code. Namespace qualifiers in type annotations are bound references
and are rewritten with their expression uses.
Plain single- and double-quoted imports both preserve their quoting, including
imports without an alias. Proposed paths are resolved against the prospective
file set for the entire rename batch, preserving runtimepath precedence and
withholding a document whose rewritten path would load a different script.
Batches with a symbolic-link source or destination produce no import edits.
The directory entries are checked before canonicalization, so moving a link is
not mistaken for moving its target. Supporting these batches requires modeling
the links' own paths and their effect on prospective lookup order; dropping
only the link operation is insufficient. Regular files accessed through linked
parent directories remain supported.

Ordinary and autoload imports now propagate statically known exported value
types through import chains. Named types retain their defining identity across
files, aliases and known inheritance relationships. Dependency changes invalidate
the consuming type results; unresolved imports and circular inference remain
conservative. Autoload diagnostics use indexed source without executing scripts.

Colon-led mapping command bodies now decode `\|` and `<Bar>` under the default
`cpoptions` rules. Editor features preserve the original source positions,
including continued mappings and multibyte text. See
[mapping support](language-support.md#mappings-and-configuration-files) for the
supported forms and key-notation limits.

- Complete support for `def` functions in Legacy scripts and `function`
  blocks in Vim9 scripts.
- Extend escaped payload parsing beyond the supported colon-led mapping bodies
  where the original source locations can be preserved reliably.
- Add more option-value checks where Vim's source gives a clear rule.
  Build-dependent and runtime-dependent values still need conservative handling.
- Extend the [reviewed Neovim option compatibility rules](neovim.md#option-compatibility).
  [MacVim option compatibility](language-support.md#macvim-option-compatibility)
  now shares the parser context and setting diagnostics, with semantic
  highlighting and hover documentation for MacVim and Neovim-only options.
  Direct script `finish` guards narrow the context of subsequent commands.
  Numeric `has('nvim-…')` version guards establish Neovim on the true path;
  their false path remains unknown because older Neovim versions also take it.
  Editor conditions are now recorded on commands and expressions; future
  Neovim function checks can consume that context without another guard walker.

## Configuration and plugin projects

Configuration requests now have a 10-second timeout and are cancelled when
superseded or when the server shuts down; failed requests retain current settings.

Import completion filters names before its filesystem validation budget,
so a precise prefix can find matches late in a large directory.

- Detect cross-file mapping conflicts only when the loading order is known.
- Revisit automatic watching of external plugin files if real projects need it.
  Watching more directories should have a clear benefit and bounded cost.

Parameter-name hints and links in comments are possible additions. Type hints
already exist; these extra conveniences are not required for the current
editing features to work.

## Before 1.0

Use real plugin projects to find incorrect diagnostics, missing navigation and
unsafe edits. Each supported behavior needs focused tests and, where Vim's
semantics are unclear, a reproduction against the pinned Vim version.

Release checks must cover the packaged executable, supported platforms and a
real editor client. Fix crashes, lost edits and incorrect rename results before
adding more features. Performance changes need comparable measurements.

Standalone `vimls --licenses` displays embedded licenses and documentation
attribution; release archives include the same notice files.

Build and release versions now use the root `VERSION` file. Local builds append
`-dev`; release packaging requires the tag to match the file.

Vim and Neovim event help is available as
[generated Go metadata](../tools/geneventdocs/README.md), preferring Vim for
shared names. Event hover and completion documentation use this data, including
Neovim-only event completion candidates.

General expression reformatting, embedded-language analysis and persistent
disk indexes remain outside the current scope. The parser still reparses
changed source; incremental AST editing is not implemented.

Completion shares syntax independently of full analysis. Local declaration
details are inferred only on resolve with snapshot validation; function parameter
and member symbol scans are reused within a request. Diagnostic and workspace
analysis yield to active completions at phase and batched traversal boundaries and back off until
150 ms after the latest edit. Queued document analysis then consumes the latest
snapshot. Content-owned cancellation abandons obsolete open-document semantic
work while preserving same-content sharing and independent waiter cancellation.
Further large-file latency work should measure parsing and client-side
rendering separately; neither is preempted by this scheduling.

### Incremental analysis pass consolidation

Configuration-only diagnostics share one source-order command scan. Leader
ordering, mapping replacement, loaded guards and encoding order retain their
individual scope rules and diagnostic order. Loaded guards are resolved after
the scan so a later marker assignment is still visible. Conditional mapping
removals continue to invalidate previously certain definitions.

Assignment validation computes the file-wide dynamic `execute` fact once and
passes it into embedded-command and lambda-body validation. Operator and builtin
argument validation pass proven expression completeness down to children,
avoiding repeated missing-node scans of complete subtrees without adding a cache
to the AST.
These diagnostic walks use the existing cooperative cancellation checkpoints.
Builtin argument validation also covers function parameter defaults and enum
value initializers, visiting shared enum argument expressions only once.

Builtin return inference reuses argument types from the same call's first
visit without making unknown types permanent across inference passes. Class
checks reuse the file's existing class index, and lambda declarations use the
previously collected scopes instead of searching every expression again.
Reference binding chooses the appropriate value or function lookup once.

Null-receiver checking records assignment targets while discovering lambda
bodies. The final diagnostic walk retains initial null facts until a write or
an execution barrier and uses command blocks to suppress E1360 within supported
Vim9 null-guard branches. It respects declaration identity and isolates temporary
facts and guards at deferred execution boundaries. This changes diagnostics only;
it adds no static type narrowing, short-circuit inference or propagation of
branch guards past a branch. See
[diagnostics](diagnostics.md) for the condition whitelist and limitations.
Function and user-command overwrite warnings share one cancellable command walk
while retaining their diagnostic phase order. Style checks collect final augroup
names and ambiguous references during their command walk; workspace names
remain available to the separate public suppression entry point.

Declaration indexing, reference binding and type inference remain separate
phases. Post-inference diagnostic order also remains significant: some checks
suppress or replace diagnostics emitted by earlier checks. Further traversal
consolidation must preserve those dependencies, rule-specific subtree pruning,
scope and dialect changes, and shared expression nodes. Syntax and style lint
rules already share `collectStyleDiagnostics` and `visitStyleExpression`; new
lint checks should reuse those walks. Focused analysis benchmarks are described
in [testing](testing.md#ci-and-additional-checks).

Text indexing and parsing now agree on LF, CRLF and CR physical lines.
Formatting and rename preserve their original byte spelling.

For parser debugging, `vimparse` accepts regular files or stdin with a 4 MiB
input limit. See the [testing guide](testing.md) for usage.
