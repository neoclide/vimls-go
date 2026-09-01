# vimls-go agent guide

## Mission

Build a Go language server for legacy Vim script and Vim9 script. The current
grammar and metadata ceiling is Vim v9.2.1015: earlier syntax remains supported,
while syntax introduced after that tag is unsupported until the pin advances.
Legacy Vim script and Vim9 script have independent root parsers. Cross-dialect
constructs are retained with loose recovery and are not errors merely because
they mix dialects; exhaustive support for `def` in a legacy-root file and
`function` in a Vim9-root file is deferred.

## First principles

- Start from observable Vim and LSP behavior. Do not copy assumptions from an
  existing language server without verifying them.
- Prefer the smallest direct implementation. Do not add Manager, Service,
  Factory, Strategy, Protocol, Adapter, Coordinator, Provider, Resolver, or
  Registry layers unless current code has multiple real implementations or
  callers that need them.
- Modify an existing package before creating a new package. Introduce an
  interface only at a real substitution boundary.
- Use Go for production code, test harnesses, generators, and repository tools.
  Vimscript is allowed as curated fixture/oracle input, and shell is acceptable
  only for small CI and test orchestration.
- Do not execute user Vim scripts during normal language-server analysis.

## Sources of truth

Use these sources in descending order:

1. The source and tests for the applicable Vim release, especially
   `src/testdir/test_vim9_*.vim`, `src/testdir/test_vimscript.vim`, and Ex command
   definitions.
2. The matching Vim runtime help, especially `vim9.txt`, `vim9class.txt`,
   `eval.txt`, `usr_41.txt`, and `repeat.txt`.
3. LSP 3.18 and JSON-RPC 2.0 specifications.
4. Reproductions made with a clean supported Vim executable.

The local official Vim checkout is `/Users/chemzqm/lib/vim`. Treat it as
read-only unless a task explicitly targets that repository. Inspect its status
before use, preserve all local changes, and query the exact release tag needed;
its current HEAD may be newer than vimls-go's configured target.

Record the Vim version and patch level for version-sensitive behavior. Never
turn behavior observed outside the pinned Vim tag into an unconditional rule.

## Language invariants

- Treat legacy Vim script and Vim9 script as related languages with independent
  root parsers that share Ex commands and neutral syntax data where useful.
- Dialect is contextual: `vim9script`, `def`, `function`, `vim9cmd`, `legacy`,
  and `scriptversion` can change the applicable rules. `vim9cmd` and `legacy`
  affect only their following command; they are not persistent parser modes.
- Do not emit a diagnostic solely because a file contains a cross-dialect
  construct. Complete parsing and semantics for `def` in a legacy-root file and
  `function` in a Vim9-root file remain a TODO, not a 1.0 coverage matrix.
- Parse command ranges, modifiers, abbreviations, bang, separators, comments,
  continuations, heredocs, and embedded command payloads contextually. A line
  split or regular-expression-only parser is not sufficient.
- Preserve byte spans and trivia in syntax data. Convert positions to the
  client's negotiated LSP encoding only at the document/protocol boundary.
- Recover from incomplete input and retain unknown commands as opaque syntax.
  User-defined or future commands are not syntax errors merely because the
  server does not know them.
- Keep legacy semantic diagnostics conservative. If dynamic behavior prevents
  proof, return `unknown` instead of inventing an error.

## Architecture boundaries

- `internal/jsonrpc`: message framing and JSON-RPC request lifecycle only.
- `internal/text`: immutable document snapshots and line/position indexes.
- `internal/syntax`: dialect-aware tokens, AST, parser, and recovery.
- `internal/analysis`: scopes, symbols, references, types, and diagnostics.
- `internal/workspace`: open documents, file discovery, imports, and indexes.
- `internal/server`: capability handlers that compose the packages above.

LSP wire types come from the pinned `go.lsp.dev/protocol` dependency. Position
encoding conversion stays at the `internal/server` and `internal/text`
boundary; do not create an empty `internal/lsp` package in advance.

Dependencies point from `server` toward the smaller packages; syntax and
analysis must not depend on transport or process state. Do not create empty
packages in advance of the milestone that needs them.

## Implementation rules

- Keep stdout reserved for LSP frames; write logs to stderr.
- Every analysis result belongs to an immutable document URI/version snapshot.
  A stale result must never overwrite diagnostics for a newer version.
- Apply incremental changes in order and test byte, rune, UTF-16, CRLF, BOM,
  combining-character, and astral-character boundaries.
- Advertise only implemented LSP capabilities.
- Preserve unknown JSON fields where the protocol requires forward
  compatibility, and return standard JSON-RPC errors for invalid requests.
- Add dependencies only when their current value exceeds the maintenance and
  supply-chain cost. Pin accepted dependencies; never use `@latest` in builds.

## Testing and validation

- Put focused tests beside their Go package and shared language fixtures under
  `testdata/`.
- Every accepted syntax form needs a positive fixture; every diagnostic or
  recovery rule needs a negative or incomplete fixture.
- Do not repeat every syntax form across mixed-dialect, incomplete-input, and
  version contexts. Use official per-form evidence plus focused tests for each
  shared recovery or context-switching mechanism.
- Use the official Vim test suite and runtime scripts as a corpus, but retain
  provenance and do not copy incompatible licensed content into generated
  artifacts without review.
- A clean Vim process is a test oracle only for curated, side-effect-free test
  inputs. Record `v:errors`, `:messages`, exit status, version, and patch level.
- Before handoff, run the checks relevant to the change. Once the Go module
  exists, the default gate is:

      gofmt -w <changed-go-files>
      go test ./...
      go vet ./...

- Do not run race tests or collect coverage unless the user or the task's
  validation requirements explicitly request them.
- Parser and framing fuzz targets must never panic, hang, or grow memory without
  bound. Add every discovered crash input to the permanent corpus.

## Delegation

Use project agents only for bounded tasks with clear inputs and ownership.
Read-only review may depend on an integrated diff; concurrent write tasks must
have disjoint owned paths:

- `language_researcher`: read-only Vim behavior and version research.
- `language_worker`: lexer, parser, AST, semantic analysis, and their tests.
- `server_worker`: text snapshots, workspace, JSON-RPC/LSP, server handlers,
  and their tests.
- `qa_reviewer`: read-only adversarial review and release evidence.

Migrate official compile diagnostics one error code at a time. Keep readable
Vim source, provenance, and inline assertions in the owning
`internal/analysis/official_compile_cases_e*_test.go` file. When the pinned Vim
release changes, add supported codes directly instead of batch-generating a
compile-case artifact.

The primary agent owns integration, public contracts, cross-package changes,
planning documents, configuration, and final validation. Give write agents an
approved task brief with explicit allowed and forbidden paths; require them to
compare changed paths with that brief before handoff. Do not have two agents
edit the same package at once.
Do not run write agents concurrently when they touch shared `testdata/`, `cmd/`,
`go.mod`, generated files, or integration fixtures. Workers must report missing
contracts instead of silently expanding scope.

Every write-agent task brief must contain these headings:

- `Goal`: one bounded observable outcome.
- `Allowed paths`: exact files or directories the agent may change.
- `Forbidden paths`: shared contracts/configuration it must not change.
- `Required behavior`: the frozen contract and failure semantics.
- `Validation`: exact commands and expected evidence.

If either path list is missing or ambiguous, a write agent must remain
read-only and ask the primary agent for a corrected brief.

## Change discipline

- Inspect `git status` before editing and preserve unrelated work.
- Keep commits milestone-scoped; stage only intended files.
- Before every commit, run `/ponytail-review` and resolve its findings. After
  the review passes, prefix the commit command with
  `VIMLS_PONYTAIL_REVIEWED=1`; the project hook rejects unreviewed commits.
- A local commit does not authorize push, release, issue closure, or PR creation.
- Update the roadmap and language-support contract when a milestone changes what
  the server actually supports.

## Go development workflow

- For Go code, use the gopls MCP tools before broad text searches when
  locating symbols, references, implementations, diagnostics, or callers.
- Prefer gopls semantic rename over search-and-replace for Go identifiers.
- After modifying a Go file, request gopls diagnostics for that file.
- Use gopls results as development guidance, not as a replacement for tests.
- Before completion, run:
  - gofmt on modified Go files
  - go test ./...
  - go vet ./...
- If the repository defines narrower validation commands, run those as well.
