# What vimls-go supports

vimls-go supports Legacy Vim script and Vim9 script through **Vim v9.2.1015**,
including classes, interfaces, enums and imports. Both dialects can be used in
the same project. See the [changelog](../CHANGELOG.md) for release availability.

## Editing features

| Feature | Support |
| --- | --- |
| Completion | Commands, functions, variables, options, events, mappings, imports and class members. |
| Hover and signature help | Types, signatures, source comments and runtime help. |
| Diagnostics | Syntax errors, unresolved names, invalid calls and Vim9 type errors. |
| Navigation | Definitions, references, implementations, call hierarchy and workspace symbols. |
| Editing | Rename, selected quick fixes, folding and selection expansion. |
| Inlay hints and Code Lens | Inferred Vim9 types, reference counts and implementation counts. |
| Semantic highlighting | Types, functions, variables, parameters and modifiers. |
| Formatting | Indentation for files, selections and supported typing events. |

The parser tolerates unfinished code. Formatting preserves expressions, line
wrapping and embedded language bodies. Rename refuses ambiguous targets and
changes that require renaming autoload files or namespaces.

Option diagnostics assume known options exist regardless of Vim build features;
assignment type checks skip unknown RHS types and Vim9 `any`.
Invalid boolean option compound assignments in a Vim9 `def` use E521 uniformly.

User-command definitions preserve attribute, name and body locations through
logical-line continuations in both dialects. Vim9 command attributes support
the same contextual completion as Legacy attributes.

Hover on a documented mapping or user-command bang variant includes `!` in
the command range and works when the cursor is on that character.
Command completion also retains an existing `!` when resolving documentation,
so a completion before `!` shows the bang variant's help without changing the
insertion text.

Completion uses the current text without waiting for full file analysis.
Commands and options use syntax and built-in metadata; local variables and
members use lexical declarations. Local declaration type details are computed
on `completionItem/resolve`; the initial list retains insertion text and snippets.
Clients that do not resolve items show the declaration kind without inferred
type detail. Resolving an item from an edited or reopened document leaves it
unchanged. Member discovery still queries the receiver and bounded nested-member
types. Function parameters and member symbols are each collected at most once
per completion request.

Autocmd event hover and completion documentation use compiled help for 156
event names: 127 Vim spellings and 29 Neovim-only additions. Shared names use
Vim's documentation; Neovim-only events are labeled accordingly. Hover applies
to event tokens, including comma-separated lists and events after an augroup,
not patterns or command bodies. Completion loads the selected event's help via
`completionItem/resolve`. Both support Markdown and plain text. Sources are
pinned in the [event documentation generator](../tools/geneventdocs/README.md).

Background diagnostics and workspace analysis yield between phases and in
batched scope, reference and type traversals while
completion requests are active and until 150 ms after the latest accepted edit.
Queued document analysis captures the latest snapshot after this wait, merging
intermediate edits. Editing or closing an open document cancels obsolete shared
semantic work, including a paused pass; identical-content work can continue.
Cancelling one requesting consumer does not cancel the shared computation.
Parsing needed by a foreground request bypasses the wait;
it still reads the whole source, so large files can increase completion latency.

Document text supports LF, CRLF and CR line endings, including mixed files.
Positions follow the negotiated UTF-8, UTF-16 or UTF-32 encoding. Formatting
and rename preserve the original line-ending bytes.

Variable types follow known initializer return types in both dialects.
For example, `let g:local = expand('~/vim-dev')` shows `string` on hover.
For `expand()`, a literal true third argument produces `list<string>`;
an omitted or literal false third argument produces `string`. Dynamic list
flags remain `unknown`. Method calls (`->expand()`) use the same rules.

## Plugin files and help

Workspace Vim files are indexed for analysis and navigation. Outside the
workspace, runtimepath indexing covers:

| Location | Used for |
| --- | --- |
| `plugin/**/*.vim`, `autoload/**/*.vim`, `import/**/*.vim` | Symbols, completion and navigation. |
| `colors/*.vim` | Color-scheme names and paths. |
| `doc/*.txt` | Runtime help. |

Runtime help loads in the background. Built-in signatures, option data and
language rules remain tied to the supported Vim version. See
[configuration](configuration.md) for runtimepath and help settings.

## Mappings and configuration files

References are followed in supported autocommand bodies, expression mappings,
`<Cmd>` bodies and static function-name callback options. Generated code is
not executed or analyzed.

Vimrc-style files receive different style suggestions from plugin files.
See [editing configuration files](userconfig.md).

## Limits to keep in mind

- Dynamic code and loading order may leave types or references unresolved.
- Consecutive simple Legacy assignments warn on known basic type changes;
  unknown values and control-flow boundaries reset this check. See
  [diagnostics](diagnostics.md#names-and-unused-code).
- Mixed-dialect `def` and `function` bodies have incomplete analysis.
- Call hierarchy excludes lambdas and deferred command bodies.
- Embedded languages and syntax newer than Vim v9.2.1015 are not analyzed.
- Files larger than 4 MiB are synchronized but not analyzed.

## Neovim compatibility

Some Neovim names are recognized, but full Neovim API completion and type
checking are not provided. Neovim-only option settings receive a Hint unless
protected by `has('nvim')`. See [Neovim compatibility](neovim.md#option-compatibility).

## MacVim option compatibility

MacVim-specific options have semantic highlighting, hover documentation and
compatibility diagnostics. Protect their settings with `has('gui_macvim')`
to suppress compatibility Hints; definite invalid settings still report errors.
See the [MacVim option list](https://macvim.org/docs/gui_mac.txt.html#macvim-options).
