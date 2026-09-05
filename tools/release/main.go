package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type releaseTarget struct{ goos, goarch, goarm string }

var targets = []releaseTarget{
	{"darwin", "amd64", ""}, {"darwin", "arm64", ""},
	{"linux", "amd64", ""}, {"linux", "arm64", ""}, {"linux", "arm", "7"},
	{"windows", "amd64", ""}, {"windows", "arm64", ""}, {"freebsd", "amd64", ""},
}

type archiveEntry struct {
	name string
	data []byte
	mode int64
}

func main() {
	version := flag.String("version", "", "release tag, for example v1.0.0")
	epoch := flag.Int64("epoch", 0, "SOURCE_DATE_EPOCH timestamp")
	output := flag.String("output-dir", "dist", "archive output directory")
	notesOutput := flag.String("notes-output", "", "optional destination for this version's CHANGELOG release notes")
	flag.Parse()
	if !regexp.MustCompile(`^v[0-9][0-9A-Za-z.+-]*$`).MatchString(*version) || *epoch <= 0 {
		fatalf("-version vX.Y.Z and a positive -epoch are required")
	}
	if err := os.MkdirAll(*output, 0o755); err != nil {
		fatalf("%v", err)
	}
	stamp := time.Unix(*epoch, 0).UTC()
	documents, err := releaseDocuments(".")
	if err != nil {
		fatalf("%v", err)
	}
	if *notesOutput != "" {
		for _, document := range documents {
			if document.name == "CHANGELOG.md" {
				notes, err := releaseNotes(string(document.data), *version)
				if err != nil {
					fatalf("%v", err)
				}
				if err := os.WriteFile(*notesOutput, []byte(notes), 0o644); err != nil {
					fatalf("%v", err)
				}
			}
		}
	}
	var assets []string
	for _, target := range targets {
		data, err := buildBinary(*version, target)
		if err != nil {
			fatalf("%v", err)
		}
		paths, err := writeTargetAssets(*output, *version, target, data, documents, stamp)
		if err != nil {
			fatalf("%v", err)
		}
		assets = append(assets, paths...)
	}
	if err := writeChecksums(filepath.Join(*output, "checksums.txt"), assets); err != nil {
		fatalf("%v", err)
	}
}

// releaseNotes selects an exact version from our "## vX.Y.Z" changelog format.
// Date/status suffixes on the heading are not part of the published notes.
func releaseNotes(changelog, version string) (string, error) {
	var body []string
	found, selected := false, false
	for _, line := range strings.Split(strings.ReplaceAll(changelog, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "## ") {
			fields := strings.Fields(line)
			selected = len(fields) > 1 && fields[1] == version
			if selected {
				if found {
					return "", fmt.Errorf("duplicate CHANGELOG section for %s", version)
				}
				found = true
			}
			continue
		}
		if selected {
			body = append(body, line)
		}
	}
	notes := strings.TrimSpace(strings.Join(body, "\n"))
	if !found || notes == "" {
		return "", fmt.Errorf("missing or empty CHANGELOG section for %s", version)
	}
	// Repository-relative documentation links need an explicit tag on a Release page.
	notes = strings.ReplaceAll(notes, "](docs/", "](https://github.com/neoclide/vimls-go/blob/"+version+"/docs/")
	return notes + "\n", nil
}

func buildBinary(version string, target releaseTarget) ([]byte, error) {
	temporary, err := os.MkdirTemp("", "vimls-release-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temporary)
	binary := filepath.Join(temporary, "vimls")
	command := exec.Command("go", "build", "-mod=readonly", "-trimpath", "-ldflags=-s -w -buildid= -X github.com/neoclide/vimls-go/internal/server.Version="+version, "-o", binary, "./cmd/vimls")
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+target.goos, "GOARCH="+target.goarch, "GOARM="+target.goarm)
	if output, err := command.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("build %s/%s: %w\n%s", target.goos, target.goarch, err, output)
	}
	return os.ReadFile(binary)
}

func releaseDocuments(root string) ([]archiveEntry, error) {
	names := []string{"README.md", "CHANGELOG.md", "docs/language-support.md"}
	licenses, err := os.ReadDir(filepath.Join(root, "LICENSES"))
	if err != nil {
		return nil, err
	}
	for _, license := range licenses {
		if !license.Type().IsRegular() {
			return nil, fmt.Errorf("non-regular license file %s", license.Name())
		}
		names = append(names, "LICENSES/"+license.Name())
	}
	sort.Strings(names)
	entries := make([]archiveEntry, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		entries = append(entries, archiveEntry{name: name, data: data, mode: 0o644})
	}
	return entries, nil
}

func writeTargetAssets(output, version string, target releaseTarget, data []byte, documents []archiveEntry, stamp time.Time) ([]string, error) {
	arch := target.goarch
	if target.goarm != "" {
		arch = "armv" + target.goarm
	}
	extension, executableSuffix := ".tar.gz", ""
	if target.goos == "windows" {
		extension, executableSuffix = ".zip", ".exe"
	}
	binary := filepath.Join(output, "vimls-"+target.goos+"-"+arch+executableSuffix)
	if err := os.WriteFile(binary, data, 0o755); err != nil {
		return nil, err
	}
	entries := append([]archiveEntry{{name: "vimls" + executableSuffix, data: data, mode: 0o755}}, documents...)
	archive := filepath.Join(output, "vimls-"+version+"-"+target.goos+"-"+arch+extension)
	if err := writeArchive(archive, entries, stamp, target.goos == "windows"); err != nil {
		return nil, err
	}
	return []string{binary, archive}, nil
}

func writeArchive(path string, entries []archiveEntry, stamp time.Time, zipped bool) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()
	if zipped {
		writer := zip.NewWriter(file)
		for _, source := range entries {
			header := &zip.FileHeader{Name: source.name, Method: zip.Deflate}
			header.SetMode(os.FileMode(source.mode))
			header.Modified = stamp
			entry, err := writer.CreateHeader(header)
			if err == nil {
				_, err = entry.Write(source.data)
			}
			if err != nil {
				return err
			}
		}
		return writer.Close()
	}
	gzipWriter, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return err
	}
	gzipWriter.Header.ModTime = stamp
	tarWriter := tar.NewWriter(gzipWriter)
	for _, source := range entries {
		header := &tar.Header{Name: source.name, Mode: source.mode, Size: int64(len(source.data)), ModTime: stamp, AccessTime: stamp, ChangeTime: stamp, Format: tar.FormatPAX}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tarWriter.Write(source.data); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}

func writeChecksums(path string, archives []string) error {
	sort.Strings(archives)
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, archive := range archives {
		input, err := os.Open(archive)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if _, err := fmt.Fprintf(file, "%x  %s\n", hash.Sum(nil), filepath.Base(archive)); err != nil {
			return err
		}
	}
	return nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, "release:", fmt.Sprintf(format, args...))
	os.Exit(1)
}
