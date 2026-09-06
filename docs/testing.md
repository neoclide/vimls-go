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

For documentation-only changes, check examples, links and `git diff --check`.
Go tests are not needed.

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

Use only curated fixtures and **Vim v9.2.1015**. A newer Vim can accept different
syntax, so its result does not automatically apply to this server.

```sh
make oracle VIM_EXECUTABLE=/path/to/vim-v9.2.1015/src/vim
make client-smoke VIM_EXECUTABLE=/path/to/vim-v9.2.1015/src/vim
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
  -args -official-case='4170:120710,4171:120781'
```

The filter matches case identifiers in the committed artifact. Use the matching
`TestOfficialVimParserFailures` test while adding cases to the failure matrix.

## Generated metadata

The generator reads the pinned Vim source. Use an upstream checkout read-only;
its current HEAD need not be the pinned tag.

```sh
make metadata-check VIM_SOURCE=/path/to/vim
```

When deliberately updating generated metadata, use `make metadata-refresh`
with the same source path and inspect the result. Do not edit generated tables
to hide a mismatch. Official compile-diagnostic fixtures are maintained one
error code at a time in `internal/analysis/official_compile_cases_e*_test.go`.

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

## Preparing a release

Choose a new version, add its `## vX.Y.Z` changelog section, commit the release
changes and validate that exact source. The changelog section must be nonempty
and unique; the packager uses it as the release notes.

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
make release VERSION=0.2.0
```

The default remote is `origin`; `RELEASE_REMOTE` can select another remote.
The command pushes only the tag. It can retry an existing annotated tag at
HEAD, but will not move an existing tag.

The [release workflow](../.github/workflows/release.yml) runs checks, builds
assets and publishes the matching changelog section. A tag containing a hyphen
is marked as a prerelease.
