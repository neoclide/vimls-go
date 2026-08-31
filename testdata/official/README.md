# Official Vim parser corpora

These files are generated from Vim tag `v9.2.1015`, commit
`5ab969f719bb09555e90e8dff8c94fc37bcbf2ae`:

- `v9.2.1015-test-files.json.gz` losslessly contains all 362 tracked `.vim`
  files below `src/testdir`, totaling 8,558,061 source bytes. The offline test
  gate parses every file with both independent parser entry points and checks
  source retention and syntax spans. It never sources or executes the files.
- `v9.2.1015-parser-corpus.json.gz` contains 3,267 heredoc scripts extracted
  from 17 selected parser and evaluator tests. It retains each upstream origin
  and the statically identifiable success or failure classification.
- `v9.2.1015-helper-inventory.json.gz` records all 5,733 `Check*` candidates in
  the full corpus. It classifies 5,208 qualified `v9.Check*` calls by helper,
  source span, and first-argument shape; every other candidate has an explicit
  out-of-scope reason. This is an extraction ledger, not a parser oracle.
- `v9.2.1015-parser-files.json` is the reviewed migration allowlist. It includes
  44 files with direct lexer, parser, command-grammar, or recovery coverage and
  defaults every other pinned test file to excluded. The pinned parser-case
  snapshot follows this allowlist instead of covering all 362 files.
- `v9.2.1015-parser-cases.json.gz` is generated only from that allowlist and is
  bound to the canonical manifest JSON SHA-256. It accounts for all 3,844
  selected helper calls: 3,805 are statically extracted into 5,261 source
  variants and 39 retain an explicit skip reason. The 1,761 success variants
  are parser-positive tests. The other 3,500 deliberately remain unclassified
  in the generated artifact and retain their Vim error arguments as provenance.
- `internal/analysis/official_compile_cases_e*_test.go` contains the
  reconstructed `def` and `vim9script` failure sources with exact error codes.
  These readable Go test fixtures give each error code one owning block; every
  retained case keeps its official Vim source-position comment and ID next to
  the Vim source. Analysis gates keep at most ten deterministic cases per error
  code, balanced between `def` and script/legacy contexts when both exist.
  Runtime-dependent cases are excluded and covered, where possible, by focused
  Go fixtures. Tests never start Vim.

The full-file corpus is a stability and lossless-recovery gate. It does not by
itself prove that vimls-go accepts and rejects every construct exactly as Vim
does. Exact conformance additionally requires focused tests and generated
official helper expectations. A Vim helper failure may occur during parsing,
compilation, type checking, name resolution, or execution, so it is promoted to
a parser-negative case only after its phase is verified separately.

The other pinned official artifacts above remain generated snapshots. Compile
fixtures are maintained directly in their owning range tests; a future Vim
upgrade must migrate supported error codes individually rather than
batch-generate replacement fixtures.

The copied source remains covered by Vim's license. `VIM-LICENSE` is the exact
license file from the pinned tag.
