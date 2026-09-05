# Test and release strategy

## Principles

- Test public behavior and failure semantics, not private implementation shape.
- Every range assertion includes the source text and checks byte-to-LSP position
  conversion separately.
- Keep parser goldens deterministic. CI never rewrites expected output.
- Green narrow tests do not prove a milestone whose contract is broader.
- A Vim executable is a development oracle, never a production dependency.
- Server tests use only the smallest source needed for one feature behavior.
  Parser corpora stay in the owning syntax package
  and are never replayed across every LSP feature. Synchronize asynchronous
  server tests with hooks or channels instead of production-duration sleeps.
  Keep the cumulative top-level test time attributed to each `_test.go` file
  below five seconds in an uncached `go test -json -count=1` run.
- Normal build and test commands use Go's global module cache with
  `-mod=readonly`. With the required versions cached they do not fetch modules
  from the network.

## Test layout

```text
internal/<package>/*_test.go       focused unit and package integration tests
testdata/legacy/                   accepted and rejected legacy scripts
testdata/vim9/                     accepted and rejected Vim9 scripts
testdata/official/                 pinned generated Vim corpus and metadata
test/integration/                  subprocess JSON-RPC/LSP scenarios
test/clients/                      clean real-Vim/vim-lsp smoke scenario
test/oracle/                       curated behavior checks against exact Vim
```

`TestLSPSubprocess` runs one clean stdio session containing both a legacy
runtime plugin (global function, user command and autoload function) and a Vim9
import/class workspace. It covers initialization and shutdown, document sync,
published diagnostics, completion and resolve, hover, signature help,
navigation, symbols, rename, semantic tokens and syntax-backed code actions.
The legacy runtime assertions also prove that indexed signatures and leading
comments reach completion/hover and that autoload definitions retain their
runtimepath source location.

Mixed-dialect, incomplete-input, Unicode, workspace, and fuzz regression cases
currently live beside their owning Go packages. Dedicated shared fixture and
Vim-oracle directories should be introduced only when real cross-package cases
need them.

Fixture metadata records expected dialect, minimum Vim version/patch, expected
diagnostics or AST snapshot, and upstream provenance when applicable.

The default upstream source checkout for research is `/Users/chemzqm/lib/vim`
and must be used read-only.
Ordinary repository tests use repository-owned fixtures and artifacts and do not
require any external Vim checkout.

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
tests. The full-file corpus exercises automatic dialect selection once per
file; the smaller embedded corpus exercises both `LegacyParser` and
`Vim9Parser`. Both assert retained source plus ordered, in-bounds command,
token, block, and diagnostic spans without sourcing or executing the files. A
separate helper inventory
classifies all 5,733 `Check*` candidates, including all 5,208 qualified
`v9.Check*` calls, so each call can later become a generated conformance case or
an explicit skip. `v9.2.1015-parser-files.json` is the fixed migration boundary:
44 syntax-relevant files are included, 24 ambiguous helper-bearing files have
explicit exclusions, and the remaining 294 files inherit the default exclusion.
The pinned parser-case snapshot covers only the 44 included files; changing the
pinned Vim version or this reviewed manifest is the only reason to revisit
excluded files.
The generated parser-case artifact hashes that manifest and accounts for every
one of the 3,844 selected helper calls. Static source recovery extracts 3,805
calls into 5,261 dialect-aware variants and records an explicit reason for the
remaining 39 skips. The 1,761 success variants are parser-positive assertions.
The artifact keeps its 3,500 failure variants unclassified and retains their Vim
error arguments as provenance, so parser progress does not rewrite the pinned
generated artifact.

Full-file parsing proves stability, recovery, and span integrity, not exact Vim
acceptance. The conformance layer must use generated helper expectations and
focused source-referenced matrices to assert accepted or rejected syntax,
error provenance, AST shape, and recovery diagnostics. Every official helper
candidate must be extracted or retained in a classified skip manifest; the
broad corpus is not a substitute for those conformance assertions.

#### Official parser failures

The compressed parser-case artifact remains the source of every exact parser
case key and input. No separate phase-classification or migration-status ledger
is maintained.

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
entire committed matrix. Add accepted cases directly to that matrix.

### Semantics

The range-partitioned `internal/analysis/official_compile_cases_e*_test.go`
files supply readable, self-contained official `def` and `vim9script` failure
sources without starting Vim. Each error code has one owning test block, and
every case preserves its official source location in an adjacent comment and
source-position ID alongside the exact error code and Vim source.

Each error code retains at most ten deterministic cases, balanced between
`def` and script/legacy contexts when both exist. Cases that require runtime
state, build features, external files, jobs, terminals, or definitions from
Vim's test harness are not run; cover the same statically decidable rule with a
focused Go fixture instead. Official cases supplement, rather than replace,
direct tests for syntax forms, recovery, diagnostic code, message, and
in-bounds source span.

The compile fixtures are not batch-regenerated. When upgrading Vim, migrate a
supported error code directly into its owning range test, preserving the
upstream source location and exact code.

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
and workspace-folder changes. Deterministic channel barriers force one cache
miss versus change, one close restore versus reopen, and one workspace rebuild
versus open edit; these tests prove the intended stale branch instead of relying
on repeated scheduler luck.

### Fuzz and properties

Current fuzz targets cover framing, position round trips, ordered incremental
edit application, complete-file parser recovery, expressions, and Vim9 types.
All targets bound source size before invoking production code.
`FuzzApplyChanges` also bounds edit count and replacement size and uses a
test-local position oracle so every
accepted ranged edit is compared with direct full-text replacement. Its seed
corpus covers LF, CRLF, BOM without a final newline, combining characters,
astral characters, and invalid UTF-8. The complete-file parser target checks
ordered, in-bounds token, AST, block and diagnostic spans for both dialects. A
five-step deterministic sequence covers
distinct BOM, combining, astral, CRLF, and EOF edit invariants. A dedicated
lexer fuzz target remains planned. Required properties:

- No panic, deadlock, unbounded loop, or uncontrolled allocation.
- Token and AST spans remain ordered and within the source.
- Recovery makes forward progress.
- Applying valid incremental edits matches full-text replacement.
- Position round trips are stable at valid character boundaries.

PR checks replay the committed seeds and `testdata/fuzz` corpus. A future
scheduled lane will run bounded live fuzzing; every discovered input must remain
under the owning package's `testdata/fuzz` directory as a regression case.

### Vim oracle

Run only curated scripts with a clean supported Vim, no user vimrc/gvimrc, no
swap or history file, and a task-specific temporary working directory. The base
invocation is `vim -Nu NONE -U NONE -n -es -X -i NONE -S <fixture>`. Capture
`v:version`, patch information, `v:errors`, `:messages`, and process status; Ex
silent mode can hide errors if only the exit code is checked.

The oracle checks acceptance/rejection and focused semantics. It does not source
untrusted workspace files and it does not justify reproducing Vim's runtime
inside the server.

`test/oracle` contains one curated legacy fixture, one curated Vim9 fixture, and
the readable `internal/vimdata/options_set_generated.vim` fixture. The metadata
generator emits one `:set` command for every pinned option; the oracle executes
all of them in one clean process and reports failures with their `optiondefs.h`
source lines. It also verifies each `getcompletion('set option=', 'cmdline')`
result is an in-order subset of the generated static `CompletionValues`: the
metadata retains every source-defined candidate, while a Vim build can omit
candidates behind optional compile features.
The curated `option_validation.vim` fixture checks one representative of each
migrated callback rule shape (exact value, comma list, flag list, and numeric
range), including Vim's native E474, E487, and E539 failures through `:set`,
Legacy option assignment, and Vim9 option assignment. Focused Go analysis tests
cover the corresponding diagnostics and ensure dynamic or not-yet-decoded
values remain unchecked.
Unavailable options are collected with `exists('+option')`; their ignored
`:set` commands are not mistaken for evidence that the current build supports them.
Its Go harness generates the clean driver in a test-owned temporary directory,
requires exactly patch v9.2.1015, and records `v:version`, the current and next
patch probes, `v:errors`, `:messages`, stdout, stderr and exit status for every
fixture. The same lane compares Legacy and Vim9 owned-line indentation from the
Go planner with Vim's `=` result. Run it against a pinned source build with:

```sh
make oracle VIM_EXECUTABLE=/path/to/vim-v9.2.1015/src/vim
```

The dedicated Ubuntu CI job checks out tag `v9.2.1015`, builds that source and
runs the same target. Ordinary offline `go test ./...` skips execution when
`VIM_EXECUTABLE` is absent.

### Real Vim client

`make client-smoke` downloads the archive for vim-lsp commit
`e10d186452743beb7b43d2b3427020832f930c2b`, verifies SHA-256
`32950ab381a9244e8e795ea60e8479631e438f91a788c19b39f10e5d54b32257`, and
extracts it below the ignored `.test-tools` directory. It does not use a Git
checkout or read user configuration.

The clean Vim fixture initializes the built vimls process, opens one incomplete
legacy file and one incomplete Vim9 file, waits for an error diagnostic from
each, applies document Formatting to both buffers, then completes the LSP
shutdown/exit sequence. It also exercises a server-to-client
`workspace/configuration` request, matching vim-lsp's default capabilities. Run
the exact pinned lane with:

```sh
make client-smoke VIM_EXECUTABLE=/path/to/vim-v9.2.1015/src/vim
```

CI builds official Vim tag `v9.2.1015` once and uses that executable for both
the oracle and client smoke targets.

## Compatibility matrix

Per change:

- Supported Go versions from `go.mod` on Linux.
- Vim v9.2.1015 for relevant oracle cases.
- Build on Linux and macOS.

Current behavior-sensitive coverage:

- A pinned clean Vim v9.2.1015 oracle lane.
- Vim v9.2.1015 with pinned vim-lsp commit
  `e10d186452743beb7b43d2b3427020832f930c2b` for legacy and Vim9 diagnostics
  plus clean shutdown.
- Linux and macOS run the formatting, test, race, vet and build gates.
- Windows builds all packages and runs the compiled stdio integration scenario.
- Linux runs `govulncheck` v1.1.4 against all packages.

Scheduled and release coverage:

- Bounded live-fuzz and benchmark-regression lanes via `.github/workflows/scheduled.yml` (daily schedule and workflow_dispatch), running 30s per fuzz target with crash artifacts, and executing fixed benchmark suites with 5 raw samples plus median/p95 summary via `tools/benchreport`.

Each Vim lane first proves its actual version and required `+eval`/Vim9 support.

Configuration and workspace identity changes have direct stale-result tests.
`TestGraphRevisionRejectsStaleDiagnostics` covers in-flight document and
configuration revisions plus index and import-graph replacement;
`TestServerCloseReopenRejectsPausedRestore` covers a close/reopen while disk
restoration is paused. These tests must remain deterministic without timing
sleeps.

## Performance budgets

Current benchmarks cover parser hot paths, command lookup, the optional legacy
reference comparison, 64 KiB content-ID construction, parser-cache hits and
changed-file full parses. The standing end-to-end workloads are:

| Benchmark | Fixed workload | Limit |
| --- | --- | --- |
| `BenchmarkParseLargeFile` | Legacy and Vim9 files at 100 KiB and 1 MiB | Confirmed time regression at most 15%; allocation regression at most 20% |
| `BenchmarkCompletionLatency` | Complete cached LSP requests in 1 KiB and 100 KiB Vim9 files | Must remain below the 100 ms completion budget; the same regression limits apply |
| `BenchmarkRuntimepathIndexing` | Two runtime roots containing 256 Vim files | Same regression limits |
| `BenchmarkReverseDependentReanalysis` | One changed leaf with 31 transitive open dependents | Same regression limits |
| `BenchmarkWorkspaceRebuild` | 32 files containing 64 functions each | Same regression limits |

Run those fixed workloads on the pinned runner with:

```sh
go test -mod=readonly ./internal/syntax ./internal/server -run '^$' \
  -bench '^(BenchmarkParseLargeFile|BenchmarkCompletionLatency|BenchmarkRuntimepathIndexing|BenchmarkReverseDependentReanalysis|BenchmarkWorkspaceRebuild)$' \
  -benchmem -benchtime=10x -count=5
```

The incremental-edit baselines continue to use
`-benchmem -benchtime=100x -count=1`; `B/op` and `allocs/op` measure allocation
pressure per operation, not retained heap or peak RSS. Add new sizes or
workloads only when a measured production case is not represented.

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

Scheduled lanes add bounded live fuzzing and benchmark regression checks via
`.github/workflows/scheduled.yml`.

The benchmark lane measures `HEAD^` and `HEAD` on the same runner/toolchain,
retains both raw outputs and commit IDs, then runs
`go run ./tools/benchreport -input current.txt -baseline baseline.txt`.
The gate requires matching workloads and at least five samples per workload;
it fails for time median/p95 growth above 15%, median bytes/allocations growth
above 20%, or completion time at/above 100 ms. Empty/malformed input is not a
successful report. Workload additions/removals require explicit baseline review.
P95 here is the nearest-rank percentile of benchmark sample means, not a
per-request latency distribution. This parent-commit CI check detects local
regressions; it does not replace a pinned release-runner comparison against an
approved release baseline, and noisy failures need confirmation before attributing
them to code. No automatic waiver or baseline update is performed.

## Release evidence

Pushing a `v*` tag runs `tools/release`, which builds CGO-free amd64 and arm64
binaries for Linux, macOS and Windows with `-trimpath`, an empty Go build ID and
the tag injected into `vimls --version`. Tar and zip entries use the commit
timestamp, stable paths and modes; `checksums.txt` lists the SHA-256 of every
archive. Generate the same assets locally with:

```sh
go run ./tools/release -version vX.Y.Z -epoch "$(git log -1 --format=%ct)"
```

A release report records exact Go and Vim versions, commands, test counts,
race/fuzz duration, benchmark runner and deltas, supported capabilities,
remaining limitations, binary hashes, and smoke-tested client configurations.
Do not claim support from compilation alone.
