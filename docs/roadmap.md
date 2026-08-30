# Delivery roadmap

## Definition of done

Version 1.0 is complete only when the language-support contract is covered for
Vim 9.1 and the latest stable Vim, the required LSP features below work through
the real stdio server, and all release gates pass. Parser acceptance alone is
not language-server completion.

## M0: repository and protocol foundation

Deliver:

- Go module, `cmd/vimls`, version output, build and test commands.
- Bounded LSP `Content-Length` framing and JSON-RPC request/notification/error
  handling.
- `initialize`, `initialized`, `shutdown`, and `exit` lifecycle.
- Parse and validate `vimls.targetVersion` from initialization/workspace
  configuration using the contract in `docs/language-support.md`.
- Logging to stderr and no non-protocol stdout output.

Exit gate: fragmented and coalesced frames, malformed JSON, unknown methods,
duplicate IDs, cancellation, shutdown order, and a real subprocess handshake
all pass under `go test -race ./...`; the 10,000-notification soak remains
stable and internal statement coverage remains at least 90%.

## M1: text snapshots and document synchronization

Deliver:

- URI/version snapshots, line index, negotiated position conversion.
- Full and incremental `didOpen`/`didChange`/`didSave`/`didClose`.
- Analysis cancellation and stale-result prevention.
- `workspace/didChangeConfiguration` reanalysis with configuration revisions.

Exit gate: table tests cover ASCII, tabs, UTF-8, UTF-16 astral characters,
combining characters, CRLF, BOM, invalid ranges, multiple ordered edits, close,
and reopen. Incremental edits produce the same final text as full replacement.

## M2: command lexer and recovering parser core

Deliver:

- Ex ranges, modifiers, command lookup/abbreviation, separators, comments,
  continuation, strings, heredocs, and opaque user commands.
- Contextual dialect state, one-command overrides, and typed nodes for basic
  declarations and blocks.
- Expression parser with stable byte spans and deterministic AST serialization
  for golden tests.

Exit gate: official/curated legacy, Vim9, invalid, and incomplete corpora parse
without panic or nontermination; recovery reaches later valid declarations.
Existing mixed-dialect cases are smoke coverage only: mixing alone produces no
diagnostic and must not prevent recovery to later commands.

## M3: full legacy and Vim9 syntax

Deliver the remaining syntax in `language-support.md`, including functions,
classes, interfaces, enums, type aliases, generics, imports/exports, complex
expressions, mappings, autocommands, and embedded payload preservation.

Current verified syntax is recorded once in `language-support.md` and in focused
package tests. Generated official positive and negative cases are the primary
per-form evidence. Focused curated tests cover mixed-dialect transitions and
incomplete-input recovery once per shared parser mechanism, rather than
duplicating every syntax form across those contexts. Version-boundary evidence
is required only for forms introduced after the 9.1 compatibility floor.

Complete parsing and semantics for `def` in a legacy-root file and `function`
in a Vim9-root file are deferred. Existing tests for those combinations remain,
but no exhaustive combination matrix is an M3 exit gate.

Exit gate: every required construct has official or focused acceptance evidence
and relevant rejection evidence. Context switching and physical-line recovery
have focused cross-cutting coverage, every version-gated form has its actual
boundary covered, and parser fuzz seeds plus the selected official Vim
runtime/source corpus produce no crashes or unbounded behavior.

## M4: semantics and diagnostics

Deliver:

- Scopes, declarations, imports, references, mutability, arity, and Vim9 types.
- Conservative legacy inference and version-target diagnostics.
- Stable diagnostic codes, ranges, severities, related information, and limits.

Current implementation has same-file scopes, declarations, references,
shadowing, conservative type facts, document symbols, target-version
diagnostics, direct Vim9 `const`/`final` reassignment diagnostics, and arity
diagnostics for direct statically named Vim built-in calls. User-function arity,
full Vim9 type checking, import resolution, and the remaining mutation forms are
still M4 work; unresolved or dynamic targets remain unknown.

Exit gate: diagnostics match golden results and safe Vim oracle cases; unknown
dynamic behavior does not create false undefined/type errors; stale diagnostics
cannot publish after a newer edit or close.

Current status (2026-08-30): lexical scopes, declarations, references,
shadowing, mutability metadata, conservative type inference, bounded syntax
diagnostics, and target-version diagnostics are implemented. M4 remains open
for statically provable user-function arity diagnostics and broader type
diagnostics; unresolved dynamic legacy names remain intentionally unknown.

## M5: navigation and workspace index

Deliver:

- Document/workspace symbols, hover, definition, declaration, references,
  document highlights, folding ranges, and selection ranges.
- Incremental index for workspace files, imports, autoload names, configured
  runtimepath roots, and multi-target builtin metadata.

Exit gate: cross-file legacy autoload and Vim9 import navigation works through a
subprocess LSP test; duplicate, missing, cyclic, symlinked, and out-of-root files
have deterministic behavior; canceled index work leaves no partial state.

Current status (2026-08-29): the document navigation surface, workspace symbol
index, open-document overlay, client-driven watched-file refresh, static Vim9
import navigation, and legacy/Vim9 autoload navigation are implemented. The
server dynamically registers Vim file watchers when the client supports it;
client-provided or locally discovered runtimepath roots and later custom
notification replacements are included in the canonical, bounded index. The
subprocess test covers watcher registration, runtimepath and workspace symbols,
plus cross-file definition and references. M5 remains open for multi-target
builtin metadata, which is currently deferred.

## M6: completion and safe edits

Deliver:

- Contextual command, modifier, variable, member, builtin, option, event, and
  import completion with resolve documentation.
- Signature help, prepare rename, rename, and syntax-backed code actions.
- Semantic tokens after syntax classifications are stable.

Exit gate: completion and signature results are relevant to dialect/scope and
bounded; rename refuses dynamic/ambiguous symbols and produces non-overlapping,
version-valid edits; token ranges remain valid for Unicode text.

Current status (2026-08-30): static import/source document links, contextual Ex
command, visible declaration, builtin function and static import-member
completion, completion resolve detail, user-function signature help, safe
same-file/static-import rename, full semantic tokens, inferred-type inlay hints,
and the deterministic missing-block-end quick fix are implemented. The stdio
subprocess test exercises these capabilities. M6 remains open for versioned
modifier/option/event metadata, builtin parameter/documentation metadata,
import-path completion, and broader stable semantic classifications.

## M7: compatibility, performance, and 1.0 release

Deliver:

- CI for Vim 9.1.0000, latest published 9.1 patch, and latest stable Vim.
- Linux/macOS builds on each change; Windows build and integration coverage
  before release.
- Fuzz corpus, large-file/workspace benchmarks, vulnerability scan, changelog,
  installation/client examples, and reproducible release archives.

Exit gate: all checks in `docs/testing.md` pass; no open crash/data-corruption,
protocol lifecycle, false-edit, or minimum-version blockers; performance budgets
hold on the documented runner; a clean install passes the repository's Go stdio
harness plus pinned smoke fixtures for Vim 9.1 with `vim-lsp` and Neovim with its
built-in LSP client. Each editor fixture must initialize the server, open one
legacy and one Vim9 file, observe the expected diagnostics, then shut down
cleanly.

Pinned client smoke baseline:

- Vim `9.1.1365` with `vim-lsp` commit
  `e10d186452743beb7b43d2b3427020832f930c2b`, checked out below
  `.test-tools/vim-lsp` by the M7 CI setup.
- Neovim `0.12.4` using its built-in LSP client.
- Vim command:
  `vim -Nu test/clients/vim-lsp/vimrc -U NONE -n -es -X -i NONE -S test/clients/vim-lsp/smoke.vim`.
- Neovim command:
  `nvim --headless -u test/clients/nvim/init.lua -l test/clients/nvim/smoke.lua`.

M7 must add and own those fixture files plus a script that clones the pinned
`vim-lsp` commit without modifying user configuration. Updating a pin requires
an intentional planning change and a successful old-versus-new smoke run.

## Required 1.0 LSP surface

- Lifecycle and cancellation.
- Incremental text synchronization and versioned diagnostics.
- Hover, definition, declaration, references, highlights.
- Document/workspace symbols, folding and selection ranges.
- Completion with resolve and signature help.
- Prepare rename and rename for provably safe symbols.
- Semantic tokens and focused code actions.

Formatting, call hierarchy, inlay hints, embedded-language delegation, and
nonstandard client extensions are post-1.0 unless real client validation shows
one is required for a usable baseline.

## Planning gates

Before each implementation slice:

1. Freeze a short task brief with owned paths, contract, fixtures, and commands.
2. Delegate only independent non-overlapping work.
3. Integrate in the primary thread.
4. Update the support contract from actual behavior, not intended behavior.

Run an adversarial QA review only at a substantial feature stabilization point
or before release, not as a blocker between every implementation slice.
