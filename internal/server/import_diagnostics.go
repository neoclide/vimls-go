package server

import (
	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/uri"
)

type importTargetSnapshot struct {
	source  string
	symbols []workspace.SymbolFact
	known   bool
}

// workspaceImportDiagnostics consumes the immutable workspace snapshot captured
// after this document's source and graph facts were installed.
func (s *Server) workspaceImportDiagnostics(snapshot workspaceAnalysisSnapshot, file *syntax.File, result *analysis.FileAnalysis) []syntax.Diagnostic {
	if snapshot.path == "" || file == nil || !snapshot.ready {
		return nil
	}
	references := workspace.CollectExternalReferencesFromAnalysis(snapshot.path, file, result)
	imports := snapshot.graph.Imports(snapshot.path)
	targets := snapshot.targets
	for path, target := range targets {
		parsed := s.parseImportTarget(path, target.source)
		target.known = target.source != "" && parsed.Dialect == syntax.Vim9 && len(parsed.Diagnostics) == 0
		targets[path] = target
	}

	loads := make([]analysis.ImportLoad, 0, len(imports))
	seenTargets := make(map[string]bool)
	for _, importFact := range imports {
		name, static := workspace.StaticImportPath(importFact.ImportPath)
		duplicate := importFact.Target != "" && seenTargets[importFact.Target]
		if importFact.Target != "" {
			seenTargets[importFact.Target] = true
		}
		loads = append(loads, analysis.ImportLoad{
			Span: importFact.PathSpan, Path: name,
			Self: importFact.Target != "" && importFact.Importer == importFact.Target, Duplicate: duplicate,
			Missing: importFact.Missing && static, Autoload: importFact.Autoload,
			Runtime: workspace.RuntimeImport(importFact.ImportPath),
		})
	}
	members := make([]analysis.ImportMember, 0, len(references))
	for _, reference := range references {
		if reference.Kind != workspace.ExternalReferenceImportMember {
			continue
		}
		// Vim can report E1048, E1049, or E117 for an autoload member in a
		// deferred body depending on whether the target was loaded first. The
		// language server does not execute scripts, so that state stays unknown.
		if reference.ImportAutoload && importReferenceInDeferredScope(result, reference.Span) {
			continue
		}
		target, unique := referencedImportTarget(imports, reference)
		member := analysis.ImportMember{Span: reference.Span, Name: reference.Name}
		if unique {
			targetSnapshot := targets[target]
			if targetSnapshot.known {
				member.TargetKnown = true
				var declaration workspace.SymbolFact
				declarationCount := 0
				for _, symbol := range targetSnapshot.symbols {
					if !symbol.TopLevel || symbol.Name != reference.Name {
						continue
					}
					declaration = symbol
					declarationCount++
					member.Exists = true
					if symbol.Exported {
						member.Exported = true
						member.Deprecated = member.Deprecated || symbol.Deprecated
					}
				}
				if declarationCount == 1 {
					member.Related = syntax.RelatedDiagnostic{
						URI: uri.File(target).String(), Source: targetSnapshot.source,
						Message: reference.Name + " is declared here", Span: declaration.SelectionRange,
					}
				}
			}
		}
		members = append(members, member)
	}
	diagnostics := analysis.AnalyzeImports(loads, members)
	for _, reference := range references {
		if reference.Kind != workspace.ExternalReferenceGlobalFunction || !reference.DirectCall || !snapshot.missingGlobalFunctions[reference.Name] {
			continue
		}
		diagnostics = append(diagnostics, syntax.Diagnostic{
			Code: "vimls/global-function-not-indexed", Message: "global function not found in workspace index: " + reference.Name, Span: reference.Span,
		})
	}
	if snapshot.indexComplete {
		for _, reference := range references {
			if reference.Kind != workspace.ExternalReferenceAutoload || !reference.DirectCall || !snapshot.missingAutoloadFunctions[reference.Name] {
				continue
			}
			diagnostics = append(diagnostics, syntax.Diagnostic{
				Code: "vimls/autoload-function-not-found", Message: "autoload function not found in current runtimepath: " + reference.Name, Span: reference.Span,
			})
		}
	}
	return diagnostics
}

// parseImportTarget preserves the parser-cache fast path when an unchanged
// open target exactly matches the immutable source captured by workspace analysis.
func (s *Server) parseImportTarget(path, source string) *syntax.File {
	s.publishMu.Lock()
	snapshot, _, open := s.openWorkspaceSnapshotLocked(path)
	s.publishMu.Unlock()
	if open && snapshot.Text() == source {
		if parsed := s.parseSnapshot(snapshot); parsed != nil {
			return parsed
		}
	}
	return syntax.Parse(source)
}

func importReferenceInDeferredScope(result *analysis.FileAnalysis, span syntax.Span) bool {
	if result == nil {
		return false
	}
	for _, scope := range result.Scopes {
		if scope == nil || span.Start < scope.Span.Start || span.End > scope.Span.End {
			continue
		}
		if scope.Kind == syntax.BlockDef || scope.Kind == syntax.BlockFunction || scope.Lambda != nil {
			return true
		}
	}
	return false
}

func referencedImportTarget(imports []workspace.ImportFact, reference workspace.ExternalReferenceFact) (string, bool) {
	target := ""
	for _, importFact := range imports {
		if importFact.ImportPath != reference.ImportPath || importFact.Autoload != reference.ImportAutoload || importFact.Target == "" {
			continue
		}
		if target != "" && target != importFact.Target {
			return "", false
		}
		target = importFact.Target
	}
	return target, target != ""
}
