# Roadmap

[Version 0.1.0](https://github.com/neoclide/vimls-go/releases/tag/v0.1.0)
is published. It provides the main editing features for Legacy Vim script and
Vim9; the [changelog](../CHANGELOG.md) lists changes made since that release.

The aim remains broad support for both languages through the pinned Vim
version. The [support guide](language-support.md) describes what works today.
The items below are remaining work, not promises for a particular release.

## Language support

Legacy variable type-change warnings now cover straight-line simple assignments.
They discard unknown types and stop at calls and control-flow boundaries; broader
flow-sensitive checking remains outside this warning's current scope.

`expand()` initializer types now distinguish strings from lists using a static
third argument in both dialects, including method calls. Dynamic flags remain
unknown.

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

Build and release versions now use the root `VERSION` file. Local builds append
`-dev`; release packaging requires the tag to match the file.

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
