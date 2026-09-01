# Language support contract

## Target

vimls-go analyzes both legacy Vim script and Vim9 script through Vim v9.2.1015.
That tag is the current grammar and metadata ceiling and the default target.
Syntax available in earlier Vim releases remains supported; syntax introduced
after v9.2.1015 is not supported until the pinned source and data advance.

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

These historical boundaries support compatibility diagnostics for earlier
targets. New language behavior must be verified against the pinned v9.2.1015
source, help, tests, and clean executable.

## Target-version configuration

The client setting `vimls.targetVersion` remains available for historical
compatibility diagnostics. It accepts `major.minor`, `major.minor.patch`, or
`latest`; omitted patches normalize to zero. The default and `latest` both
select `9.2.1015`, not the version of an executable found on `PATH`. Selecting
an older target does not make that Vim release part of the support contract.

Configuration precedence is:

1. `initializationOptions.targetVersion` for an explicit session override.
2. The `vimls.targetVersion` value returned by `workspace/configuration`.
3. The `9.2.1015` default.

The server rejects versions below 9.1 and malformed values with a visible
configuration warning while retaining the previous valid target. On
`workspace/didChangeConfiguration`, it fetches or applies the new value,
increments an internal configuration revision, reparses/reanalyzes every open
document, and rebuilds version-sensitive workspace metadata. Results from the
old configuration revision are stale and cannot publish.

## Implemented navigation behavior

The server currently provides document/workspace symbols, hover, definition,
declaration, references, document highlights, folding ranges, and selection
ranges. Workspace files come from initialized workspace folders (or `rootUri`),
plus the effective runtimepath, with open snapshots overriding disk content.
`initializationOptions.runtimepath` is an array of filesystem path strings; an
explicit empty array disables runtime indexing. When the option is absent, the
server checks the conventional local Vim installation directories for the host
operating system and uses the newest installed runtime under the first matching
location. It starts no Vim process, reads no user configuration, and leaves the
runtimepath empty when none of those directories exists. Runtime and workspace
roots are indexed by canonical realpath, so aliases are parsed once, while
runtime lookup retains the first configured path's precedence. When the client
advertises `workspace.didChangeWatchedFiles.dynamicRegistration`, the server
uses `client/registerCapability` to ask the language client to watch `**/*.vim`
below every workspace and runtimepath root. The client owns the filesystem
watchers and sends `workspace/didChangeWatchedFiles`; the server consumes those
notifications and atomically replaces the rebuilt index.

The custom `vimls/didChangeRuntimepath` notification replaces runtimepath at
runtime. Its parameters are `{ "runtimepath": string[] }`. The server rebuilds
the bounded index and, when dynamic registration is supported, unregisters the
old Vim watcher registration before registering the new roots.

Cross-file navigation resolves statically provable direct members of Vim9 and
legacy `:import` namespaces, including default filename-derived aliases,
qualified type names, and `import autoload`. It also resolves legacy
`foo#bar#Name` autoload references to `autoload/foo/bar.vim`. Only exported
import targets and unique declarations are authoritative. Dynamic import
expressions, private or ambiguous items, missing files, unsafe symlink targets,
and paths outside initialized workspace/runtimepath roots return empty results
without executing Vim script or guessing runtime state.

## Implemented import dependency analysis

Statically resolved Vim9 imports form a directed workspace graph keyed by
canonical realpath. Published graph snapshots are immutable; an import-relevant
file or workspace change publishes a newer revision. The graph stores both
edge directions, supports shared dependencies and cycles, and computes
transitive reverse dependents so a changed target reanalyzes affected open
importers. The asynchronous workspace build swaps its index and graph as one
consistent pair, and open document snapshots override disk content.

Only readable, safely indexed targets produce dependency edges. Static import
facts with dynamic, missing, unreadable, unsafe, or unindexed targets are kept
without inventing an edge. Imports nested in `def` or `function` bodies likewise
produce no cross-file relationship or diagnostic cascade. The graph revision
is part of stale-diagnostic rejection, so a result computed from an older graph
cannot publish after the import state changes.

Import diagnostics currently cover provable `E1048` missing members, `E1049`
non-exported members, `E1053` ordinary static load failures and runtimepath
autoload load failures, `E1054` when an import alias follows a conflicting
script variable, constant, final, class, or type alias, and `E1088` when a
static import resolves to the importing script's canonical identity. Dynamic
imports, unbound forward receivers, relative or absolute autoload load
failures, and deferred autoload member access inside callable bodies remain
conservative `unknown` where Vim's result depends on runtime state or uses
another error contract.

Static Vim9 import paths and direct `:source` filenames become document links
only when the workspace resolver finds one safe regular file. Completion is
contextual and bounded: command positions use the pinned Ex command table,
expression positions use visible declarations and pinned builtin functions,
legacy `a:` and `l:` prefixes expose only visible arguments and locals, and
explicit `g:`, `b:`, `w:`, `t:`, `s:` and `v:` declarations retain their
namespace spelling. Forward variables are excluded while statically declared
functions remain callable before their declaration. A statically resolved
import namespace exposes only exported members.
Unknown and dynamic contexts return no inferred candidates.
Completion advertises only `.`, `:` and `&` as trigger characters; tests bind
them to member, command/scoped-name and option contexts respectively. Autoload,
index, environment, mapping-token and quote triggers remain manual until their
bounded providers can return a useful result immediately after the character.
Clients that declare snippet support receive required-argument call snippets
for direct builtins and parameter-name snippets for same-file functions. They
also receive a legacy `function`/`endfunction` or Vim9 `def`/`enddef` block
template according to the root dialect; plain-text clients receive none of
these templates.
Mapping commands complete the pinned `:map-arguments` values only before the
LHS and omit flags already present on the command. Ordinary mapping RHS text
remains opaque and never receives expression completion solely because it is a
mapping payload.
`:highlight` completion retains local group names and adds the v9.2.1015
argument keys, attributes, terminal color names, reset values, and the portable
GUI color suggestions listed in `syntax.txt`. Dynamic `v:colornames`, numeric
colors/fonts, and arbitrary terminal/font payloads are intentionally not
enumerated.
Completion resolve and builtin-call hover include the pinned broad return type
when Vim's metadata provides one; builtin-function, Ex-command, option, and
predefined-variable hover plus builtin signature help also include bounded
pinned Vim documentation using the client's preferred Markdown or plain-text
format. Accepted Ex-command abbreviations resolve to canonical command help.
Unknown dynamic return helpers are omitted.

Signature help currently covers direct built-in calls and built-in `->` method
calls using pinned Vim help signatures, plus statically bound same-file user
functions and exported functions reached through a static Vim9 import. User
signatures use their parsed parameters, defaults, and return type; imported
signatures honor open-document overlays and workspace revisions. Method
signatures omit the receiver parameter and adjust the active parameter.
Directly bound function-typed values use their inferred or declared argument,
optional, variadic, and return-type facts. A same-file declaration shadows a
built-in name. Local Vim9 object/class methods, explicit and default
constructors, and inherited methods are resolved with object-versus-class
receiver validation; contextual `this` and `super` calls use their enclosing
class. Imported aggregates cover direct constructors/static methods, explicitly
typed objects, constructor-inferred objects, and chained constructor calls;
local type aliases, local return values, and copy initializers retain the same
binding. Imported factory return values, same-block direct assignments, and
statically typed list/dictionary extraction also retain imported aggregate
types. A later dynamic assignment invalidates the earlier fact, and assignments
inside conditional blocks are not treated as unconditional. Open target buffers
override indexed source. Dynamic callees remain unsupported. Rename
covers same-file bound symbols and cross-file static import members. Legacy
`s:` and `<SID>` function spellings share one local binding; navigation and
highlights include both, while rename preserves each occurrence's prefix.
Cross-file autoload navigation accepts the optional declaration/reference
`g:` spelling, but autoload rename remains rejected because changing its name
also changes the runtimepath file contract. Imported Vim9 aggregate navigation
and rename cover statically resolved constructors, static methods, typed object
methods, factory-return methods, and enum values across indexed files. Open
target buffers override disk content. References scan only statically proven
receivers, and rename is disabled while the workspace index is incomplete.
Other dynamic, ambiguous, and namespace-changing edits are rejected. Open-file
edits carry their captured snapshot versions and closed indexed files use a
null version; workspace changes retry once and then return `ContentModified`.

Full semantic tokens combine command/modifier/comment syntax with bound symbol
declarations and references, using the negotiated position encoding. Inlay
hints expose only already-inferred variable/constant types that were not
written explicitly. Code actions are limited to a unique, known missing block
terminator and never execute Vim script.

Document symbols, folding ranges, and selection ranges retain their nested
function, class, interface, and enum structure through end-of-file when a block
terminator is still missing, so incomplete editing state remains navigable.

For statically resolved local Vim9 members, navigation follows inherited
methods and variables, default constructors, and enum values. When an object
type or constructor initializer proves both an interface or abstract member and
its concrete implementation, Declaration returns the contract while Definition
returns the implementation. References and highlights join both declarations
with calls through either statically proven receiver type. Constructor rename
is rejected; other member rename is offered only for the complete proven local
binding set.

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
- Supporting Vim syntax introduced after v9.2.1015 without first advancing and
  verifying the pinned source and metadata.

## Version evidence

Every behavior-sensitive fixture records its Vim tag. The primary corpus and
oracle lane use v9.2.1015; historical boundary fixtures verify compatibility
diagnostics for earlier targets.

When official help and observed behavior disagree, record a focused upstream
test reproduction and treat the executable behavior for that exact version as
authoritative.

## Primary references

- [Vim v9.2.1015 source and tests](https://github.com/vim/vim/tree/v9.2.1015/src/testdir)
- [Vim v9.2.1015 `vim9.txt`](https://github.com/vim/vim/blob/v9.2.1015/runtime/doc/vim9.txt)
- [Vim v9.2.1015 `eval.txt`](https://github.com/vim/vim/blob/v9.2.1015/runtime/doc/eval.txt)
- [Enum tests at 9.1.0219](https://github.com/vim/vim/blob/v9.1.0219/src/testdir/test_vim9_enum.vim)
- [Tuple types at 9.1.1232](https://github.com/vim/vim/blob/v9.1.1232/runtime/doc/vim9.txt)
- [Generic functions at 9.1.1577](https://github.com/vim/vim/blob/v9.1.1577/runtime/doc/vim9.txt)
