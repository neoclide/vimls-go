# Changelog

## v0.1.0 — unreleased

Initial release of vimls-go, a language server for Legacy Vim script and Vim9
script. Syntax and metadata are pinned through Vim v9.2.1015. See
[candidate validation](docs/release-candidate.md) for validation evidence.

### Language support

- Independent Legacy and Vim9 parsers with recovery for incomplete code,
  contextual dialect switching, and preserved source positions.
- Static diagnostics for syntax, declarations, references, function arguments,
  Vim9 types, mutability and supported option values. Diagnostic severity,
  disabled codes and result limits are configurable.
- Conservative analysis of dynamic Vim script: unresolved runtime behavior
  remains unknown rather than producing speculative type errors.
- Source files are analyzed without sourcing or executing user Vim scripts.

### Editor features

- Context-aware completion for commands, functions, variables, options,
  autocommands, mappings, imports and class members, with lazy documentation
  and function-call snippets.
- Hover and signature help with parameter names, types and source comments.
- Definition, declaration, references, document highlights and import links;
  cross-file navigation for Vim9 imports and Legacy/Vim9 autoload functions.
- Document/workspace symbols, folding, selection ranges, semantic tokens and
  inferred-type inlay hints.
- Type hierarchy, call hierarchy, implementation lookup, and reference or
  implementation Code Lens where static analysis can resolve the target.
- Safe rename, focused syntax quick fixes, and document/range/on-type
  indentation formatting that preserves expressions and literal payloads.
- Incremental document synchronization, push or negotiated pull diagnostics,
  UTF-8/UTF-16/UTF-32 positions, and stdio or optional TCP transport.

### Runtimepath and help

- Workspace indexing reports progress separately from external runtimepath
  indexing. External directories scan concurrently with a log per directory.
- Runtimepath updates are debounced and applied incrementally: retained roots
  reuse their parsed data, removed roots are cleared, and new roots are added
  after the active batch completes.
- Runtime help from `doc/*.txt` is collected in the background for global
  variables, global/autoload functions, Ex commands and `<Plug>` mappings.
  Hover and completion read cached help; failed help files are isolated.
- Built-in function and command descriptions come from the active runtimepath,
  avoiding duplicate embedded documentation. Hover appends help as a separate
  document with its absolute file path and line number.
- If initialization supplies no usable runtimepath, vimls-go optionally queries
  a clean `vim` process for its default directories using `globpath()` and JSON.
  Discovery has a timeout, does not load vimrc/plugins or viminfo, and fails
  silently. Vim is not required for core language analysis.

### Editing details

- Option hover separates name/type/default/scope/build information from the
  help body and removes redundant build-feature notes.
- `no` and `inv` option prefixes belong to the complete semantic token and hover
  range, including abbreviated option names.
- Complete `<Plug>(name)` expressions are recognized for hover.
- Heredoc flags and opening/closing markers use the `special` semantic token;
  literal payloads remain opaque.
- Unknown uppercase user commands produce an E492 warning after indexing and
  help loading complete. Explicit command definitions and Ex-command help tags
  both count as known names.
- Legacy user-command positions retain command classification in completion,
  semantic tokens and hover.
- Catch-all patterns and Vim error codes such as `E31` do not trigger the
  warning about matching human-readable error messages.

### Distribution and limitations

- Standalone binaries and archives are prepared for macOS, Linux and Windows
  on amd64/arm64, plus Linux ARMv7 and FreeBSD amd64. Archives include support
  documentation and license notices; downloads include SHA-256 checksums.
- Dynamic source targets, load order and runtime-created values are not fully
  resolved. Exhaustive cross-root mixed-dialect semantics, flow-sensitive
  narrowing and external Legacy value propagation remain deferred.
- Formatting is indentation-only. Rename refuses ambiguous/dynamic targets
  and changes that would require renaming autoload files or namespaces.
- Syntax introduced after Vim v9.2.1015 and embedded-language delegation are
  outside this release's scope. Runtime help and client-dependent features
  require the corresponding runtime files and client capabilities.

See [language support](docs/language-support.md) and
[configuration](docs/configuration.md) for the complete behavior and settings.
