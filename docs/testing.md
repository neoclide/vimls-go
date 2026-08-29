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
testdata/mixed/                    contextual dialect transitions
testdata/incomplete/               editor typing states and recovery
testdata/unicode/                  byte/rune/UTF-16/line-ending boundaries
testdata/workspace/                import, autoload, runtimepath projects
testdata/fuzz/                     permanent crash and regression corpus
test/integration/                  subprocess JSON-RPC/LSP scenarios
test/vim/                          curated side-effect-free Vim oracle cases
```

Fixture metadata records expected dialect, minimum Vim version/patch, expected
diagnostics or AST snapshot, and upstream provenance when applicable.

The default upstream source checkout for provenance and tagged corpus reads is
`/Users/chemzqm/lib/vim`. Tests and generators must address explicit tags or
commits and must not modify that checkout or depend on its current HEAD.

## Required layers

### Syntax

Lexer tests assert token kind, text, byte span, trivia, and dialect. Parser
goldens assert a normalized AST and diagnostics for positive, negative, mixed,
and incomplete input. Essential ambiguity cases include `|`, quotes versus
comments, command abbreviations, ranges/modifiers, continuations, heredocs,
mapping payloads, `vim9cmd`, `legacy`, `def`, and `function`.

The default offline gate also reads the generated v9.2.1015 corpus below
`testdata/official/`. It contains 3,267 cases extracted from 17 official Vim
test files and feeds every source to both `LegacyParser` and
`Vim9Parser`, asserting retained source plus ordered, in-bounds command, token,
block, and diagnostic spans. Focused source-referenced matrices separately
assert exact AST shapes and recovery diagnostics; the broad corpus is not used
as a substitute for those semantic assertions. `make test-official` compares
the generated corpus with the exact tag in `/Users/chemzqm/lib/vim` without
modifying that checkout or accessing the network.

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
