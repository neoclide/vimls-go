# Official Vim parser corpus

`v9.2.1015-parser-corpus.json.gz` is generated from the heredoc scripts in the
selected official Vim parser and evaluator tests at tag `v9.2.1015`, commit
`5ab969f719bb09555e90e8dff8c94fc37bcbf2ae`. It lets the default offline test
gate exercise both independent parser entry points without cloning Vim.

Regenerate it only from the pinned local Vim checkout:

```sh
GOPROXY=off GOSUMDB=off go run -mod=readonly ./tools/genofficial \
  -vim-source /Users/chemzqm/lib/vim
```

The extracted source remains covered by Vim's license. See the `LICENSE` file
at the root of the referenced Vim source tree.
