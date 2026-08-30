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

// workspaceImportDiagnostics captures graph metadata and target symbol facts
// under one workspace lock, then performs the conservative analysis without
// holding server state. The returned revision must still be current when the
// diagnostics are published.
func (s *Server) workspaceImportDiagnostics(documentURI string, file *syntax.File, result *analysis.FileAnalysis) (uint64, bool, []syntax.Diagnostic) {
	path, ok := workspaceURIPath(uri.URI(documentURI))
	if !ok || file == nil {
		s.workspaceMu.Lock()
		revision := s.workspaceGraphView.Revision()
		s.workspaceMu.Unlock()
		return revision, true, nil
	}
	references := workspace.CollectExternalReferencesFromAnalysis(path, file, result)

	s.workspaceMu.Lock()
	graph := s.workspaceGraphView
	revision := graph.Revision()
	if !workspacePathInRoots(path, workspaceIndexRoots(s.workspaceRoots, s.runtimePaths)) {
		s.workspaceMu.Unlock()
		return revision, true, nil
	}
	if !graph.Ready() {
		s.workspaceDependents[path] = struct{}{}
		s.workspaceMu.Unlock()
		return revision, false, nil
	}
	imports := graph.Imports(path)
	targets := make(map[string]importTargetSnapshot)
	for _, importFact := range imports {
		if importFact.Target == "" {
			continue
		}
		if _, exists := targets[importFact.Target]; exists {
			continue
		}
		source, sourceOK := s.workspaceIndex.Source(importFact.Target)
		if !sourceOK {
			targets[importFact.Target] = importTargetSnapshot{}
			continue
		}
		matches := s.workspaceIndex.FileSymbols(importFact.Target)
		symbols := make([]workspace.SymbolFact, 0, len(matches))
		for _, match := range matches {
			symbols = append(symbols, match.Fact)
		}
		targets[importFact.Target] = importTargetSnapshot{source: source, symbols: symbols}
	}
	s.workspaceMu.Unlock()
	for path, target := range targets {
		parsed := syntax.Parse(target.source)
		target.known = target.source != "" && parsed.Dialect == syntax.Vim9 && len(parsed.Diagnostics) == 0
		targets[path] = target
	}

	loads := make([]analysis.ImportLoad, 0, len(imports))
	for _, importFact := range imports {
		name, static := workspace.StaticImportPath(importFact.ImportPath)
		loads = append(loads, analysis.ImportLoad{
			Span: importFact.PathSpan, Path: name,
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
				for _, symbol := range targetSnapshot.symbols {
					if !symbol.TopLevel || symbol.Name != reference.Name {
						continue
					}
					member.Exists = true
					member.Exported = member.Exported || symbol.Exported
				}
			}
		}
		members = append(members, member)
	}
	return revision, true, analysis.AnalyzeImports(loads, members)
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
