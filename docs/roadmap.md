# Roadmap

The goal is broad support for Legacy Vim script and Vim9 through the
[pinned Vim version](language-support.md). This page lists remaining work;
there is no release date attached to these plans.

See [language support](language-support.md) for what works today and the
[changelog](../CHANGELOG.md) for past changes.

## Language support

- Complete support for `def` functions in Legacy scripts and `function`
  blocks in Vim9 scripts.
- Parse more escaped command bodies, building on the supported
  [colon-led mappings](language-support.md#mappings-and-configuration-files).
  Navigation and edits must still point to the correct text in the source file.
- Check more option values where Vim's source defines a clear rule. Avoid
  reporting errors when validity depends on the user's build or runtime state.
- Cover more [Neovim option differences](neovim.md#option-compatibility).
  Use the existing editor-condition tracking for future Neovim function checks.

## Configuration and plugin projects

- Detect mapping conflicts across files when their loading order is known.
- Consider watching plugin files outside the workspace if real projects need
  updates without restarting the server. Keep the number of watched directories
  and the cost of rescanning under control.
- Consider parameter-name hints and clickable links in comments.

## Before 1.0

- Test real plugin projects for false diagnostics, missing navigation and
  incorrect edits. Add focused regression tests, and check unclear Vim behavior
  against the pinned version.
- Validate release packages on supported platforms and in a real editor client.
- Prioritize crashes, lost edits and incorrect rename results.
  Continue checking cancelled requests and repeated file operations for lost
  import edits.
- Investigate delays in large files. Measure server parsing and editor rendering
  separately, and compare the same workload before and after performance changes.
- Reduce repeated analysis passes where measurements show a benefit. Preserve
  diagnostic order, scope and dialect rules, and cancellation behavior. See the
  [testing guide](testing.md#ci-and-additional-checks) for analysis benchmarks.

## Outside the current scope

- General expression reformatting.
- Analysis of embedded languages.
- Persistent indexes on disk.
- Updating only the changed parts of the syntax tree; the parser currently
  reparses changed source.
