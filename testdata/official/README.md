# Official Vim test fixtures

The current corpus comes from the [pinned Vim baseline](../../docs/language-support.md).
It lets ordinary Go tests use Vim's test inputs without downloading or executing
Vim.

| File | Purpose |
| --- | --- |
| `<tag>-test-files.json.gz` | A lossless copy of tracked Vim scripts under `src/testdir`. |
| `<tag>-parser-corpus.json.gz` | Embedded scripts extracted from selected parser and evaluator tests. |
| `<tag>-helper-inventory.json.gz` | An inventory of upstream helper calls, including reasons a call could not be used. |
| `<tag>-parser-files.json` | The reviewed parser migration allowlist, with an explicit reason for each selection or exclusion. |
| `<tag>-parser-cases.json.gz` | Extracted parser inputs and expectations, tied to the allowlist by the SHA-256 of `json.Marshal` on its typed manifest. |
| `<tag>-corpus-lock.json` | The reviewed provenance, manifest hash and source-derived inventory totals for the active corpus. |

`<tag>` is the active pin. The current lock is the authoritative reviewed count
record; generated artifacts retain their own tag and commit provenance.

The `v9.2.1015-*` files are historical archives for the original migration
from commit `5ab969f719bb09555e90e8dff8c94fc37bcbf2ae`. They remain byte-for-byte
preserved so older handwritten regression provenance can still be traced to its
recorded source. Existing handwritten compile regressions retain that original
source unless their own provenance comment records a newer revision.

## What the tests prove

Full-file parsing checks that the parser keeps source text and valid ranges
and can recover without crashing. It does not prove complete agreement with
Vim.

Focused cases check language behavior. An upstream failure can come from
parsing, compilation, type checking or execution; identify which phase failed
before turning it into a parser-negative test.

Compile diagnostics are kept as readable Vim snippets in
[internal/analysis](../../internal/analysis), in the
`official_compile_cases_e*_test.go` files. Each case retains its upstream
location, identifier and expected error code.

## Updating the current corpus

The generator reads only Git objects for the fixed tag and refuses a checkout
where that tag resolves to another commit. Run:

```sh
go run ./tools/genofficial -vim-root /path/to/vim
```

`-output-dir` selects where the four generated gzip artifacts are written,
`-manifest` selects the reviewed parser-file manifest, and `-lock` selects the
reviewed corpus lock. The generator compares the complete computed inventory to
that lock before writing artifacts. `-print-lock` prints a deterministic
candidate lock to standard output without writing artifacts or a lock file; use
it only while reviewing a deliberate source update. The generator writes
deterministic gzip streams; their timestamp is zero and their OS byte is 255.
Every selected helper call must produce a case or keep an explicit skip reason.
`make official-refresh VIM_SOURCE=/path/to/vim` refreshes the current files.
`make official-check VIM_SOURCE=/path/to/vim` regenerates them in a temporary
directory and compares all four artifacts. It also rebuilds parser cases from
the committed manifest, including its typed-manifest hash check.

Maintain compile-diagnostic cases one error code at a time in the owning test
file. Keep at most ten deterministic cases per code, covering compiled and
script-level contexts where both are relevant. Do not replace these tests with
a bulk-generated batch or execute runtime-dependent cases in ordinary tests.

Commands for focused triage and the clean Vim oracle are in
[testing](../../docs/testing.md).

## License

Copied source retains Vim's license. [VIM-LICENSE](VIM-LICENSE) is the exact
license text from the pinned tag.
