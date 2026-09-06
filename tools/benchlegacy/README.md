# Comparing Legacy parsers

This separate Go module compares vimls-go with
[go-vimlparser](https://github.com/vim-jp/go-vimlparser). It does not add the
reference parser to the server's dependencies.

Use local checkouts of both projects and a clean runtime tree from Vim
`v9.2.1015`. Set these paths for your machine, then create a temporary Go
workspace:

```sh
vimls_source=/path/to/vimls-go
vim_source=/path/to/vim
reference_source=/path/to/go-vimlparser
bench_work=$(mktemp -d)

test "$(git -C "$vim_source" describe --tags --exact-match)" = v9.2.1015
test -z "$(git -C "$vim_source" status --porcelain -- runtime)"

cd "$bench_work"
go work init "$vimls_source" "$vimls_source/tools/benchlegacy" "$reference_source"
cd "$vimls_source/tools/benchlegacy"

GOMAXPROCS=1 GOPROXY=off GOSUMDB=off GOWORK="$bench_work/go.work" \
  go test -run '^$' \
  -bench '^BenchmarkLegacyParsers/(vimls-go|go-vimlparser)-common$' \
  -benchmem -benchtime=1x -count=5 -args -root "$vim_source/runtime"
```

The comparison includes only Legacy files that both parsers accept.
Discovery, file reads and selection happen before timing. The reference
parser's required reader construction remains timed.

Keep the runtime tree, file order, toolchain and worker count fixed between
runs. Personal plugin collections change too often to serve as the standing
performance input. A parser that stops at an error cannot be compared fairly
with one that continues through the rest of the file.

## Workspace workers

Measure parallel workspace parsing separately:

```sh
GOMAXPROCS=4 GOPROXY=off GOSUMDB=off GOWORK="$bench_work/go.work" \
  go test -run '^$' \
  -bench '^BenchmarkLegacyParsers/vimls-go-all-workers-(1|2|4)$' \
  -benchmem -benchtime=1x -count=5 -args -root "$vim_source/runtime"
```

Here the worker count is the changing variable. These results describe batch
throughput, not a comparison with the single-worker reference parser.

## Isolated parser profiles

Use the profile test to exclude corpus setup and the reference parser from
vimls-go's profile:

```sh
profile_dir=$(mktemp -d)
GOMAXPROCS=1 GOPROXY=off GOSUMDB=off GOWORK="$bench_work/go.work" \
  go test -count=1 -run '^TestProfileVimlsBatch$' -args \
  -profile-dir "$profile_dir" -profile-workers 1 -profile-runs 5 \
  -root "$vim_source/runtime"

go tool pprof -top "$profile_dir/cpu.pprof"
go tool pprof -top -sample_index=alloc_space \
  -base="$profile_dir/allocs-before.pprof" "$profile_dir/allocs-after.pprof"
```

Use a new output directory each time; existing profiles are not overwritten.
Profiles use allocation sampling. Read `-benchmem` for bytes and allocations
per operation, and do not interpret those values as retained heap or RSS.
