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

The full-file corpus is a stability and lossless-recovery gate. It does not by
itself prove that vimls-go accepts and rejects every construct exactly as Vim
does. Exact conformance additionally requires focused tests and generated
official helper expectations.

Regenerate them only from the pinned local Vim checkout:

```sh
GOPROXY=off GOSUMDB=off go run -mod=readonly ./tools/genofficial \
  -vim-source /Users/chemzqm/lib/vim
```

The copied source remains covered by Vim's license. `VIM-LICENSE` is the exact
license file from the pinned tag.
