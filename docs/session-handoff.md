# Session handoff — 2026-08-30

Read this file before continuing implementation. It records only unfinished
work and contracts that are easy to lose across sessions; the code and focused
tests remain the source of truth.

## Repository state

- `main` is at `cefc2ed`. Nothing was pushed.
- Preserve the user-owned untracked `vim-lsp-pending-errors.md`. It is the merged
  static-error reference that replaces the earlier
  `vim-lsp-detectable-errors.md` and `vim9_compile_errors.md` files.
- Vim 9.2.1015 source is `/Users/chemzqm/lib/vim` and is read-only for this
  project. Legacy references are `/Users/chemzqm/lib/vim-vimlparser` and
  `/Users/chemzqm/lib/go-vimlparser`; esbuild is
  `/Users/chemzqm/lib/esbuild`.

The latest integration commits are:

- `e1c28c9` changed the Go module and internal imports to
  `github.com/neoclide/vimls-go`.
- `ed4449d` added the directed Vim9 import graph and server lifecycle wiring.
- `976a3b6` kept unknown or unreadable import targets out of graph edges and
  made reverse-dependent traversal transitive.
- `6df66b5` is the user's agent-guide update and must be preserved.
- `cefc2ed` added the statically provable import diagnostics described below.

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

## Completed import dependency milestone

The Vim9 import dependency model is a directed graph, never a dependency tree.
Published snapshots are immutable. A Vim9 file or workspace state change
creates a newer graph revision; callers never mutate a published snapshot.

Implemented behavior:

- Canonical realpath is node identity. Importer-to-imported and reverse edges
  support shared dependencies, diamonds, and cycles.
- Replacing or removing a file atomically updates both edge directions. Reverse
  dependent traversal is transitive and deterministic.
- The asynchronous workspace pass builds the index and graph together and
  swaps the consistent pair atomically. Open snapshots override disk content.
- Static import facts are retained even when no edge can be proven. Dynamic,
  missing, unreadable, unsafe, or unindexed targets do not invent edges or
  block graph readiness.
- Changes reanalyze all open reverse dependents. Graph revision participates in
  stale-diagnostic rejection, including non-file documents.
- Imports nested in `def` or `function` bodies do not create graph edges or
  cross-file diagnostic cascades.
- Static analysis now reports provable `E1048`, `E1049`, and `E1053` cases, plus
  `E1054` when an import alias follows a conflicting script variable, `const`,
  `final`, class, or type alias. Forward receivers, dynamic imports, and
  deferred autoload member resolution remain conservative `unknown`.

The focused workspace, analysis, and server selections passed with
`-count=1 -timeout=3s`. Full tests, race, and vet were intentionally not run
during this fast iteration.

## Next implementation

Return to the remaining `false` entries in the official compile ledger. The
next bounded slices are the statically provable `E46` readonly special-value
assignments, followed by `E118`/`E119` user-function arity. Preserve dynamic
calls and test-harness-global variants as `unknown` rather than guessing.

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
2. Implement the provable `E46` special-value assignments with focused tests.
3. Implement statically resolved `E118`/`E119` user-function arity cases.
4. Continue remaining `false` ledger entries in independent analysis batches.
5. Only after that, run the deferred full correctness and performance gates.
