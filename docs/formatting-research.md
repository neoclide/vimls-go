# Vim script Formatting research

This document defines the recommended design for LSP document formatting in
vimls-go. The behavior baseline is Vim **v9.2.1015**. Earlier supported Vim 9.1
and 9.2 patches remain compatibility targets; syntax added after v9.2.1015 is
outside the current language contract.

Status: implemented for document and range indentation Formatting.

## Decision

The first Formatting implementation should be a **source-preserving indentation
formatter** for both Legacy Vim script and Vim9 script:

- expose `textDocument/formatting` and `textDocument/rangeFormatting`;
- compute indentation from the existing dialect-aware syntax tree and physical
  source spans;
- return small, line-local edits that replace leading whitespace only;
- preserve every other source byte, including command spelling, spaces inside
  expressions, comments, continuations and line endings;
- leave a line unchanged whenever its structure or whitespace ownership is not
  proven;
- never start Vim, source runtime scripts or execute the document in the
  language-server process.

This deliberately gives Formatting the same responsibility as Vim's `=`
operator: indentation, not general pretty-printing. Operator spacing, command
canonicalization, line wrapping and expression reflow should not be included in
the initial feature.

## Why indentation is the correct baseline

Vim itself does not provide a general Vim script pretty-printer. Its official
filetype support installs an [`indentexpr`](https://github.com/vim/vim/blob/v9.2.1015/runtime/indent/vim.vim),
implemented by [`vimindent.vim`](https://github.com/vim/vim/blob/v9.2.1015/runtime/autoload/dist/vimindent.vim).
The expression returns the desired indentation column for one line. It powers
the `=` operator and insert-mode indentation; it does not rewrite the rest of a
line. Vim's [`indent.txt`](https://github.com/vim/vim/blob/v9.2.1015/runtime/doc/indent.txt)
and [`options.txt`](https://github.com/vim/vim/blob/v9.2.1015/runtime/doc/options.txt)
make this distinction explicit.

The official implementation is also not directly reusable by a language
server. It reads mutable buffer state, syntax highlighting, search state and
buffer-local options through functions such as `getline()`, `indent()`,
`searchpair()` and `synstack()`. Running it would require starting Vim and
loading a document into an editor process. A line-for-line Go port would copy a
second regex parser into the server and discard the structural knowledge that
vimls-go already has.

Vim's own [indent test harness](https://github.com/vim/vim/blob/v9.2.1015/runtime/indent/testdir/runtest.vim)
is still the best behavioral oracle. It applies `=` to source fixtures and
compares them with expected files for Legacy
[`vim.in`](https://github.com/vim/vim/blob/v9.2.1015/runtime/indent/testdir/vim.in)/[`vim.ok`](https://github.com/vim/vim/blob/v9.2.1015/runtime/indent/testdir/vim.ok)
and Vim9
[`vim9.in`](https://github.com/vim/vim/blob/v9.2.1015/runtime/indent/testdir/vim9.in)/[`vim9.ok`](https://github.com/vim/vim/blob/v9.2.1015/runtime/indent/testdir/vim9.ok).
Those fixtures cover both dialects, nested Ex commands, functions, bracket
blocks, continuations, comments and heredocs.

## Meaning of Formatting in vimls-go

### Included in the first version

| Input | Required behavior |
| --- | --- |
| Whole document or range | Use the complete document for syntax context, then return only safe indentation edits requested by the operation. |
| Legacy and Vim9 files | Format both dialects from the same request path while respecting each command's contextual dialect. |
| Named blocks | Indent bodies and align branch/end commands with their owning block. |
| Function and `def` bodies | Indent bodies and multiline declarations when the parser has complete ownership. |
| Bracketed expressions | Indent continued list, dictionary, call, subscript and parenthesized lines; align closing delimiters. |
| Legacy continuations | Recognize the leading `\` on the following physical line and preserve it while changing only preceding indentation. |
| Vim9 continuations | Handle operators, method/member chains and other parser-recognized continuations without rewriting expression text. |
| Ordinary comments | Follow the surrounding structural indentation when the comment is not part of an opaque payload. |
| Mixed dialect commands | Respect `vim9script`, `scriptversion`, `vim9cmd` and `legacy` as represented by the parser. |
| Incomplete input | Format only independently proven lines and preserve the rest. |

General continuation indentation should initially match Vim's documented
default: three indentation levels. Bracket interiors use one additional level,
without Vim's optional `more_in_bracket_block` extra level. This keeps the
default deterministic without adding server settings before a second real
style is required.

### Explicitly excluded

- spaces around operators, commas, colons or assignment;
- quote selection or string escaping;
- expanding command abbreviations or changing command case;
- joining, splitting or wrapping lines;
- rewriting `|`, comments or continuation markers;
- sorting declarations, imports, dictionaries or commands;
- formatting embedded Python, Ruby, Perl, Lua, shell or other languages;
- `textDocument/onTypeFormatting`;
- project-specific style configuration beyond the standard LSP indentation
  options.

The exclusions are semantic safeguards, not merely missing style choices.
Whitespace can change Vim script meaning. Trailing whitespace is part of some
mapping definitions, as documented by [`map-trailing-white`](https://github.com/vim/vim/blob/v9.2.1015/runtime/doc/map.txt),
and heredoc indentation has `trim`-dependent semantics described in
[`eval.txt`](https://github.com/vim/vim/blob/v9.2.1015/runtime/doc/eval.txt).

## Source regions that must be preserved

Formatting should first classify physical source spans by ownership. A span is
editable only when the parser proves that it is ordinary leading indentation.
The following content remains byte-identical:

- complete heredoc units in the first version, including `trim` heredocs;
- text bodies for commands such as append/insert/change;
- `loadkeymap` bodies;
- embedded-language bodies;
- mapping right-hand sides and their trailing whitespace;
- substitute patterns/replacements and other command-specific opaque payloads;
- unknown commands, opaque tails and regions affected by unrecovered parse
  errors;
- BOM, newline spelling (`LF` or `CRLF`) and final-newline state.

Some of these regions could later support an atomic indentation shift. For
example, a complete `trim` heredoc may be movable when its header, body and end
marker are shifted together. That is intentionally deferred until tests prove
that tabs, blank lines and termination rules retain identical values.

## Proposed implementation

No new formatting package, service, registry or formatter interface is needed.
The implementation can be two small pieces:

1. A pure syntax-layer function, placed next to the existing AST, accepts a
   parsed immutable file plus `tabSize` and `insertSpaces`. It returns byte-span
   replacements for proven leading indentation.
2. The document and range handlers convert those byte spans to the client's
   negotiated LSP position encoding and return `protocol.TextEdit` values.

The syntax layer should use a minimal internal edit value containing a source
span and replacement text. It must not depend on LSP types.

### Indentation planning

For each physical line, in source order:

1. Identify its command, token, block, expression continuation and contextual
   dialect from the existing parse result.
2. Reject the line if its leading whitespace overlaps or depends on a protected
   or ambiguous region.
3. Derive a desired indentation column from structural parents:
   - block headers use the parent level;
   - block bodies add one level;
   - branch and end commands dedent to their owning block;
   - closing delimiters align with their opening construct;
   - bracket contents add one level;
   - other proven continuations use three levels, matching Vim's default.
4. Encode that column using the request's `tabSize` and `insertSpaces`.
5. Emit an edit only when the encoded prefix differs from the existing prefix.

With `insertSpaces=true`, the prefix contains spaces only. Otherwise it uses as
many tabs of `tabSize` columns as possible followed by any remainder spaces. A
zero `tabSize` is invalid request data rather than a reason to guess an editor
setting.

Blank lines should remain unchanged. Unknown structure should keep its current
indentation rather than borrowing state from a nearby line. The official
indent expression uses `-1` for the same keep-current-indent outcome when it
cannot determine a safe answer.

The current AST and token stream already preserve physical byte spans,
whitespace, comments, continuations, heredoc metadata, command-specific
payloads and block relationships. That is sufficient for leading-indent edits.
It is not a lossless concrete syntax tree for expression trivia, so it is not a
safe basis for reconstructing whole expressions or commands.

### LSP behavior

Both handlers should follow the
[document and range Formatting contracts](https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.18/language/formatting.md):

- advertise `documentFormattingProvider` only when the handler is complete;
- advertise `documentRangeFormattingProvider` at the same time;
- honor required `tabSize` and `insertSpaces` for newly generated indentation;
- return an empty edit list when the document needs no safe changes;
- return sorted, non-overlapping, line-local edits rather than one replacement
  for the complete document;
- keep optional `trimTrailingWhitespace`, `insertFinalNewline` and
  `trimFinalNewlines` inactive in the first version;
- capture one immutable document URI/version snapshot for the calculation and
  return `ContentModified` if that snapshot becomes stale before the response.

Small edits preserve editor diagnostics, selections and other tracked ranges.
This also follows the VS Code language-feature guidance to return the
[`smallest possible text edits`](https://github.com/microsoft/vscode-docs/blob/main/api/language-extensions/programmatic-language-features.md).

Range formatting must not parse or indent the selected substring in isolation.
The shared planner computes the same candidate edits from the complete immutable
document, so a block opener, delimiter or continuation before the range still
provides context. The range handler then filters that plan. It returns an edit
only when the complete leading-whitespace span is inside the half-open request
range; a zero-width insertion must be before the range end. In particular:

- a first line selected from a character after its indentation is unchanged;
- a line at the exclusive range end is unchanged;
- no edit expands the requested range or changes a partially selected prefix;
- protected heredoc, opaque and payload spans remain protected even when fully
  selected;
- formatting the full document range produces the same edits as document
  formatting.

This needs no second formatter or syntax-unit expansion. It is a boundary
filter over the same independently safe, line-local edits.

On-type formatting should also wait. It operates on incomplete input under a
tighter latency budget and needs a separate contract for which typed characters
trigger edits.

## Alternatives considered

| Approach | Result | Reason |
| --- | --- | --- |
| Start Vim and run `gg=G` | Reject for production | Executes an external editor/runtime, depends on version and mutable options, adds process latency, and conflicts with the server's no-execution rule. Keep it only as a test oracle. |
| Translate `vimindent.vim` to Go | Reject | Duplicates parsing through regex/search state and would drift independently from both Vim and vimls-go's AST. |
| Reprint the AST | Reject | Current expression trivia is not lossless, and reconstructed commands could change abbreviations, bars, comments, continuations or opaque payloads. |
| Use an external formatter | Reject | [`vim-format`](https://github.com/twcarbone/vim-format) describes itself as work in progress with basic Legacy syntax, while archived [`vimlfmt`](https://github.com/deathlyfrantic/vimlfmt) describes itself as very alpha. Neither supplies the required Legacy plus Vim9 v9.2.1015 contract. |
| AST-backed leading-indent edits | Accept | Reuses proven structure, preserves source text, produces minimal LSP edits and can fail closed per line. |

The local `/Users/chemzqm/lib/vim-language-server` comparison also has no
document-formatting implementation to reuse. This feature therefore needs a
vimls-go contract rather than compatibility with that server.

## Validation strategy

Formatting needs stronger invariants than snapshot-only tests because a
visually plausible whitespace change can alter program meaning.

### Pure formatter tests

- Legacy and Vim9 table tests for every owned block, branch, delimiter and
  continuation rule.
- Exact byte-preservation tests for heredocs, mappings, text bodies,
  `loadkeymap`, embedded languages, unknown commands and malformed tails.
- `LF`, `CRLF`, BOM, tabs, spaces and empty/final-line cases.
- UTF-8, UTF-16 and UTF-32 LSP edit-range tests, including astral and combining
  characters before an edited line.
- Sorted, non-overlapping and minimal-edit assertions.
- Range tests for mid-line starts, exclusive ends, empty ranges, complete lines
  and ranges nested inside blocks whose opener is outside the range.
- An assertion that no range edit crosses either requested boundary and that a
  full-document range equals document formatting.
- Idempotence: applying Formatting twice produces no second edit.
- Parse preservation: after applying edits, the command/block/expression shape
  is unchanged when spans and trivia are ignored, and no new diagnostic is
  introduced.

### Official Vim oracle

Pin all oracle evidence to Vim v9.2.1015. Use the official `vim.in`/`vim.ok` and
`vim9.in`/`vim9.ok` pairs with two explicit classifications:

- **owned lines** must produce the same indentation columns as Vim under
  equivalent indentation options;
- **protected or uncertain lines** must stay byte-identical even if Vim's
  mutable-buffer algorithm would change them.

The whole-document range result is the oracle for whole-document LSP
Formatting. Vim's indent implementation keeps state while processing adjacent
lines, so an isolated `==` is not a stronger oracle when it differs from a
range operation.

Curated clean-Vim tests may run the official indent script against temporary,
side-effect-free fixtures and record the exact Vim patch level, messages,
`v:errors` and exit status. This is test-only behavior; normal analysis must
never invoke Vim or source a user's runtimepath.

### Server and client tests

- initialize advertises document and range Formatting only after both handler
  tests pass;
- a request returns the same edits for the same immutable snapshot;
- an intervening document version rejects the stale result;
- applying returned edits through a real LSP client gives the expected buffer;
- the Vim v9.2.1015 plus pinned vim-lsp smoke covers both a Legacy and a Vim9
  document once the capability is enabled.

## Delivery slices

1. **Freeze fixtures and ownership.** Curate representative official cases and
   mark each physical line as owned or protected.
2. **Add the pure indentation planner.** Implement only leading-indent byte
   edits beside the syntax tree and satisfy preservation/idempotence tests.
3. **Add document and range LSP Formatting.** Share one plan, filter range
   edits, convert positions, enforce snapshot freshness, advertise both
   capabilities and add real-client smoke coverage.
4. **Evaluate, do not assume, further scope.** Add safe `trim` heredoc shifting
   or optional whitespace cleanup only when each has an independent semantic
   contract and oracle evidence.

General expression pretty-printing should remain deferred until the parser has
a genuinely lossless concrete representation of all expression and command
trivia. It is not a prerequisite for useful, predictable Vim script
Formatting.

## Acceptance criteria

Formatting is ready to advertise when all of the following are true:

- Legacy and Vim9 owned-line fixtures match the v9.2.1015 indentation oracle;
- protected regions are byte-identical before and after formatting;
- every returned edit is minimal, sorted and non-overlapping;
- range formatting never edits outside its half-open range, and a full-document
  range is equivalent to document formatting;
- formatting is idempotent and preserves parse structure and diagnostics;
- all negotiated position encodings and newline forms pass;
- stale snapshots cannot return edits;
- the real Vim/vim-lsp smoke passes for both dialects.
