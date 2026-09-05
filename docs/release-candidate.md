# Release-candidate evidence

Date: 2026-09-05. Candidate label: `v1.0.0-rc.1` (local validation only;
not a tag or published release). Baseline: `a0381ea`.

## Scope and remaining gates

The release contract is [language-support.md](language-support.md), with
parser and semantic evidence kept separate in [syntax-coverage.md](syntax-coverage.md).
Explicit deferred features remain deferred. P0–P2 local tests pass; M7 is not
closed until the exact candidate passes native Windows and CI race/coverage.
No remote write, tag, or publication is authorized by this work.

## Local environment and commands

- Host: Darwin 25.5.0, x86_64; Go `go1.27.0 darwin/amd64`.
- Oracle: `/usr/local/bin/vim`, Vim 9.2, patches 1–1015; patch 1016 absent.
- Client: vim-lsp `e10d186452743beb7b43d2b3427020832f930c2b`.
- `go test -json -count=1 ./...`, `go vet ./...`, `make`: pass.
  The JSON run records 6,031 passing test/subtest events across 17 tested
  packages, three explicit skips, and zero failures. These are events, not
  6,031 independent top-level tests. The oracle skips are run separately below.
- `VIM_EXECUTABLE=/usr/local/bin/vim go test -v -count=1 ./test/oracle`: pass.
- `make client-smoke VIM_EXECUTABLE=/usr/local/bin/vim`: pass; both dialects
  observe their expected diagnostic, apply indentation, shut down, and report
  `v:errors=[]`.
- Raw local output: ignored `.test-tools/rc-2026-09-05/` (`tests.json`,
  `oracle.txt`, `client-smoke.txt`). This document retains conclusions even
  when local artifacts are unavailable.

## Required LSP evidence

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

## Remote evidence

[Baseline CI run 33961022065](https://github.com/neoclide/vimls-go/actions/runs/33961022065)
is for **a0381ea**, not this candidate. Linux/macOS test+race+vet+build,
coverage, vulnerability and Vim oracle/client jobs passed. Windows failed on
URI/path casing and a diagnosticscan expected-path assertion; both are fixed
locally in `d473ee4`. Native Windows and all candidate-SHA CI results remain
pending. Local race/coverage were intentionally not run; historical green
jobs cannot close the new candidate gates.

## P3 progress

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

Bounded fuzzing, baseline-comparable performance, twice-built reproducible
archives and unpacked-binary/client smoke are in progress. Their results will
be recorded here before handoff.
