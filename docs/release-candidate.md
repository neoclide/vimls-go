# Release-candidate evidence

## Current v0.1.0 acceptance — 2026-09-05

The first release target is **v0.1.0**. The local archive validation label is
`v0.1.0-rc.1`; neither is a published release or tag. Production source is
`4fb39e4365b8fe7e710f7582e4944237644f788f`, with an uncommitted integration-test
repair and release-documentation updates. This is working-tree acceptance,
not evidence for a clean final release commit. The previous `549c5b7` record
below is historical and must not be used to certify the current candidate.

### CI repair and remaining remote gates

[Run 33973871423](https://github.com/neoclide/vimls-go/actions/runs/33973871423)
tested `4fb39e4` and failed. Linux/macOS tests and coverage encountered the
obsolete single-document option-hover assertion. Windows encountered
`ContentModified` because workspace-symbol readiness did not imply that the
separate runtimepath indexing phase had completed. Vulnerability and Vim
oracle/client jobs passed on that source.

The local repair asserts the two option-hover documents and the full
`nonumber` range, and awaits the runtimepath request before index-backed
hierarchy queries. It preserves the server's stale-snapshot rejection.
`TestLSPSubprocess` passed **20 uncached repetitions**. Final validation now
explicitly disables Go test-result caching: integration tests dynamically build
the server, whose changes are not all tracked by the test cache.

Native Windows plus Linux/macOS CI, including race and the 90% coverage gate,
remain pending for the repaired commit. Local race/coverage were not rerun.
No push, workflow dispatch, tag or publication was performed. An older green
CI run cannot close these gates.

### Current local checks

Host toolchain: `go1.27.0 darwin/amd64`. CI uses Go 1.26.x; local results do not
substitute for that toolchain/platform matrix. Oracle: `/usr/local/bin/vim`,
Vim 9.2 patches 1–1015, with patch 1016 absent.

| Check | Result |
| --- | --- |
| `go test -mod=readonly -count=1 -json ./...` | Pass: 6,106 passing test/subtest events across 18 tested packages, zero failures. The optional parser triage is skipped; oracle tests skipped here are run separately below. |
| `go test -mod=readonly ./test/integration -run '^TestLSPSubprocess$' -count=20` | Pass |
| `make format-check vet build` | Pass |
| `gopls check test/integration/lsp_subprocess_test.go` | Pass |
| `make oracle VIM_EXECUTABLE=/usr/local/bin/vim` | Pass |
| `make client-smoke VIM_EXECUTABLE=/usr/local/bin/vim` | Pass: both dialects receive the expected diagnostic, format, shut down and exit with `v:errors=[]`. |

Raw current output is in ignored `.test-tools/rc-0.1-refresh/`: `tests.json`,
`build-vet.txt`, `gopls.txt`, `oracle.txt` and `client-smoke.txt`. These summaries
remain useful when local logs are unavailable. Test/subtest events are not a
count of independent top-level tests.

### Current performance comparison

Compared historical candidate `549c5b7` with the current working tree on the
same host/toolchain, using `GOMAXPROCS=4`, `-benchtime=10x`, `-count=5` and the
existing ParseLargeFile, CompletionLatency, RuntimepathIndexing,
ReverseDependentReanalysis and WorkspaceRebuild benchmarks. The unchanged
benchreport budgets are 15% median/p95 time, 20% bytes/allocations and 100 ms
completion latency.

The first pair exceeded time budgets for completion and indexing/reanalysis.
A second pair in reverse execution order passed all budgets without changing
source, workload, baseline or thresholds. This is a noisy local result, not a
claim that the first regression report passed. Both rounds are retained as
`benchmark-{baseline,current,report}.txt` and
`benchmark-{baseline,current,report}-confirm.txt` in the current output folder.

| Workload | Confirmation median | Confirmation p95 |
| --- | ---: | ---: |
| Parse legacy 100 KiB / 1 MiB | 1.43 / 35.27 ms | 1.54 / 36.27 ms |
| Parse Vim9 100 KiB / 1 MiB | 1.99 / 22.06 ms | 2.16 / 22.49 ms |
| Completion 1 KiB / 100 KiB | 0.181 / 0.180 ms | 0.199 / 0.187 ms |
| Workspace rebuild | 33.00 ms | 33.69 ms |
| Runtimepath indexing | 81.64 ms | 84.87 ms |
| Reverse-dependent reanalysis | 37.50 ms | 37.71 ms |

These indexing benchmarks do not measure runtime-help extraction separately.
The historical fuzz runs below have not been refreshed for current source.

### Current archives and clean-install acceptance

Built twice with separate `build-one` / `build-two` output directories under
`.test-tools/rc-0.1-refresh/`:

```sh
GOMAXPROCS=4 go run -mod=readonly ./tools/release \
  -version v0.1.0-rc.1 -epoch 1788620670 \
  -output-dir .test-tools/rc-0.1-refresh/build-one
```

All **16 assets** (eight executables and eight archives) are byte-identical
between builds and match their SHA-256 manifest. All eight archives contain
exactly the expected executable, README, current 0.1 CHANGELOG, support
contract and two license files; packaged documents match the working tree.

- Checksum manifest SHA-256:
  `720b009746e64b97bd53b58d71f4bb211faa5c737e5a20dde27946c3f9b699b8`.
- Unpacked Darwin/amd64 executable SHA-256:
  `ef355432af413b956af1257c2d985fe266f3166ca84d95c7a960a1606ecdb766`.
- Integration-test file SHA-256 identifying the local repair:
  `2995b192d2dc641bdaf7d77e620a8e31d3e1e5c5c81bb4ac47bc7a783a59e8ba`.

The unpacked host executable reports `vimls v0.1.0-rc.1`. Build metadata records
Go 1.27.0, `CGO_ENABLED=0`, revision `4fb39e4` and **`vcs.modified=true`**.
These are reproducible local validation artifacts, not final release assets.
A clean final commit still needs its own build and acceptance.

The unpacked binary passed all **11 integration tests** using
`VIMLS_TEST_BINARY` and `VIMLS_TEST_VERSION=v0.1.0-rc.1`, then passed the pinned
Vim/vim-lsp smoke with `VIMLS_BINARY` pointing to it. Both dialects produced the
expected diagnostic and indentation edit; shutdown responded, the process
exited, and `v:errors=[]`. Other architectures were cross-built, not executed.
Logs: `archive-validation.txt`, `archive-integration.json`, `archive-client.txt`.

## Historical acceptance — source 549c5b7

Everything below, including its former 1.0 candidate label, hashes and gate
status, describes the earlier source only. It is retained for provenance.

Date: 2026-09-05. Candidate label: `v1.0.0-rc.1` (local validation only;
not a tag or published release). Baseline: `a0381ea`.
Final candidate source: `549c5b7bbed567a7369c78d73f0eaa7f682f691d`.
The subsequent evidence-only commit is not the archive source SHA.

### Scope and remaining gates

The release contract is [language-support.md](language-support.md), with
parser and semantic evidence kept separate in [syntax-coverage.md](syntax-coverage.md).
Explicit deferred features remain deferred. P0–P3 local gates pass; M7 is not
closed until the exact candidate passes native Windows and CI race/coverage.
No remote write, tag, or publication is authorized by this work.

### Local environment and commands

- Host: Darwin 25.5.0, x86_64; Go `go1.27.0 darwin/amd64`.
- Oracle: `/usr/local/bin/vim`, Vim 9.2, patches 1–1015; patch 1016 absent.
- Client: vim-lsp `e10d186452743beb7b43d2b3427020832f930c2b`.
- `go test -json -count=1 ./...`, `go vet ./...`, `make`: pass.
  The final JSON run records 6,034 passing test/subtest events across 17 tested
  packages, three explicit skips, and zero failures. These are events, not
  6,034 independent top-level tests. The two oracle skips are run separately
  below; the third is the opt-in official parser failure-triage report.
- `VIM_EXECUTABLE=/usr/local/bin/vim go test -v -count=1 ./test/oracle`: pass.
- `make client-smoke VIM_EXECUTABLE=/usr/local/bin/vim`: pass; both dialects
  observe their expected diagnostic, apply indentation, shut down, and report
  `v:errors=[]`.
- Raw local output: ignored `.test-tools/rc-2026-09-05/` (`tests-final.json`,
  `oracle-final.txt`, `client-smoke.txt`). This document retains conclusions even
  when local artifacts are unavailable.

### Required LSP evidence

Initialization capability shapes are tested in
`internal/server/initialize_capability_matrix_test.go`; the implementation is
`Server.Initialize` in `internal/server/server.go`. Advertised capabilities
were matched against the following executable scenarios, not parser tests.

| Required surface | Evidence |
| --- | --- |
| Initialize/shutdown/exit, completion/resolve, hover/signatures, navigation, symbols, folding/selection, safe rename, semantic tokens/delta, inlays, links, code actions, hierarchy/implementations, Code Lens/resolve, formatting | `TestLSPSubprocess`, `test/integration/lsp_subprocess_test.go` |
| Incremental sync, open-disk overlay, exact Unicode rename/navigation ranges | `TestStdioSharedScenario` and `TestTCPSubprocess`; UTF-16 astral/combining input followed by a ranged edit |
| Document/workspace pull, result IDs, closed-file diagnostics | `TestDocumentPullDiagnosticsSubprocess` |
| Negotiated multi-range formatting | `TestRangesFormattingSubprocess` |
| Cancellation and lifecycle order | `TestServerCancelsInFlightRequest`, `TestCancelledRequestMatrix`, `TestServerShutdownWaitsForBackgroundWork` in the server package |
| Stale diagnostics and safe edits | `TestGraphRevisionRejectsStaleDiagnostics`, `TestRenameRejectsStaleClosedFile`, `TestRenameOverlayScanKeepsCapturedSnapshot`, `TestFormattingRejectsInvalidParamsAndStaleSnapshot` |

Concurrency branches use package hooks/barriers. Subprocess timing alone is
not evidence that a stale branch was reached. Unpacked archives can now use
the same integration suite via `VIMLS_TEST_BINARY` and `VIMLS_TEST_VERSION`.

### Remote evidence

[Baseline CI run 33961022065](https://github.com/neoclide/vimls-go/actions/runs/33961022065)
is for **a0381ea**, not this candidate. Linux/macOS test+race+vet+build,
coverage, vulnerability and Vim oracle/client jobs passed. Windows failed on
URI/path casing and a diagnosticscan expected-path assertion; both are fixed
locally in `d473ee4`. Native Windows and all candidate-SHA CI results remain
pending. Local race/coverage were intentionally not run; historical green
jobs cannot close the new candidate gates.

### P3 progress

Adversarial review tightened legacy tuple mutation evidence to plain adjacent
`let ... =` assignments with static literal contents. In pinned Vim, tuple
`+=` raises E734; `silent!` can ignore that failure and retain a mutable nested
list. The analyzer must not infer the attempted RHS as a successfully assigned
tuple. A direct negative analysis test and the pinned oracle cover this case.
Full tests, vet, build and the complete oracle pass after the guard refinement.

`govulncheck@v1.1.4 ./...` reports **No vulnerabilities found** on the local Go
1.27 toolchain. CHANGELOG and packaged support/license documents are added;
the release workflow now marks hyphenated prerelease tags as prereleases.
No workflow was triggered. Full tests, vet and build pass for these changes.

#### Bounded fuzzing

Eight targets passed with `-run '^$' -fuzz '^TARGET$' -fuzztime 30s -parallel 4`.
No crash input was found. Framing/text/parser implementations did not change
after these runs. Completion-context fuzzing was repeated on final source after
the last analysis guard refinement; this is not a substitute for semantic tests.

| Package / target | Executions |
| --- | ---: |
| jsonrpc / FuzzReader | 635,228 |
| text / FuzzPositionRoundTrip | 3,078,581 |
| text / FuzzApplyChanges | 442,081 |
| syntax / FuzzFileParsersNeverPanic | 304,587 |
| syntax / FuzzLegacyExpressionNeverPanics | 714,917 |
| syntax / FuzzVim9ExpressionNeverPanics | 742,601 |
| syntax / FuzzVim9TypeNeverPanics | 875,702 |
| server / FuzzCompletionContext | 264,917; final rerun 334,302 |

#### Performance

Runner: the host above, Intel i7-9750H @ 2.60GHz, `GOMAXPROCS=4`, Go 1.27.0.
Baseline `a0381ea` and candidate `549c5b7` use the unchanged fixed workloads from
[testing.md](testing.md): `-benchmem -benchtime=10x -count=5`. Other local
validation jobs were stopped before measurement. Each round records five
samples per workload on each side; p95 is a percentile of sample means, not
individual request latency.

The first baseline-then-candidate round failed the 15% time gate for completion
1 KiB median (172.33 -> 205.17 us) and 100 KiB p95 (205.62 -> 266.37 us).
All other metrics passed. A second full paired round, candidate then baseline,
passed the unchanged gate for every workload. No baseline, workload, threshold,
or allocation budget was reset; no stable regression was confirmed. Both raw
rounds and reports are retained as `benchmark-{baseline,current,report}.txt`
and `benchmark-{baseline,current,report}-confirm.txt`.

Confirmation results:

| Workload | Candidate median | Candidate p95 | Median delta | p95 delta |
| --- | ---: | ---: | ---: | ---: |
| Parse legacy 100 KiB | 1.388 ms | 1.809 ms | -0.07% | +1.80% |
| Parse legacy 1 MiB | 37.028 ms | 38.527 ms | +2.49% | +4.03% |
| Parse Vim9 100 KiB | 2.005 ms | 2.082 ms | -2.97% | -3.84% |
| Parse Vim9 1 MiB | 23.236 ms | 24.316 ms | +0.78% | -0.24% |
| Completion 1 KiB | 0.182 ms | 0.230 ms | +3.08% | +2.81% |
| Completion 100 KiB | 0.177 ms | 0.229 ms | -0.02% | +9.08% |
| Workspace rebuild | 34.485 ms | 35.043 ms | +1.57% | -0.57% |
| Runtimepath indexing | 86.470 ms | 87.905 ms | -1.87% | -8.36% |
| Reverse-dependent reanalysis | 40.433 ms | 42.189 ms | -2.97% | -5.54% |

All confirmed allocation/byte changes are below 20%; the largest allocation
increase is reverse reanalysis, 54,124 -> 55,085 (+1.78%). Completion stays well
below 100 ms. These results are specific to this recorded host/toolchain, not
a promise about all clients or a replacement for candidate CI.

#### Reproducible archives and clean-install acceptance

From the clean final source, ran twice with separate ignored output directories:

```sh
GOMAXPROCS=4 go run -mod=readonly ./tools/release \
  -version v1.0.0-rc.1 -epoch 1788606487 \
  -output-dir .test-tools/rc-2026-09-05/final-one
# Repeat the identical command with final-two.
```

Both builds produced byte-identical checksums for **all 16 assets**: eight raw
executables and eight archives. `shasum -a 256 -c checksums.txt` passed for all
assets. All eight archives have exactly the expected executable, CHANGELOG,
README, support contract and two license files; every packaged document was
compared byte-for-byte with the source. Archive mode/timestamp determinism is
also tested by the packager tests. Preliminary `build-one`/`build-two` artifacts
are superseded by `final-one`/`final-two` and are not the candidate.

| Archive suffix (prefix `vimls-v1.0.0-rc.1-`) | SHA-256 |
| --- | --- |
| darwin-amd64.tar.gz | `92ce854e93bf589bb8e30b563c86787744660f28c6b140de285a17cddd074e3e` |
| darwin-arm64.tar.gz | `b62f06bafbb25068340756aa4859cec0574a1c7dc21198415c04ac773f2bbeb8` |
| freebsd-amd64.tar.gz | `889ff72c299ed280cd83a8a5b342b1cb969afc08e694e71ddfe13b7e2d907fc9` |
| linux-amd64.tar.gz | `a3034279a294cf6720f8b12e2d8eabc28d5e9d57783be2de60c9f30488ae0352` |
| linux-arm64.tar.gz | `0deea9a7f38eeba111750bb4fb73f385557f9fc1692515b11490c884d47a335e` |
| linux-armv7.tar.gz | `9caebec465a4fe847f63a693f6a87a372f67930f6445de81b85b32ada9cd25cc` |
| windows-amd64.zip | `b364e2722d6d2da04cc3631e6aeed8ff73411421da4f1e9c5e1f477cac4dcb18` |
| windows-arm64.zip | `f13eb1de189aa1d82757a051f61b21cd188e2e29472386f4018aa66b0a602f9b` |

The complete 16-entry checksum manifest hashes to
`df028a9c87ba3475fbc5b9649655e8b2ec1d6049027f928552337f80b1eb5c88`.
The unpacked host executable hashes to
`51bd711cebf614a61138363e12444290d84ee00014c2a7310053fcc5c03b0614`.
`go version -m` confirms Go 1.27.0, `CGO_ENABLED=0`, the final source revision,
and `vcs.modified=false`; `--version` prints `vimls v1.0.0-rc.1`.

The unpacked host binary passed **all 11 integration tests** using the documented
`VIMLS_TEST_BINARY`/`VIMLS_TEST_VERSION` override, with zero failures. The pinned
Vim/vim-lsp smoke was run again with `VIMLS_BINARY` set to that unpacked binary:
both expected diagnostics, both indentation edits, shutdown response, exited
status and `v:errors=[]` passed. Raw records are `archive-integration.json` and
`archive-client.txt`. Other architectures were cross-built, not executed.

Windows/amd64 server, diagnosticscan and integration test binaries also compile
with `GOOS=windows GOARCH=amd64 go test -c`. This does **not** close the native
Windows execution gate. No tag, push, release, or remote workflow dispatch was
performed. Finishing M7 requires separate authorization to deliver the candidate
for native/current-SHA CI, then recording those results.
