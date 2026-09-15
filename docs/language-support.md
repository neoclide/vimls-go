# What vimls-go supports

vimls-go supports Legacy Vim script and Vim9 script through **Vim v9.2.1015**,
including classes, interfaces, enums and imports. Both dialects can be used in
the same project. See the [changelog](../CHANGELOG.md) for release availability.

## Editing features

| Feature | Support |
| --- | --- |
| Completion | Commands, functions, variables, options, events, mappings, imports and class members. |
| Hover and signature help | Types, signatures, source comments, runtime help and feature introduction history. |
| Diagnostics | Syntax errors, unresolved names, invalid calls, Vim9 type errors and target Vim version checks. |
| Navigation | Definitions, references, implementations, call hierarchy and workspace symbols. |
| Editing | Rename, linked editing, selected quick fixes, folding and selection expansion. |
| Inlay hints and Code Lens | Inferred Vim9 types, reference counts and implementation counts. |
| Semantic highlighting | Types, functions, variables, parameters and modifiers. |
| Formatting | Indentation for files, selections and supported typing events. |

The parser tolerates unfinished code. Formatting preserves expressions, line
wrapping and embedded language bodies. Rename refuses ambiguous targets and
changes that require renaming autoload files or namespaces.

Linked editing covers the declaration and its references in the current file.
It is offered only for symbols whose rename is a single-file edit: script-local
and local variables, function parameters, non-exported Vim9 symbols and
`<SID>`-free script-local Legacy names. It is withheld for exported, autoload
and global symbols (including explicit `g:` declarations inside functions), for
class and interface members, and for symbols declared in another file, because
editing only the open document would leave the rest of the workspace
inconsistent. Class, interface, enum and type-alias names are also withheld
until type annotation references are included. A reference spelled differently
from the declaration, such as `<SID>name` beside `s:name`, is withheld as well,
because linked ranges must carry identical text.

Renaming a file through the client's file operation rewrites the Vim9 `:import`
statements that name it, and also the relative imports of the renamed files
themselves when their own spelling moves with them. Each import keeps its
original form: a relative path stays relative to the importing script, an
absolute path stays absolute, and a `runtimepath` import stays below the same
`import/` or `autoload/` directory. A `:import` without an `as` alias derives its
namespace from the filename, so the derived declaration and every bound
reference are rewritten together for plain single- or double-quoted literals.
The proposed path is checked against the whole file rename batch and the
runtimepath search order; a path that would resolve to another script is
withheld. An import is left alone, and its document is skipped rather than partly
edited, when the new location cannot be expressed in the original form, when
the new filename is not a valid Vim identifier, when the
document's content cannot be verified against the indexed source, or when the
derived namespace has a textual occurrence that the analysis does not bind, such
as a type annotation or a comment. A rename is never refused for these reasons.

Types are inferred from known values and function return types in both dialects.
Uncertain dynamic behavior may leave types or references unresolved. See
[diagnostics](diagnostics.md) for diagnostic coverage and warning policies.

LF, CRLF and CR line endings are supported. Formatting and rename preserve
existing line endings.

### Imports

`import 'libs.vim'` exposes exported members as `libs.Two`; use `as` to choose
a different namespace name.

Ordinary `import` completion shows full runtimepath file paths and offers nearby
files and directories with a `./` prefix. `import autoload` completes runtimepath
files and directories one level at a time. Typing `/` continues path completion.
The current file is excluded from import suggestions.

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
