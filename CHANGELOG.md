# Changelog

## v0.1.6 — unreleased

- Preserve runtime help prose following inline error and concept tags, so
  builtin hovers such as `popup_create()` include their full documentation.
- Withhold file-rename import edits that would load a different script through
  runtimepath precedence, accounting for all files in the rename batch. Support
  plain double-quoted imports without an alias as well as single-quoted imports.
- Withhold linked editing for explicit globals at every scope and for type
  names whose type annotation references are not yet included.
- Rewrite Vim9 `:import` statements when a file is renamed through the client's
  file operation, including the relative imports of the renamed files
  themselves and the derived namespace of an import without an `as` alias. Each
  import keeps its original form, and a document is skipped rather than partly
  edited when an import cannot be rewritten or its content cannot be verified.
- Add linked editing ranges: the declaration and its references in the current
  file are edited together while typing. Ranges are offered only for symbols
  whose rename is a single-file edit, so exported, autoload and global symbols,
  class and interface members, symbols declared in another file, and references
  spelled differently from the declaration return no ranges instead of a partial
  rename.
- Report a Vim error diagnostic when a referenced command, option, builtin
  function or autocommand event was added after the configured target version,
  naming the version that introduced it.
- Show the introduction history of commands, options, builtin functions and
  autocommand events in hover.

## v0.1.5

- Support for vim9 'import' statement without as.
- Support import completion with relative file paths.
- Complete full runtimepath import file paths, including nested files; autoload paths complete files and subdirectories one level at a time, including after `/`.
- Support autoload keyword completion in import statements.
- Support vim9 script type completion.
- Fix a parser crash while entering an incomplete lambda parameter type.
- Fix import `as` completion for quoted paths and malformed trailing expressions, omit namespace-only import candidates, and avoid duplicate spaces on type-completion retriggers.

## v0.1.4

- Add diagnostics for wrong option assignments.
- Show option documentation for option variable.
- Add documentation for builtin map commands.
- Add documentation for autocmd events.

## v0.1.3

- Keep cached plugin command completions available while background analysis is
  running, including commands from workspace and runtimepath indexes.
- Add setup documentation for the yegappan/lsp client.
- Validate the version's CHANGELOG section before `make release` creates or
  pushes a tag, rejecting missing, empty or duplicate release notes.

## v0.1.2

- Produce completion candidates without waiting for background type analysis;
  resolve local type details on demand and let analysis yield while typing.
- Infer variable types from known initializer return types, including the
  string and list forms of `expand()`.
- Warn when consecutive simple Legacy assignments change a variable's known
  basic type, using `vimls/variable-type-change`.
- Stabilize configuration-shutdown and diagnostic-cache tests; correct the
  text-edit fuzz reference model for CR and CRLF line endings.

## v0.1.1

- Preserve LF, CRLF and CR line endings across text positions, diagnostics,
  formatting and rename.
- Cancel superseded configuration requests and bound unanswered requests to
  10 seconds while retaining the current settings on failure.
- Find import path completions after prefix filtering in large directories.
- Avoid repeated suffix scans for unterminated heredocs and reduce metadata
  lookup and document-edit overhead.

- Reduce repeated scanning and allocation while collecting long Vim9 automatic
  continuations, including expressions, function signatures and block lambdas.

- Accept stdin with `vimparse -`; limit file and stdin input to 4 MiB and reject
  named special files before parsing.
- Add completion, hover, navigation and error checking inside mapping expression
  prompts such as `<C-R>=...<CR>`. Commands before a prompt keep their normal
  highlighting and help.
- Report missing script-local functions in calls and supported mappings.
- Wait for both workspace and runtimepath indexing before reporting missing
  functions or commands, avoiding warnings while plugin files are still loading.
- Show the specific help for named `<Plug>` mappings. Function hovers now
  respect the client's Markdown or plain-text preference.
- Log runtimepath directories as they finish and show one elapsed time for the
  complete update.

## v0.1.0 — 2026-09-05

First published release. Supports Legacy Vim script and Vim9 syntax through
Vim v9.2.1015.

- Completion, hover, signature help, definitions, references and workspace search.
- Vim9 type checking, inferred-type hints, class and call hierarchies, and
  interface implementation lookup.
- Symbol rename, small syntax fixes, semantic highlighting and indentation
  formatting.
- Plugin and autoload indexing from runtimepath, with help from runtime
  documentation.
- Adjustable diagnostics and separate treatment of vimrc-style configuration
  files.
- Standalone downloads for macOS, Linux, Windows and FreeBSD.

The server analyzes source without executing user scripts. Dynamic names and
runtime-generated code cannot always be resolved. Formatting changes indentation
only, and rename is limited to targets that can be resolved safely.

[Release downloads](https://github.com/neoclide/vimls-go/releases/tag/v0.1.0)
include binaries, archives and SHA-256 checksums.
