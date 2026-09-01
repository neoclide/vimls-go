#!/bin/sh
set -eu

commit=e10d186452743beb7b43d2b3427020832f930c2b
archive_sha256=32950ab381a9244e8e795ea60e8479631e438f91a788c19b39f10e5d54b32257
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository=$(CDPATH= cd -- "$script_dir/../.." && pwd)
destination=${VIMLS_VIM_LSP:-"$repository/.test-tools/vim-lsp"}
marker=$destination/.vimls-commit

if test -f "$marker" && test "$(sed -n '1p' "$marker")" = "$commit" && test -f "$destination/autoload/lsp.vim"; then
  exit 0
fi
if test -e "$destination"; then
  echo "remove or relocate mismatched vim-lsp directory: $destination" >&2
  exit 1
fi

temporary=$(mktemp -d "${TMPDIR:-/tmp}/vimls-go-vim-lsp.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
archive=$temporary/vim-lsp.tar.gz
curl -fsSL "https://codeload.github.com/prabirshrestha/vim-lsp/tar.gz/$commit" -o "$archive"
if command -v sha256sum >/dev/null 2>&1; then
  printf '%s  %s\n' "$archive_sha256" "$archive" | sha256sum -c -
else
  printf '%s  %s\n' "$archive_sha256" "$archive" | shasum -a 256 -c -
fi
mkdir -p "$(dirname -- "$destination")"
tar -xzf "$archive" -C "$temporary"
mv "$temporary/vim-lsp-$commit" "$destination"
printf '%s\n' "$commit" >"$marker"
