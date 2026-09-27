package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

const corpusLockSchemaVersion = 1

type corpusLock struct {
	SchemaVersion   int                 `json:"schemaVersion"`
	Tag             string              `json:"tag"`
	Commit          string              `json:"commit"`
	ManifestSHA256  string              `json:"manifestSHA256"`
	Embedded        embeddedCorpusLock  `json:"embedded"`
	TestFiles       testFilesLock       `json:"testFiles"`
	HelperInventory helperInventoryLock `json:"helperInventory"`
	Heredocs        heredocLock         `json:"heredocs"`
	ParserFiles     parserFilesLock     `json:"parserFiles"`
	ParserCases     parserCasesLock     `json:"parserCases"`
}

type embeddedCorpusLock struct {
	Files      int `json:"files"`
	Cases      int `json:"cases"`
	Successes  int `json:"successes"`
	Failures   int `json:"failures"`
	Structural int `json:"structural"`
}

type testFilesLock struct {
	Files    int `json:"files"`
	RawBytes int `json:"rawBytes"`
}

type helperInventoryLock struct {
	HelperNames int                    `json:"helperNames"`
	Records     int                    `json:"records"`
	Summary     helperInventorySummary `json:"summary"`
}

type heredocLock struct {
	Total     int `json:"total"`
	Evaluated int `json:"evaluated"`
}

type parserFilesLock struct {
	Entries          int `json:"entries"`
	IncludedFiles    int `json:"includedFiles"`
	ExcludedFiles    int `json:"excludedFiles"`
	IncludedCalls    int `json:"includedCalls"`
	ExcludedCalls    int `json:"excludedCalls"`
	ImplicitExcludes int `json:"implicitExcludes"`
}

type parserCasesLock struct {
	Files   int               `json:"files"`
	Records int               `json:"records"`
	Summary parserCaseSummary `json:"summary"`
}

func readCorpusLock(path string) (corpusLock, error) {
	var lock corpusLock
	source, err := os.ReadFile(path)
	if err != nil {
		return lock, err
	}
	if err := json.Unmarshal(source, &lock); err != nil {
		return lock, fmt.Errorf("decode corpus lock: %w", err)
	}
	if lock.SchemaVersion != corpusLockSchemaVersion || lock.Tag != vimTag || lock.Commit != vimCommit {
		return lock, fmt.Errorf("unexpected corpus lock provenance: schema %d, tag %q, commit %q", lock.SchemaVersion, lock.Tag, lock.Commit)
	}
	if len(lock.ManifestSHA256) != 64 {
		return lock, fmt.Errorf("corpus lock has invalid manifest SHA-256 %q", lock.ManifestSHA256)
	}
	if _, err := hex.DecodeString(lock.ManifestSHA256); err != nil {
		return lock, fmt.Errorf("corpus lock has invalid manifest SHA-256 %q: %w", lock.ManifestSHA256, err)
	}
	return lock, nil
}

func buildCorpusLock(embedded corpus, testFiles testFilesCorpus, inventory helperInventory, manifest parserFileManifest, parserCases parserCaseCorpus) (corpusLock, error) {
	lock := corpusLock{
		SchemaVersion:  corpusLockSchemaVersion,
		Tag:            vimTag,
		Commit:         vimCommit,
		ManifestSHA256: parserCases.ManifestHash,
		Embedded:       embeddedCorpusLock{Files: len(embedded.Files), Cases: len(embedded.Cases)},
		TestFiles:      testFilesLock{Files: len(testFiles.Files)},
		HelperInventory: helperInventoryLock{
			HelperNames: len(inventory.HelperNames),
			Records:     len(inventory.Records),
			Summary:     inventory.Summary,
		},
		ParserCases: parserCasesLock{Files: len(parserCases.Files), Records: len(parserCases.Records), Summary: parserCases.Summary},
	}
	if embedded.Tag != vimTag || embedded.Commit != vimCommit || testFiles.Tag != vimTag || testFiles.Commit != vimCommit || inventory.SchemaVersion != 1 || inventory.Tag != vimTag || inventory.Commit != vimCommit || parserCases.SchemaVersion != 1 || parserCases.Tag != vimTag || parserCases.Commit != vimCommit || parserCases.Manifest != parserFileManifestName {
		return lock, fmt.Errorf("official corpus inputs have mismatched provenance")
	}
	for _, file := range testFiles.Files {
		lock.TestFiles.RawBytes += len(file.Source)
		for _, heredoc := range scanHelperHeredocs(file.Source) {
			lock.Heredocs.Total++
			if heredoc.Evaluate {
				lock.Heredocs.Evaluated++
			}
		}
	}
	for _, testCase := range embedded.Cases {
		switch testCase.Outcome {
		case "success":
			lock.Embedded.Successes++
		case "failure":
			lock.Embedded.Failures++
		case "":
			lock.Embedded.Structural++
		default:
			return lock, fmt.Errorf("embedded corpus has unknown outcome %q", testCase.Outcome)
		}
	}
	var err error
	lock.ParserFiles, err = summarizeParserFiles(testFiles, inventory, manifest)
	if err != nil {
		return lock, err
	}
	if parserCases.Summary.Calls != len(parserCases.Records) || parserCases.Summary.ExtractedCalls+parserCases.Summary.SkippedCalls != parserCases.Summary.Calls || parserCases.Summary.AcceptedCases+parserCases.Summary.UnclassifiedCases != parserCases.Summary.Cases {
		return lock, fmt.Errorf("parser case summary is internally inconsistent: %+v", parserCases.Summary)
	}
	return lock, nil
}

func validateCorpusLock(reviewed, candidate corpusLock) error {
	if reviewed != candidate {
		return fmt.Errorf("computed corpus differs from reviewed lock\ncomputed: %+v\nreviewed: %+v", candidate, reviewed)
	}
	return nil
}
