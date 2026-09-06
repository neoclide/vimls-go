# Roadmap

[Version 0.1.0](https://github.com/neoclide/vimls-go/releases/tag/v0.1.0)
is published. It provides the main editing features for Legacy Vim script and
Vim9; the [changelog](../CHANGELOG.md) lists changes made since that release.

The aim remains broad support for both languages through the pinned Vim
version. The [support guide](language-support.md) describes what works today.
The items below are remaining work, not promises for a particular release.

## Language support

- Complete support for `def` functions in Legacy scripts and `function`
  blocks in Vim9 scripts.
- Improve type information for values imported into Legacy code and for code
  whose type becomes more specific after a condition.
- Decode more escaped command and mapping payloads where the original source
  locations can be preserved reliably.
- Add more option-value checks where Vim's source gives a clear rule.
  Build-dependent and runtime-dependent values still need conservative handling.
- Extend the [reviewed Neovim option compatibility rules](neovim.md#option-compatibility).
  Editor conditions are now recorded on commands and expressions; future
  Neovim function checks can consume that context without another guard walker.

## Configuration and plugin projects

- Follow more static `:source` relationships, including cycles.
- Detect cross-file mapping conflicts only when the loading order is known.
- Improve path completion and navigation for `:source`, `:runtime` and
  `:packadd`.
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

General expression reformatting, embedded-language analysis and persistent
disk indexes remain outside the current scope. The parser still reparses
changed source; incremental AST editing is not implemented.
