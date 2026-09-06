# Changelog

## Unreleased

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
