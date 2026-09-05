# Changelog

## v1.0.0-rc.1 — unreleased

First release candidate for the conservative Legacy Vim script and Vim9 script
language-server contract through Vim v9.2.1015. This entry does not imply that
the candidate has been tagged, published, or passed all platform gates.

### Available

- Independent dialect parsers, pinned Vim metadata/help, static diagnostics,
  completion, hover, signatures, workspace/import/autoload navigation and safe
  rename without executing user Vim scripts.
- Incremental synchronization, push and negotiated document/workspace pull
  diagnostics, semantic tokens/delta, symbols, structure, hierarchy,
  implementations, Code Lens, inlays and focused syntax repairs.
- Source-preserving document/range/multi-range/on-type indentation.
- External runtimepath indexing limited to recursive `plugin`, `autoload`,
  and `import` Vim scripts; colorscheme completion uses top-level color file
  names and paths. Workspace-contained roots are scanned once.

### Candidate fixes and acceptance

- Windows workspace path identity follows host filesystem rules after URI
  conversion; diagnosticscan tests compare canonical output paths.
- Fixed tuple indexes infer their element type; direct legacy tuple tests cover
  references, destructuring, iteration, container-preserving slices, literal
  cardinality and conservatively proven immutable-item assignments.
- Expression mappings reject literal Blob/Funcref results while accepting Vim's
  supported container/numeric conversions. Deferred dynamic results stay unknown.
- Stdio/TCP acceptance covers Unicode positions and unsaved disk overlays.
  The integration suite can test unpacked release binaries without rebuilding.
- Deterministic archives retain standalone binary download names, README,
  release notes, support limitations and license notices.

### Boundaries

Dynamic source targets/load order and runtime-created values remain unknown.
Cross-root mixed-dialect semantics, flow-sensitive narrowing, external legacy
value propagation and syntax newer than the pin are not candidate requirements.
Formatting is indentation-only; autoload/namespace-changing rename is rejected.
See `docs/language-support.md` for the full contract and
`docs/release-candidate.md` for exact validation and pending platform gates.
