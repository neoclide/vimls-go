package server

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/vimhelp"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
)

const maxRuntimeHelpFileBytes = 16 << 20

// updateRuntimeHelpLocked runs at the runtimepath commit point under workspaceMu.
// All published maps and their values are immutable. Removed roots disappear
// immediately; retained roots are reused even while added roots are loading.
func (s *Server) updateRuntimeHelpLocked() {
	if s.analysisStopped || s.analysisContext.Err() != nil {
		return
	}
	if s.runtimeHelpCancel != nil && !slices.Contains(s.runtimePaths, s.runtimeHelpRoot) {
		s.runtimeHelpCancel()
	}
	directories := make(map[string][]string, len(s.runtimePaths))
	files := make(map[string][]vimhelp.SymbolDocumentation)
	missing := false
	for _, root := range s.runtimePaths {
		paths, ok := s.runtimeHelpRoots[root]
		if !ok {
			missing = true
			continue
		}
		directories[root] = paths
		for _, path := range paths {
			files[path] = s.runtimeHelpFiles[path]
		}
	}
	s.runtimeHelpRoots, s.runtimeHelpFiles = directories, files
	s.runtimeHelp = runtimeHelpIndex(s.runtimePaths, directories, files)
	if !missing || s.runtimeHelpRunning {
		return
	}
	s.runtimeHelpRunning = true
	s.runtimeHelpWG.Add(1)
	go s.collectRuntimeHelpWorker()
}

// A single worker coalesces runtimepath updates. Adding/reordering roots while
// a retained root is loading does not cancel or restart that root's parse.
func (s *Server) collectRuntimeHelpWorker() {
	defer s.runtimeHelpWG.Done()
	for {
		s.publishMu.Lock()
		s.workspaceMu.Lock()
		root := ""
		for _, candidate := range s.runtimePaths {
			if _, cached := s.runtimeHelpRoots[candidate]; !cached {
				root = candidate
				break
			}
		}
		if root == "" || s.analysisContext.Err() != nil {
			s.runtimeHelpRunning = false
			s.runtimeHelpRoot, s.runtimeHelpCancel = "", nil
			refresh := s.runtimeHelpNeedsRefresh && !s.analysisStopped && s.analysisContext.Err() == nil
			s.runtimeHelpNeedsRefresh = false
			if refresh {
				// Command diagnostics now consume the completed help catalog.
				// Invalidate results captured while that catalog was still loading.
				s.workspaceRevision++
				s.notifyWorkspaceIndexChangedLocked()
			}
			s.workspaceMu.Unlock()
			s.publishMu.Unlock()
			if refresh {
				s.scheduleDiagnosticRefresh()
				for _, snapshot := range s.documents.Snapshots() {
					s.startAnalysis(snapshot.URI())
				}
			}
			return
		}
		ctx, cancel := context.WithCancel(s.analysisContext)
		s.runtimeHelpRoot, s.runtimeHelpCancel = root, cancel
		files := make(map[string][]vimhelp.SymbolDocumentation, len(s.runtimeHelpFiles))
		for path, docs := range s.runtimeHelpFiles {
			files[path] = docs
		}
		s.workspaceMu.Unlock()
		s.publishMu.Unlock()
		directories := make(map[string][]string)
		loaded := s.collectRuntimeHelp(ctx, []string{root}, directories, files)
		s.workspaceMu.Lock()
		if loaded && ctx.Err() == nil && slices.Contains(s.runtimePaths, root) {
			// Merge only this root into the latest snapshot. A root removed
			// during the read must not reappear through the old file cache.
			mergedRoots := make(map[string][]string, len(s.runtimeHelpRoots)+1)
			mergedFiles := make(map[string][]vimhelp.SymbolDocumentation, len(s.runtimeHelpFiles))
			for cachedRoot, paths := range s.runtimeHelpRoots {
				mergedRoots[cachedRoot] = paths
			}
			for path, docs := range s.runtimeHelpFiles {
				mergedFiles[path] = docs
			}
			mergedRoots[root] = directories[root]
			for _, path := range directories[root] {
				mergedFiles[path] = files[path]
			}
			s.runtimeHelpRoots, s.runtimeHelpFiles = mergedRoots, mergedFiles
			s.runtimeHelp = runtimeHelpIndex(s.runtimePaths, mergedRoots, mergedFiles)
		}
		cancel()
		s.workspaceMu.Unlock()
	}
}

func (s *Server) collectRuntimeHelp(ctx context.Context, added []string, directories map[string][]string, files map[string][]vimhelp.SymbolDocumentation) bool {
	retainedBytes := 0
	for _, docs := range files {
		retainedBytes += runtimeHelpBytes(docs)
	}
	for _, root := range added {
		paths, warnings, err := workspace.DiscoverRuntimeHelpFiles(ctx, []string{root})
		if err != nil {
			return false
		}
		for _, warning := range warnings {
			s.logf("vimls: runtime help: %s", warning)
		}
		directories[root] = nil
		for _, path := range paths {
			if ctx.Err() != nil {
				return false
			}
			if _, exists := files[path]; !exists {
				if len(files) >= maxWorkspaceFiles {
					s.logf("vimls: runtime help file limit reached")
					break
				}
				if hook := s.testHooks.beforeRuntimeHelpRead; hook != nil {
					hook(ctx, path)
				}
				if ctx.Err() != nil {
					return false
				}
				data, err := readRuntimeHelpFile(path)
				var docs []vimhelp.SymbolDocumentation
				if err == nil && ctx.Err() == nil {
					docs, err = s.parseRuntimeHelp(path, data)
				}
				if size := runtimeHelpBytes(docs); size > maxIndexBytes-retainedBytes {
					err = fmt.Errorf("runtime help memory limit reached")
				} else if err == nil {
					retainedBytes += size
				}
				if err != nil {
					s.logf("vimls: runtime help: %s: %v", path, err)
					files[path] = nil
				} else {
					files[path] = docs
				}
			}
			directories[root] = append(directories[root], path)
		}
	}
	return ctx.Err() == nil
}

func runtimeHelpBytes(docs []vimhelp.SymbolDocumentation) int {
	bytes := 0
	for _, doc := range docs {
		// Conservatively count alias bodies repeatedly, including entry/key overhead.
		bytes += len(doc.Markdown) + len(doc.Name) + len(doc.Tag) + len(doc.Source) + 256
	}
	return bytes
}

func (s *Server) parseRuntimeHelp(path string, data []byte) (docs []vimhelp.SymbolDocumentation, err error) {
	// Optional runtime documentation must never turn a parser bug into a
	// process-wide failure. Recover at the file boundary so the scan continues.
	defer func() {
		if failure := recover(); failure != nil {
			docs = nil
			err = fmt.Errorf("help parser panic: %v", failure)
		}
	}()
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("help file is not valid UTF-8")
	}
	if hook := s.testHooks.beforeRuntimeHelpParse; hook != nil {
		hook(path)
	}
	return vimhelp.ExtractSymbols(path, data), nil
}

func readRuntimeHelpFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxRuntimeHelpFileBytes {
		return nil, fmt.Errorf("not a regular help file of at most %d bytes", maxRuntimeHelpFileBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxRuntimeHelpFileBytes+1))
	if len(data) > maxRuntimeHelpFileBytes {
		return nil, fmt.Errorf("help file exceeds %d bytes", maxRuntimeHelpFileBytes)
	}
	return data, err
}

func runtimeHelpIndex(roots []string, directories map[string][]string, files map[string][]vimhelp.SymbolDocumentation) map[string]vimhelp.SymbolDocumentation {
	index := make(map[string]vimhelp.SymbolDocumentation)
	for _, root := range roots {
		for _, path := range directories[root] {
			for _, doc := range files[path] {
				name := doc.Name
				if doc.Kind == "plug mapping" {
					name = strings.ToLower(name)
				} else if doc.Kind != "global variable" {
					name = strings.TrimPrefix(name, "g:")
				}
				if _, exists := index[name]; !exists && doc.Markdown != "" {
					index[name] = doc
				}
			}
		}
	}
	return index
}

func runtimeHelpName(document *navigationDocument, resolvedKind analysis.SymbolKind) string {
	if text := document.analysis.File.Text(document.occurrence); plugMappingTagText(text) {
		return strings.ToLower(text)
	}
	commandName := ""
	walkCommands(document.analysis.File.Commands, func(command *syntax.Command) {
		if commandName != "" || command.Name != document.occurrence {
			return
		}
		if command.Kind == syntax.CommandUser {
			commandName = ":" + document.analysis.File.Text(command.Name)
		} else if command.Kind == syntax.CommandBuiltin {
			commandName = ":" + command.Canonical
		}
	})
	if commandName != "" {
		return commandName
	}
	if document.external != nil {
		name := strings.TrimPrefix(document.external.Name, "g:")
		switch document.external.Kind {
		case workspace.ExternalReferenceGlobalFunction:
			return name
		case workspace.ExternalReferenceGlobalVariable:
			return "g:" + name
		case workspace.ExternalReferenceAutoload:
			if document.external.DirectCall || isFunctionSymbolKind(resolvedKind) {
				return name
			}
			return "g:" + name
		}
		return ""
	}
	if declaration := document.declaration; declaration != nil {
		// Reuse dialect/scope classification, so a local Vim9 function or
		// parameter never inherits documentation for an unrelated global.
		for _, event := range analysis.CollectNameDeclarationEvents(document.analysis.File) {
			if event.Span == declaration.Span && event.Scope == analysis.NameDeclarationGlobal && !event.Delete {
				if event.Kind == analysis.NameDeclarationVariable {
					return "g:" + event.Name
				}
				return event.Name
			}
		}
		return ""
	}
	if function, _, ok := builtinFunctionAt(document.analysis.File, document.occurrence); ok {
		return function.Name
	}
	call := callAt(document.analysis.File, document.occurrence.Start)
	if call != nil && len(call.Children) > 0 {
		callee := call.Children[0]
		if callee.Kind == syntax.ExpressionIdentifier && callee.Span == document.occurrence {
			return strings.TrimPrefix(callee.Value, "g:")
		}
		if callee.Kind == syntax.ExpressionMember && document.analysis.File.Text(callee.Operator) == "->" {
			if span, ok := expressionMemberSpan(callee); ok && span == document.occurrence {
				return strings.TrimPrefix(callee.Value, "g:")
			}
		}
	}
	for _, reference := range document.analysis.References {
		if reference.Span == document.occurrence && strings.HasPrefix(reference.Name, "g:") {
			return reference.Name
		}
	}
	return ""
}

func plugMappingTagText(text string) bool {
	return len(text) > len("<Plug>()") && strings.EqualFold(text[:len("<Plug>(")], "<Plug>(") && text[len(text)-1] == ')'
}

func (s *Server) runtimeHelpMarkdown(name string) string {
	s.workspaceMu.Lock()
	doc, found := s.runtimeHelp[name]
	s.workspaceMu.Unlock()
	if !found {
		return ""
	}
	return doc.Markdown
}

func (s *Server) appendRuntimeHelp(document *navigationDocument, contents protocol.HoverContents, resolvedKind analysis.SymbolKind) protocol.HoverContents {
	name := runtimeHelpName(document, resolvedKind)
	if name == "" {
		return contents
	}
	s.workspaceMu.Lock()
	doc, found := s.runtimeHelp[name]
	s.workspaceMu.Unlock()
	if !found {
		return contents
	}
	body := stripRuntimeHelpHeading(doc.Markdown, doc.Name, doc.Tag)
	help := fmt.Sprintf("%s\n\n`%s:%d`", body, doc.Source, doc.Line)
	if s.languageFeatures.hoverMarkup != protocol.MarkupKindMarkdown {
		value := ""
		if current, ok := contents.(*protocol.MarkupContent); ok {
			value = current.Value + "\n\n---\n\n"
		}
		return boundedMarkupContent(protocol.MarkupKindPlainText, value+markdownToPlainText(help))
	}
	var sections protocol.MarkedStringSlice
	switch current := contents.(type) {
	case protocol.MarkedStringSlice:
		sections = append(sections, current...)
	case *protocol.MarkupContent:
		sections = append(sections, protocol.String(current.Value))
	case protocol.String:
		sections = append(sections, current)
	}
	return append(sections, protocol.String(boundedDocumentationText(help)))
}

func stripRuntimeHelpHeading(markdown, name, tag string) string {
	lines := strings.Split(markdown, "\n")
	removed := 0
	for removed < len(lines) {
		line := strings.TrimSpace(lines[removed])
		if line == name || line == tag || strings.HasPrefix(line, name+"(") && strings.HasSuffix(line, ")") {
			removed++
			continue
		}
		break
	}
	if removed == 0 {
		return markdown
	}
	return strings.TrimSpace(strings.Join(lines[removed:], "\n"))
}
