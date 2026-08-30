GO ?= go
GO_MOD ?= -mod=readonly
COVERAGE_MIN ?= 90
VIM_SOURCE ?= /Users/chemzqm/lib/vim

.PHONY: build check coverage format-check generate-official race test vet

build:
	mkdir -p bin
	$(GO) build $(GO_MOD) -o bin/vimls ./cmd/vimls
	$(GO) build $(GO_MOD) -o bin/vimparse ./cmd/vimparse
	$(GO) build $(GO_MOD) -o bin/vim9parse ./cmd/vim9parse

test:
	$(GO) test $(GO_MOD) ./...

generate-official:
	$(GO) run $(GO_MOD) ./tools/genofficial -vim-source "$(VIM_SOURCE)"

race:
	$(GO) test $(GO_MOD) -race ./...

vet:
	$(GO) vet $(GO_MOD) ./...

format-check:
	@test -z "$$(gofmt -l $$(find cmd internal test tools -name '*.go' -type f))"

coverage:
	$(GO) test $(GO_MOD) -coverpkg=./internal/... -coverprofile=coverage.out ./...
	$(GO) run $(GO_MOD) ./tools/covercheck -profile coverage.out -min $(COVERAGE_MIN)

check: format-check test race vet coverage build
