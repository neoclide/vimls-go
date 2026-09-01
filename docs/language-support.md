# Language server features

vimls-go supports Legacy Vim script and Vim9 script from Vim 9.1 through
Vim v9.2.1015. Syntax added after v9.2.1015 is not supported until the project
updates its pinned Vim version.

The server analyzes source files without starting Vim or executing Vim script.
It works with workspace files and configured `runtimepath` files, including
plugins, autoload scripts, imports, syntax files and color schemes.

## Available features

| Feature | Support |
| --- | --- |
| Diagnostics | Syntax errors, version compatibility, unresolved names, invalid calls, imports and statically provable Vim9 type errors. |
| Completion | Commands, variables, functions, options, events, mappings, highlight and syntax groups, imports, members, autoload names, color schemes and other context-specific values. |
| Hover | Symbol kind, type, signature, source comments and pinned Vim help. Variables always show a type, including `unknown` and explicit Vim9 `any`. |
| Signature help | Built-in functions and statically resolved user functions, imported functions, function values, methods and constructors. |
| Navigation | Definition, declaration, references, document highlights and document links for statically resolved local, imported, global and autoload symbols. |
| Symbols and structure | Document symbols, workspace symbols, folding ranges and selection ranges. |
| Safe editing | Prepare rename, rename, semantic highlighting, inferred-type inlay hints and a small set of syntax quick fixes. |
| Workspace updates | Incremental document synchronization, workspace folders, watched Vim files and runtimepath refreshes. |

Legacy and Vim9 files may use the same workspace. The server understands
`vim9script`, `scriptversion`, `vim9cmd` and `legacy` when choosing the relevant
language rules.

## Current limitations

- Formatting, on-type formatting, call hierarchy, type hierarchy and
  implementation lookup are not available.
- Rename is offered only when every affected symbol can be resolved safely.
  Autoload function rename and other namespace-changing edits are rejected.
- Code actions are limited to a few unambiguous syntax repairs; there is no
  source-wide rewriting or speculative type fix.
- Dynamic behavior such as `execute()`, `eval()`, runtime-created functions,
  mutable runtimepath state and dynamically formed names cannot always be
  resolved. The server returns no result or keeps the type `unknown` instead of
  guessing.
- Embedded Python, Ruby, Perl, Lua, shell and other heredoc languages are
  preserved as source ranges but are not analyzed.
- Native `def` inside a Legacy-root file and Legacy `function` inside a
  Vim9-root file are retained safely, but their mixed-dialect semantics are not
  complete.
- Syntax introduced after Vim v9.2.1015 is not supported.

## Configuration

The default target is Vim v9.2.1015. `vimls.targetVersion` may select an older
Vim 9.1 or 9.2 patch for compatibility diagnostics. Runtime roots and
diagnostic severity can also be configured.

See [Client configuration](configuration.md) for the supported settings and
runtimepath notification.
