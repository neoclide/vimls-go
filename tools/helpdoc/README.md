# Runtime help extraction experiment

Build and run from the repository root:

```sh
go build -o bin/helpdoc ./tools/helpdoc
./bin/helpdoc -runtimepath-file /path/to/runtimepath.txt \
  -output /path/to/runtime-help.md -repeat 5 > /path/to/stats.json
```

Alternatively use `-runtimepath '/one/root,/another/root'`. The file input is
the same comma-separated Vim option value, with pasted CR/LF line wraps removed;
commas in paths can be escaped as `\,`. It does not launch Vim or source plugins.
Output directories must already exist. The Markdown destination is overwritten.

The experiment scans exactly `doc/*.txt` under each runtimepath root, preserving
root order and deduplicating canonical roots and files. It does not recursively
scan `doc/`, translated `*.??x` files, or packages outside the supplied roots.
Missing roots and inaccessible directories are reported in both Markdown and
JSON; a failure to read a discovered file or write the report fails the command.

Extraction uses help definitions directly, without relying on generated `tags`:

- `*g:name*`: global variables, including names containing `#`.
- `*Name()*`, `*g:Name()*`, and lowercase built-in function tags: global functions.
- `*plugin#name()*` and `*plugin#name*`: autoload functions.
- `*<Plug>(plugin-name)*`: named `<Plug>` mappings.
- Adjacent alias tags share a description. Duplicate definitions remain separate
  entries with full source paths and one-based tag line numbers.

Every help tag and section separator bounds the preceding description. Example
blocks preserve literal text, references become inline code, and help headings
become Markdown headings, using the existing `vimhelp.ToMarkdown` converter.
The help markup reference is the read-only official Vim `v9.2.1015` version of
`runtime/doc/helphelp.txt`, section `help-writing`.

Help is free-form: untagged descriptions, dictionary-member/pattern tags, and
plain function names without `()` are not inferred. Tags are documentation
claims, not proof of a runtime definition. The tool does not validate symbols by
parsing or executing the associated Vim scripts. Section boundaries are a
heuristic and may require plugin-specific refinement after inspecting results.
The converter supports Vim help markup, not arbitrary embedded HTML/Markdown.

Each run rediscovers and rereads all inputs and retains extracted Markdown in
memory. JSON reports discovery, reading, extraction plus Markdown conversion,
total load time, and total Go allocation. Forced GC before/after each run and
the heap-delta samples are excluded from load timings; automatic GC is included.
The final retained heap delta is an approximate Go heap measurement, not RSS.
`markdown_body_bytes` sums per entry, counting shared alias bodies repeatedly.
Report assembly and writing are measured separately after the final run; write
time measures `os.WriteFile`, without an `fsync`. Filesystem caches are not
cleared: the first run is not a guaranteed cold-cache measurement. Build and
process startup are excluded from the internal measurements.

The server shares the same extractor and loads runtime help in the background.
This tool measures complete scans; server runtimepath updates reuse retained
directories and parse only newly added directories. Local inputs and results can be kept in ignored
`bin/helpdoc-report/`, so third-party documentation is not committed.

Focused validation:

```sh
go test ./tools/helpdoc ./internal/vimhelp
```
