# Official Vim syntax migration ledger

This ledger is the release-scoped research record for migrating Vim's official
tests into parser conformance assertions. It records classifications and rule
groups; the exact source for every case remains in
`testdata/official/v9.2.1015-parser-cases.json.gz` and is not duplicated here.

## Pinned snapshot

- Vim tag: `v9.2.1015`
- Vim commit: `5ab969f719bb09555e90e8dff8c94fc37bcbf2ae`
- vimls-go classification baseline: `7857a24`
- Included test files: 44
- Failure variants: 3,500
- Existing parser-negative assertions at the baseline: 362
- Current parser-negative syntax assertions: 892 (`ed2cbef`)

The 3,500 variants are partitioned by source file. A variant belongs to exactly
one phase: `syntax`, `type`, `name`, `semantic`, `runtime`, or `unknown`.
`unknown` is an explicit research result; it must not be guessed into another
phase merely to reduce the count.

## Research partition

| Group | Source files | Failure variants |
| --- | --- | ---: |
| A | `test_vim9_expr.vim` | 872 |
| B | `test_vim9_class.vim`, `test_vim9_assign.vim`, `test_vim9_interface.vim`, `test_eval_stuff.vim`, `test_user_func.vim`, `test_usercommands.vim`, `test_let.vim`, `test_trycatch.vim`, `test_autocmd.vim` | 837 |
| C | `test_vim9_script.vim`, `test_vim9_generics.vim`, `test_vim9_cmd.vim`, `test_vim9_import.vim`, `test_vim9_typealias.vim` | 852 |
| D | `test_tuple.vim`, `test_vim9_func.vim`, `test_expr.vim`, `test_blob.vim`, `test_listdict.vim`, `test_vim9_enum.vim` | 939 |
| **Total** | 21 files with failures | **3,500** |

The other 23 included files have no failure variants and need no negative-case
migration: `test_cmdmods.vim`, `test_comparators.vim`, `test_const.vim`,
`test_ex_equal.vim`, `test_excmd.vim`, `test_filter_cmd.vim`, `test_global.vim`,
`test_highlight.vim`, `test_lambda.vim`, `test_map_functions.vim`,
`test_mapping.vim`, `test_method.vim`, `test_nested_function.vim`,
`test_put.vim`, `test_registers.vim`, `test_retab.vim`, `test_set.vim`,
`test_shift.vim`, `test_sort.vim`, `test_source.vim`, `test_substitute.vim`,
`test_unlet.vim`, and `test_vimscript.vim`. Their generated success variants
remain covered by the parser-positive corpus tests.

## Status contract

- `migrated`: the exact cases are present in `TestOfficialVimParserFailures`.
- `ready`: the parser already returns the official code, but the exact cases
  are not yet in that matrix.
- `pending-fix`: the case is syntax, but production behavior still differs.
- `blocked`: the syntax phase is known, but a required parser contract or Vim
  version rule is unresolved.

Implementation work consumes stable rule-group IDs from this ledger. It may
combine groups that touch the same parser path, but it must not start a new
source-research pass for cases already classified here.

After changing the expected map, run the migration report instead of counting
keys or source groups by hand:

```sh
go test -mod=readonly ./internal/syntax \
  -run '^TestOfficialVimParserMigrationReport$' -count=1 -v
```

The report validates that every migrated key still exists in the pinned
artifact and prints the authoritative total plus Group A-D counts in one
machine-readable line.

## Phase accounting

The four source-group reports account for every failure variant in the pinned
artifact.

| Group | Syntax | Type | Name | Semantic | Runtime | Unknown | Total |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| A | 325 | 270 | 65 | 37 | 45 | 130 | 872 |
| B | 191 | 239 | 77 | 251 | 69 | 10 | 837 |
| C | 369 | 78 | 111 | 239 | 52 | 3 | 852 |
| D | 183 | 484 | 54 | 47 | 166 | 5 | 939 |
| **Total** | **1,068** | **1,071** | **307** | **574** | **332** | **148** | **3,500** |

The original baseline classification counted 1,072 syntax variants. Review of
the expression-command boundary group reclassified three value-dependent
records as runtime. A later source trace reclassified the E1411 object
assignment record as type analysis, so the corrected baseline split is 346
migrated, 89 ready, and 633 pending-fix. The 362-entry expected map therefore
contains 346 verified syntax keys and 16 cleanup keys that are non-syntax or
stale. The cleanup membership is recorded in the source-group sections below.

Commit `05d176c` made two invalid class-variable cases ready. Commit `55169af`
then migrated all 91 cases that were ready in the current parser: Group A 47,
Group B 16, Group C 23, and Group D 5. The current split is therefore 437
migrated, zero ready, and 635 pending-fix. Commit `83781bd` implemented and
migrated the 10 Group A List-delimiter cases, making the current split 447
migrated, zero ready, and 625 pending-fix. Commit `cbe7267` implemented the 10
remaining Group A Blob/register cases, making the current split 457 migrated,
zero ready, and 615 pending-fix. Commit `c340e30` implemented the three stable
Vim 9.1+ `:vim9script [noclear]` argument cases, making the current split 460
migrated, zero ready, and 612 pending-fix. Commit `2329653` migrated the four
missing Dictionary-value variants, making the current split 464 migrated, zero
ready, and 608 pending-fix. Commit `067bb55` migrated all 15 malformed-number
variants from legacy functions, `def` functions, and Vim9 scripts, making the
current split 479 migrated, zero ready, and 593 pending-fix.
Commit `3bd2d68` migrated three Vim9 `def` assignment failures, making the
current split 482 migrated, zero ready, and 590 pending-fix.
Commit `f411f1a` migrated the four Vim9 adjacent-unary-sign variants, making
the current split 486 migrated, zero ready, and 586 pending-fix.
Commit `347226c` migrated the three malformed special-key expression variants,
making the current split 489 migrated, zero ready, and 583 pending-fix.
Commit `191fadb` migrated the final four Dictionary-delimiter variants, making
the current split 493 migrated, zero ready, and 579 pending-fix.
Commit `1e10f55` migrated the Vim9 EX_XFILE missing-backtick case, making the
current split 494 migrated, zero ready, and 578 pending-fix.
Commit `f3e1093` migrated the seven Vim9 attached-hash comment cases, making
the current split 501 migrated, zero ready, and 571 pending-fix.
Commit `91d2277` migrated both contexts of the Vim9 line-ending method-arrow
record, making the current split 503 migrated, zero ready, and 569 pending-fix.
Commit `aedd462` migrated the four Vim9 non-delimited `is`/`isnot` token
variants, making the current split 507 migrated, zero ready, and 565
pending-fix.
Commit `f036019` migrated both contexts of the spaced callable-call case,
making the current split 509 migrated, zero ready, and 563 pending-fix.
Commit `a9e5971` migrated the four computed Dictionary-key bracket cases,
making the current split 513 migrated, zero ready, and 559 pending-fix.
Commit `21333d9` migrated the compiled-function missing-call-comma case,
making the current split 514 migrated, zero ready, and 558 pending-fix.
Commit `63cc8a9` migrated the three Vim9 missing-member-name variants, making
the current split 517 migrated, zero ready, and 555 pending-fix.
Commit `7f60a4d` migrated both missing inline-function brace variants, making
the current split 519 migrated, zero ready, and 553 pending-fix.
Commit `9024ed1` migrated all 11 invalid Vim9 hash-curly comment variants,
making the current split 530 migrated, zero ready, and 542 pending-fix.
Commit `a3d92d2` migrated the two compiled-function invalid dot-key variants.
The same review moved `2402:71062/script`, `3145:94088/vim9-script`, and
`4188:121761/vim9-script` from syntax to runtime: their outcomes require
evaluating a string or inspecting a value's runtime type. Historical pending
totals above predate that correction; the authoritative current split is 532
migrated, zero ready, and 537 pending-fix.
Commit `bca737c` migrated the script-context spaced-call command variant,
making the current split 533 migrated, zero ready, and 536 pending-fix.
Commit `a05b752` migrated both contexts of the inline-function same-line
command case, making the current split 535 migrated, zero ready, and 534
pending-fix.
Commit `b5cf4fb` migrated both missing inline-lambda heredoc-marker contexts,
making the current split 537 migrated, zero ready, and 532 pending-fix.
Commit `0f70449` verified and migrated all 12 invalid unary-chain variants,
making the current split 549 migrated, zero ready, and 520 pending-fix.
Commit `b50bd90` migrated the four function-call comma-spacing variants,
making the current split 553 migrated, zero ready, and 516 pending-fix.
Commit `07be705` migrated the eight List comma-spacing variants, making the
current split 561 migrated, zero ready, and 508 pending-fix.
Commit `0814cad` migrated the four lambda-parameter comma-spacing variants,
making the current split 565 migrated, zero ready, and 504 pending-fix.
Commit `81f9860` migrated all 20 Dictionary delimiter-spacing variants, making
the current split 585 migrated, zero ready, and 484 pending-fix.
Commit `d29ae92` verified and migrated both ternary question-mark spacing
variants, making the current split 587 migrated, zero ready, and 482
pending-fix.
Commit `84326e2` migrated all 12 lambda-arrow spacing variants, making the
current split 599 migrated, zero ready, and 470 pending-fix.
Commit `9bc420c` migrated the seven unterminated string variants, making the
current split 606 migrated, zero ready, and 463 pending-fix.
Commit `d8d82f7` migrated the four missing method-call-parenthesis variants,
making the current split 610 migrated, zero ready, and 459 pending-fix.
Commit `7750739` migrated the six malformed Vim9 atom variants, making the
current split 616 migrated, zero ready, and 453 pending-fix.
Commit `e23b1f2` migrated the three postfix-closing-delimiter variants, making
the current split 619 migrated, zero ready, and 450 pending-fix.
Commit `ca95465` migrated the eight missing-operand variants, making the
current split 627 migrated, zero ready, and 442 pending-fix.
Commit `e3883d6` migrated the malformed method-tail variant, making the current
split 628 migrated, zero ready, and 441 pending-fix. Commit `e5173fe` migrated
the final ready Group A Dictionary case, making the current split 629 migrated,
zero ready, and 440 pending-fix.
Commit `a56cd10` migrated the interface method-body variant, making the current
split 630 migrated, zero ready, and 439 pending-fix. Commit `c05b9ae` migrated
two already-ready Group B variants, making the current split 632 migrated, zero
ready, and 437 pending-fix. Commit `37eec08` migrated all four Group B dialect
declaration variants, making the current split 636 migrated, zero ready, and
433 pending-fix. Commits `51ee4ea` and `89a557d` completed all 15 register
declaration variants, making the current split 651 migrated, zero ready, and
418 pending-fix.

The commit chronology above preserves the counts known at each checkpoint.
After reclassifying E1411, the authoritative current split is 651 migrated,
zero ready, and 417 pending-fix. Commit `efb397e` migrated the remaining E1127
member-call recovery case, making the current split 652 migrated, zero ready,
and 416 pending-fix. Commit `d3d86b1` migrated the two Vim9 member-dot spacing
cases, making the authoritative current split 654 migrated, zero ready, and
414 pending-fix. Commit `52e3f5d` migrated both Vim9 increment-spacing
contexts, making the authoritative current split 656 migrated, zero ready, and
412 pending-fix. Commit `9727226` migrated all 15 invalid class-body command
cases, making the authoritative current split 671 migrated, zero ready, and
397 pending-fix. Commit `e2f7aba` migrated five malformed generic-call type
lists, making the authoritative current split 676 migrated, zero ready, and
392 pending-fix. Commit `7fa8066` completed the six remaining constructor and
class-method modifier cases, making the authoritative current split 682
migrated, zero ready, and 386 pending-fix. Commit `ae0e2d3` completed the 16
remaining declaration type-delimiter cases, making the authoritative current
split 698 migrated, zero ready, and 370 pending-fix. Commit `dbb1db6`
completed all 15 declaration-shape cases, making the authoritative current
split 713 migrated, zero ready, and 355 pending-fix. Commit `1ac1cef`
completed the three assignment-shape cases and the remaining member-dot case,
making the authoritative current split 717 migrated, zero ready, and 351
pending-fix. Commit `a1d50fc` migrated the missing call-argument case, making
the authoritative current split 718 migrated, zero ready, and 350 pending-fix.
Commit `a535cff` completed the two aggregate placement cases, making the
authoritative current split 720 migrated, zero ready, and 348 pending-fix.
Commit `97fbf9a` migrated the top-level object type tail, making the
authoritative current split 721 migrated, zero ready, and 347 pending-fix.
Commit `2bdaaf2` migrated four aggregate terminator tails, making the
authoritative current split 725 migrated, zero ready, and 343 pending-fix.
Commit `258f7ef` migrated the two invalid heredoc headers, making the
authoritative current split 727 migrated, zero ready, and 341 pending-fix.
Commit `5930c8d` migrated the numeric declaration-name case, making the
authoritative current split 728 migrated, zero ready, and 340 pending-fix.
Commit `c608781` migrated both declaration type-tail variants, making the
authoritative current split 730 migrated, zero ready, and 338 pending-fix.
Commit `ffa8e9b` migrated the typed assignment tail, making the authoritative
current split 731 migrated, zero ready, and 337 pending-fix. Commit `87b7f8a`
migrated both mismatched aggregate closers, making the authoritative current
split 733 migrated, zero ready, and 335 pending-fix. Commit `1b7b04f` migrated
both invalid abstract headers, making the authoritative current split 735
migrated, zero ready, and 333 pending-fix. Commit `4f818a5` completed the final
trailing-command case, making the authoritative current split 736 migrated,
zero ready, and 332 pending-fix. Commit `4748011` migrated all eleven eval
heredoc interpolation failures, making the authoritative current split 747
migrated, zero ready, and 321 pending-fix. Commit `162eb64` completed the two
direct class-member separator cases, making the authoritative current split
749 migrated, zero ready, and 319 pending-fix. Commit `c03403f` migrated three
generic declaration terminator recoveries, making the authoritative current
split 752 migrated, zero ready, and 316 pending-fix. Commit `e583448` migrated
the inherited generic-name case, commit `bc08c8c` migrated three attached-hash
command tails, and commit `807bd22` migrated two generic-call recoveries. The
authoritative current split is 758 migrated, zero ready, and 310 pending-fix.
Commit `3ab05e0` migrated 22 Group C cases already proven ready: all 17 E1100
single-command variants, two generic-call delimiter variants, both E1059
typed-`for` variants, and the final E1125 declaration variant. The
authoritative current split is 780 migrated, zero ready, and 288 pending-fix.
Commit `a12a767` migrated the five E1202 generic-call whitespace variants,
making the authoritative current split 785 migrated, zero ready, and 283
pending-fix.
Commit `bf1c397` migrated both E1241 one-byte `:global` separator variants,
making the authoritative current split 787 migrated, zero ready, and 281
pending-fix.
Commit `46f565b` migrated the four direct Vim9 range-without-colon variants,
making the authoritative current split 791 migrated, zero ready, and 277
pending-fix.
Commit `b74d592` migrated the two static import-path alias failures, making the
authoritative current split 793 migrated, zero ready, and 275 pending-fix.
Commit `ad1edd0` migrated the E1043 invalid export command and E1044 invalid
export argument failures, making the authoritative current split 795 migrated,
zero ready, and 273 pending-fix.
Commit `608ed48` migrated both E182 attached user-command comment failures,
making the authoritative current split 797 migrated, zero ready, and 271
pending-fix.
Commit `74ccec2` migrated two E1554 incomplete generic references, making the
authoritative current split 799 migrated, zero ready, and 269 pending-fix.
Commit `77b5e9b` migrated two additional E1554 typed tails made ready by the
same recovery, making the authoritative current split 801 migrated, zero ready,
and 267 pending-fix.
Commit `c48d396` migrated the E1185 unterminated redirection failure, making the
authoritative current split 802 migrated, zero ready, and 266 pending-fix.
Commit `7cbd3f2` migrated the E116 incomplete generic-call argument failure,
making the authoritative current split 803 migrated, zero ready, and 265
pending-fix.
Commit `70251bb` migrated four invalid Vim9 condition recoveries, making the
authoritative current split 807 migrated, zero ready, and 261 pending-fix.
Commit `5433450` migrated the `dsearch` attached-comment tail, making the
authoritative current split 808 migrated, zero ready, and 260 pending-fix.
Commit `c035bd3` migrated 13 empty generic-call type lists, making the
authoritative current split 821 migrated, zero ready, and 247 pending-fix.
Commit `a793234` migrated four invalid Vim9 try-branch/terminator cases, making
the authoritative current split 825 migrated, zero ready, and 243 pending-fix.
Commit `60be383` migrated two legacy numeric-version cases, making the
authoritative current split 827 migrated, zero ready, and 241 pending-fix.
Commit `c8c0f44` migrated 16 missing/mismatched Vim9 block terminators, making
the authoritative current split 843 migrated, zero ready, and 225 pending-fix.
Commit `ccb01cd` migrated the embedded `windo if` terminator case, making the
authoritative current split 844 migrated, zero ready, and 224 pending-fix.
Commit `1df5bda` migrated five generic-call delimiter cases already accepted by
the parser, making the authoritative current split 849 migrated, zero ready,
and 219 pending-fix.
Commit `a79d7f5` migrated six invalid Vim9 branch/closer cases, making the
authoritative current split 855 migrated, zero ready, and 213 pending-fix.
Commit `9f8142c` migrated `vim9script` inside a function, making the
authoritative current split 856 migrated, zero ready, and 212 pending-fix.
Commit `ef49637` migrated eight invalid Vim9 range variants, making the
authoritative current split 864 migrated, zero ready, and 204 pending-fix.
Commit `f84432f` migrated two empty Vim9 variable commands, making the
authoritative current split 866 migrated, zero ready, and 202 pending-fix.
Commit `1082d53` migrated the attached-hash `menutrans clear` case, making the
authoritative current split 867 migrated, zero ready, and 201 pending-fix.
Commit `d8741b9` migrated the local Vim9 `func!` case, making the authoritative
current split 868 migrated, zero ready, and 200 pending-fix.
Commit `10f6de7` migrated two Vim9 scope-brace cases, making the authoritative
current split 870 migrated, zero ready, and 198 pending-fix.
Commits `8735abf` and `a7b5679` migrated 16 built-in Vim9 command-spacing
cases, making the authoritative current split 886 migrated, zero ready, and
182 pending-fix.
Commit `648ba36` migrated two Vim9 `try` handler cases, making the authoritative
current split 888 migrated, zero ready, and 180 pending-fix.
Commit `b4ee307` migrated two invalid Vim9 `defer` call cases, making the
authoritative current split 890 migrated, zero ready, and 178 pending-fix.
Commit `ed2cbef` migrated empty Vim9 `elseif` and legacy-function header cases,
making the authoritative current split 892 migrated, zero ready, and 176
pending-fix.

For editor recovery, `eece91f` intentionally keeps an invalid first
`:vim9script` command in Vim9 dialect after reporting E475 or E983. Vim itself
returns before switching execution state, but treating the remainder as legacy
would misparse the user's clearly declared file language while they edit the
argument. Commit `5f30da1` applies the same intent to a misplaced top-level
`:vim9script`: E1039 remains the primary diagnostic while following physical
lines use Vim9 syntax. A shortened Vim9 command is retained by canonical name,
but its invalid same-line argument tail stays opaque so detail parsing cannot
add a secondary diagnostic.

## Syntax rule groups

Case sets use `file:{line:offset/context,...}` notation. Expanding the shared
file prefix produces the exact corpus keys.

| Group ID | Status | Exact cases | Official result | Baseline result | Vim evidence | Implementation location |
| --- | --- | --- | --- | --- | --- | --- |
| `vim9-vim9script-arguments` | `migrated` (`c340e30`) | `test_vim9_import.vim:{2972:73136/script,2978:73280/script,2984:73418/script}` | E475, E983, E475 | no diagnostic | `runtime/doc/repeat.txt:416-424`; `src/vim9script.c:103-132` | `internal/syntax/scanner.go` prologue state |
| `vim9-xfile-missing-backtick` | `migrated` (`1e10f55`) | `test_vim9_cmd.vim:{267:6525/def}` | E1083 | no diagnostic | `runtime/doc/editing.txt:418-460`; `src/ex_cmds.h:33,66-67,538-540`; `src/vim9cmds.c:2311-2431` | command metadata generation and `internal/syntax/vim9_command.go` |
| `vim9-dictionary-missing-value` | `migrated` (`2329653`) | `test_vim9_expr.vim:{3265:97250/def,3266:97290/script,3274:97438/def,3274:97438/vim9-script}` | E723 in `def`, E15 at script level | `vimls/missing-expression` | `runtime/doc/eval.txt:761-790`; `runtime/doc/vim9.txt:70-82`; `src/vim9expr.c:1912-2076` | `internal/syntax/expression.go` plus context mapping in `scanner.go` |
| `vim9-assignment-missing-rhs` | `migrated` (`3bd2d68`) | `test_vim9_assign.vim:{515:12614/def,581:14074/def}` | E1097 | `vimls/missing-expression` | `runtime/doc/vim9.txt:786-817,903-910`; `src/vim9compile.c:918-945,3190-3238` | command-expression assignment and declaration paths in `scanner.go` |
| `vim9-assignment-trailing-paren` | `migrated` (`3bd2d68`) | `test_vim9_assign.vim:{1202:28637/def}` | E488 | `vimls/trailing-expression` | `runtime/doc/message.txt:752-757`; `src/vim9compile.c:3750-3810,4312-4320` | assignment recognition in `scanner.go` |
| `vim9-hash-comment-spacing` | `migrated` (`f3e1093`) | `test_vim9_script.vim:{3579:76110/def,3599:76552/def,3635:77305/def,3937:84202/script,4091:87356/script,4098:87482/script,4112:87742/script}` | E488 | `vimls/missing-expression` or `vimls/trailing-expression` | `runtime/doc/vim9.txt:206-230`; `src/ex_docmd.c:5959-5971` | expression-list recovery in `vim9_command.go` and diagnostic mapping in `scanner.go` |
| `vim9-for-header-ready` | `migrated` (`55169af`) | `test_vim9_script.vim:{3067:64156/vim9-script,3068:64217/vim9-script,3070:64343/vim9-script,3071:64405/def,3071:64405/vim9-script}` | E690 | E690 | `src/vim9cmds.c:941-983` | official failure matrix only |
| `vim9-delfunction-comment-ready` | `migrated` (`55169af`) | `test_vim9_script.vim:{3926:83957/script}` | E488 | E488 | `src/userfunc.c:6260-6282` | official failure matrix only |
| `vim9-mark-range-ready` | `migrated` (`55169af`) | `test_vim9_script.vim:{4851:108739/def,4851:108739/vim9-script}` | E481 | E481 | `src/vim9script.c:145-164` | official failure matrix only |

These 9 groups account for 26 syntax variants already verified before the
full source-file partition. Source-group reports may add groups, but must not
duplicate these exact keys.

### Group A inventory: `test_vim9_expr.vim`

Selectors below use `Ecode:call-lines`; each call line expands to every matching
artifact context carrying that code. This accounts for all 325 syntax variants:
325 migrated and zero pending-fix.

| Group ID | Codes | Variants | Migrated | Ready | Pending-fix |
| --- | --- | ---: | ---: | ---: | ---: |
| `expr-incomplete-delimiter` | E15, E109, E1002, E107, E1097, E110, E1104, E111, E114, E115 | 84 | 84 | 0 | 0 |
| `expr-operator-whitespace` | E1004, E1068, E1069 | 157 | 157 | 0 | 0 |
| `expr-operator-structure` | E260, E274, E1123, E1127, E1139, E1171 | 13 | 13 | 0 | 0 |
| `expr-list-delimiter` | E696, E697 | 10 | 10 | 0 | 0 |
| `expr-dict-delimiter` | E720, E722, E723 | 23 | 23 | 0 | 0 |
| `expr-heredoc-end` | E1145 | 2 | 2 | 0 | 0 |
| `expr-literal-register` | E354, E973 | 12 | 12 | 0 | 0 |
| `expr-command-boundary` | E476, E488, E492 | 13 | 13 | 0 | 0 |
| `expr-comment-token` | E1170 | 11 | 11 | 0 | 0 |

```text
expr-incomplete-delimiter
  E15: 199,201,681,985,1720,1721,1722,1723,1874,1894,1899,1904,2142,2345,2601,3266,3362,3610,3611,3743,3776,3781,3786,3791,3796,3801,4023,4311
  E1002: 3362,3610,3611,4162
  E107: 3862,4171,4479
  E109: 169
  E1097: 198,200,680,984,2141,2343,2600,2602,3743,4153,4310,4321
  E110: 3744,4154,4495
  E1104: 2347
  E111: 2603,4322,4328
  E114: 2462,2473
  E115: 2463,2464

expr-operator-whitespace
  E1004: 111,116,121,126,131,172,173,174,180,183,184,185,191,520,661,666,671,705,804,809,814,827,1654,1659,1664,1669,1674,1679,1701,1702,1703,1706,1707,1708,1711,1712,1713,1716,1717,1718,1910,1914,1919,1924,1929,1934,1941,1977,2209,2214,2219,2624,2626,2627,2772,2773,2774,2944,2945,2946
  E1068: 2346,2595,2665,3130,3131,3132,3217,3227,3867
  E1069: 2594,2660,2779,2952,3129,3133,3207,3212,3222,4489

expr-operator-structure
  E260: 4190
  E274: 4480
  E1123: 3866
  E1127: 2617,4475
  E1139: 3252,3260
  E1171: 2855,2856

expr-list-delimiter
  E696: 2794,2795,2967,4166,4193
  E697: 4165,4192

expr-dict-delimiter
  E720: 2791,2965,3135
  E722: 2790,2964,3136,4196,4199
  E723: 3137,3138,3156,3157,3265,3274,4195,4198

expr-heredoc-end
  E1145: 2863,2864

expr-literal-register
  E354: 3637,3638,3639,3640,4163
  E973: 2445

expr-command-boundary
  E476: 4487
  E488: 2835,3161,3162,4170,4190,4484,4485
  E492: 4487

expr-comment-token
  E1170: 3122,3123,3124,3125,3127,4201
```

Authority: `src/testdir/test_vim9_expr.vim`, `src/vim9expr.c`,
`src/list.c`, `src/register.c`, `src/vim9compile.c`, `src/typval.c`,
`src/vim9cmds.c`, `src/ex_docmd.c`, `src/eval.c`, and `src/blob.c`.
Implementation paths are `internal/syntax/expression.go`, `scanner.go`,
`vim9_command.go`, and `syntax_command.go`.

Commit `83781bd` migrated `expr-list-delimiter`. For Vim9 compiled Lists,
`src/vim9expr.c:1610-1642` distinguishes a missing comma (E696) from a missing
closing bracket after a comma (E697); the script evaluator follows
`src/list.c:1728-1767`, and `src/errors.h:1793-1796` defines both messages.

Commit `cbe7267` completed `expr-literal-register`. Blob bytes follow
`src/typval.c:2386-2478`, including E973 for an incomplete hex pair.
Register reads follow `src/register.c:177-218`; assignment destinations use the
stricter `src/vim9compile.c:1480-1487,1499-1575`, which rejects the read-only
registers `.`, `%`, `:`, and `~` but maps `@@` to the unnamed register.

Commit `2329653` migrated the four missing Dictionary-value variants. The AST
retains the key and a zero-width missing-value node, reports E723 in a compiled
`def` and E15 at Vim9 script level, then resumes parsing at the next line.

Commit `191fadb` completed `expr-dict-delimiter`. Missing closing braces retain
the partial Dictionary AST and report one E723 without adding a redundant outer
parenthesis diagnostic; command-start Dictionaries remain expressions rather
than being mistaken for Vim9 scope blocks.

Commit `91d2277` migrated `test_vim9_expr.vim:4190:121912` in both contexts.
A line-ending `->` retains the left expression, operator, and zero-width
missing callable without consuming the next physical line; Vim9 script reports
E260, while the compiled `def` context reports E488.

Commit `aedd462` migrated the four `4484:129910` and `4485:129977`
`expr-command-boundary` variants. Non-delimited `is2` and `isnot2` remain
opaque trailing bytes after the string initializer, report E488 at their first
byte, and recover at the next physical declaration.

Commit `f036019` migrated `test_vim9_expr.vim:4480:129784` in both contexts.
Whitespace between the parenthesized method callable and its argument list
reports E274 while preserving the legal method-call prefix and recovering at
the next physical command.

Commit `a9e5971` migrated the four `3252:96997` and `3260:97154` variants.
A computed Dictionary key missing its closing bracket reports one E1139,
retains the valid key/value prefix and incomplete key in the AST, and recovers
without swallowing its enclosing function or the next physical command.

Commit `21333d9` migrated `test_vim9_expr.vim:3866:112596/def`. The missing
comma leaves both call arguments in the AST, reports E1123 in a compiled
function and E116 at Vim9 script level, and recovers at the next declaration.

Commit `63cc8a9` migrated both contexts of
`test_vim9_expr.vim:2617:78496` and the compiled-function context of
`4475:129586`. A dot without an adjacent member name retains an incomplete
member AST and reports E1127 inside `def`; the script-level call reports E116,
matching Vim, and parsing resumes at the next physical command.

Commit `7f60a4d` completed `expr-operator-structure` with
`test_vim9_expr.vim:{2855:84948/def,2856:85018/script}`. An inline function
missing `}` retains its typed lambda, command body, and incomplete block AST,
reports one E1171, and recovers before the surrounding function terminators or
the next physical command.

Commit `9024ed1` completed `expr-comment-token`. Vim9 `#{` now reports one
E1170 without being parsed as a legacy Dictionary, while preserving any valid
expression prefix and recovering at the next physical command. Fold comments
starting with `#{{` remain valid, and legacy Dictionary syntax is unchanged.

Commit `a3d92d2` migrated the compiled `def` contexts of
`test_vim9_expr.vim:{3161:94986,3162:95074}`. Member keys containing `#` or
`:` retain their complete member AST; compiled functions report E488 at the
invalid suffix, while the paired script contexts remain free of syntax
diagnostics so runtime analysis can account for E716.

The command-boundary review reclassified three records as runtime rather than
syntax. `2402:71062/script` fails only after `eval()` decodes and evaluates a
string containing a newline. `3145:94088/vim9-script` and
`4188:121761/vim9-script` depend on the value before the member operator; the
same source shape succeeds for a Dictionary. The syntax parser must not execute
strings or invent value types merely to reproduce those runtime E488 codes.

Commit `bca737c` migrated `test_vim9_expr.vim:4487:130048/vim9-script`.
Whitespace before the argument parenthesis keeps `CallMe` in the Ex-command
path: Vim9 script reports E492 on the command name, while the already-migrated
compiled context retains E476 and both recover at the next physical command.

Commit `a05b752` completed `expr-command-boundary` with both contexts of
`test_vim9_expr.vim:2835:84508`. Vim9 requires an inline-function opening `{`
to end its physical line except for a comment. A same-line command now reports
E488 at that payload, remains outside `LambdaBody`, and does not swallow the
following physical command.

Commit `b5cf4fb` completed `expr-heredoc-end` with
`test_vim9_expr.vim:{2863:85195/def,2864:85270/script}`. When an unfinished
inline function contains an unfinished heredoc, the outer declaration owns one
E1145 rather than leaking the nested E990 and an additional E1171. The lambda,
body command, incomplete heredoc, and following physical commands remain in
the AST.

Reconciliation against the exact expected map corrected two pre-existing row
counts without changing the Group A total. `4328:125740/vim9-script` was
already migrated as E111, so before the next batch `expr-incomplete-delimiter`
had 43 migrated and 41 pending variants. At that checkpoint,
`4196:122230/script` was the one pending `expr-dict-delimiter` variant, so that
row had 22 migrated and one pending.

Commit `0f70449` migrated the 12 `expr-incomplete-delimiter` variants at call
lines 3776, 3781, 3786, 3791, 3796, and 3801. The existing expression parser
already reports one E15 for each invalid `++`, `--`, and mixed or separated
unary-sign chain in both compiled and script contexts, so no production change
or duplicate fixture was needed.

Commit `b50bd90` migrated the four `expr-operator-whitespace` variants at call
lines 3867 and 4489. Vim9 call arguments now report E1068 for whitespace before
a comma and E1069 when whitespace is missing after it, while retaining the
complete call AST. Legacy calls keep their permissive spacing behavior.

Commit `07be705` migrated the eight List variants at call lines 2594, 2595,
2660, and 2665. List commas now use the same Vim9 E1068/E1069 rules while
preserving every parsed item; legacy List spacing remains unchanged. The four
lambda-parameter variants were deliberately left for their separate parser
path rather than coupling two roots in one change.

Commit `0814cad` migrated those four lambda-parameter variants at call lines
2779 and 2952. The parameter parser now reports E1069 at a comma without
following whitespace while retaining both parameters, the arrow and body, and
the enclosing call. A trailing comma is not misclassified as this spacing
error, and legacy lambdas remain unchanged.

Commit `81f9860` migrated the 20 Dictionary variants at call lines 3129-3133,
3207, 3212, 3217, 3222, and 3227. Vim9 Dictionary colons and commas now report
the first E1068/E1069 spacing violation while retaining all parseable key/value
children and following commands. Legacy Dictionary spacing and the independent
Vim9 `#{` error path remain unchanged.

Commit `d29ae92` migrated both ternary variants at call line 111. Existing
Vim9 ternary validation already reported one E1004 on the unspaced `?` while
retaining all three operands and recovery, so no production change or duplicate
fixture was needed.

Commit `84326e2` completed `expr-operator-whitespace` with the 12 lambda-arrow
variants at call lines 2772-2774 and 2944-2946. An unspaced `=>` now reports
one E1004 at the arrow while retaining the full lambda and enclosing expression
AST. Valid expression/block lambdas, incomplete bodies, and legacy lambdas keep
their prior behavior.

Commit `9bc420c` migrated the seven unterminated string variants at call lines
2462-2464 and 2473. Ordinary and interpolated strings retain partial AST nodes
and report E114/E115 from their opening token without swallowing the next
command. Because Vim's failure helpers stop at the first compilation error
while loose editor parsing continues, the official failure gate now requires
the first error to match exactly and permits only ordered, non-overlapping
`vim/E*` diagnostics on later physical lines; same-line cascades still fail.

Commit `d8d82f7` migrated the four E107 variants at call lines 3862 and 4479.
The method-arrow parser now accepts scoped callable names such as `g:Echo`,
reports the missing argument list at the line end, retains both callable and
receiver nodes, and resumes at the next physical command. Parenthesized lambda
callables keep Vim's distinct `Missing parentheses: lambda` message.

Commit `7750739` migrated the six invalid-atom variants at call lines 3362,
3610, and 3611. Repeated bare dollar sigils form one malformed token, and an
invalid `.#` member tail retains its valid receiver plus a missing member node.
The parser reports E1002 in a compiled `def` and E15 at Vim9 script level,
keeps the malformed remainder opaque, and resumes on the next physical line.

Commit `e23b1f2` migrated the three closing-delimiter variants at call lines
3744, 4328, and 4495. A completed call or parenthesized expression keeps E110
for a missing `)`, while a completed slice followed by another statement keeps
E111 instead of being flattened to def-level E1097. The following statement
remains a separate recovered command and is never consumed as continuation.

Commit `ca95465` migrated the eight missing-operand variants at call lines 198,
199, 2600, 2601, 3743, 4310, and 4311. Ternary, index, slice, and
parenthesized expressions retain an explicit missing operand, report one E1097
inside a compiled `def` or E15 at Vim9 script level, and recover at the next
physical statement. Completed expressions continue to use their delimiter- or
ternary-specific diagnostics, and legacy expression recovery is unchanged.

Commit `e3883d6` completed `expr-incomplete-delimiter` with
`test_vim9_expr.vim:4023:116670/def`. The expression parser recognizes an
adjacent identifier-call tail after a method/index chain, retains the valid
method prefix, records the malformed remainder as one opaque missing node, and
reports E15 without parsing the rest of that physical line. Valid indexed and
qualified method call ASTs and legacy parsing remain unchanged.

Commit `e5173fe` completed `expr-dict-delimiter` by migrating the already-ready
`test_vim9_expr.vim:4196:122230/script` E722 assertion. No production change
was needed; the partial Dictionary already retained its AST and emitted one
official diagnostic.

The baseline expected map has 122 Group A keys, all with a unique official
syntax code. `src/errors.h:269` defines E109 for the missing colon used by call
line 169, and `src/errors.h:2647` defines E1004 for the whitespace message
passed through `msg` by the listed call lines. Both helpers run those cases in
`/def` and `/vim9-script`, so all 122 keys are verified syntax migrations.
Group A has no expected-map cleanup entries.

### Group B inventory: declarations, classes, and small files

Aliases are `A=test_vim9_assign.vim`, `C=test_vim9_class.vim`,
`I=test_vim9_interface.vim`, `L=test_let.vim`, `T=test_trycatch.vim`, and
`U=test_usercommands.vim`. A trailing `*` marks a baseline expected-map key.

| Group ID | Variants | Migrated | Ready | Pending-fix |
| --- | ---: | ---: | ---: | ---: |
| `B-assign-spacing` | 19 | 19 | 0 | 0 |
| `B-type-delimiter` | 26 | 26 | 0 | 0 |
| `B-declaration-shape` | 15 | 15 | 0 | 0 |
| `B-command-abbreviation` | 10 | 10 | 0 | 0 |
| `B-register-declaration` | 15 | 15 | 0 | 0 |
| `B-assignment-shape` | 3 | 3 | 0 | 0 |
| `B-incomplete-expression` | 2 | 2 | 0 | 0 |
| `B-dialect-declaration` | 4 | 4 | 0 | 0 |
| `B-dot-member-delimiter` | 6 | 6 | 0 | 0 |
| `B-brace-recovery` | 15 | 15 | 0 | 0 |
| `B-heredoc` | 2 | 2 | 0 | 0 |
| `B-hash-comment` | 1 | 1 | 0 | 0 |
| `B-user-command-arguments` | 2 | 2 | 0 | 0 |
| `B-class-modifier` | 8 | 8 | 0 | 0 |
| `B-class-variable` | 6 | 6 | 0 | 0 |
| `B-class-body-command` | 15 | 15 | 0 | 0 |
| `B-interface-dialect-body` | 3 | 3 | 0 | 0 |
| `B-new-static-abstract` | 8 | 8 | 0 | 0 |
| `B-implements` | 2 | 2 | 0 | 0 |
| `B-class-interface-scope` | 2 | 2 | 0 | 0 |
| `B-trailing-command` | 5 | 5 | 0 | 0 |
| `B-trailing-characters` | 20 | 20 | 0 | 0 |
| `B-structural-block` | 2 | 2 | 0 | 0 |
| **Total** | **191** | **191** | **0** | **0** |

This table reflects the current parser through `162eb64`. Revalidation corrected
the original `B-new-static-abstract` ready count: only
`C:5957:132958/script` was ready; `C:5937:132470/script` and
`C:5947:132721/script` both had recovery diagnostics. Separately, `05d176c`
made `C:{5977:133493,5987:133759}/script` ready, and `55169af` migrated all
three current cases.

Commit `3bd2d68` migrated `B-incomplete-expression` and the
`A:1202:28637/def` trailing-character case. Missing RHS expressions remain as
zero-width AST nodes with E1097, while the stray `)` retains its exact span and
maps to E488.

Commit `a56cd10` completed `B-interface-dialect-body` with
`I:71:1672/script`. An interface method signature remains a direct aggregate
member, while an illegal body reports one E1345 on its first command and stays
in recovery through `enddef`; `endinterface` and the following top-level
command remain intact. Commit `c05b9ae` completed `B-hash-comment` and migrated
the already-ready `A:3123:76907/script` E488 trailing-character case without a
production change. Commit `37eec08` completed `B-dialect-declaration`: legacy
`:var`, Vim9 `final` without a value, and Vim9 `:let` now report E1124, E1125,
and E1126 respectively while retaining declaration AST and dialect recovery.
Commits `51ee4ea` and `89a557d` completed `B-register-declaration`. Register
targets remain in the declaration AST; Vim9 scripts report E1066, while a
compiled `def` preserves Vim's E354 priority for read-only register names.

`A:3507:89038/script` (E1411) was removed from the syntax inventory. Vim emits
it in `compile_load_lhs_with_index()` only after resolving `o` as an object;
the same `o += 4` syntax is valid when `o` is a number. It belongs to type
analysis, while syntax retains the complete compound-assignment AST.

Commit `efb397e` migrated `C:2848:63764/script`. A missing member in
`super.()` retains the dot, missing-member AST, and malformed call span while
reporting one E1127; the same-line tail no longer cascades to E488, and the
enclosing class/function terminators remain recoverable.

Commit `d3d86b1` migrated `C:{409:10761,1929:44717}/script`. Whitespace after
a member dot on the same physical line reports E1202 over the whitespace while
retaining the member and any following call AST. Cross-line loose recovery and
legacy member access keep their existing behavior.

Commit `52e3f5d` migrated both contexts of `A:3039:74843`. Whitespace after a
Vim9 `++` or `--` command reports E1202 over the whitespace while retaining the
target and unary-expression AST. Adjacent operators and legacy commands remain
unchanged.

Commit `9727226` completed `B-class-body-command`. A command that is not a
valid direct Vim9 class member now reports E1318 while retaining any expression
AST. Bare method declarations and invalid abstract-method bodies recover through
their `enddef`; ordinary invalid lines resume at the next physical command, and
valid members, modifiers, method bodies, and class terminators keep their prior
behavior.

Commit `7fa8066` completed `B-new-static-abstract`. A `new` or `_new`
constructor with a return type reports E1365 while retaining the function
signature and return-type AST. Invalid `static` ordering reports E1368, and an
`abstract` modifier not followed by `def` reports E1371. The invalid member
command remains available to later analysis, while secondary diagnostics from
the same physical command are suppressed without affecting the following
class member.

Commit `ae0e2d3` completed `B-type-delimiter`. Vim9 declaration whitespace
before a type colon reports E1059, missing whitespace after it reports E1069,
and an unterminated `func(...)` type reports E110. Single-letter `a` and `l`
declarations retain separate name and type spans instead of being mistaken for
legacy scope prefixes. Heredoc headers are checked before their bodies become
opaque, so the diagnostic remains line-local while the heredoc and following
commands are preserved.

Commit `dbb1db6` completed `B-declaration-shape`. A non-class `const` with no
initializer reports E1021 even when it has a type. A Vim9 `var` with neither
type nor initializer reports E1022; direct class/interface `var`, `final`, and
`const` members use the same E1022 shape rule, while non-class `final` keeps
E1125 and invalid aggregate names keep E1317/E1329 priority. Malformed
`final/const def` members enter a bounded class-method recovery state through
their `enddef`, so the declaration AST, class terminator, and following command
remain intact without a secondary E1318.

Commit `4748011` completed `B-brace-recovery`. Eval heredocs now retain their
interpolation expression AST: E1278 reports an unmatched closing brace, E1279
reports an unclosed interpolation, and the `L:803` plus
`A:3284/{def|vim9-script}` cases use E15 for an empty interpolation. Ordinary
heredocs remain opaque. Recovery stops at the current physical line and covers
normal markers, command-block closure, missing-marker recovery, and EOF without
parsing the same body twice.
Commit `a1d50fc` handled the separate `C:434:11328/script` missing
object-method call argument (`a.Foo(,)`) in the call-argument parser. It reports
E15 while retaining an `ExpressionMissing` argument and the complete call AST;
the similar `C:422:11065/script` missing-member case remains a distinct,
already-migrated rule.

Commit `1ac1cef` completed `B-assignment-shape` and
`B-dot-member-delimiter`. An invalid semicolon destructuring target reports
E1080 without rejecting valid multi-item list destructuring. Vim9 declaration
member and index targets report E1087 while retaining their expression-shaped
target and initializer AST. `super .ToString()` reports E1356 from the current
expression position, while retaining the member, call, concatenation tail, and
enclosing method; no file-wide text search is used.

Commit `a535cff` completed `B-class-interface-scope`. A Vim9 `class` or
`interface` nested anywhere inside a `def` or legacy `function` reports E1429
or E1436 from the existing block-parent chain. The nested aggregate AST and
block are still built, so its terminator, the outer function terminator, and
following commands remain available after recovery.

Commit `87b7f8a` migrated the two aggregate closer mismatches in
`B-trailing-command`. An `endinterface` while a class is active, or `endclass`
while an interface is active, reports E476 with the expected closer. The active
aggregate is retained and its eventual missing-end cascade is suppressed.

Commit `1b7b04f` migrated the two invalid `abstract` headers in
`B-trailing-command`. An unknown command word after the Vim9 `abstract`
modifier reports E475 over the complete invalid argument. A recovery-only
class block pairs the following `endclass` without adding modifier or
missing-end cascades.

Commit `4f818a5` completed `B-trailing-command`. A top-level Vim9 environment
declaration now reports E475 while retaining the `$`-prefixed declaration name
and parsed type. The compiled-def E1016 variant remains outside this parser
group and is not silently reclassified.

Commit `162eb64` completed `B-trailing-characters`. Direct class/interface
member declarations no longer split at a top-level bar: E488 covers the bar
and same-line tail, the tail stays opaque, and the next physical line recovers.
The main command stream carries the current aggregate-member context in one
pass; embedded scanners and method bodies retain their existing behavior.
Commit `ffa8e9b`
migrated the typed assignment `A:3105`: the parser retains its name, type,
assignment target/operator, and RHS AST while E488 spans the official
`: number = 20` tail. Commit `c608781` migrated both `A:2923`
declaration type-tail variants by contextually mapping `vimls/trailing-type`
to E488 while retaining the parsed type. Commit `5930c8d` migrated `A:2309` by
diagnosing a numeric first
byte only in Vim9 `var`/`const`/`final` declaration context while retaining its
name and parsed type. Commit `258f7ef` migrated the two
heredoc headers `A:{2052,2053}`: their recognized marker and heredoc body remain
attached to the command while ordinary RHS parsing is suppressed, preventing
same-line diagnostic cascades. Commit `2bdaaf2` migrated the distinct aggregate
terminator cases `C:{68,76}` and
`I:{111,324}` by diagnosing before separator recursion, leaving the same-line
tail opaque while retaining the closer for block pairing. Commit `97fbf9a` migrated
the separate `C:11742` type path: top-level `object<any,any>` maps its second
type argument tail to E488 while retaining the complete generic type AST; the
same form inside a `def` keeps E1009. Each remaining path preserves the
successfully parsed prefix and recovers at the next logical command.

```text
B-assign-spacing
  A:1551:36793/def*,1552:36842/def*,1553:36892/def*,1555:36943/script*,1557:37089/script*,1558:37156/script*,1559:37223/script*,1561:37400/script*
  A:2096:53307/{def*|vim9-script*},2102:53491/{def*|vim9-script*},2108:53673/{def*|vim9-script*},2708:67708/def*,2712:67795/def*,2716:67883/def*
  C:1907:44214/script*,1914:44393/script*

B-type-delimiter
  E1008 C:11721:265332/{def*|vim9-script*}
  E1009 A:1633:41010/def*,1636:41143/def*; C:11731:265708/{def*|vim9-script*},11736:265882/def*
  E1059 A:385:9704/{def|vim9-script}; C:261:7035/script
  E1068 A:1632:40950/def*; C:11726:265505/{def*|vim9-script*}
  E1069 A:69:1636/def,70:1685/def,71:1740/def,2001:51173/{def|vim9-script},2533:63718/script,2535:63760/{def|vim9-script}; C:270:7286/script
  E110 A:2323:58151/{def|vim9-script},2328:58263/{def|vim9-script}

B-declaration-shape
  E1021 A:2303:57696/script,2313:57945/def
  E1022 A:198:4982/def,1587:38586/def; C:1210:27906/script,8645:196853/script,8655:197097/script,8665:197341/script,8684:197828/script,8694:198071/script,8979:205114/script,8989:205358/script,8999:205602/script,9018:206089/script,9028:206332/script

B-command-abbreviation
  A:2318:58046/{def*|vim9-script*}; C:52:1401/script,60:1626/script,1379:31866/script,1473:34263/script,2964:66432/script; I:79:1888/script*,87:2103/script,95:2332/script

B-register-declaration
  E1066 A:1596:38991/vim9-script,1597:39061/vim9-script,1598:39131/vim9-script,1599:39201/vim9-script,1601:39289/vim9-script,1603:39368/vim9-script,1606:39447/def,1607:39494/def,1608:39543/script
  E354 A:1596:38991/def,1597:39061/def,1598:39131/def,1599:39201/def,1601:39289/def,1603:39368/def

B-assignment-shape
  E1080 A:1571:37884/def
  E1087 A:2311:57842/def,2312:57894/def

B-incomplete-expression
  A:515:12614/def,581:14074/def

B-dialect-declaration
  E1124 A:73:1858/script
  E1125 A:2239:56425/script
  E1126 A:2633:66108/{def|vim9-script}

B-dot-member-delimiter
  E1127 C:2848:63764/script
  E1202 A:3039:74843/{def|vim9-script}; C:409:10761/script,1929:44717/script
  E1356 C:11074:250574/script

B-brace-recovery
  E1026 U:1007:34285/script*
  E1128 U:1046:35079/script*
  E1278 A:3292:80843/{def|vim9-script}
  E1279 L:789:18932/script,796:19078/script; A:3270:80367/{def|vim9-script},3277:80522/{def|vim9-script}
  E15 L:803:19222/script; A:3284:80675/{def|vim9-script}; C:422:11065/script,434:11328/script

B-heredoc
  A:2063:52656/script*,2080:52938/script*

B-hash-comment
  C:307:8307/script

B-user-command-arguments
  U:328:8893/script*,334:9034/script*

B-class-modifier
  E1315 C:27:664/script*,2485:56344/script*; I:308:7096/script*,361:8375/script*,535:12343/script*,1098:23808/script*
  E1316 C:11:199/script*,2955:66130/script

B-class-variable
  E1317 C:279:7538/script*,288:7810/script*,297:8073/script*
  E1329 C:5967:133218/script*,5977:133493/script,5987:133759/script

B-class-body-command
  C:149:3907/script,158:4194/script,167:4472/script,176:4699/script,185:4917/script,194:5136/script,203:5373/script,213:5611/script,222:5896/script,231:6195/script,240:6485/script,499:12885/script,509:13117/script,2134:48889/script,5681:127073/script

B-interface-dialect-body
  E1342 I:29:581/script*
  E1344 I:59:1387/script*
  E1345 I:71:1672/script

B-new-static-abstract
  E1365 C:3611:79232/script,3629:79631/script
  E1368 C:1482:34515/script*,5937:132470/script,5947:132721/script,5957:132958/script
  E1371 C:5594:125030/script,5603:125259/script

B-implements
  I:545:12575/script*,553:12767/script*

B-class-interface-scope
  C:10103:230103/script; I:1416:30780/script

B-trailing-command
  E475 A:217:5470/vim9-script; C:35:883/script,43:1104/script
  E476 I:159:3829/script,167:4035/script

B-trailing-characters
  A:1202:28637/def,2052:52384/def,2053:52451/def,2309:57801/script,2923:72274/{def|vim9-script},3105:76410/script,3123:76907/script
  C:68:1833/script,76:2041/script,84:2250/script,93:2494/script,102:2735/script,112:2975/script,2972:66662/script,11742:266028/script
  I:103:2569/script,111:2790/script,316:7304/script*,324:7508/script

B-structural-block
  T:2019:44596/script*,2029:44739/script*
```

The baseline Group B expected-map cleanup contained seven non-syntax keys,
removed in `1aff54b`:

- name: `C:19:420/script` (E1314), `I:49:1114/script` (E1343);
- semantic: `C:1388:32121/script` (E1331), `C:2386:54166/script` (E1352),
  `I:1086:23527/script` (E1381), `I:224:5223/script` (E1350), and
  `I:237:5473/script` (E1351).

### Group C inventory: script, generics, commands, imports, and type aliases

Aliases are `S=test_vim9_script.vim`, `G=test_vim9_generics.vim`,
`C=test_vim9_cmd.vim`, `I=test_vim9_import.vim`, and
`T=test_vim9_typealias.vim`.

| Group ID | Variants | Migrated | Ready | Pending-fix |
| --- | ---: | ---: | ---: | ---: |
| `C-BLOCK` | 51 | 37 | 0 | 14 |
| `C-DECL` | 3 | 3 | 0 | 0 |
| `C-EXCMD` | 115 | 81 | 0 | 34 |
| `C-EXPR` | 19 | 11 | 0 | 8 |
| `C-GENERIC` | 109 | 60 | 0 | 49 |
| `C-IMPORT` | 14 | 11 | 0 | 3 |
| `C-MODIFIER` | 57 | 56 | 0 | 1 |
| `C-REDIR` | 1 | 1 | 0 | 0 |
| **Total** | **369** | **260** | **0** | **109** |

```text
C-EXPR
  E15 C:65:1303/script,388:9139/script; G:854:19046/script; S:2125:44736/vim9-script,3069:64279/vim9-script
  E16 S:57:1170/{def|vim9-script},2521:52593/{def|vim9-script}
  E107 S:5429:121774/script,5593:125312/script
  E109 S:2323:48121/def
  E110 S:2322:48073/def
  E114 S:2320:47969/def
  E115 C:50:1025/script; S:2321:48021/def
  E116 G:304:6695/script
  E129 S:3901:83284/script,5454:122336/script

C-BLOCK
  E170 S:1538:31411/def,1539:31464/def,3078:64889/def,3469:74060/def
  E171 C:1654:34486/def; S:1540:31527/def,2081:44033/def
  E580 S:2079:43931/def
  E581 S:2078:43891/def
  E582 S:2077:43844/def
  E583 S:2105:44408/def
  E584 S:2115:44563/def
  E586 S:3465:73868/def,3466:73912/def
  E587 S:3467:73967/def,3468:74008/def
  E588 S:3077:64847/def,3464:73824/def
  E600 S:1542:31637/def,1543:31676/def,1544:31725/def,1545:31783/def,1546:31851/def,1547:31930/def,1561:32219/def,1562:32276/def,1563:32344/def,1564:32421/def
  E602 S:1537:31369/def
  E603 S:1532:31072/def
  E606 S:1535:31245/def
  E607 S:1536:31288/def,1565:32509/def,1566:32608/def
  E690 S:3067:64156/vim9-script,3068:64217/vim9-script,3070:64343/{def|vim9-script},3071:64405/{def|vim9-script}
  E789 S:3787:80639/script
  E1025 S:386:9535/def
  E1026 S:387:9573/def
  E1032 S:1043:21458/script,1541:31577/def
  E1067 S:1534:31181/def
  E1097 S:3067:64156/def,3068:64217/def,3069:64279/def
  E1143 S:1568:32719/def,2125:44736/def

C-EXCMD
  E1065 T:55:1659/script
  E1083 C:267:6525/def
  E1100 S:2043:42762/script,2045:42875/script,2047:42988/script,2049:43102/script,2051:43216/script,2053:43330/script,2055:43444/script,4846:108652/{def|vim9-script},4856:108819/{def|vim9-script},4861:108900/{def|vim9-script},4866:108981/{def|vim9-script},4871:109064/{def|vim9-script}
  E1144 C:1500:31716/script,1508:31863/script; S:3590:76362/script,3610:76804/script,3618:76970/def,3624:77087/script,3629:77194/def,3641:77421/def,3647:77538/script,3660:77800/def,3666:77915/script,3678:78133/script,3724:79220/script,3859:82421/script,3905:83376/script,3997:85329/script,4795:107577/{def|vim9-script},4800:107662/{def|vim9-script}
  E179 C:1865:38529/script,1873:38655/script
  E182 S:3886:82965/script,3890:83060/script
  E398 T:69:2004/script,97:2698/script
  E399 S:3991:85235/script
  E402 S:3796:80846/script,3822:81544/script
  E404 S:3831:81754/script,3839:81943/script
  E406 S:3809:81195/script
  E413 S:3694:78489/script
  E416 S:3686:78303/script,3705:78798/script
  E461 I:3151:77235/script; S:3131:66338/def,3139:66506/def
  E464 C:1545:32592/{def|vim9-script}; S:5083:114089/script,5092:114275/script,5099:114475/script
  E474 S:3741:79578/script
  E475 I:2972:73136/script,2984:73418/script; S:2381:49427/def,3732:79394/script,3778:80424/script,3805:81070/script,3813:81312/script,3848:82155/script
  E476 C:2076:42938/def; G:346:7617/script; S:4836:108460/{def|vim9-script},4841:108556/{def|vim9-script},4876:109149/{def|vim9-script},4881:109297/{def|vim9-script},5550:124415/script
  E477 S:4135:88194/script
  E481 S:72:1434/{def|vim9-script},78:1542/{def|vim9-script},83:1641/{def|vim9-script},88:1738/{def|vim9-script},4851:108739/{def|vim9-script}
  E488 C:472:10765/{def|vim9-script},1903:39368/def,1904:39418/def; S:2042:42704/script,2044:42817/script,2046:42930/script,2088:44163/def,2096:44294/def,2382:49493/def,2437:50783/def,2459:51306/def,3476:74196/def,3557:75701/script,3561:75787/script,3564:75852/script,3570:75953/def,3573:76014/def,3579:76110/def,3599:76552/def,3635:77305/def,3654:77678/def,3715:79011/script,3926:83957/script,3937:84202/script,3942:84304/script,3946:84394/script,3967:84795/script,4077:87087/script,4091:87356/script,4098:87482/script,4112:87742/script,4126:88011/script; T:125:3484/script
  E492 C:22:487/script,2076:42938/vim9-script; G:296:6532/script,312:6869/script; S:4836:108460/vim9-script,4841:108556/vim9-script,4876:109149/vim9-script,4881:109297/vim9-script
  E1170 S:3541:75427/script

C-GENERIC
  E1008 G:38:743/script,46:898/script,62:1232/script,173:3893/script,181:4050/script,189:4209/script,288:6380/script,320:7038/script,381:8423/script,702:15213/script,796:17462/script,1202:27001/script,1468:31960/script,1971:42622/script,1982:42856/script,2594:57148/script,2719:60085/script,2858:63121/script,2937:65010/script,2946:65198/script,2973:65815/script,3048:67627/script,3057:67814/script,3084:68421/script
  E1009 S:195:4394/def,196:4452/def
  E1059 S:2823:58387/{def|vim9-script},3107:65719/{def|vim9-script},5621:125950/script
  E1068 G:133:3045/script,141:3216/script
  E1069 C:2057:42308/vim9-script,2071:42738/vim9-script; G:165:3725/script,205:4543/script,373:8246/script,389:8598/script,814:17961/script,1394:30506/script,2710:59881/script,2846:62880/script,2955:65397/script,2964:65613/script,3066:68011/script,3075:68222/script; T:90:2522/script
  E1315 T:83:2341/script,118:3270/script
  E1394 T:111:3055/script
  E1552 G:22:376/script,83:1769/script,2615:57599/script
  E1553 G:54:1052/script,92:2032/script,805:17703/script,2605:57388/script
  E1554 G:280:6221/script,923:20811/script,932:21066/script,941:21321/script,1077:24455/script,1089:24747/script,1101:25039/script,1706:36532/script,1720:36821/script,1734:37119/script,2087:45368/script,2099:45711/script,2105:45860/script,2226:48986/script,2244:49426/script,2254:49642/script,2354:52131/script,2364:52374/script,2374:52597/script,2683:59253/script,2692:59455/script,2701:59667/script,2810:62141/script,2822:62380/script,2834:62629/script
  E1555 G:30:565/script,256:5703/script,264:5880/script,650:14312/script,787:17195/script,914:20548/script,969:21961/script,1065:24155/script,1139:25810/script,1191:26790/script,1214:27205/script,1298:28752/script,1457:31742/script,1479:32151/script,1557:33623/script,1692:36240/script,2145:46759/script,2304:50718/script,2424:53708/script,2584:56941/script,2665:58818/script,2786:61632/script,2919:64579/script,3030:67202/script,3536:79340/script,3546:79564/script

C-IMPORT
  E1038 S:1817:37904/def
  E1039 S:1810:37557/script
  E1040 S:1811:37626/script
  E1043 I:1728:42236/script
  E1044 I:1729:42305/script
  E1047 I:531:14329/script,536:14451/script,541:14571/script
  E1060 I:512:13833/script,519:14018/script,525:14180/script
  E1257 I:603:15955/script
  E1261 I:591:15641/script
  E983 I:2978:73280/script

C-MODIFIER
  E1050 C:1202:25888/script; S:365:9084/def,366:9128/def,367:9173/def,368:9218/def,2021:42097/script
  E1082 C:1275:27368/{def|vim9-script},1280:27472/{def|vim9-script}
  E1176 C:1227:26455/{def|vim9-script},1233:26572/def,1240:26731/def,1249:26910/{def|vim9-script},1256:27030/{def|vim9-script},1263:27152/{def|vim9-script},1270:27274/{def|vim9-script}
  E1202 G:149:3384/script,157:3555/script,197:4369/script,213:4713/script,221:4889/script,229:5072/script,336:7374/script,357:7889/script,365:8067/script,397:8780/script,728:15637/script
  E1205 S:4934:110489/{def|vim9-script},4939:110638/{def|vim9-script},4944:110788/{def|vim9-script},4949:110938/{def|vim9-script},4990:111799/{def|vim9-script},4995:111946/{def|vim9-script}
  E1241 C:2062:42490/{def|vim9-script},2081:43043/{def|vim9-script}
  E1242 C:2107:43740/{def|vim9-script},2111:43834/{def|vim9-script},2128:44136/{def|vim9-script},2132:44231/{def|vim9-script}

C-DECL
  E1125 S:271:6623/def
  E1397 T:62:1839/script
  E1398 T:76:2174/script

C-REDIR
  E1185 C:1990:41128/def
```

Authority is distributed across `src/eval.c`, `src/vim9expr.c`,
`src/vim9cmds.c`, `src/vim9compile.c`, `src/vim9generics.c`,
`src/vim9class.c`, `src/ex_docmd.c`, `src/userfunc.c`, and `src/usercmd.c`.

Commit `1e10f55` migrated the `C-EXCMD` E1083 variant. The generated command
table now retains Vim's EX_XFILE/EX_FILES/EX_FILE1 property, valid filename
expansions retain each embedded expression in the command AST, and a missing
closing backtick keeps the same-line tail opaque before recovery resumes on the
next physical line.

Commit `f3e1093` migrated seven `C-EXCMD` E488 variants. A hash attached to the
preceding expression is retained as the one-byte trailing token, while the
valid expression AST remains intact and parsing resumes with the next physical
line. A hash after whitespace remains an ordinary Vim9 comment. Commit
`bc08c8c` added the three matching `execute`, `echo`, and `echomsg` official
variants whose attached hash tails were already diagnosed by that parser path.

Commit `e2f7aba` migrated the five generic-call records at
`G:{3048:67627,3057:67814,3066:68011,3075:68222,3084:68421}/script`.
Missing or empty type arguments now report E1008 or E1069 while retaining known
type arguments, explicit missing nodes, and the following call AST. An absent
outer `>` recovers only at an argument opener on the same physical line;
comparisons, legacy expressions, valid generic calls, and existing empty-list
handling remain unchanged.

Commit `c03403f` migrated the generic declaration recoveries at
`G:{54:1052,62:1232,2605:57388}/script`. When a missing `>` is followed by an
empty parameter list, the parser retains the known type parameters and reports
E1553 or E1008 at that physical-line boundary. A non-empty parameter list keeps
the existing loose `vimls/missing-generic-end` recovery instead of being
reclassified broadly.

Commit `e583448` migrated `G:2626:57847/script`. After each function signature
is mapped back from its logical-line view, a nested generic `def` checks only
the type parameters of ancestor `def` blocks. Reusing an inherited name reports
E1561 at the inner name; sibling functions, top-level functions, ordinary
parameters, and legacy functions do not share that scope.

Commit `807bd22` migrated the already-supported generic-call recoveries at
`G:{1971:42622,1982:42856}/script`. Their missing type argument reports E1008
while preserving the partial call expression and line-local recovery.

Commit `3ab05e0` migrated the 22 ready cases that required no production-code
change: 17 E1100 one-command forms in `test_vim9_script.vim`, E1069/E1008 at
`G:{2964:65613,2973:65815}/script`, both E1059 variants at
`S:3107:65719`, and E1125 at `S:271:6623/def`.

Commit `a12a767` migrated all five E1202 generic-call whitespace variants at
`G:{336:7374,357:7889,365:8067,397:8780,728:15637}/script`. Command-start
generic calls now enter the expression parser, retain their call/type-argument
AST, and report only the offending whitespace before physical-line recovery.

Commit `bf1c397` migrated both E1241 variants at `C:2062:42490`. The Vim9
global-command scanner now rejects `:`, `-`, and `.` only when they immediately
follow the one-byte `g` or `v` aliases; full command names, valid regexp
delimiters, and the existing whitespace E1242 rule remain unchanged.

Commit `46f565b` migrated the direct E1050 variants at
`S:{365:9084,366:9128,367:9173,368:9218}/def`. At a fresh Vim9 command
boundary, an Ex range without `:` now retains its range/command structure,
reports E1050, and recovers at the next physical line; existing automatic
expression continuations remain handled before command scanning.

Commit `b74d592` migrated E1261 at `I:591:15641/script` and E1257 at
`I:603:15955/script`. A statically known string import without `as` now checks
only its final path component: `.vim` requires an alias, a normal `name.vim`
remains valid, and any other suffix requires `as`; dynamic path expressions
remain conservative while the complete Import AST is retained.

Commit `ad1edd0` migrated E1043 at `I:1728:42236/script` and E1044 at
`I:1729:42305/script`. Export validation now derives Vim's exact exportable
command set from `EX_EXPORT`; non-exportable built-ins are rejected while
unknown commands keep their normal recovery, and `:function` listing or search
arguments are rejected without weakening valid exported definitions.

Commit `608ed48` migrated E182 at
`S:{3886:82965,3890:83060}/script`. A `#` attached directly to a Vim9
user-command name is now diagnosed before replacement-body parsing, while a
space-separated comment and legacy command definitions retain their existing
behavior.

Commit `74ccec2` migrated E1554 at `G:{280:6221,1706:36532}/script`. A tight
generic `<` without a closing `>` now retains an incomplete generic-reference
AST, reports the error on that physical line, and leaves the following
`enddef` or command available to ordinary recovery.

Commit `77b5e9b` migrated the same E1554 recovery with partial type tails at
`G:{1720:36821,1734:37119}/script`; the incomplete operator span retains
`<number` or `<number,` without generating a same-line diagnostic cascade.

Commit `c48d396` migrated E1185 at `C:1990:41128/def`. Block construction now
tracks an open `redir =>` or `redir =>>` in the nearest Vim9 `def`, clears it on
`redir END`, and reports the missing terminator at `enddef` without affecting
top-level or legacy redirection.

Commit `7cbd3f2` migrated E116 at `G:304:6695/script`. The retained
`ExpressionCall` and generic type arguments now make the missing call delimiter
unambiguous, so top-level Vim9 recovery maps it to E116 while ordinary missing
parentheses retain their existing E110 behavior.

Commit `70251bb` migrated E114, E115, E110, and E109 at
`S:{2320:47969,2321:48021,2322:48073,2323:48121}/def`. Invalid Vim9 condition
headers retain their partial expression AST but no longer leave a false
unclosed block; delimiter diagnostics use Vim codes and stop at the physical
line without a trailing-expression cascade. Legacy block recovery remains
unchanged.

Commit `5433450` migrated E488 at `S:4126:88011/script`. The shared
`ex_findpat()` command family now recognizes a closed `/pattern/` before
checking its tail: an attached `#` remains an invalid opaque tail, whitespace
before `#` starts a Vim9 comment, and a following bar starts the next command.

Commit `c035bd3` migrated 13 E1555 call/reference variants at
`G:{256:5703,650:14312,787:17195,969:21961,1139:25810,1214:27205,1298:28752,1479:32151,1557:33623,1692:36240,2919:64579,3030:67202,3536:79340}/script`.
An empty closed `<>` now retains an explicit missing type argument in the AST
and reports the call-site Vim diagnostic; ordinary generic declarations,
non-empty type lists, and incomplete `<` recovery retain their existing paths.

Commit `a793234` migrated `S:{1532:31072,1535:31245,1536:31288,1537:31369}/def`.
Vim9 try-block construction now reports E603/E606/E602 for unmatched
`catch`/`finally`/`endtry`, and E607 for a repeated `finally`, while retaining
the commands and recovering at later physical lines. Mismatched control-block
terminators remain separate E170/E171 work.

Commit `60be383` migrated `C:{50:1025,65:1303}/script`. Legacy expression
lexing now uses the command's effective `scriptversion`: version 2 enables a
leading-dot Float and version 4 enables apostrophe digit separators, while a
Vim9 `legacy` command at version 1 reports E15 or E115 and recovers at the next
physical line. Vim9 expressions remain independent of legacy script versions.

Commit `c8c0f44` migrated all ten E600 cases plus six E170/E171 variants in
`test_vim9_script.vim`. When `enddef` reaches an unclosed Vim9 `try`, `for`,
`while`, or `if`, block construction now emits Vim's terminator code while
retaining the incomplete block. A mismatched `endtry` reports the active
control block's missing terminator once, and invalid condition headers still
suppress that derived cascade.

Commit `ccb01cd` extends the same mapping to a do-command payload inside a
Vim9 `def`: an incomplete `windo if` reports E171 while retaining its embedded
command and block AST. The top-level incomplete-payload recovery remains the
generic `vimls/missing-end` diagnostic because it has no compiling `def`
context.

Commit `1df5bda` migrated four generic calls with a missing type (E1008) and
one call with an attached closing angle after a comma (E1069). These cases use
the existing generic-expression parser and require no production-code change.
The E1561 duplicate generic parameter case remains excluded as declaration
semantics, consistent with the Group C baseline cleanup.

Commit `a79d7f5` maps standalone Vim9 `elseif`, `else`, and `endif` to
E582/E581/E580, detects a repeated `else` as E583, and maps unmatched `endfor`
or `endwhile` to E588. Block and later-command AST recovery is unchanged. The
E584 case remains pending because line-oriented recovery correctly continues
to the following `else` and exposes a second branch error, while Vim's compile
helper stops after the first error.

Commit `9f8142c` reports E1038 when `vim9script` occurs inside a `def` or legacy
function and leaves the surrounding dialect unchanged. Top-level misplaced
`vim9script` continues to report E1039 and still switches later commands to
Vim9 for editor recovery.

Commit `ef49637` reports E481 when a Vim9 `eval`, `if`, `echo`, or `cd` command
has an Ex range. The range and command remain in the AST, and the existing
physical-line recovery makes only the invalid line tail opaque.

Commit `f84432f` maps empty Vim9 `unlet`, `lockvar`, and `unlockvar` commands
to E179. Valid targets and counts keep their existing AST, while legacy empty
commands retain the conservative internal missing-argument diagnostic.

Commit `1082d53` reports E474 for Vim9 `menutrans clear#...`, where the attached
hash is an invalid argument rather than a comment. `menutrans clear #...` and
legacy menu-translation payloads remain valid.

Commit `d8741b9` reports E477 for `func!` on a script-local Vim9 function,
retains the signature and function block, and suppresses the derived EOF
missing-end diagnostic. Explicit global `function! g:Name()` and legacy
`function!` definitions remain valid.

Commit `10f6de7` maps a standalone Vim9 `}` to E1025 and an unclosed scope `{`
to E1026. The scope block and following physical-line commands remain in the
AST during recovery.

Commit `8735abf` retains Vim's `EX_NONWHITE_OK` command-table property in the
pinned generated metadata. Commit `a7b5679` then reports E1144 when a recognized
built-in Vim9 command is followed by an illegal attached byte, while preserving
the command and making its details opaque for line recovery. Unknown and future
commands remain opaque. The external `Comd#`, locally defined `Foo3Bar`, and
name-dependent `exit_cb:` variants remain outside this parser-only batch.

Commit `648ba36` reports E1032 when a closed Vim9 `try` has neither `catch` nor
`finally`. The block closes normally, so it cannot also produce the E600 used
for a genuinely missing `endtry`; legacy `try`/`endtry` remains accepted.

The baseline Group C expected map had 79 keys: 78 syntax and one semantic key,
`G:2456:54444/script` (E1561 duplicate generic type variable). Commit
`1aff54b` removed that key from the parser-negative matrix. The only Group C
unknowns are `C:2148:44597/script` and
`S:5007:112274/{def,vim9-script}`.

### Group D inventory: tuple, function, legacy expression, blob, list/dict, enum

Group D contains 183 syntax variants: 116 migrated and 67 pending-fix. In the
exact inventory below, `M` and `P` mean those two statuses.
`test_blob.vim` has no syntax failure variants.

#### `test_expr.vim` (30)

```text
E1004 M ready: 1051:41636/{def|vim9-script},1052:41709/{def|vim9-script},1053:41782/{def|vim9-script},1054:41855/{def|vim9-script}
E1004 M recovery: 250:7987/{def|vim9-script}
E15 M mapping: 250:7987/legacy
E15 M unary-sign: 261:8317/{def|vim9-script},264:8414/{def|vim9-script}
E15 M broken-number: 770:31551/{legacy|def|vim9-script},771:31625/{legacy|def|vim9-script},772:31701/{legacy|def|vim9-script},773:31760/{legacy|def|vim9-script},774:31836/{legacy|def|vim9-script}
```

Commit `067bb55` migrated the 15 broken-number variants. The lexer retains each
malformed literal as one Number node with its complete byte span, reports E15,
and resumes parsing at the next physical line. The grammar comes from
`runtime/doc/eval.txt:1705-1741`; `src/charset.c:2341-2533` supplies the strict
alphanumeric-suffix behavior used by Vim's evaluator.

Commit `f411f1a` migrated the four adjacent-unary-sign variants. Vim9 rejects
an immediately following `+` or `-` after either unary sign; the parser reports
one E15 on that sign pair while retaining the complete recovering unary AST.
The rule is documented by `runtime/doc/vim9.txt:1207-1208,1319-1327` and
implemented by Vim's `src/eval.c:5066-5084`.

Commit `347226c` completed all 30 `test_expr.vim` syntax variants. For
`echo "\<C-">`, the AST retains the string and trailing `>` operator; legacy
maps the missing operand to E15, while Vim9 stops at the operator-spacing E1004
without a secondary diagnostic. See `runtime/doc/eval.txt:1178-1184` and
`runtime/doc/vim9.txt:932-944`.

#### `test_listdict.vim` (7)

```text
E1004 M ready: 529:14207/def
E1097 M ready: 1532:48170/def
E1127 M ready: 1521:47598/def
E15 M ready: 1521:47598/{legacy|vim9-script}
E15 P recovery: 1530:48090/{def|vim9-script}
```

#### `test_tuple.vim` (37)

```text
E1004 M ready: 138:3809/def
E1008 M ready: 62:1444/{def|vim9-script},67:1600/{def|vim9-script}
E1010 M ready: 120:3271/{def|vim9-script}
E1010 P missing: 167:4773/{def|vim9-script}
E1015 P recovery: 159:4498/def
E1068 M ready: 82:2083/{def|vim9-script},92:2394/def,97:2538/{def|vim9-script},143:3972/def,151:4245/def
E1069 M ready: 72:1754/{def|vim9-script},77:1924/{def|vim9-script},87:2239/{def|vim9-script}
E15 M mapping: 143:3972/vim9-script,151:4245/vim9-script
E15 P missing: 143:3972/legacy,151:4245/legacy
E15 P recovery: 159:4498/{legacy|vim9-script}
E1526 M ready: 112:3010/{legacy|def|vim9-script}
E1527 M ready: 104:2756/{legacy|def|vim9-script}
E1539 M ready: 127:3486/{def|vim9-script}
```

#### `test_vim9_enum.vim` (34)

```text
E1065 M ready: 60:1409/script,68:1610/script,76:1834/script
E1068 M ready: 233:5465/script,1527:34659/script,1541:34927/script
E1069 M ready: 242:5659/script
E1123 M ready: 151:3717/script,161:3943/script,172:4190/script,182:4421/script,194:4637/script,204:4829/script,252:5905/script,378:8621/script
E1170 P missing: 978:22009/script
E1315 M ready: 36:799/script
E1414 M ready: 12:230/script
E1415 M ready: 28:569/script
E1418 M ready: 288:6615/script,298:6850/script
E1419 P missing: 214:5043/script,264:6157/script
E1419 P recovery: 224:5261/script
E1420 P mapping: 84:2047/script,100:2430/script
E1435 P missing: 1706:39098/script
E488 M ready: 108:2615/script
E488 P missing: 116:2820/script
E488 P recovery: 123:3013/script
E488 M ready: 132:3226/script
E492 P mapping: 44:1002/script,52:1215/script,92:2227/script
```

#### `test_vim9_func.vim` (75)

```text
E1005 M ready: 2887:66332/def
E1006 P missing: 2123:47271/def
E1007 M ready: 2078:45879/def
E1008 M ready: 2462:55226/script,2464:55349/script,2481:55796/script
E1010 P missing: 645:13404/script,659:13755/script,660:13796/script,2486:56238/script
E1010 P recovery: 2077:45815/def
E1055 M ready: 2482:55906/script
E1057 P mapping: 408:8674/script,2466:55472/script
E1059 M ready: 2448:54929/script,2455:55077/script
E1065 M ready: 398:8465/script
E1068 M ready: 426:8986/script,434:9120/script,441:9237/script,2495:56457/script,2885:66191/def
E1068 P recovery: 781:16158/vim9-script
E1069 M ready: 1157:24812/script,2441:54782/script,2483:56005/script,2485:56148/script,2505:56703/script,2886:66262/def,2888:66507/def
E1069 P missing: 1695:37074/{def|vim9-script}
E1077 P mapping: 2484:56080/script
E110 M ready: 2086:46156/def,2087:46235/def
E1151 P mapping: 373:8000/script
E1152 P recovery: 382:8151/script
E1157 M ready: 1689:36926/{def|vim9-script}
E1160 P missing: 2034:44861/script
E1170 P missing: 73:1788/def
E1172 P missing: 1626:35394/{def|vim9-script}
E1173 P missing: 392:8331/script,1145:24549/script,2361:52851/script,2378:53272/script,2388:53495/script
E125 M ready: 948:20577/script,955:20687/script
E126 P mapping: 416:8808/script
E1267 P missing: 99:2271/script,107:2406/script,1049:22663/script,1061:22873/script,1069:23011/script,1077:23147/script
E129 P mapping: 3734:85678/script
E15 P mapping: 828:17920/def
E16 P mapping: 3817:87815/def
E476 P mapping: 4520:103771/script,1298:27807/script
E476 P missing: 4728:108040/script
E476 P recovery: 781:16158/def
E488 M ready: 971:20965/script,3746:85903/script
E488 P missing: 1755:38517/{def|vim9-script}
E488 P recovery: 1748:38346/{def|vim9-script},2403:53812/script
E492 P missing: 3889:89267/script
E720 P missing: 3539:81179/{def|vim9-script}
E884 P mapping: 3740:85793/script
```

Authority is `src/vim9expr.c`, `src/vim9type.c`, `src/vim9cmds.c`,
`src/userfunc.c`, `src/vim9class.c`, `src/tuple.c`, and `src/errors.h`.

Group D unknowns are `test_tuple.vim:1913:53484/{legacy,def,vim9-script}`
and `test_vim9_func.vim:{2407:53889/script,2413:54060/script}`; their helpers
assert runtime text rather than a unique Vim error code.

The baseline Group D expected-map cleanup contained eight keys, removed in
`1aff54b`:

- semantic: `test_vim9_enum.vim:{329:7496/script,339:7706/script}` (E1416),
  `test_vim9_enum.vim:369:8371/script` (E1417), and
  `test_listdict.vim:1532:48170/{legacy,vim9-script}` (E111);
- type: `test_vim9_func.vim:{2114:47060/script,2883:66057/def}` (E1180);
- stale mapping: `test_listdict.vim:523:14052/script` is mapped to E1004,
  while the official failure is E716; E1004 belongs to `529:14207/def`.

## Non-syntax and unknown inventory

The exact syntax membership above and exact unknown membership below define the
parser migration boundary. Every other failure variant is excluded from parser
conformance as type, name-resolution, semantic, or runtime according to the
phase-accounting table. Those excluded variants remain available in the pinned
artifact for later analysis work; they must not be reconsidered as parser tests
without correcting this ledger.

### Unknown exact membership

Group A has 130 unknown variants. The following 63 `line:offset` selectors for
`test_vim9_expr.vim` each expand to both `/def` and `/vim9-script`:

```text
512:14208,513:14282,514:14356,676:18331,677:18387,678:18444,
819:22367,820:22423,821:22480,1643:46651,1648:46774,
1725:49309,1726:49406,1727:49503,
1728:49606,1730:49710,1731:49807,1732:49905,1733:50002,
1734:50100,1735:50198,1736:50296,1737:50390,1739:50491,
1740:50591,1741:50697,1742:50790,1743:50889,1744:50981,
1746:51080,1747:51176,1748:51273,1749:51369,1750:51466,
1751:51563,1753:51661,1754:51757,1755:51854,1756:51950,
1757:52047,1758:52144,1761:52260,1762:52393,1763:52519,
1764:52642,2027:59391,2028:59451,2029:59512,2032:59630,
2033:59690,2034:59751,2037:59870,2038:59935,2039:60001,
2253:66458,2254:66518,2255:66579,2258:66697,2259:66757,
2260:66818,2263:66936,2264:66996,2265:67057
```

The remaining four Group A unknowns are
`365:10293/script`, `373:10456/script`, `403:11207/script`, and
`433:11965/script`.

Group B has 10 unknowns:

```text
test_vim9_assign.vim:1556:37009/script
test_vim9_assign.vim:1560:37304/script
test_vim9_assign.vim:1562:37482/script
test_vim9_assign.vim:1623:40371/def
test_vim9_assign.vim:1624:40474/def
test_vim9_assign.vim:1626:40579/def
test_vim9_assign.vim:1627:40687/def
test_vim9_assign.vim:1630:40858/def
test_vim9_assign.vim:2904:71923/script
test_vim9_class.vim:2231:51003/script
```

Group C has three unknowns:

```text
test_vim9_cmd.vim:2148:44597/script
test_vim9_script.vim:5007:112274/def
test_vim9_script.vim:5007:112274/vim9-script
```

Group D has five unknowns:

```text
test_tuple.vim:1913:53484/legacy
test_tuple.vim:1913:53484/def
test_tuple.vim:1913:53484/vim9-script
test_vim9_func.vim:2407:53889/script
test_vim9_func.vim:2413:54060/script
```

Together these sets contain `130 + 10 + 3 + 5 = 148` variants.

## Updating the ledger

1. Refresh the complete research partition only when the pinned Vim release,
   allowlist, or corpus generator changes.
2. Select one or more non-overlapping `ready` or `pending-fix` group IDs.
3. Run `TestOfficialVimParserFailureTriage` with the group's exact filters.
4. Implement the parser change and add only the official cases to
   `TestOfficialVimParserFailures`.
5. Run the focused official test, commit the implementation, then update the
   group status and commit in this ledger.

Parser output is deliberately not stored as a generated snapshot: triage
computes it from the current code. Vim phase classification and source evidence
are the durable research results.
