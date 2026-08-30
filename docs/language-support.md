# Language support contract

## Target

vimls-go analyzes both legacy Vim script and Vim9 script from Vim 9.1.0000
through Vim 9.2.1015. Vim 9.1 is the compatibility floor. The parser recognizes
the complete 9.2.1015 syntax surface, while the selected target version controls
compatibility diagnostics for later forms.

The server analyzes the language used by Vim configuration and plugin files. It
does not implement a Vim editor client and it does not treat Neovim Lua as Vim
script.

## Dialect model

The parser exposes two independent root entry points:

- `LegacyParser` parses a legacy-root file.
- `Vim9Parser` parses a Vim9-root file.
- `Parse` dispatches to Vim9 only when the first effective command is
  `vim9script`; otherwise it dispatches to legacy.
- `vim9cmd` applies Vim9 rules to its following command only.
- `legacy` applies legacy rules to its following command only where Vim permits
  it. Neither modifier creates a persistent lexer mode.
- `scriptversion` changes applicable legacy expression rules.

The current parser retains `def` in a legacy-root file and `function` in a
Vim9-root file through contextual parsing and loose recovery. Their presence
alone must not produce a diagnostic, but complete parsing and semantic support
for those two combinations is a TODO. Existing mixed-dialect tests remain as
smoke coverage; there is no requirement to enumerate every mixed combination.

Within either root parser, parse state retains enclosing
script/function/class contexts and an explicit one-command override. The syntax
tree records the resolved context so semantic analysis does not have to infer
the dialect again.

## Syntax required for 1.0

The following requirements apply to each construct in its native root language;
they do not create a cross-product requirement across mixed-dialect contexts.
The 1.0 parser must accept and recover across:

- Ex ranges, addresses, command modifiers, abbreviations, bang, counts,
  registers, bars, comments, and line continuation. The `filter[!]` modifier
  retains its force bang, delimited or undelimited regexp, optional `g`/`j`/`f`
  flags, and the following command as separate syntax.
- Legacy and Vim9 strings, numbers, blobs, lists, dictionaries, tuples, lambdas,
  function references, option/environment/register references, interpolation,
  indexing, slicing, calls, method chains, and unary/binary expressions.
- `let`, `unlet`, `const`, `final`, `var`, assignment and destructuring.
- `function`/`endfunction`, `def`/`enddef`, arguments, defaults, varargs, closures,
  attributes, return types, and function types.
- `if`/`elseif`/`else`/`endif`, `for`/`endfor`, `while`/`endwhile`,
  `try`/`catch`/`finally`/`endtry`, `throw`, `return`, `break`, `continue`, and
  `finish`.
- `import`, `export`, autoload imports, `class`/`endclass`, `abstract class`,
  `interface`/`endinterface`, `enum`/`endenum`, `type` aliases, generics,
  constructors, members, and methods as available in the configured Vim version.
- Mappings, including a dialect-aware expression AST for `<expr>` right-hand
  sides while retaining every raw right-hand side; autocommands, user commands,
  heredocs, regexp-delimited
  `global`/`vglobal` and fold-filtered `folddoopen`/`folddoclosed` nested Ex
  command lists; structured `substitute`/`smagic`/`snomagic` and `~` payloads
  with regexp and replacement delimiters, previous-pattern forms, flags,
  counts, and `\=expr` replacements; structured `highlight` list, query,
  clear, link, default, and attribute forms with exact Ex boundaries; and
  structured `syntax keyword`, `syntax match`, `syntax region`, and
  `syntax cluster` items with group names, keywords, options, regexp
  delimiters, offsets, repeated region patterns, and cluster membership
  operations; structured query/set forms for `syntax case`, `syntax conceal`,
  and `syntax spell`; structured `syntax include` items with an optional
  cluster and the complete unexpanded filename payload; structured explicit
  and implicit `syntax list` queries and `syntax clear` group/cluster operands;
  structured `syntax sync` queries, settings, `linecont` patterns, clear lists,
  sync matches, and sync regions, including `grouphere`/`groupthere` targets;
  structured `syntax iskeyword` opaque chartab payloads and `syntax foldlevel`
  query/set forms; structured `syntax on`, `enable`, `manual`, `off`, and
  `reset` runtime modes without executing their sourced scripts; structured
  `set`, `setlocal`, and `setglobal` option items with prefix, name, operator,
  value, and complete-item spans, including escaped payloads, terminal option
  names, and Vim9 whitespace recovery; and
  commands with opaque or embedded-language payloads without losing
  synchronization.
- Incomplete and malformed forms produced while a user is typing.

Unknown user commands and syntax introduced by a newer Vim are retained as
opaque nodes. The server may report a target-version compatibility diagnostic
only when it can identify the construct confidently.

## Versioned syntax

The parser may recognize syntax newer than the configured target so that it can
recover and explain the mismatch. Recognition does not imply target
compatibility. The version manifest records the first supported Vim patch for
every gated form. Known anchors include:

| Form | Minimum target | Behavior below the minimum |
| --- | --- | --- |
| Classes, interfaces, and `:type` aliases in the 9.1 baseline | 9.1.0000 | Parse normally |
| `ctermfont` in `:highlight` | 9.1.0030 | Preserve the attribute and emit one compatibility diagnostic |
| Enums | 9.1.0219 | Preserve syntax and emit one compatibility diagnostic |
| Heredocs inside Vim9 `:command { ... }` blocks | 9.1.0312 | Preserve syntax and emit one compatibility diagnostic |
| Inline text after `:append`, `:change`, or `:insert` | 9.1.0574 | Preserve the text body and emit one compatibility diagnostic |
| `:pbuffer` | 9.1.0934 | Preserve the command and emit one compatibility diagnostic |
| `:iput` | 9.1.1224 | Preserve the command and emit one compatibility diagnostic |
| Tuple types and tuple values | 9.1.1232 | Preserve syntax and emit one compatibility diagnostic |
| `object<{type}>` | 9.1.1274 | Preserve the type and emit one compatibility diagnostic |
| `:redrawtabpanel` | 9.1.1391 | Preserve the command and emit one compatibility diagnostic |
| `:uniq` | 9.1.1477 | Preserve the command and emit one compatibility diagnostic |
| `:clipreset` and `:wlrestore` | 9.1.1485 | Preserve the command and emit one compatibility diagnostic |
| Generic functions | 9.1.1577 | Preserve syntax and emit one compatibility diagnostic |

Before implementing another gated feature, `language_researcher` must verify its
first patch against the official Vim tags/tests and add it to the manifest and
fixtures. A Vim 9.1.0000 oracle lane must reject later-only forms while the
server still recovers to subsequent declarations.

## Target-version configuration

The client setting is `vimls.targetVersion`. It accepts `major.minor`,
`major.minor.patch`, or `latest`; omitted patches normalize to zero, so `9.1`
means `9.1.0000`. The default is `9.1.0000`. `latest` selects the highest Vim
version described by the server's embedded, tested feature manifest, currently
`9.2.1015`, not the version of an executable found on `PATH`.

Configuration precedence is:

1. `initializationOptions.targetVersion` for an explicit session override.
2. The `vimls.targetVersion` value returned by `workspace/configuration`.
3. The `9.1.0000` default.

The server rejects versions below 9.1 and malformed values with a visible
configuration warning while retaining the previous valid target. On
`workspace/didChangeConfiguration`, it fetches or applies the new value,
increments an internal configuration revision, reparses/reanalyzes every open
document, and rebuilds version-sensitive workspace metadata. Results from the
old configuration revision are stale and cannot publish.

## Semantics required for 1.0

- Explicit legacy scopes: `g:`, `b:`, `w:`, `t:`, `s:`, `l:`, `a:`, `v:` and
  environment, option, and register references.
- Vim9 script-local defaults, block scopes, arguments, closures, imports,
  exports, autoload names, class/interface/enum/type members, and visibility.
- Declaration, definition, reference, shadowing, mutability, arity, and safe
  rename analysis.
- Vim9 type checking for primitives, containers, tuples, functions, objects,
  interfaces, enums, generics, `any`, `void`, and null values supported by the
  selected target. `unknown` is an internal analyzer state, not a Vim9 source
  type.
- Conservative legacy inference and conservative handling of `execute()`,
  `eval()`, dynamically formed names, user commands, and runtimepath mutation.

Arity diagnostics currently cover direct calls to statically named Vim built-in
functions using the pinned function metadata. User-defined functions, scoped or
member calls, and dynamically resolved call targets remain conservative
`unknown` until their signatures can be resolved without guessing.

## File discovery

Include `.vim` files and Vim configuration/plugin paths such as vimrc, gvimrc,
exrc, `plugin/`, `autoload/`, `import/`, `after/`, `ftplugin/`, `indent/`,
`compiler/`, `syntax/`, `colors/`, `keymap/`, and packages below `pack/`.
Configuration must allow extra filename patterns and runtimepath roots. File
extension alone is not sufficient to identify a Vim script.

## Explicit non-goals

- Executing user code to obtain symbols or types.
- Proving arbitrary dynamically generated Ex commands.
- Analyzing embedded Python, Ruby, Perl, Lua, shell, or another heredoc language
  beyond preserving its range for a future embedded-language integration.
- Providing exact static types for every legacy value.
- Supporting Vim releases older than 9.1.

## Version evidence

Every version-sensitive fixture records its minimum Vim version or patch. The
compatibility corpus contains at least:

- Vim 9.1.0000 behavior for the minimum boundary.
- The latest published 9.1 patch used by CI.
- The latest stable Vim release.
- Official accept/fail examples for each syntax feature added after 9.1, plus
  mixed legacy/Vim9 examples where that feature actually crosses a dialect
  boundary.

When official help and observed behavior disagree, record a focused upstream
test reproduction and treat the executable behavior for that exact version as
authoritative.

## Primary references

- [Vim v9.2.1015 source and tests](https://github.com/vim/vim/tree/v9.2.1015/src/testdir)
- [Vim v9.2.1015 `vim9.txt`](https://github.com/vim/vim/blob/v9.2.1015/runtime/doc/vim9.txt)
- [Vim v9.2.1015 `eval.txt`](https://github.com/vim/vim/blob/v9.2.1015/runtime/doc/eval.txt)
- [Vim v9.1.0000 compatibility baseline](https://github.com/vim/vim/tree/v9.1.0000)
- [Vim 9.1 `runtime/filetype.vim`](https://github.com/vim/vim/blob/v9.1.0000/runtime/filetype.vim)
- [Enum tests at 9.1.0219](https://github.com/vim/vim/blob/v9.1.0219/src/testdir/test_vim9_enum.vim)
- [Tuple types at 9.1.1232](https://github.com/vim/vim/blob/v9.1.1232/runtime/doc/vim9.txt)
- [Generic functions at 9.1.1577](https://github.com/vim/vim/blob/v9.1.1577/runtime/doc/vim9.txt)
