# Coverage findings

## 2026-09-01: project coverage is below the requested target

The full internal profile produced by:

```sh
go test -mod=readonly -coverpkg=./internal/... -coverprofile=coverage.out ./...
go run -mod=readonly ./tools/covercheck -profile coverage.out -min 95
```

reports `92.26% (23486/25455)`. The existing 90% gate therefore cannot prove
the requested 95% line/statement coverage target.

The largest uncovered groups are LSP hierarchy, completion, language features,
workspace navigation, rename, and document-structure paths. This is a test
coverage gap; no production behavior defect has been established from the
coverage data alone. Add behavior-level tests for those paths, then raise the
checked-in coverage threshold only after the complete profile reaches 95%.
