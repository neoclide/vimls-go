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

Document text supports LF, CRLF and CR line endings, including mixed files.
Positions follow the negotiated UTF-8, UTF-16 or UTF-32 encoding. Formatting
and rename preserve the original line-ending bytes.

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
