# vimls-go

vimls-go is a Go language server for legacy Vim script and Vim9 script. Its
grammar and metadata currently support Vim syntax through v9.2.1015. Earlier
syntax remains supported; syntax introduced after that tag is not yet covered.

The repository is being built milestone by milestone from the contracts in
`docs/`. The server currently provides bounded stdio and TCP transports,
lifecycle and target-version handling, immutable document snapshots,
incremental synchronization, background parsing, document symbols, syntax and
target-version diagnostics, and conservative Vim9 immutable-assignment
diagnostics. Position conversion supports UTF-8, UTF-16, and UTF-32; UTF-16 is
the default when the client does not negotiate another encoding.

## Build

Go 1.26 or newer is required.

```sh
make build
./bin/vimls --version
```

The build also creates two independent parser debugging tools.
`bin/vimparse [file]` always parses legacy Vim script and
`bin/vim9parse [file]` always parses Vim9 script. With no file, or with `-`,
they read stdin. Both write a JSON syntax tree and bypass the file-level
`vim9script` dispatcher.

Dependencies use Go's global module cache (`go env GOMODCACHE`), so the project
does not keep a `vendor/` directory. Prime the cache after intentionally
changing dependency versions with:

```sh
go mod download
```

Once the required versions are cached, prove that the local dependency set is
sufficient by running the complete gate with module downloads disabled:

```sh
GOPROXY=off GOSUMDB=off make check
```

Normal builds use `-mod=readonly`; `go.mod` and `go.sum` remain the reproducible
dependency contract. Network access is only needed when a required module is
not already present in the global cache.

The server communicates over stdin/stdout. Logs use stderr so stdout remains a
valid LSP byte stream.

## Install

Build and install the server directly from a tagged release with:

```sh
go install github.com/neoclide/vimls-go/cmd/vimls@vX.Y.Z
```

Alternatively, download the archive for your operating system and architecture
from the matching GitHub release, verify it against `checksums.txt`, and place
`vimls` (or `vimls.exe` on Windows) somewhere on `PATH`. Configure the Vim LSP
client to start that executable over stdio; vimls never reads editor
configuration itself.

For debugging, listen for one TCP session instead:

```sh
./bin/vimls --listen 127.0.0.1:4389
```

Use port `0` to let the operating system choose a free port; the selected
address is printed to stderr. Binding beyond loopback should be an explicit
choice because LSP has no authentication or transport encryption.

## Validate

```sh
make check
```

`make check` runs formatting verification, unit and subprocess tests, the race
detector, `go vet`, coverage enforcement, and a clean build. Coverage is measured
across production packages below `internal/` and must remain at least 90%.

The normal offline test gate losslessly includes all 362 `.vim` files below
Vim v9.2.1015's `src/testdir` (8,558,061 source bytes), plus 3,267 extracted
official scripts and a classified inventory of all 5,733 `Check*` candidates.
An explicit 44-file syntax-test allowlist prevents the conformance migration
from revisiting the other 318 test files. From that boundary, 3,844 helper calls
produce 5,261 source variants: 1,761 official parser-positive cases and 3,500
failure cases retained with their Vim error arguments as provenance. The
stability gate still parses every source through both independent parser entry
points without executing it, using committed artifacts in this repository.

See [the roadmap](docs/roadmap.md), [architecture](docs/architecture.md),
[language server features](docs/language-support.md), and
[test strategy](docs/testing.md). Semantics derived from Vim's tests are
recorded in the [static diagnostic reference](docs/diagnostics.md). Remaining
version-pinned research about historical error codes used by Vim9 is retained
in the [pre-E1000 research appendix](docs/vim9-errors-under-1000.md). Supported
official compile-diagnostic cases live in self-contained range tests under
`internal/analysis/official_compile_cases_e*_test.go`.

## Refresh pinned Vim metadata

All command, builtin-function, option, variable, event, modifier, and completion
metadata describes official Vim v9.2.1015 only.

Point `VIM_SOURCE` at an official Vim Git checkout. The metadata generator
reads the pinned tag with `git show`, so the checkout's current branch may be
newer:

```sh
VIM_SOURCE=/path/to/vim make metadata-check
```

`metadata-check` regenerates the four generated Go tables into a temporary
directory, compares them byte-for-byte with the repository, and runs metadata,
duplicate-name, help-tag, and generator tests. To intentionally refresh the
committed generated tables after advancing the pin, run:

```sh
VIM_SOURCE=/path/to/vim make metadata-refresh
VIM_SOURCE=/path/to/vim make metadata-check
```

The generator first verifies that the configured Vim tag resolves to its
hard-coded commit. Review the generated diff and update curated completion
metadata/provenance tests in the same pin-advance change.
