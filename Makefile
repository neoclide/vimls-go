GO ?= go
GO_MOD ?= -mod=readonly
COVERAGE_MIN ?= 90

.PHONY: build check coverage format-check metadata-check metadata-refresh oracle race test vet

build:
	mkdir -p bin
	$(GO) build $(GO_MOD) -o bin/vimls ./cmd/vimls
	$(GO) build $(GO_MOD) -o bin/vimparse ./cmd/vimparse
	$(GO) build $(GO_MOD) -o bin/vim9parse ./cmd/vim9parse

test:
	$(GO) test $(GO_MOD) ./...

race:
	$(GO) test $(GO_MOD) -race ./...

vet:
	$(GO) vet $(GO_MOD) ./...

format-check:
	@test -z "$$(gofmt -l $$(find cmd internal test tools -name '*.go' -type f))"

metadata-refresh:
	@test -n "$(VIM_SOURCE)" || (echo "set VIM_SOURCE to the official Vim checkout" >&2; exit 1)
	$(GO) run $(GO_MOD) ./tools/genmetadata -vim-root "$(VIM_SOURCE)"

metadata-check:
	@test -n "$(VIM_SOURCE)" || (echo "set VIM_SOURCE to the official Vim checkout" >&2; exit 1)
	@set -eu; \
	metadata_tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$metadata_tmp"' EXIT; \
	$(GO) run $(GO_MOD) ./tools/genmetadata -vim-root "$(VIM_SOURCE)" -output-dir "$$metadata_tmp"; \
	cmp internal/vimdata/commands_generated.go "$$metadata_tmp/commands_generated.go"; \
	cmp internal/vimdata/functions_generated.go "$$metadata_tmp/functions_generated.go"; \
	cmp internal/vimdata/options_generated.go "$$metadata_tmp/options_generated.go"; \
	cmp internal/vimdata/variables_generated.go "$$metadata_tmp/variables_generated.go"
	$(GO) test $(GO_MOD) ./internal/vimdata ./tools/genmetadata ./tools/internal/vimhelp

oracle:
	@test -n "$(VIM_EXECUTABLE)" || (echo "set VIM_EXECUTABLE to the pinned Vim v9.2.1015 binary" >&2; exit 1)
	VIM_EXECUTABLE="$(VIM_EXECUTABLE)" $(GO) test $(GO_MOD) -v ./test/oracle

coverage:
	$(GO) test $(GO_MOD) -coverpkg=./internal/... -coverprofile=coverage.out ./...
	$(GO) run $(GO_MOD) ./tools/covercheck -profile coverage.out -min $(COVERAGE_MIN)

check: format-check test race vet coverage build
