package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type parserFileManifest struct {
	SchemaVersion      int                `json:"schemaVersion"`
	Tag                string             `json:"tag"`
	Commit             string             `json:"commit"`
	Scope              string             `json:"scope"`
	DefaultDisposition string             `json:"defaultDisposition"`
	Files              []parserFileRecord `json:"files"`
}

type parserFileRecord struct {
	Path        string `json:"path"`
	Disposition string `json:"disposition"`
	Reason      string `json:"reason"`
}

func readParserFileManifest(path string) (parserFileManifest, error) {
	var manifest parserFileManifest
	source, err := os.ReadFile(path)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(source, &manifest); err != nil {
		return manifest, err
	}
	if manifest.SchemaVersion != 1 || manifest.Tag != vimTag || manifest.Commit != vimCommit {
		return manifest, fmt.Errorf("unexpected parser file manifest provenance: schema %d, tag %q, commit %q", manifest.SchemaVersion, manifest.Tag, manifest.Commit)
	}
	if manifest.Scope == "" || manifest.DefaultDisposition != "exclude" {
		return manifest, fmt.Errorf("invalid parser file manifest scope or default disposition")
	}
	for index, file := range manifest.Files {
		if file.Path == "" || file.Reason == "" || (file.Disposition != "include" && file.Disposition != "exclude") {
			return manifest, fmt.Errorf("invalid parser file record %d: %#v", index, file)
		}
		if index > 0 && manifest.Files[index-1].Path >= file.Path {
			return manifest, fmt.Errorf("parser file manifest is not strictly sorted at %d", index)
		}
	}
	return manifest, nil
}

func includedParserFilePaths(manifest parserFileManifest) map[string]struct{} {
	paths := make(map[string]struct{})
	for _, file := range manifest.Files {
		if file.Disposition == "include" {
			paths[file.Path] = struct{}{}
		}
	}
	return paths
}

func selectParserMigrationFiles(corpus testFilesCorpus, manifest parserFileManifest) ([]testFileRecord, error) {
	include := includedParserFilePaths(manifest)
	files := make([]testFileRecord, 0, len(include))
	for _, file := range corpus.Files {
		if _, ok := include[file.Path]; !ok {
			continue
		}
		files = append(files, file)
		delete(include, file.Path)
	}
	if len(include) != 0 {
		return nil, fmt.Errorf("parser file manifest includes paths absent from pinned corpus: %v", include)
	}
	return files, nil
}
