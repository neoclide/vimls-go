# Official Vim test fixtures

These files come from Vim tag **v9.2.1015**, commit
`5ab969f719bb09555e90e8dff8c94fc37bcbf2ae`. They let ordinary Go tests use
Vim's test inputs without downloading or executing Vim.

| File | Purpose |
| --- | --- |
| `v9.2.1015-test-files.json.gz` | A lossless copy of the tracked Vim scripts under `src/testdir`. |
| `v9.2.1015-parser-corpus.json.gz` | Embedded scripts extracted from selected parser and evaluator tests. |
| `v9.2.1015-helper-inventory.json.gz` | An inventory of upstream helper calls, including reasons a call could not be used. |
| `v9.2.1015-parser-files.json` | The reviewed list of upstream files used for parser-case migration. |
| `v9.2.1015-parser-cases.json.gz` | Extracted parser inputs and expectations, tied to the reviewed list by a hash. |

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

## Updating the fixtures

Keep the version pin, reviewed file list and generated artifacts consistent.
Every selected helper call must produce a case or keep an explicit skip reason.

Maintain compile-diagnostic cases one error code at a time in the owning test
file. Keep at most ten deterministic cases per code, covering compiled and
script-level contexts where both are relevant. Do not replace these tests with
a bulk-generated batch or execute runtime-dependent cases in ordinary tests.

Commands for focused triage and the clean Vim oracle are in
[testing](../../docs/testing.md).

## License

Copied source retains Vim's license. [VIM-LICENSE](VIM-LICENSE) is the exact
license text from the pinned tag.
