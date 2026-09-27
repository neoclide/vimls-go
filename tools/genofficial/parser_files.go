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

func summarizeParserFiles(corpus testFilesCorpus, inventory helperInventory, manifest parserFileManifest) (parserFilesLock, error) {
	summary := parserFilesLock{Entries: len(manifest.Files)}
	allPaths := make(map[string]struct{}, len(corpus.Files))
	for _, file := range corpus.Files {
		allPaths[file.Path] = struct{}{}
	}
	qualifiedCalls := make(map[string]int)
	for _, record := range inventory.Records {
		if record.Disposition == "pending-extraction" {
			qualifiedCalls[record.Path]++
		}
	}
	dispositions := make(map[string]parserFileRecord, len(manifest.Files))
	for _, file := range manifest.Files {
		if _, ok := allPaths[file.Path]; !ok {
			return summary, fmt.Errorf("parser file manifest path is absent from pinned corpus: %q", file.Path)
		}
		dispositions[file.Path] = file
		if file.Disposition == "include" {
			summary.IncludedFiles++
			summary.IncludedCalls += qualifiedCalls[file.Path]
		} else {
			summary.ExcludedFiles++
		}
	}
	for path, calls := range qualifiedCalls {
		file, ok := dispositions[path]
		if !ok {
			return summary, fmt.Errorf("qualified helper file %q with %d calls lacks an explicit disposition", path, calls)
		}
		if file.Disposition != "include" {
			summary.ExcludedCalls += calls
		}
	}
	summary.ImplicitExcludes = len(corpus.Files) - summary.IncludedFiles - summary.ExcludedFiles
	return summary, nil
}
