# Roadmap

[Version 0.1.0](https://github.com/neoclide/vimls-go/releases/tag/v0.1.0)
is published. It provides the main editing features for Legacy Vim script and
Vim9; the [changelog](../CHANGELOG.md) lists changes made since that release.

The aim remains broad support for both languages through the pinned Vim
version. The [support guide](language-support.md) describes what works today.
The items below are remaining work, not promises for a particular release.

## Language support

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

`expand()` initializer types now distinguish strings from lists using a static
third argument in both dialects, including method calls. Dynamic flags remain
unknown.

Linked editing reports the declaration and its references in the current file so
a client can rename the symbol by typing. It is offered only where a
single-document edit is provably complete, so exported, autoload and global
symbols, class and interface members, symbols declared in another file, and
references spelled differently from the declaration are all withheld.
Explicit `g:`, `b:`, `w:`, `t:` and `v:` names are withheld at every scope,
including declarations inside functions, because their scopes are shared with
other scripts.
Extending it to those symbols needs a client that applies edits across documents
while typing; members additionally need the rename scope described below.
Class, interface, enum and type-alias names, as well as all import aliases, are
withheld until the analysis records their type annotation references. Explicit
`as` aliases are not exempt: expression occurrences alone can leave variable,
parameter, return and nested type annotations naming the old namespace.

Renaming a member from its declaration currently edits only the declaring file.
The same symbol renamed from a use site in an importing file edits both files, so
the two entry points disagree and the declaration-side result is incomplete. A
member of an exported aggregate is reachable from other files, but
`CollectSymbolFacts` reports the member itself as unexported, so
`workspaceLocalTarget` does not select the workspace path.

A renamed file rewrites the `:import` statements that name it, using the import
graph's reverse edges to find the importing documents. Each import is rewritten
in its own form, and the relative imports of the renamed files are recomputed for
their new location. The derived namespace of an import without an `as` alias is
rewritten together with its bound references, and only when the namespace has no
textual occurrence outside the literal that the analysis does not bind. That
completeness rule is what withholds a document whose namespace appears in a type
annotation or a comment: `analysis.References` records neither, so the bound
references alone cannot be proven to be every use site. Teaching the analysis to
bind namespaced type names would let those documents be rewritten too.
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

- Complete support for `def` functions in Legacy scripts and `function`
  blocks in Vim9 scripts.
- Improve type information for values imported into Legacy code and for code
  whose type becomes more specific after a condition.
- Decode more escaped command and mapping payloads where the original source
  locations can be preserved reliably.
- Add more option-value checks where Vim's source gives a clear rule.
  Build-dependent and runtime-dependent values still need conservative handling.
- Extend the [reviewed Neovim option compatibility rules](neovim.md#option-compatibility).
  [MacVim option compatibility](language-support.md#macvim-option-compatibility)
  now shares the parser context and setting diagnostics, with semantic
  highlighting and hover documentation for MacVim and Neovim-only options.
  Direct script `finish` guards narrow the context of subsequent commands.
  Editor conditions are now recorded on commands and expressions; future
  Neovim function checks can consume that context without another guard walker.

## Configuration and plugin projects

Configuration requests now have a 10-second timeout and are cancelled when
superseded or when the server shuts down; failed requests retain current settings.

- Follow more static `:source` relationships, including cycles.
- Detect cross-file mapping conflicts only when the loading order is known.
- Improve path completion and navigation for `:source`, `:runtime` and
  `:packadd`.
  Import completion filters names before its filesystem validation budget,
  so a precise prefix can find matches late in a large directory.
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

Text indexing and parsing now agree on LF, CRLF and CR physical lines.
Formatting and rename preserve their original byte spelling.

For parser debugging, `vimparse` accepts regular files or stdin with a 4 MiB
input limit. See the [testing guide](testing.md) for usage.
