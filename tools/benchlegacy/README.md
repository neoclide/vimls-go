# Legacy parser comparison

This nested benchmark module compares the legacy parser with
`github.com/vim-jp/go-vimlparser` without adding the reference parser to the
language server's production module graph.

Use a temporary Go workspace so both local checkouts are built without network
access:

```sh
bench_work=$(mktemp -d)
vim_source=/Users/chemzqm/lib/vim
test "$(git -C "$vim_source" describe --tags --exact-match)" = v9.2.1015
test -z "$(git -C "$vim_source" status --porcelain -- runtime)"
cd "$bench_work"
go work init \
  /path/to/vimls-go \
  /path/to/vimls-go/tools/benchlegacy \
  /path/to/go-vimlparser

cd /path/to/vimls-go/tools/benchlegacy
GOMAXPROCS=1 GOPROXY=off GOSUMDB=off GOWORK="$bench_work/go.work" \
  go test -run '^$' -bench '^BenchmarkLegacyParsers/(vimls-go|go-vimlparser)-common$' \
  -benchmem -benchtime=1x -count=20 -args \
  -root "$vim_source/runtime"
```

After the single-worker parser comparison, measure the bounded workspace batch
path separately:

```sh
GOMAXPROCS=4 GOPROXY=off GOSUMDB=off GOWORK="$bench_work/go.work" \
  go test -run '^$' \
  -bench '^BenchmarkLegacyParsers/vimls-go-all-workers-(1|2|4)$' \
  -benchmem -benchtime=1x -count=20 -args \
  -root "$vim_source/runtime"
```

Keep `GOMAXPROCS=4` fixed for all three batch lanes; the `workers` suffix is the
only concurrency variable. Do not compare a parallel lane with the
single-worker go-vimlparser result as parser efficiency.

The official `v9.2.1015` runtime tree is the standing performance corpus. Do not
add personal runtimepath or plugin roots to routine A/B commands. Those trees
may be useful as occasional correctness smoke inputs, but their changing files
and size make them unsuitable as the parser performance gate.

Discovery, file reads, line splitting, dialect classification, and the
common-success intersection are outside the timed section. The reference lane
does include `NewStringReader`, because it is required preprocessing and its
reader is consumed by each parse. Only files that are legacy according to
vimls-go, produce no vimls-go diagnostics, and parse to completion with
go-vimlparser enter the comparable `common` corpus. `vimls-go-loose-all`
measures recovery over every discovered legacy file, but is not compared with
the fail-fast reference parser.

## Isolated parser profiles

Go's top-level `-cpuprofile` and `-memprofile` include corpus discovery,
classification, and the go-vimlparser reference pass. Use the explicitly
enabled profile test instead when attributing vimls-go parser costs. It reads
and hashes the corpus first, then records only `workspace.ParseSources`:

```sh
profile_dir=$(mktemp -d)
GOMAXPROCS=1 GOPROXY=off GOSUMDB=off GOWORK="$bench_work/go.work" \
  go test -run '^TestProfileVimlsBatch$' -args \
  -profile-dir "$profile_dir" -profile-workers 1 -profile-runs 3 \
  -root "$vim_source/runtime"

go tool pprof -top "$profile_dir/cpu.pprof"
go tool pprof -top -sample_index=alloc_space \
  -base="$profile_dir/allocs-before.pprof" \
  "$profile_dir/allocs-after.pprof"
go tool pprof -top -sample_index=inuse_space \
  -base="$profile_dir/heap-before.pprof" \
  "$profile_dir/heap-after.pprof"
```

Use a new empty `profile-dir` for each run; profile files are created with
exclusive access and are never overwritten. Allocation attribution uses Go's
normal sampling rate so CPU samples are not distorted by per-allocation
profiling. The test log also reports separate top-level and nested command-slice
length, capacity, waste, byte totals, and length distributions so backing-array
growth can be distinguished from the reported `Command` value size. Continue
to use `-benchmem` for exact bytes/op and allocs/op.
