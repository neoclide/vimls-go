# Inspecting runtime help

This tool extracts the help that vimls-go can show for plugin functions,
variables, commands and named `<Plug>` mappings. Use it to check a missing
description or measure help-loading work.

Run from the repository root:

```sh
go build -o bin/helpdoc ./tools/helpdoc
./bin/helpdoc -runtimepath-file /path/to/runtimepath.txt \
  -output /path/to/runtime-help.md -repeat 5 > /path/to/stats.json
```

The input file contains the comma-separated value of Vim's `runtimepath`.
Line wraps are removed; escape a comma inside a path as `\,`.
You can also pass `-runtimepath '/one/root,/another/root'` directly.

Output directories must already exist. The Markdown report is overwritten,
and the JSON statistics are written to stdout. The tool does not launch Vim
or source plugins.

## What it reads

It reads `doc/*.txt` under the supplied roots, in runtimepath order. It does
not recurse below `doc`, read translated help files, or find packages outside
those roots.

Descriptions come from help tags such as `*g:name*`, `*Name()*`,
`*plugin#name()*` and `*<Plug>(plugin-name)*`. Reports keep source paths and
line numbers so you can inspect a surprising result. Untagged prose and
dictionary-member descriptions are not inferred.

A help tag is a documentation entry, not proof that a function exists. The
server uses source indexing separately for that.

## Reading the measurements

The JSON separates discovery, reading, extraction and report writing. Each
repeat performs a complete scan; the running server can reuse unchanged roots,
so these timings do not describe every server update.

Filesystem caches are not cleared. The first sample is not guaranteed to be
cold, and the reported Go heap change is not process RSS. Use the same inputs
and toolchain for comparisons.

Keep local reports in ignored `bin/helpdoc-report/` if they contain third-party
documentation. Focused checks for this tool are:

```sh
go test -count=1 ./tools/helpdoc ./internal/vimhelp
```
