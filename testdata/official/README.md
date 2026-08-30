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
  defaults every other pinned test file to excluded. Migration generators must
  use this allowlist instead of rescanning all 362 files.
- `v9.2.1015-parser-cases.json.gz` is generated only from that allowlist and is
  bound to the canonical manifest JSON SHA-256. It accounts for all 3,844
  selected helper calls: 3,805 are statically extracted into 5,261 source
  variants and 39 retain an explicit skip reason. The 1,761 success variants
  are parser-positive tests. The other 3,500 deliberately remain unclassified
  in the generated artifact; their reviewed phase classification and
  parser-negative migration status are maintained in
  [`docs/official-syntax-migration.md`](../../docs/official-syntax-migration.md).
- `v9.2.1015-compile-cases.json.gz` supplies reconstructed `def` and
  `vim9script` failure sources with exact error codes when statically known.
  Analysis gates run at most 30 deterministic, self-contained cases per error
  code. Runtime-dependent cases are excluded and covered, where possible, by
  focused Go fixtures. Tests never start Vim.

The full-file corpus is a stability and lossless-recovery gate. It does not by
itself prove that vimls-go accepts and rejects every construct exactly as Vim
does. Exact conformance additionally requires focused tests and generated
official helper expectations. A Vim helper failure may occur during parsing,
compilation, type checking, name resolution, or execution, so it is promoted to
a parser-negative case only after its phase is verified separately.

Regenerate them only from the pinned local Vim checkout:

```sh
GOPROXY=off GOSUMDB=off go run -mod=readonly ./tools/genofficial \
  -vim-source /Users/chemzqm/lib/vim
```

The copied source remains covered by Vim's license. `VIM-LICENSE` is the exact
license file from the pinned tag.
