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
	"sort"
	"strings"
	"time"
)

var targets = []struct{ goos, goarch string }{
	{"darwin", "amd64"}, {"darwin", "arm64"},
	{"linux", "amd64"}, {"linux", "arm64"},
	{"windows", "amd64"}, {"windows", "arm64"},
}

func main() {
	version := flag.String("version", "", "release tag, for example v1.0.0")
	epoch := flag.Int64("epoch", 0, "SOURCE_DATE_EPOCH timestamp")
	output := flag.String("output-dir", "dist", "archive output directory")
	flag.Parse()
	if *version == "" || !strings.HasPrefix(*version, "v") || *epoch <= 0 {
		fatalf("-version vX.Y.Z and a positive -epoch are required")
	}
	if err := os.MkdirAll(*output, 0o755); err != nil {
		fatalf("%v", err)
	}
	stamp := time.Unix(*epoch, 0).UTC()
	var archives []string
	for _, target := range targets {
		name := "vimls-go_" + strings.TrimPrefix(*version, "v") + "_" + target.goos + "_" + target.goarch
		executable := "vimls"
		if target.goos == "windows" {
			executable += ".exe"
		}
		temporary, err := os.MkdirTemp("", "vimls-release-")
		if err != nil {
			fatalf("%v", err)
		}
		binary := filepath.Join(temporary, executable)
		command := exec.Command("go", "build", "-mod=readonly", "-trimpath", "-ldflags=-buildid= -X github.com/neoclide/vimls-go/internal/server.Version="+*version, "-o", binary, "./cmd/vimls")
		command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+target.goos, "GOARCH="+target.goarch)
		if output, err := command.CombinedOutput(); err != nil {
			fatalf("build %s/%s: %v\n%s", target.goos, target.goarch, err, output)
		}
		data, err := os.ReadFile(binary)
		_ = os.RemoveAll(temporary)
		if err != nil {
			fatalf("%v", err)
		}
		extension := ".tar.gz"
		if target.goos == "windows" {
			extension = ".zip"
		}
		archive := filepath.Join(*output, name+extension)
		if err := writeArchive(archive, name+"/"+executable, data, stamp, target.goos == "windows"); err != nil {
			fatalf("%v", err)
		}
		archives = append(archives, archive)
	}
	if err := writeChecksums(filepath.Join(*output, "checksums.txt"), archives); err != nil {
		fatalf("%v", err)
	}
}

func writeArchive(path, name string, data []byte, stamp time.Time, zipped bool) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if zipped {
		writer := zip.NewWriter(file)
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o755)
		header.Modified = stamp
		entry, err := writer.CreateHeader(header)
		if err == nil {
			_, err = entry.Write(data)
		}
		if err != nil {
			return err
		}
		return writer.Close()
	}
	gzipWriter, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return err
	}
	gzipWriter.Header.ModTime = stamp
	tarWriter := tar.NewWriter(gzipWriter)
	header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), ModTime: stamp, AccessTime: stamp, ChangeTime: stamp, Format: tar.FormatPAX}
	if err := tarWriter.WriteHeader(header); err != nil {
		return err
	}
	if _, err := tarWriter.Write(data); err != nil {
		return err
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
