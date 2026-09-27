# Development, tests and releases

Use Go 1.26 or newer. Ordinary tests use the fixtures in this repository and do
not need an installed Vim.

## Everyday changes

Run focused tests while editing. For example:

```sh
go test -mod=readonly -count=1 ./internal/syntax ./internal/analysis
```

After integrating a Go or behavior change, format the changed files and run the
final checks once:

```sh
gofmt -w path/to/changed.go
go test -mod=readonly -count=1 ./...
go vet -mod=readonly ./...
make
```

`make` builds `bin/vimls` and `bin/vimparse`; `make test` runs the uncached
test suite. Keep `-count=1`: integration tests build a server subprocess, and
Go's test cache does not track every source change that affects it.

The root [`VERSION`](../VERSION) file contains the base version without a leading
`v`. Local `make` builds append `-dev` to that value, even without Git metadata.
This affects `vimls -version` and LSP server info. Direct `go build` without
linker flags retains the source default `dev`.
Release builds embed the matching release tag, including its leading `v`.

`make incr` increases the patch number in `VERSION` and commits only that file
with the message `chore: bump version to X.Y.Z`. Other staged changes remain
staged. The file must contain a single `MAJOR.MINOR.PATCH` value.

For documentation-only changes, check examples, links and `git diff --check`.
Go tests are not needed.

To inspect a syntax tree, pass a regular file to `bin/vimparse file.vim` or pipe
source to stdin:

```sh
printf 'vim9script\necho 1\n' | bin/vimparse -
```

Files and stdin have a 4 MiB input limit. Named device files, directories and
FIFOs are rejected; use `-` for a pipe. Input failures produce a message on stderr
and exit status 1, with no JSON on stdout. Invalid argument counts return 2.
Syntax diagnostics remain part of the JSON syntax tree and do not change the
successful exit status.

## Where to put tests

| Location | Tests |
| --- | --- |
| `internal/<package>/*_test.go` | Focused behavior in the owning package. |
| `testdata/legacy`, `testdata/vim9` | Shared Vim script fixtures. |
| `testdata/official` | Pinned Vim source and extracted cases. |
| `test/integration` | The real server process over stdio and TCP. |
| `test/oracle` | Curated comparisons with a clean, pinned Vim executable. |
| `test/clients` | A real Vim/vim-lsp editing session. |

A bug fix needs a case that fails for the original behavior. Accepted syntax
needs a positive case; invalid or unfinished input needs a negative or recovery
case. Check source ranges as well as messages when the editor uses those ranges.

For document edits, cover ordered changes and character boundaries: bytes,
UTF-16, CRLF, BOM, combining characters and characters outside the BMP.
For cancellation and stale results, use barriers to force the ordering.
Avoid sleeps that merely hope to hit the bug.

Keep server fixtures small. Large parser corpora belong in parser tests and
should not be repeated for every editor feature.

## Comparing behavior with Vim

Use only curated fixtures and the current [pinned Vim baseline](language-support.md).
A newer Vim can accept different syntax, so its result does not automatically
apply to this server.

```sh
VIMRUNTIME=/path/to/vim/runtime \
  make oracle VIM_EXECUTABLE=/path/to/vim/src/vim
VIMRUNTIME=/path/to/vim/runtime \
  make client-smoke VIM_EXECUTABLE=/path/to/vim/src/vim
```

The oracle records the Vim version and patch probes, `v:errors`, `:messages`,
output streams and exit status. It uses a clean process without user
configuration. Ordinary `go test` skips this lane unless `VIM_EXECUTABLE`
is set.

The client smoke test downloads a pinned vim-lsp archive into `.test-tools`,
opens Legacy and Vim9 files, checks diagnostics and indentation, then shuts
down. It does not change your editor configuration.

Official corpus provenance and maintenance rules are in
[testdata/official/README.md](../testdata/official/README.md).
Parsing a large corpus without crashing proves recovery and range handling;
it does not prove that every Vim rule is implemented.

To inspect selected official parser failures:

```sh
go test -mod=readonly ./internal/syntax \
  -run '^TestOfficialVimParserFailureTriage$' -count=1 -v \
  -args -official-case='test_vim9_assign.vim:1623:40054'
```

The filter matches case identifiers in the committed artifact. Use the matching
`TestOfficialVimParserFailures` test while adding cases to the failure matrix.

## Generated metadata

The generators, oracle and client smoke checks use the current
[pinned Vim baseline](language-support.md). `tools/vimsource` is the machine
readable source for CI, Make and Vimscript probes. The Vim and Neovim upstream
checkouts are read-only; their current HEAD may be newer than the pinned object.
Neovim remains independently pinned.

```sh
make metadata-check VIM_SOURCE=/path/to/vim NEOVIM_SOURCE=/path/to/neovim
make official-check VIM_SOURCE=/path/to/vim
VIMRUNTIME=/path/to/vim/runtime \
  make vim-check VIM_SOURCE=/path/to/vim NEOVIM_SOURCE=/path/to/neovim \
    VIM_EXECUTABLE=/path/to/vim/src/vim
```

When deliberately updating generated metadata, use `make metadata-refresh`
with the same source paths and inspect the result. Both metadata targets include
the event documentation generator. Use `make eventdocs-check` or
`make eventdocs-refresh` with those paths to operate on event documentation only.
The CI Vim oracle job checks all generated metadata using both pinned checkouts.
When changing documentation excerpts, update the modification date and
description in [the documentation notice](../LICENSES/VIM-DOC.txt). Keep
original source attribution and license references in regenerated output.
Do not edit generated tables
to hide a mismatch. Official compile-diagnostic fixtures are maintained one
error code at a time in `internal/analysis/official_compile_cases_e*_test.go`.

To update the baseline, follow this single sequence:

1. Record the old pin, then update the active source pin in
   `internal/vimdata/source.go` and independently review the manual Vim source.
2. Copy and review the new tag's parser-file manifest, including its provenance,
   selections and exclusions.
3. Print a candidate `<tag>-corpus-lock.json` with
   `go run ./tools/genofficial -vim-root /path/to/vim -print-lock`; review it
   and save it as the new lock.
4. Run `make metadata-refresh` and `make official-refresh`, then generate a
   parser-assertion report and explicit preview. Set `old_tag` and `new_tag`
   to the actual old and new Vim tags:

   ```sh
   go run ./tools/genofficial \
     -rebase-from "testdata/official/${old_tag}-parser-cases.json.gz" \
     -rebase-to "testdata/official/${new_tag}-parser-cases.json.gz" \
     -rebase-output /tmp/official_parser_cases_preview.go \
     > /tmp/official_parser_cases_rebase.json
   ```

   Fully identical duplicate groups match in artifact order. A
   `-rebase-review OLD_ID=NEW_ID` mapping is valid only when the input source
   identity is unchanged; changed source or diagnostics still require manual
   assertion maintenance.

   For entries whose source changed or disappeared, first make a temporary
   assertion-file copy, remove only those marked entries, and pass it with
   `-rebase-assertions` to migrate the remaining assertions. Merge the changed
   assertions back into the final preview by hand. Delete an assertion only
   after confirming upstream removed its case, then inspect the final diff for
   omissions.
5. Update support, license and roadmap text for actual semantic changes. Do not
   replace historical provenance across the tree. Finish with `make vim-check`.

Normal official generation verifies the reviewed lock before writing the four
artifacts. The lock contains source-derived totals and the typed manifest
SHA-256, not parser diagnostic assertions. `make vim-check` runs formatting, metadata and corpus
checks, the external oracle, the uncached Go suite, vet, and the client smoke
test, so do not rerun those stages manually after it succeeds.

`make official-refresh VIM_SOURCE=/path/to/vim` regenerates the current official
Vim corpus. `make official-check` regenerates it into a temporary directory and
compares the four generated artifacts. The v9.2.1015 corpus files remain
historical archives, and handwritten compile regressions retain their recorded
source provenance unless their own comment names a newer revision. The corpus
checks recovery and range handling; it does not establish full Vim syntax
coverage.

## CI and additional checks

[CI](../.github/workflows/ci.yml) tests Linux, macOS and Windows. Separate jobs
cover race checks, coverage, vulnerabilities and the pinned Vim/client checks.
The current coverage threshold is 90%.

Race and coverage runs are additional checks, not required local commands for
every edit. Run them when requested or when the validation scope calls for them.
The [scheduled workflow](../.github/workflows/scheduled.yml) runs bounded fuzzing
and benchmark comparisons. Retain discovered crashes as regression inputs.

For a performance change, keep the source versions, toolchain, input, worker
count and sampling method comparable. The standing workloads include parsing,
completion, runtime indexing and workspace updates:

```sh
go test -mod=readonly -p 1 ./internal/syntax ./internal/server -run '^$' \
  -bench '^(BenchmarkParseLargeFile|BenchmarkCompletionLatency|BenchmarkRuntimepathIndexing|BenchmarkReverseDependentReanalysis|BenchmarkWorkspaceRebuild)$' \
  -benchmem -benchtime=1s -count=20
```

`tools/benchreport` compares matching workloads with at least 20 samples on each
side. The scheduled lane runs packages serially and measures each sample for
one second. With 20 samples, nearest-rank P95 is the second-slowest sample;
one timing outlier does not set the relative timing gate. Its gates allow at most 15%
growth in median/p95 sample time and 20% growth in median allocations; completion
must stay below 100 ms. Confirm noisy failures before attributing them to a
change. P95 here describes benchmark sample means, not individual requests.

For comparison with go-vimlparser and parser-only profiling, follow
[tools/benchlegacy](../tools/benchlegacy/README.md).

For diagnostic traversal changes, use the focused analysis workloads:

```sh
go test -mod=readonly -p 1 ./internal/analysis -run '^$' \
  -bench '^(BenchmarkDiagnosticAnalysis|BenchmarkTraversalAnalysis|BenchmarkConfigDiagnostics|BenchmarkNestedAssignmentDiagnostics)$' \
  -benchmem -benchtime=1s -count=20
```

These benchmarks parse outside the timed loop. `BenchmarkDiagnosticAnalysis`
covers small files, configuration files, embedded commands, calls and long
expressions. `BenchmarkTraversalAnalysis` covers nested builtin calls, lambda
bodies, null receivers and augroups. `BenchmarkRuntimeAnalysis` accepts a fixed
source file through `VIMLS_BENCH_SOURCE` and skips when that variable is unset:

```sh
VIMLS_BENCH_SOURCE=/path/to/fixed-runtime.vim go test -mod=readonly -p 1 \
  ./internal/analysis -run '^$' -bench '^BenchmarkRuntimeAnalysis$' \
  -benchmem -benchtime=1s -count=20
```

Record the runtime source revision and use identical bytes for both runs. The
two collector benchmarks isolate configuration checks and nested assignment
validation; their improvements do not represent end-to-end
editor latency. Keep `GOMAXPROCS` identical when comparing runs.

## Event documentation

For the merged Vim and Neovim event documentation, regeneration commands and
pinned source revisions are in the [generator guide](../tools/geneventdocs/README.md).
Run `go test -count=1 ./tools/geneventdocs ./internal/vimdata` and compare
regenerated Go with the checked-in metadata after changing the extractor.

## Preparing a release

Update `VERSION`, add its matching `## vX.Y.Z` changelog section, commit the release
changes and validate that exact source. The changelog section must be nonempty
and unique; the packager uses it as the release notes. `make release` checks the
section and rejects a tag that differs from `VERSION` before creating or pushing
a tag. The release packager enforces the same version check in GitHub Actions.
To check it alone (after setting `VERSION` to `0.2.0`):

```sh
go run -mod=readonly ./tools/release -version v0.2.0 -check-changelog
```

You can build release assets locally without publishing. For example, after
choosing version `v0.2.0`:

```sh
release_dir=$(mktemp -d)
go run -mod=readonly ./tools/release -version v0.2.0 \
  -epoch "$(git log -1 --format=%ct)" \
  -output-dir "$release_dir" -notes-output "$release_dir/notes.md"
```

Use a clean source tree and the same Go toolchain when checking reproducibility.
The output includes platform binaries, archives and `checksums.txt`. Keep the
output outside the checkout so it does not make the build appear modified.

Unpack the archive for your machine and check that executable, rather than
letting integration tests rebuild from source:

```sh
VIMLS_TEST_BINARY=/absolute/path/to/unpacked/vimls \
  VIMLS_TEST_VERSION=v0.2.0 go test -mod=readonly -count=1 ./test/integration
```

Record the source commit, toolchain, checks, archive hashes and remaining
limitations. Do not reuse another commit's CI or benchmark results as evidence
for the release.

## Publishing

`make release` creates and pushes a tag, which triggers a **public GitHub
release**. Run it only when publication is authorized and the working tree is
clean. For the example version above:

```sh
make release
```

The tag comes only from the `VERSION` file. The default remote is `origin`;
`RELEASE_REMOTE` can select another remote.
The command pushes only the tag. If that tag already exists locally, it prints
a message and exits successfully before checking the worktree or publishing.

The [release workflow](../.github/workflows/release.yml) runs checks, builds
assets and publishes the matching changelog section. A tag containing a hyphen
is marked as a prerelease.
