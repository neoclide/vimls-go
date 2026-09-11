# Autocmd event documentation

This Go generator collects Vim and Neovim event help into
[`internal/vimdata/autocmd_docs_generated.go`](../../internal/vimdata/autocmd_docs_generated.go).
There is no runtime JSON or source-checkout dependency. Event hover reads this
data directly; completion includes the merged event names and loads each
selected event's documentation through `completionItem/resolve`.

| Editor | Pinned revision | Inventory |
| --- | --- | --- |
| Vim `v9.2.1015` | `5ab969f719bb09555e90e8dff8c94fc37bcbf2ae` | `src/autocmd.c` |
| Neovim `v0.12.0-2035-g73923b0dd8` | `73923b0dd85bb936ba2f63ee916dabaa0603340d` | `src/nvim/auevents.lua` |

The Neovim revision is a development snapshot from the supplied local checkout,
not a stable release or an extension of the project's pinned Vim syntax.
The generator reads committed Git objects and preserves local checkout changes.

Vim provides 127 event spellings and Neovim provides 150 (including aliases).
The merged table contains 156 unique names: all 127 Vim entries and 29
Neovim-only entries. Name collisions always retain the Vim entry, including
its documentation, alias target and provenance. Matching is case-insensitive.

## Data contract

`vimdata.LookupAutocmdEventDocumentation(name)` returns structured documentation;
`vimdata.AutocmdEventDocumentations()` returns a caller-owned list.
Each entry includes `Name`, `AliasOf`, `Editor`, `Revision`, `Source`, `Tag`,
`Line` (one-based help-tag line), and Markdown `Documentation`.

The text comes from `runtime/doc/autocmd.txt`, supplemented for Neovim by
`diagnostic.txt`, `lsp.txt`, `pack.txt`, and `deprecated.txt`. Prose and examples
are converted using the existing Vim help Markdown converter, not summarized.
Adjacent or shared alias headings share their body but retain tag locations.
If an alias lacks its own help, its canonical event supplies the tag and source.

Neovim's `TermChanged` has no dedicated help block at the pinned snapshot; the
generator reports this. The final table contains the documented Vim entry for
that name. Every merged entry must have documentation or generation fails.

## Regeneration

Run from the repository root:

```sh
make eventdocs-refresh VIM_SOURCE=/Users/chemzqm/lib/vim \
  NEOVIM_SOURCE=/Users/chemzqm/lib/neovim
make eventdocs-check VIM_SOURCE=/Users/chemzqm/lib/vim \
  NEOVIM_SOURCE=/Users/chemzqm/lib/neovim
```

`metadata-refresh` and `metadata-check` include these targets. CI runs
`metadata-check` with the pinned upstream revisions. For direct generator use,
`go run ./tools/geneventdocs -vim-root ... -neovim-root ... -output /temporary/path/events.go`
regenerates outside the checkout. Run
`go test -count=1 ./tools/geneventdocs ./internal/vimdata` after extractor changes.
Review inventory, aliases and documentation differences before changing pins.

## Provenance and licenses

The extraction, Markdown conversion, merge and Go packaging are this project's
modifications. Manual excerpts retain the Open Publication License, v1.0 or
later; [VIM-DOC.txt](../../LICENSES/VIM-DOC.txt) records copyright, original
sources, modification date and attribution. Update that notice when changing
the extracted documentation.
Applicable Neovim contributor notices are also retained in the
[complete upstream license](../../LICENSES/NEOVIM.txt), including Apache 2.0
and the terms for parts contributed under the Vim license.
