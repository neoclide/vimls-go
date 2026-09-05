# Language server features

vimls-go supports Legacy Vim script and Vim9 script from Vim 9.1 through
Vim v9.2.1015. Syntax added after v9.2.1015 is not supported until the project
updates its pinned Vim version.

The server analyzes source files without starting Vim or executing Vim script.
It works with workspace files and configured `runtimepath` files. Outside
workspace folders it parses recursive `plugin`, `autoload`, and `import` Vim
scripts; top-level `colors/*.vim` files provide only colorscheme name/path
completion metadata. Other runtime subtrees are not scanned.

## Available features

| Feature | Support |
| --- | --- |
| Diagnostics | Syntax errors, unresolved names, invalid calls, imports and statically provable Vim9 type errors. Capable clients use document and workspace pull diagnostics; legacy clients use push diagnostics. |
| Completion | Commands, variables, functions, options, events, mappings, highlight and syntax groups, imports, members, autoload names, color schemes and other context-specific values. |
| Hover | Symbol kind, type, signature, source comments and pinned Vim help. Variables always show a type, including `unknown` and explicit Vim9 `any`. |
| Signature help | Built-in functions and statically resolved user functions, imported functions, function values, methods and constructors. |
| Navigation | Definition, declaration, type definition, references, document highlights and document links for statically resolved local, imported, global and autoload symbols. |
| Hierarchies and implementations | Direct type supertypes/subtypes, interface and abstract-class implementations, compatible member providers, and incoming/outgoing calls for statically resolved named callables. |
| Code Lens | Reference counts for named Legacy and Vim9 functions, methods and constructors. Implementation counts are limited to Vim9 abstract-class and interface methods. Clickable navigation depends on client support for `editor.action.showReferences`. |
| Symbols and structure | Document symbols, workspace symbols, folding ranges and selection ranges. |
| Safe editing | Prepare rename, rename, semantic highlighting, inferred-type inlay hints and a small set of syntax quick fixes. |
| Formatting | Source-preserving indentation for whole documents, selected ranges and on-type newline/continuation triggers. Clients that declare `textDocument.rangeFormatting.rangesSupport` may format several selected ranges in one request. Only proven leading whitespace is changed. |
| Workspace updates | Incremental document synchronization, workspace folders, watched Vim files and delta runtimepath refreshes through `vimls/didChangeRuntimepath` (request or compatible notification). |

Legacy and Vim9 files may use the same workspace. The server understands
`vim9script`, `scriptversion`, `vim9cmd` and `legacy` when choosing the relevant
language rules.

## Current limitations

- File-command `++opt` and `+cmd` prefixes preserve their original source spans
  and outer command boundaries. Escaped `+cmd` payloads remain opaque; they are
  not decoded or checked as standalone commands. User-command replacements and
  undecoded mapping key notation likewise receive only conservative diagnostics.
- Document, range, multi-range and on-type formatting adjust indentation only;
  they do not rewrite expressions, spacing, wrapping or embedded languages.
- Legacy tuples have direct type/reference, fixed-index, destructuring and loop
  analysis. Slice arity and nonliteral destructuring cardinality remain unknown;
  legacy tuple mutation errors require adjacent literal assignments, not merely
  a stored type that dynamic execution could invalidate.
- Rename is offered only when every affected symbol can be resolved safely.
  Autoload function rename and other namespace-changing edits are rejected.
- Code actions are limited to a few unambiguous syntax repairs; there is no
  source-wide rewriting or speculative type fix.
- Dynamic behavior such as `execute()`, `eval()`, runtime-created functions,
  mutable runtimepath state and dynamically formed names cannot always be
  resolved. The server returns no result or keeps the type `unknown` instead of
  guessing.
- Dynamically sourced runtime files are not discovered unless they also match
  the external runtimepath layout above or are inside a workspace folder.
- Call hierarchy excludes lambdas and deferred mapping, autocommand and user
  command bodies. Type aliases are followed only when they resolve uniquely to
  a class, interface or enum.
- Embedded Python, Ruby, Perl, Lua, shell and other heredoc languages are
  preserved as source ranges but are not analyzed.
- Native `def` inside a Legacy-root file and Legacy `function` inside a
  Vim9-root file are retained safely, but their mixed-dialect semantics are not
  complete.
- Syntax introduced after Vim v9.2.1015 is not supported.

## Configuration

Runtime roots can be configured and changed dynamically.

See [Client configuration](configuration.md) for the supported settings and
runtimepath request.
