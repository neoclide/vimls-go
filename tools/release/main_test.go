package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWriteArchiveIsDeterministic(t *testing.T) {
	stamp := time.Unix(1700000000, 0).UTC()
	for _, zipped := range []bool{false, true} {
		entries := []archiveEntry{{name: "vimls", data: []byte("binary"), mode: 0o755}, {name: "LICENSES/MIT.txt", data: []byte("license"), mode: 0o644}}
		first := filepath.Join(t.TempDir(), "first")
		second := filepath.Join(t.TempDir(), "second")
		if err := writeArchive(first, entries, stamp, zipped); err != nil {
			t.Fatal(err)
		}
		if err := writeArchive(second, entries, stamp, zipped); err != nil {
			t.Fatal(err)
		}
		left, _ := os.ReadFile(first)
		right, _ := os.ReadFile(second)
		if !bytes.Equal(left, right) {
			t.Fatal("archives differ")
		}
	}
}

func TestReleaseAssetsPreserveDownloadContract(t *testing.T) {
	documents, err := releaseDocuments(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) < 3 {
		t.Fatal("README and both licenses are required")
	}
	output := t.TempDir()
	stamp := time.Unix(1700000000, 0).UTC()
	var assets, names []string
	for _, target := range targets {
		paths, err := writeTargetAssets(output, "v1.2.3", target, []byte("binary"), documents, stamp)
		if err != nil {
			t.Fatal(err)
		}
		assets = append(assets, paths...)
		for _, path := range paths {
			names = append(names, filepath.Base(path))
		}
		entries := map[string][]byte{}
		check := func(name string, mode os.FileMode, modified time.Time, r io.Reader) {
			t.Helper()
			wantMode := os.FileMode(0o644)
			if name == "vimls" || name == "vimls.exe" {
				wantMode = 0o755
			}
			if mode.Perm() != wantMode || !modified.Equal(stamp) {
				t.Fatalf("%s mode/time = %v %v", name, mode, modified)
			}
			data, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			entries[name] = data
		}
		if target.goos == "windows" {
			reader, err := zip.OpenReader(paths[1])
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range reader.File {
				r, err := file.Open()
				if err != nil {
					t.Fatal(err)
				}
				check(file.Name, file.Mode(), file.Modified, r)
				r.Close()
			}
			reader.Close()
		} else {
			file, err := os.Open(paths[1])
			if err != nil {
				t.Fatal(err)
			}
			gz, err := gzip.NewReader(file)
			if err != nil {
				t.Fatal(err)
			}
			reader := tar.NewReader(gz)
			for {
				header, err := reader.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				check(header.Name, os.FileMode(header.Mode), header.ModTime, reader)
			}
			gz.Close()
			file.Close()
		}
		executable := "vimls"
		if target.goos == "windows" {
			executable += ".exe"
		}
		if len(entries) != len(documents)+1 || string(entries[executable]) != "binary" {
			t.Fatalf("archive entries = %#v", entries)
		}
		for _, document := range documents {
			if !bytes.Equal(entries[document.name], document.data) {
				t.Fatalf("missing or changed %s", document.name)
			}
		}
	}
	want := []string{}
	for _, platform := range []string{"darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64", "linux-armv7", "windows-amd64", "windows-arm64", "freebsd-amd64"} {
		extension, suffix := ".tar.gz", ""
		if strings.HasPrefix(platform, "windows-") {
			extension, suffix = ".zip", ".exe"
		}
		want = append(want, "vimls-"+platform+suffix, "vimls-v1.2.3-"+platform+extension)
	}
	slices.Sort(want)
	slices.Sort(names)
	if !slices.Equal(names, want) {
		t.Fatalf("assets = %v, want %v", names, want)
	}
	checksumPath := filepath.Join(output, "checksums.txt")
	if err := writeChecksums(checksumPath, assets); err != nil {
		t.Fatal(err)
	}
	checksums, err := os.ReadFile(checksumPath)
	if err != nil || len(strings.Split(strings.TrimSpace(string(checksums)), "\n")) != len(assets) {
		t.Fatalf("checksums = %s, %v", checksums, err)
	}
	for _, path := range assets {
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(checksums), fmt.Sprintf("%x  %s\n", sha256.Sum256(data), filepath.Base(path))) {
			t.Fatalf("checksum missing for %s, %v", path, err)
		}
	}
	workflow, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil || !bytes.Contains(workflow, []byte("go run -mod=readonly ./tools/release")) || bytes.Contains(workflow, []byte("TARGETS=(")) {
		t.Fatalf("workflow is not using the tested packager: %v", err)
	}
}
