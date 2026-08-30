# Session handoff — 2026-08-30

Read this file before continuing implementation. It records only unfinished
work and contracts that are easy to lose across sessions; the code and focused
tests remain the source of truth.

## Repository state

- `main` is at `2e5707f` after `codex/navigation` was rebased onto the parser
  work and fast-forwarded into the main worktree. Nothing was pushed.
- Preserve the user-owned untracked `vim-lsp-errors.md`. It is the merged
  static-error reference that replaces the earlier
  `vim-lsp-detectable-errors.md` and `vim9_compile_errors.md` files.
- Vim 9.2.1015 source is `/Users/chemzqm/lib/vim` and is read-only for this
  project. Legacy references are `/Users/chemzqm/lib/vim-vimlparser` and
  `/Users/chemzqm/lib/go-vimlparser`; esbuild is
  `/Users/chemzqm/lib/esbuild`.

The latest integration commits are:

- `d48242c` canonical workspace/runtime paths, safe scanning, and open-buffer
  overlay fixes.
- `4fcbe42` unknown-option warnings on assignment targets.
- `6f86b44` deep comparison for generated builtin-function metadata tests.
- `2e5707f` split the oversized server test suite by responsibility.

The navigation branch now provides async workspace indexing, static import and
autoload resolution, navigation, rename, completion, structure, semantic-token
and code-action foundations. Runtime paths use one conventional local Vim
installation, never start Vim, and failures remain best-effort.

Workspace identity is canonical realpath. Discovery de-duplicates aliases
before applying limits, skips permission-denied entries, skips a workspace root
that is exactly the user home or system temporary root, and skips every
`node_modules` directory. A real project below home or below the temporary root
is still scanned. Open documents must override disk content even when the
client URI and index path use different symlink spellings.

## Current goal

The active goal is still: detect every Vim9 compile error that can be proven by
pure static analysis, with representative Go tests. Do not mark this complete
until the support inventory is complete and audited.

`internal/analysis/official_compile_cases_test.go` is the live ledger. Its
completed and pending code lists are authoritative. Each error code may retain
at most 30 representative official cases. Do not migrate cases that require
starting Vim, sourcing user scripts, jobs/channels, filesystem side effects, or
test-suite global functions; write focused Go coverage for the static rule.

Permanently excluded static-analysis codes are:

`E1028 E1146 E1154 E1191 E1271 E1277 E1362 E1412 E1413`

Do not prematurely mark `E46`, `E118`, or `E119` complete. The last focused
triage found:

- `E46`: 11 representative cases; 1 ready, 4 mapping mismatches, 6 missing.
- `E118`: 6 cases; 0 ready, 1 mapping mismatch, 5 missing.
- `E119`: 17 cases; 12 ready, 5 missing.

The missing work includes readonly `v:true`/`v:false`/`v:null`/`v:none`
assignments and statically resolvable nested def/lambda arity. Exclude only
truly dynamic or test-harness-global variants.

## Next implementation: directed import graph

Build the Vim9 import dependency model before implementing graph-dependent
diagnostics. It is a directed graph, never a dependency tree.

Required graph behavior:

- Canonical realpath is node identity.
- Store importer-to-imported edges and reverse edges. Shared dependencies,
  diamonds, and cycles are valid.
- Replacing a file atomically replaces all of its outgoing edges and matching
  reverse edges. Removal leaves no stale reverse edge.
- Expose an immutable snapshot with a monotonically increasing revision and a
  ready state. Graph revision participates in stale-diagnostic rejection.
- Retain enough static import metadata for diagnostics, including importer,
  target, path span, alias, and autoload form.
- Dynamic imports, missing files, unreadable files, and unresolved paths remain
  unknown; they do not invent dependency edges or block graph readiness.
- Build the graph during the existing asynchronous workspace pass after
  `initialized`; do not block stdio. Build index and graph off-thread and swap
  the consistent pair atomically.
- Open-buffer snapshots override disk. A changed target reanalyzes reverse
  dependents. The server owns no filesystem watcher; it only registers watchers
  with the client.

Existing syntax and index data should be reused: `syntax.Import` already keeps
path/alias/autoload spans, `workspace.SymbolFact.Exported` records exports, and
`ExternalReferenceImportMember` records module-member uses. Vim import syntax
is `import {filename} [as {name}]` or `import autoload ...`; there is no
JavaScript-style named import list.

After the graph is stable, implement the graph-dependent compile diagnostics
`E1048`, `E1049`, `E1053`, and the import-dependent parts of `E1054`. Keep them
pending until then.

## Language contracts

- Target Vim behavior is 9.2.1015. Do not add `targetVersion` work now.
- Legacy Vim script and Vim9 are separate parsers/dialects sharing Ex command
  and expression machinery. Mixing alone is not an error.
- Invalid `vim9script` arguments produce a diagnostic, but the rest of that
  file is still parsed as Vim9.
- Loose recovery is physical-line oriented: on an unrecoverable line error,
  stop consuming that line and resume at the next line. Preserve later AST.
- Unknown commands stay opaque. Dynamic legacy behavior stays unknown rather
  than producing speculative diagnostics.
- Never execute user Vimscript during normal analysis or static tests.

## Test and commit discipline

- No selected tests from one source test file may run longer than 3 seconds.
  Always use an anchored `-run` selection, `-count=1`, and `-timeout=3s`.
- Do not run the entire `internal/server` suite while developing one feature.
  Its former monolithic test file is split into `server_test.go`,
  `document_sync_test.go`, `diagnostics_test.go`, `lifecycle_test.go`, and
  `transport_test.go`; select only the changed file's test names.
- Server tests that call `Initialized` must pass
  `{"runtimepath":[]}` unless default-runtime discovery is the behavior under
  test. This prevents machine-local Vim runtime scans and timing pollution.
- Run only the focused failing/fixed tests after each edit. Make a small local
  commit after each verified slice; do not push without separate authorization.
- Full tests, race, vet, categorized validation, and benchmarking remain
  deferred until broad static compile diagnostics are implemented.
- Final performance comparison uses only Vim's installed runtime corpus, five
  runs, and compares against `/Users/chemzqm/lib/go-vimlparser` for legacy
  parsing. Do not return to the full user RTP corpus for routine benchmarks.

Recommended continuation order:

1. Inspect `git status`, this handoff, and the compile ledger.
2. Add focused graph tests for a chain, diamond, cycle, atomic replace/remove,
   missing/dynamic import, realpath alias, revision, and ready state.
3. Integrate the graph into the existing async workspace build and open-buffer
   update path with focused server tests.
4. Implement `E1048`, `E1049`, `E1053`, and applicable `E1054` cases.
5. Continue the remaining `false` entries in the compile ledger in independent
   syntax/analysis batches.
6. Only after that, run the deferred full correctness and performance gates.
