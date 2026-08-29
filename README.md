# vimls-go

vimls-go is a Go language server for legacy Vim script and Vim9 script. It
targets Vim 9.1 and newer stable releases.

The repository is being built milestone by milestone from the contracts in
`docs/`. The server currently provides bounded stdio and TCP transports,
lifecycle and target-version handling, immutable document snapshots,
incremental synchronization, and UTF-8/UTF-16/UTF-32 position conversion.
UTF-16 is the default when the client does not negotiate another encoding.

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
failure cases whose reviewed phase and parser-migration status are recorded in
the official syntax migration ledger. The stability gate still parses every
source through both independent parser entry points without executing it. To
additionally compare the committed corpora and copied Vim license byte-for-byte
with the pinned local checkout, run:

```sh
GOPROXY=off GOSUMDB=off make test-official
```

See [the roadmap](docs/roadmap.md), [architecture](docs/architecture.md),
[language support contract](docs/language-support.md), and
[test strategy](docs/testing.md). The pinned Vim failure-phase research and
implementation queue live in the
[official syntax migration ledger](docs/official-syntax-migration.md).
