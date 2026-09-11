GO ?= go
GO_MOD ?= -mod=readonly
TEST_PARALLEL ?= 4
COVERAGE_MIN ?= 90
VIM_EXECUTABLE ?= vim
RELEASE_REMOTE ?= origin
export RELEASE_REMOTE

.PHONY: build check clean client-smoke client-tools coverage format-check incr metadata-check metadata-refresh oracle race release test vet

build:
	mkdir -p bin
	@set -eu; \
	build_version="$$(cat VERSION)-dev"; \
	echo "Building bin/vimls ($$build_version)"; \
	$(GO) build $(GO_MOD) -ldflags "-X github.com/neoclide/vimls-go/internal/server.Version=$$build_version" -o bin/vimls ./cmd/vimls
	$(GO) build $(GO_MOD) -o bin/vimparse ./cmd/vimparse

incr:
	@set -eu; \
	new_version="$$(awk -F. 'NR == 1 && NF == 3 && $$1 ~ /^[0-9]+$$/ && $$2 ~ /^[0-9]+$$/ && $$3 ~ /^[0-9]+$$/ { printf "%s.%s.%d\n", $$1, $$2, $$3 + 1; valid = 1; next } { invalid = 1 } END { if (!valid || invalid) exit 1 }' VERSION)" || \
		{ echo 'Invalid VERSION file: expected MAJOR.MINOR.PATCH.' >&2; exit 1; }; \
	printf '%s\n' "$$new_version" > VERSION; \
	git commit --only -m "chore: bump version to $$new_version" -- VERSION; \
	echo "VERSION is now $$new_version"

clean:
	$(GO) clean -cache -testcache -fuzzcache

test:
	$(GO) test $(GO_MOD) -count=1 -p $(TEST_PARALLEL) ./...

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
	cmp internal/vimdata/options_set_generated.vim "$$metadata_tmp/options_set_generated.vim"; \
	cmp internal/vimdata/variables_generated.go "$$metadata_tmp/variables_generated.go"
	$(GO) test $(GO_MOD) ./internal/vimdata ./tools/genmetadata ./internal/vimhelp

oracle:
	@test -n "$(VIM_EXECUTABLE)" || (echo "set VIM_EXECUTABLE to the pinned Vim v9.2.1015 binary" >&2; exit 1)
	VIM_EXECUTABLE="$(VIM_EXECUTABLE)" $(GO) test $(GO_MOD) -v ./test/oracle

client-tools:
	./test/clients/setup-vim-lsp.sh

client-smoke: build client-tools
	@set -eu; \
	client_tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$client_tmp"' EXIT; \
	client_status=0; \
	VIMLS_BINARY="$(CURDIR)/bin/vimls" \
	VIMLS_VIM_LSP="$(CURDIR)/.test-tools/vim-lsp" \
	VIMLS_CLIENT_WORKSPACE="$(CURDIR)/test/clients/vim-lsp/workspace" \
	VIMLS_CLIENT_RESULT="$$client_tmp/result.txt" \
	VIMLS_CLIENT_LOG="$$client_tmp/vim-lsp.log" \
	"$(VIM_EXECUTABLE)" -Nu test/clients/vim-lsp/vimrc -U NONE -n -es -X -i NONE -S test/clients/vim-lsp/smoke.vim || client_status=$$?; \
	test ! -f "$$client_tmp/result.txt" || cat "$$client_tmp/result.txt"; \
	if test $$client_status -ne 0; then test ! -f "$$client_tmp/vim-lsp.log" || cat "$$client_tmp/vim-lsp.log"; fi; \
	test $$client_status -eq 0

coverage:
	$(GO) test $(GO_MOD) -coverpkg=./internal/... -coverprofile=coverage.out ./...
	$(GO) run $(GO_MOD) ./tools/covercheck -profile coverage.out -min $(COVERAGE_MIN)

check: format-check test race vet coverage build

# Push only the release tag; GitHub Actions builds and publishes its commit.
release:
	@set -eu; \
	release_tag="v$$(cat VERSION)"; \
	printf '%s\n' "$$release_tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$$' || \
	  { echo 'Invalid VERSION file: expected a version such as 0.1.3 without a leading v.' >&2; exit 1; }; \
	if git show-ref --verify --quiet "refs/tags/$$release_tag"; then \
	  echo "Local tag $$release_tag already exists; skipping release."; \
	  exit 0; \
	fi; \
	test -z "$$(git status --porcelain)" || \
	  { echo 'Commit or stash working-tree changes before releasing.' >&2; exit 1; }; \
	$(GO) run $(GO_MOD) ./tools/release -version "$$release_tag" -check-changelog; \
	git remote get-url "$$RELEASE_REMOTE" >/dev/null; \
	git tag -a "$$release_tag" -m "Release $$release_tag"; \
	git push "$$RELEASE_REMOTE" "refs/tags/$$release_tag:refs/tags/$$release_tag"
