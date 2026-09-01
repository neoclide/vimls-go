# Legacy Vim script and Vim9 LSP backlog

## Purpose

This document turns the remaining language-server work into an executable
backlog. It complements `docs/roadmap.md`: the roadmap defines milestones,
while this file records concrete language and editor behavior that is still
missing or needs stronger evidence.

The comparison baseline is:

- `vimls-go` commit `95d5ad676f42de7e65aab569f0f99cf2e9fea6f0`
  (2026-09-01).
- `/Users/chemzqm/lib/vim-language-server` commit
  `f6e1808e441c64f47f6fb86886eb9ad5a4e74156` (2023-06-26).

The older TypeScript server is a legacy Vim script usability reference, not a
language authority. Its bundled `vimparser.js` has no native Vim9 model, its
diagnostics expose only one parser error, and several features are selected by
regular expressions over the current line. Do not port those mechanisms.

## Current comparison

| Area | vim-language-server | Current vimls-go | Remaining work |
| --- | --- | --- | --- |
| Dialects | Legacy Vim script AST | Independent legacy and Vim9 roots, contextual `vim9cmd`/`legacy`, loose mixed-dialect recovery | Finish the native syntax and semantic contracts for both dialects; retain unknown constructs safely |
| Synchronization | Incremental text updates followed by delayed parsing in a child process | Ordered incremental snapshots, negotiated UTF-8/16/32 positions, immutable versions, cancellation and stale-result rejection | Add real-client and stress evidence; incremental AST work is separate and must preserve `Parse` as the oracle |
| Diagnostics | One parser error from the bundled parser | Syntax, compatibility, scope, type, import and many official Vim error diagnostics | Complete statically provable Vim9 checks and conservative legacy checks; keep dynamic behavior unknown |
| Completion | Commands, functions, variables, options, events, colorschemes, `has()` features, `expand()` tokens, mapping arguments, highlight arguments and legacy snippets | Commands, modifiers, scoped declarations, builtins, import/object members, options, events, syntax/highlight groups, user commands and import paths; scored edits and lazy resolve | Fill the useful legacy completion gaps, add Vim9-aware snippets and metadata/version filtering |
| Hover | Vim help text for builtins | Symbol kind/type and pinned builtin-function, Ex-command, option and predefined-variable help | Add signature-oriented user symbol details without inventing documentation |
| Signature help | Builtin functions | Direct builtin functions, statically bound same-file/imported user functions, directly typed function values, and local/imported class methods and constructors | Propagate imported aggregate types through more indirect value flows |
| Navigation | Legacy identifier/function index | Same-file and graph-backed import/autoload navigation, declaration, workspace symbols and safe cross-file rename | Complete class/member/inheritance and provable legacy global/autoload cases |
| Structure | Functions and variables; function folds | Nested symbols for functions, classes, interfaces, enums, types and imports; syntax-backed folds and selection ranges | Improve incomplete/mixed syntax recovery without invalid ranges |
| Modern LSP | Basic hover/navigation/editing | Document links, semantic tokens, code actions, inlay hints, workspace folders, watched files and position negotiation | Broaden stable token classifications and safe actions; do not add capabilities before implementation |

Primary implementation evidence:

- The older server advertises its protocol surface in `src/index.ts`; its
  completion categories live under `src/handles/completion/`, builtin help and
  signatures in `src/server/builtin.ts`, snippets in `src/server/snippets.ts`,
  and legacy workspace indexing in `src/server/parser.ts` and
  `src/server/workspaces.ts`.
- vimls-go advertises capabilities in `internal/server/server.go`; completion
  is in `internal/server/completion.go`, hover/signature/document links in
  `internal/server/language_features.go` and `internal/server/navigation.go`,
  semantic tokens/actions in `internal/server/semantic_actions.go`, workspace
  behavior in `internal/server/workspace*.go`, and language facts in
  `internal/syntax`, `internal/analysis` and `internal/vimdata`.

## Priority rules

1. Observable Vim behavior and the pinned Vim sources decide correctness.
2. Syntax and binding correctness come before more completion sources.
3. Legacy analysis remains conservative; dynamic names, `execute()`, `eval()`,
   mutable runtimepath and user commands must not produce guessed results.
4. Vim9 diagnostics require exact static proof and the native Vim error code
   where one exists.
5. A feature is complete only after package tests and a real stdio request
   cover both source spans and negotiated LSP positions.

## P0: finish the language foundation

### Coverage ledger

- [x] Create a reviewable syntax coverage table from the requirements in
  `docs/language-support.md`. For each construct, link an official Vim source
  or runtime-help location, the owning parser test, incomplete-input recovery
  evidence, and any version boundary.
- [x] Record semantic coverage separately from parse acceptance. A form that
  produces an AST is not complete until its declarations, references, scopes,
  types and diagnostics are explicitly classified as implemented, unknown or
  deferred.
- [x] Keep the ledger bounded by parser mechanism. Do not duplicate every form
  across legacy, Vim9, mixed-dialect and incomplete-input matrices when one
  shared recovery test proves the mechanism.

The maintained ledger is `docs/syntax-coverage.md`. Its rows are parser
mechanisms rather than syntax spellings, and its semantic column classifies
declarations, references, scopes and types independently from diagnostics.

Exit gate: every `language-support.md` requirement has an owner and evidence;
there are no unqualified claims such as "full expressions" or "full Vim9".

### Legacy Vim script syntax and binding

- [ ] Complete structured parsing and recovery for legacy forms still retained
  as opaque arguments where semantics need them: scoped assignments,
  destructuring, function attributes/varargs, mappings, autocommands, user
  commands and nested Ex payloads.
- [ ] Verify binding and navigation for `g:`, `b:`, `w:`, `t:`, `s:`, `l:`,
  `a:` and `v:` names, including `<SID>`, autoload names, function arguments,
  closures and shadowing. Leave editor-created buffer/window/tab/global state
  unknown when the current file cannot prove it.
- [ ] Complete statically provable legacy global function/variable and autoload
  references across workspace/runtimepath files, including duplicate and
  load-order ambiguity behavior.
- [ ] Add conservative checks for legacy mutability, callable arity and
  container operations only where Vim behavior is independent of runtime
  state.
- [ ] Test `scriptversion`, legacy continuations, bars/comments, abbreviations,
  command modifiers and one-command `vim9cmd` overrides at their actual command
  boundaries.

Exit gate: curated plugin/vimrc fixtures provide definition, references, hover,
completion and diagnostics without executing a Vim script or inventing errors
for dynamic names.

### Vim9 syntax and binding

- [ ] Complete declarations and expressions for typed variables, destructuring,
  lambdas, method chains, function values and nested generic/container/tuple
  types, with byte-stable recovery for incomplete input.
- [ ] Complete class, abstract class, interface and enum semantics: constructors,
  static/object members, access control, inheritance, implementation,
  overriding, generic methods, enum values and `this`/`super` binding.
- [ ] Complete `def` scopes and closures, default/optional/variadic parameters,
  return analysis, `defer`, reachable control flow and statically bound calls.
- [ ] Complete import/export/autoload semantics for aliases, exported members,
  types, classes, cycles, diamonds, open-buffer overlays and reverse-dependent
  reanalysis. Dynamic and unsafe targets remain unknown.
- [ ] Finish Vim9 type checking for primitive values, containers, tuples,
  functions, objects, interfaces, enums, generics, `any`, `void` and null
  values. Preserve `unknown` as an analyzer state, never a source type.
- [ ] Continue the official compile-diagnostic migration one Vim error code per
  reviewable test change, using readable cases in the owning
  `official_compile_cases_e*_test.go` file.

Exit gate: every supported diagnostic has official evidence, exact code/message
and byte range tests; incomplete or dynamic code does not create a false error.

### Pinned behavior and builtin data

- [x] Pin the syntax/metadata ceiling and default target to Vim v9.2.1015.
  Earlier syntax stays supported; later syntax waits for a pin update.
- [ ] Separate Vim and Neovim metadata instead of reusing the old server's
  `isNeovim` flag as a parser switch. Add a target only after its behavior and
  data source are defined.
- [ ] Add a documented refresh/verification command for pinned metadata and
  fail tests on duplicate names and invalid help tags.

Exit gate: generated and handwritten metadata is traceable to v9.2.1015 and
does not silently mix Vim or Neovim data.

## P1: close high-value LSP usability gaps

### Completion

Port the old server's useful completion categories through the current AST,
workspace index and pinned data. Do not port its provider registry or line
regular expressions.

- [x] Extend the existing predefined `v:` variable and local declaration
  completion to all statically visible scoped names, respecting the active
  legacy/Vim9 scope, position, forward-visibility rules and shadowing.
- [x] Add legacy workspace global function completion with the same ambiguity
  and root-safety rules as navigation; include indexed autoload functions in
  both dialects and derive Vim9 exported autoload prefixes from their paths.
- [x] Add legacy workspace global variable completion with the same ambiguity
  and root-safety rules as navigation.
- [x] Add `has()` feature names and `expand()` special tokens from pinned Vim
  help/source data.
- [x] Add the pinned `:map-arguments` set before the mapping LHS, omit already
  used flags, and keep ordinary mapping right-hand sides outside completion.
- [x] Add `:highlight` argument keys and finite help-listed values, while
  retaining local syntax/highlight group completion. Dynamic `v:colornames`
  and numeric payloads remain user input rather than enumerations.
- [x] Add `:colorscheme` names from safe indexed runtimepath `colors/`
  directories, respecting runtimepath precedence and workspace limits.
- [x] Add command-specific completion for augroups, user-command attributes,
  `:set` operators/values and other finite enums only after their command AST
  identifies the cursor position.
- [x] Advertise the trigger characters actually supported by tests. `.`, `:`,
  `&`, `#`, `<`, `"` and `'` select proven member, command/scoped-name, option,
  autoload, mapping-token, `has()` and `expand()` contexts. `[` and `$` remain
  unadvertised until a bounded provider has a useful result immediately after
  that character.
- [x] Enable direct builtin and same-file function-call snippets only when the
  client supports snippets. Legacy `function`/`endfunction` and Vim9
  `def`/`enddef` block templates are dialect-specific; plain-text clients keep
  ordinary completion edits.
- [x] Add documentation and deterministic truncation tests for every new source.

Exit gate: completion tests cover prefix replacement, Unicode/CRLF positions,
comments and strings, incomplete input, client capabilities, cancellation,
budget expiry, stable ordering and the 2,000-item cap.

### Hover and signature help

- [x] Return bounded pinned Vim help for builtin functions while keeping
  inferred user-symbol kind/type facts separate from official documentation.
- [x] Return bounded pinned Vim help for options and predefined variables.
- [x] Return bounded pinned Vim help for Ex commands, including canonical help
  for accepted abbreviations.
- [x] Add builtin function signature help from pinned Vim metadata, including
  Legacy/Vim9 requests, active parameters, variadic signatures and shadowing.
- [x] Resolve signature help for exported functions through statically bound
  Vim9 imports, including open-buffer overlays and stale-workspace rejection.
- [x] Resolve signature help for directly bound function-typed values, including
  optional and variadic function types.
- [x] Resolve signature help for local object/class methods, explicit/default
  constructors, and inherited methods with receiver-category validation.
- [x] Resolve contextual `this`/`super` method calls without guessing dynamic
  receiver types.
- [x] Resolve imported aggregate constructors, static methods, explicitly typed
  objects, constructor-inferred objects, and chained constructor calls with
  open-buffer and stale-workspace validation.
- [x] Propagate imported aggregate types through local type aliases, local
  returned values, and typed or inferred copy initializers.
- [x] Propagate imported aggregate types through imported factory return values,
  later assignments, and statically typed container extraction.
- [x] Handle nested calls, method-call receiver adjustment, optional/default
  arguments and variadic parameters when computing the active parameter.
- [x] Respect each client's ordered Markdown/plain-text preference for Hover and
  Signature Help and keep documentation UTF-8-safe within 16 KiB.

Exit gate: builtin, legacy user function, Vim9 `def`, imported function and
method calls each have focused stdio tests for hover/signature content and
active parameter.

### Navigation, symbols and rename

- [x] Complete definition/declaration/reference resolution for inherited Vim9
  members, implemented interface members, constructors and enum values.
- [x] Distinguish declaration from definition when an interface/abstract member
  and an implementation are both statically known.
- [x] Verify legacy `<SID>`/`s:` equivalence and autoload spellings across
  definition, references, highlights and rename.
- [x] Preserve hierarchy and ranges for incomplete functions, classes,
  interfaces and enums in document symbols, folding and selection ranges.
- [x] Extend rename only for newly proven bindings. Reject edits that would
  change namespace spelling, export visibility, import aliases or autoload file
  contracts unless the complete edit set is known.

Exit gate: every cross-file edit carries open-document versions, closed files
use null versions, edits do not overlap, and a workspace mutation during the
request returns `ContentModified`.

### Semantic tokens and code actions

- [ ] Finish stable token classification for legacy scope prefixes, options,
  registers, environment variables, user commands, functions, parameters,
  imports, types, classes/interfaces/enums, members and deprecated symbols.
- [ ] Add token modifiers only when analysis proves declaration, readonly,
  static, deprecated or default-library status.
- [ ] Add safe syntax-backed quick fixes beyond a missing block terminator only
  when there is exactly one valid edit, for example an unambiguous missing
  keyword or dialect-specific terminator.
- [ ] Do not add formatting, source-wide rewriting or speculative type fixes to
  the 1.0 surface.

Exit gate: token streams are sorted, non-overlapping and valid in negotiated
position encoding; every code action round-trips through a reparsed document.

## P2: diagnostics and configuration quality

- [ ] Define stable policy for parser, compatibility, unresolved, unused and
  deprecation severities, including whether each category can be disabled.
- [ ] Publish related information for cross-file import/export/type errors when
  the authoritative declaration is known.
- [x] Keep the 200-diagnostic limit deterministic and indicate truncation
  without replacing more useful earlier diagnostics.
- [ ] Test configuration changes against in-flight parsing, workspace graph
  replacement, close/reopen and target-version changes.
- [x] Document supported initialization options and notifications with complete
  client configuration examples.

Exit gate: configuration changes never publish results from an older document,
configuration, index or import-graph revision.

## P3: integration, performance and release evidence

- [ ] Add stdio golden scenarios containing both a realistic legacy plugin and
  a realistic Vim9 import/class project. Exercise initialize, sync,
  diagnostics, completion/resolve, hover, signature, navigation, symbols,
  rename and shutdown.
- [ ] Add the pinned Vim v9.2.1015 oracle lane defined by
  `docs/testing.md`; record version and patch level for every behavior-sensitive
  fixture.
- [ ] Add real Vim/vim-lsp and Neovim built-in LSP smoke tests without reading
  or modifying user configuration.
- [ ] Fuzz parser, framing and position/edit boundaries; retain every crash,
  hang or memory-growth input in the permanent corpus.
- [ ] Benchmark large files, runtimepath indexing, completion latency, reverse
  dependent reanalysis and full workspace rebuilds against documented limits.
- [ ] Complete Linux/macOS CI, Windows build/integration coverage, vulnerability
  scanning, install documentation and reproducible release archives.

Exit gate: `gofmt` is clean, `go test ./...`, `go test -race ./...` and
`go vet ./...` pass, real clients complete the legacy and Vim9 smoke scenarios,
and no advertised capability lacks a protocol-level test.

## Explicitly do not copy from vim-language-server

- The bundled JavaScript legacy parser or its AST shapes.
- Regex-only context selection for commands, expressions or nested calls.
- A child parser process with fixed 50-second request timeouts.
- Diagnostics that collapse all parser failures to one extracted error.
- Unbounded or load-order-dependent workspace symbol guesses.
- The `isNeovim` boolean as a substitute for versioned syntax and builtin data.
- Its provider registry: current completion has one real caller and should stay
  in the existing server package until another caller creates a real boundary.

## Suggested execution order

1. Coverage ledger and one bounded parser/semantic gap.
2. Scope/type/import correctness needed by that gap.
3. Hover, signature, completion or navigation behavior enabled by the new fact.
4. Stdio protocol test and Vim oracle evidence.
5. Update `language-support.md` and `roadmap.md` from the verified result.

Each implementation slice should be small enough to review independently and
must preserve unknown/dynamic behavior rather than widening the task silently.
