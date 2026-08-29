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
- Current parser-negative syntax assertions: 530 (`9024ed1`)

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

## Phase accounting

The four source-group reports account for every failure variant in the pinned
artifact.

| Group | Syntax | Type | Name | Semantic | Runtime | Unknown | Total |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| A | 328 | 270 | 65 | 37 | 42 | 130 | 872 |
| B | 192 | 238 | 77 | 251 | 69 | 10 | 837 |
| C | 369 | 78 | 111 | 239 | 52 | 3 | 852 |
| D | 183 | 484 | 54 | 47 | 166 | 5 | 939 |
| **Total** | **1,072** | **1,070** | **307** | **574** | **329** | **148** | **3,500** |

At baseline, the 1,072 syntax variants split into 346 migrated, 89 ready, and
637 pending-fix. The 362-entry expected map therefore contains 346 verified
syntax keys and 16 cleanup keys that are non-syntax or stale. The cleanup
membership is recorded in the source-group sections below.

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

For editor recovery, `eece91f` intentionally keeps an invalid first
`:vim9script` command in Vim9 dialect after reporting E475 or E983. Vim itself
returns before switching execution state, but treating the remainder as legacy
would misparse the user's clearly declared file language while they edit the
argument.

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
artifact context carrying that code. This accounts for all 328 syntax variants:
226 migrated and 102 pending-fix.

| Group ID | Codes | Variants | Migrated | Ready | Pending-fix |
| --- | --- | ---: | ---: | ---: | ---: |
| `expr-incomplete-delimiter` | E15, E109, E1002, E107, E1097, E110, E1104, E111, E114, E115 | 84 | 42 | 0 | 42 |
| `expr-operator-whitespace` | E1004, E1068, E1069 | 157 | 107 | 0 | 50 |
| `expr-operator-structure` | E260, E274, E1123, E1127, E1139, E1171 | 13 | 13 | 0 | 0 |
| `expr-list-delimiter` | E696, E697 | 10 | 10 | 0 | 0 |
| `expr-dict-delimiter` | E720, E722, E723 | 23 | 23 | 0 | 0 |
| `expr-heredoc-end` | E1145 | 2 | 0 | 0 | 2 |
| `expr-literal-register` | E354, E973 | 12 | 12 | 0 | 0 |
| `expr-command-boundary` | E476, E488, E492 | 16 | 8 | 0 | 8 |
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
  E488: 2402,2835,3145,3161,3162,4170,4188,4190,4484,4485
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
| `B-type-delimiter` | 26 | 10 | 0 | 16 |
| `B-declaration-shape` | 15 | 0 | 0 | 15 |
| `B-command-abbreviation` | 10 | 10 | 0 | 0 |
| `B-register-declaration` | 15 | 0 | 0 | 15 |
| `B-assignment-shape` | 3 | 0 | 0 | 3 |
| `B-incomplete-expression` | 2 | 2 | 0 | 0 |
| `B-dialect-declaration` | 4 | 0 | 0 | 4 |
| `B-dot-member-delimiter` | 7 | 0 | 0 | 7 |
| `B-brace-recovery` | 15 | 3 | 0 | 12 |
| `B-heredoc` | 2 | 2 | 0 | 0 |
| `B-hash-comment` | 1 | 0 | 0 | 1 |
| `B-user-command-arguments` | 2 | 2 | 0 | 0 |
| `B-class-modifier` | 8 | 8 | 0 | 0 |
| `B-class-variable` | 6 | 6 | 0 | 0 |
| `B-class-body-command` | 15 | 0 | 0 | 15 |
| `B-interface-dialect-body` | 3 | 2 | 0 | 1 |
| `B-new-static-abstract` | 8 | 2 | 0 | 6 |
| `B-implements` | 2 | 2 | 0 | 0 |
| `B-class-interface-scope` | 2 | 0 | 0 | 2 |
| `B-trailing-command` | 5 | 0 | 0 | 5 |
| `B-trailing-characters` | 20 | 6 | 0 | 14 |
| `B-structural-block` | 2 | 2 | 0 | 0 |
| **Total** | **192** | **76** | **0** | **116** |

This table reflects the current parser through `3bd2d68`. Revalidation corrected
the original `B-new-static-abstract` ready count: only
`C:5957:132958/script` was ready; `C:5937:132470/script` and
`C:5947:132721/script` both had recovery diagnostics. Separately, `05d176c`
made `C:{5977:133493,5987:133759}/script` ready, and `55169af` migrated all
three current cases.

Commit `3bd2d68` migrated `B-incomplete-expression` and the
`A:1202:28637/def` trailing-character case. Missing RHS expressions remain as
zero-width AST nodes with E1097, while the stray `)` retains its exact span and
maps to E488.

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
  E1411 A:3507:89038/script

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
| `C-BLOCK` | 51 | 6 | 0 | 45 |
| `C-DECL` | 3 | 2 | 0 | 1 |
| `C-EXCMD` | 115 | 30 | 0 | 85 |
| `C-EXPR` | 19 | 0 | 0 | 19 |
| `C-GENERIC` | 109 | 23 | 0 | 86 |
| `C-IMPORT` | 14 | 6 | 0 | 8 |
| `C-MODIFIER` | 57 | 45 | 0 | 12 |
| `C-REDIR` | 1 | 0 | 0 | 1 |
| **Total** | **369** | **112** | **0** | **257** |

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
line. A hash after whitespace remains an ordinary Vim9 comment.

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
