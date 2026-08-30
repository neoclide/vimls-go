# Test and release strategy

## Principles

- Test public behavior and failure semantics, not private implementation shape.
- Every range assertion includes the source text and checks byte-to-LSP position
  conversion separately.
- Keep parser goldens deterministic. CI never rewrites expected output.
- Green narrow tests do not prove a milestone whose contract is broader.
- A Vim executable is a development oracle, never a production dependency.
- Normal build and test commands use Go's global module cache with
  `-mod=readonly`. With the required versions cached they do not fetch modules
  from the network.

## Test layout

```text
internal/<package>/*_test.go       focused unit and package integration tests
testdata/legacy/                   accepted and rejected legacy scripts
testdata/vim9/                     accepted and rejected Vim9 scripts
testdata/mixed/                    cross-dialect smoke/recovery cases, not a matrix
testdata/incomplete/               editor typing states and recovery
testdata/unicode/                  byte/rune/UTF-16/line-ending boundaries
testdata/workspace/                import, autoload, runtimepath projects
testdata/fuzz/                     permanent crash and regression corpus
test/integration/                  subprocess JSON-RPC/LSP scenarios
test/vim/                          curated side-effect-free Vim oracle cases
```

Shared fixture directories are used when a case is large or reused across
packages. Small focused cases may remain inline beside the package test; an
empty fixture category is not by itself missing coverage.

Fixture metadata records expected dialect, minimum Vim version/patch, expected
diagnostics or AST snapshot, and upstream provenance when applicable.

The default upstream source checkout for provenance and tagged corpus reads is
`/Users/chemzqm/lib/vim`. Tests and generators must address explicit tags or
commits and must not modify that checkout or depend on its current HEAD.

## Required layers

### Syntax

Lexer tests assert token kind, text, byte span, trivia, and dialect. Generated
official cases and focused tests assert form-specific acceptance, rejection,
AST shape, and diagnostics. Small cross-cutting goldens cover mixed dialects
and incomplete-input recovery for the parser mechanisms that own those states;
they are not repeated for every syntax form. Essential ambiguity cases include
`|`, quotes versus comments, command abbreviations, ranges/modifiers,
continuations, heredocs, mapping payloads, `vim9cmd`, `legacy`, `def`, and
`function`.

Cross-dialect smoke cases assert retention, forward recovery, and no diagnostic
solely for mixing. They do not claim complete semantics for `def` in a
legacy-root file or `function` in a Vim9-root file, and those combinations are
not multiplied across the per-form syntax suite.

The default offline gate also reads generated v9.2.1015 artifacts below
`testdata/official/`. The full-file corpus losslessly contains all 362 tracked
`.vim` files below Vim's `src/testdir` (8,558,061 source bytes). The embedded
corpus contains 3,267 cases extracted from 17 official parser and evaluator
tests. Both feed every source to `LegacyParser` and `Vim9Parser`, asserting
retained source plus ordered, in-bounds command, token, block, and diagnostic
spans without sourcing or executing the files. A separate helper inventory
classifies all 5,733 `Check*` candidates, including all 5,208 qualified
`v9.Check*` calls, so each call can later become a generated conformance case or
an explicit skip. `v9.2.1015-parser-files.json` is the fixed migration boundary:
44 syntax-relevant files are included, 24 ambiguous helper-bearing files have
explicit exclusions, and the remaining 294 files inherit the default exclusion.
Migration tools must process only the 44 included files; changing the pinned Vim
version or this reviewed manifest is the only reason to revisit excluded files.
The generated parser-case artifact hashes that manifest and accounts for every
one of the 3,844 selected helper calls. Static source recovery extracts 3,805
calls into 5,261 dialect-aware variants and records an explicit reason for the
remaining 39 skips. The 1,761 success variants are parser-positive assertions.
The artifact keeps its 3,500 failure variants unclassified and retains their Vim
error arguments as provenance. Their reviewed phase classification and syntax
implementation status live separately in the official syntax migration ledger,
so parser progress does not rewrite the pinned generated artifact.

Full-file parsing proves stability, recovery, and span integrity, not exact Vim
acceptance. The conformance layer must use generated helper expectations and
focused source-referenced matrices to assert accepted or rejected syntax,
error provenance, AST shape, and recovery diagnostics. Every official helper
candidate must be extracted or retained in a classified skip manifest; the
broad corpus is not a substitute for those conformance assertions.
`make test-official` compares the generated corpora with the exact tag in
`/Users/chemzqm/lib/vim` without modifying that checkout or accessing the
network.

#### Official failure migration

Research the complete pinned failure corpus once per Vim release and keep its
phase classification, syntax rule groups, source references, and migration
status in [`official-syntax-migration.md`](official-syntax-migration.md). The
compressed parser-case artifact remains the source of every exact case key and
input; the ledger must reference it rather than copy fixture source. Do not
rescan the corpus with a new research task for every implementation batch.

Implementation batches consume one or more non-overlapping ledger group IDs.
Use the triage filter to verify the parser's current result, not to rediscover
the batch. The filter accepts comma-separated substrings and reports every
matching dialect variant, which prevents a migration from silently covering
only one generated context:

```sh
go test -mod=readonly ./internal/syntax \
  -run '^TestOfficialVimParserFailureTriage$' -count=1 -v \
  -args -official-case='4170:120710,4171:120781'
```

`ready` means the parser already produces the official error code, `mapping`
means it produces one different diagnostic, `missing` means it produces none,
and `recovery` means it produces multiple diagnostics. `unknown` means the Vim
helper error argument cannot be mapped unambiguously to its generated variants
and still needs source inspection.

After adding the selected cases to the parser-failure matrix, run only that
batch during iteration:

```sh
go test -mod=readonly ./internal/syntax \
  -run '^TestOfficialVimParserFailures$' -count=1 \
  -args -official-case='4170:120710,4171:120781'
```

The default test invocation omits `-official-case` and continues to verify the
entire committed matrix. After the focused test and local commit, update the
ledger status and commit reference; a changed parser result does not require a
new Vim-source research pass.

### Semantics

Table and workspace fixtures cover scopes, shadowing, closures, imports/exports,
autoload, cycles, members, generics, null values and `null_<type>` behavior,
empty containers, mutability, arity, version gates, and safe/unsafe rename.
Assertions include stable code, severity, byte span, LSP range, and related
location. Oracle fixtures separately cover legacy byte string indexing and
Vim9 character indexing.

### Protocol and lifecycle

Exercise byte-counted `Content-Length`, partial/coalesced frames, negative,
non-decimal, malformed, and oversized lengths, malformed JSON, request IDs,
unknown methods, cancellation, initialize ordering, shutdown/exit, EOF, blocked
writes, and stdout purity. Invalid framing closes the stream without attempting
resynchronization. JSON arrays are rejected as invalid requests because batch is
not supported. Run the compiled server as a subprocess for the final path.

Document scenarios cover out-of-order client versions, multiple changes in one
notification, stale analysis, close during analysis, reopen, save, cancellation,
and workspace-folder changes.

### Fuzz and properties

Fuzz framing, position conversion, lexer, parser, and incremental edit
application. Required properties:

- No panic, deadlock, unbounded loop, or uncontrolled allocation.
- Token and AST spans remain ordered and within the source.
- Recovery makes forward progress.
- Applying valid incremental edits matches full-text replacement.
- Position round trips are stable at valid character boundaries.

PR checks replay the committed corpus. Scheduled CI runs bounded live fuzzing;
every discovered input becomes a regression seed.

### Vim oracle

Run only curated scripts with a clean supported Vim, no user vimrc/gvimrc, no
swap or history file, and a task-specific temporary working directory. The base
invocation is `vim -Nu NONE -U NONE -n -es -X -i NONE -S <fixture>`. Capture
`v:version`, patch information, `v:errors`, `:messages`, and process status; Ex
silent mode can hide errors if only the exit code is checked.

The oracle checks acceptance/rejection and focused semantics. It does not source
untrusted workspace files and it does not justify reproducing Vim's runtime
inside the server.

## Compatibility matrix

Per change:

- Supported Go versions from `go.mod` on Linux.
- Vim 9.1.0000 and latest stable Vim for relevant oracle cases.
- Build on Linux and macOS.

Scheduled and release:

- Latest published 9.1 patch in addition to the minimum and latest stable.
- Windows build and subprocess protocol test.
- Full race, fuzz, corpus, vulnerability, and benchmark lanes.

Each Vim lane first proves its actual version and required `+eval`/Vim9 support.
Neovim may be used as an LSP client interoperability lane, but it is not a Vim9
language oracle.

## Performance budgets

Establish baselines at the milestone that introduces each operation rather than
inventing numbers before code exists. Benchmarks include 1 KiB, 10 KiB, 100 KiB,
and 1 MiB documents; small and large workspaces; full parse; one-line edit;
diagnostics; index replacement; completion; and references.

On a pinned release runner, fail a confirmed median or p95 time regression above
15% or allocation regression above 20% unless the change records and approves
the reason. Independently enforce hard configured bounds for frame size, input
file size, queued work, diagnostics, completion items, and indexed files.
Boundary tests assert every default and exceeded behavior in the architecture
resource-limit table.

The local legacy reference benchmark lives in the nested
`tools/benchlegacy` module. It uses a temporary Go workspace to compare the
current parser with a local go-vimlparser checkout without adding that project
to production dependencies or requiring network access. The primary ranking
uses only the precomputed common-success legacy corpus; it records excluded
Vim9 files, parser failures, corpus hash, command/expression/comment output,
warmup, sample count, `GOMAXPROCS`, time, bytes, and allocations. A fail-fast
full-corpus timing is never compared directly with loose recovery throughput.

The standing parser-performance corpus is only the official Vim runtime tree
from the pinned `v9.2.1015` checkout. Do not append a personal runtimepath or
plugin collection to routine A/B runs. A personal runtimepath may be used as an
occasional correctness smoke, but it is not a performance gate. Runtime A/B
runs use five samples with the same root, file order, `GOMAXPROCS`, worker count,
and warmup. Retain the complete AST and report median time, bytes, and
allocations separately from discovery and I/O. Unexpected corpus diagnostics
must be classified against Vim and the reference parsers; the benchmark must
not suppress them to improve its success count.

When profiling parser internals, use the nested module's explicitly enabled
`TestProfileVimlsBatch` instead of top-level `-cpuprofile` or `-memprofile`.
The latter include corpus discovery, dialect classification, and the reference
parser. The exact offline command and before/after `pprof` invocation live in
[`tools/benchlegacy/README.md`](../tools/benchlegacy/README.md#isolated-parser-profiles).
Use sampled profiles to attribute costs and `-benchmem` to report exact
bytes/op and allocs/op.

## Default gates after bootstrap

```sh
gofmt -w <changed-go-files>
go test -mod=readonly ./...
go test -mod=readonly -race ./...
go vet -mod=readonly ./...
go test -mod=readonly -coverpkg=./internal/... -coverprofile=coverage.out ./...
go run -mod=readonly ./tools/covercheck -profile coverage.out -min 90
go test -mod=readonly -run TestLSPSubprocess ./test/integration
```

Scheduled/release gates add committed fuzz seeds, bounded fuzzing, benchmarks,
Vim compatibility lanes, `govulncheck`, cross-platform builds, version output,
and a clean stdio handshake.

## Release evidence

A release report records exact Go and Vim versions, commands, test counts,
race/fuzz duration, benchmark runner and deltas, supported capabilities,
remaining limitations, binary hashes, and smoke-tested client configurations.
Do not claim support from compilation alone.
