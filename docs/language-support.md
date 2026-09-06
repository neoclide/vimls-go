# What vimls-go supports

vimls-go supports Legacy Vim script and Vim9 script through **Vim v9.2.1015**,
including Vim9 classes, interfaces, enums and imports. Both dialects can be used
in the same project.

These docs describe the current source. Check the
[changelog](../CHANGELOG.md) for features that have not reached a release yet.

## Writing and reading code

| Feature | What you can use it for |
| --- | --- |
| Completion | Commands, functions, scoped variables, options, events, mappings, highlight groups, imports, class members, autoload names and color schemes. |
| Hover | See a symbol's type, function signature, source comments and available runtime help. |
| Signature help | Check parameters while calling a built-in function, user function, method or constructor. |
| Diagnostics | Find syntax errors, missing names, invalid calls and Vim9 type errors that can be checked from source. |
| Inlay hints | See inferred Vim9 variable and return types beside the code. |
| Semantic highlighting | Distinguish types, functions, variables, parameters and modifiers. |

Completion follows the command being edited. For example, `:set` offers options,
`:autocmd` offers events, and mappings offer key names and modifiers.

The parser handles unfinished code so that an incomplete line does not prevent
you from working on the rest of the file.

## Finding and changing things

Definition, declaration, type-definition and reference lookup work with
resolved local symbols, workspace files, imports and autoload functions.
You can also browse the file outline, search workspace symbols, fold blocks
and expand a selection.

For Vim9 classes, you can follow inheritance and find interface or abstract
method implementations. Call hierarchy shows incoming and outgoing calls for
named functions, methods and constructors that the server can resolve.

Code Lens shows reference counts and, for interface and abstract-class methods,
implementation counts. Clicking a count requires a client that supports
`editor.action.showReferences`.

Rename checks the affected references before offering edits. It refuses
ambiguous targets and changes that would require renaming autoload files or
namespaces. Quick fixes cover a small set of clear syntax errors.

Formatting adjusts indentation for a file or selection, and on supported typing
events. It leaves expressions, spacing within lines, line wrapping and embedded
language bodies alone.

## Plugin files and help

The server indexes Vim files in your workspace. Outside the workspace,
`runtimepath` is scanned more narrowly:

| Location under each runtime root | Used for |
| --- | --- |
| `plugin/**/*.vim`, `autoload/**/*.vim`, `import/**/*.vim` | Symbols, completion and navigation. |
| `colors/*.vim` | Color-scheme names and paths. These files are not parsed. |
| `doc/*.txt` | Help for functions, variables, Ex commands and named `<Plug>` mappings. |

Other directories under a runtime root, such as `ftplugin` and `syntax`, are
not scanned. They can still be analyzed if they are inside a workspace folder.

Help loads in the background, so an early hover may have a signature before
its help text is ready. Help tags identify documentation; they do not always
mean a source definition is available.

Built-in function and command descriptions come from runtime help. Built-in
signatures, option data and the language rules remain tied to the supported
Vim version. For path setup and refreshing changed help files, see
[configuration](https://github.com/neoclide/vimls-go/blob/main/docs/configuration.md).

## Mappings and configuration files

The server follows references in supported autocommand bodies, `<expr>`
mappings, `<Cmd>` bodies and static function-name callback options.

Recognized mapping expression prompts include `<C-R>=`, its register-insertion
variants, command-line `<C-\>e`, and Normal-mode `"=` / `@=`. Key sequences
are interpreted in their mode: `<C-O>=` is an indent command, for example.
Cancelled prompts and expressions with undecoded editing keys are left alone.
The result of an expression is never executed or treated as generated code.

Vimrc-style files receive different style suggestions from plugin files.
See [editing configuration files](https://github.com/neoclide/vimls-go/blob/main/docs/userconfig.md).

## Limits to keep in mind

- Dynamic `execute`, `eval`, function names and loading order cannot always be
  resolved. Types may remain `unknown`, and navigation may return no result.
- A file that loads another script dynamically may need the script added to a
  workspace or a supported runtime directory.
- Mixing `def` into a Legacy script, or `function` into a Vim9 script, is
  accepted, but analysis inside those functions is incomplete.
- Call hierarchy does not include lambdas or calls inside deferred mappings,
  autocommands and user commands.
- Embedded Lua, Python, shell and other languages are preserved but not analyzed.
- Syntax added after Vim v9.2.1015 is unsupported until the version pin advances.
- Files larger than 4 MiB are synchronized but not analyzed.

Some Neovim names are recognized for compatibility, but full Neovim API
completion and type checking are not provided. See
[Neovim setup](https://github.com/neoclide/vimls-go/blob/main/docs/neovim.md).
