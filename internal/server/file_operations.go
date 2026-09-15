package server

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// willRenameImportGlob is the only file kind a Vim :import can name. Folder
// renames are deliberately not registered: a folder operation reports the
// folder alone, so the imports it affects cannot be enumerated from the
// request.
const willRenameImportGlob = "**/*.vim"

// WillRenameFiles rewrites the Vim9 :import statements that name a file being
// renamed, including the imports of the renamed files themselves when their own
// relative spelling changes with their new location.
//
// Every edit is derived from the importing document's current content, and a
// document is edited only when all of its imports to the renamed files can be
// re-expressed in their original spelling form and the resulting text validates
// as a whole. An incomplete index, an unverifiable content or an import that
// cannot be rewritten yields no edit for the affected document instead of a
// guessed or partial rewrite.
//
// The rename itself is never refused. Imports are one of several reference kinds
// a file rename may need to update, and failing the whole operation would be
// worse than leaving one import for the user to correct.
func (s *Server) WillRenameFiles(ctx context.Context, params *protocol.RenameFilesParams) (*protocol.WorkspaceEdit, error) {
	if len(params.Files) == 0 {
		return s.clientWorkspaceEdit(nil)
	}
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
	for attempt := range 2 {
		changes, retry, err := s.renameFileImportChanges(ctx, params.Files)
		if err != nil {
			return nil, err
		}
		if !retry {
			return s.clientWorkspaceEdit(changes)
		}
		if attempt == 0 {
			continue
		}
	}
	// The workspace kept changing under the request. Editing a file set that is
	// already superseded could publish a path that no longer resolves.
	return s.clientWorkspaceEdit(nil)
}

// renameFileImportChanges computes the import rewrites for one captured
// workspace state. retry reports that the state was superseded while the edits
// were being built, so a fresh capture may still produce a usable result.
func (s *Server) renameFileImportChanges(ctx context.Context, files []protocol.FileRename) ([]protocol.DocumentChange, bool, error) {
	state := s.captureWorkspaceNavigationState()
	if state.index == nil || !state.index.Complete() || !state.graph.Ready() {
		// A superseded or partial file set cannot prove which documents import
		// the renamed files, so no import is rewritten.
		return nil, false, nil
	}
	renamed := renameFileTargets(files)
	if len(renamed) == 0 {
		return nil, false, nil
	}
	s.mu.Lock()
	encoding := s.encoding
	s.mu.Unlock()
	importers := renameFileImporters(state, renamed)
	changes := make([]protocol.DocumentChange, 0, len(importers))
	for _, importer := range importers {
		if ctx.Err() != nil {
			return nil, false, protocol.ErrRequestCancelled
		}
		change, err := s.renameFileImporterChange(ctx, state, encoding, importer, renamed)
		if err != nil {
			if errors.Is(err, protocol.ErrRequestCancelled) {
				return nil, false, err
			}
			// An unverifiable or unrewritable document is skipped. Editing it
			// from a stale reading, or with a guessed path, would leave a
			// broken import behind either way.
			continue
		}
		if change != nil {
			changes = append(changes, change)
		}
	}
	if ctx.Err() != nil {
		return nil, false, protocol.ErrRequestCancelled
	}
	s.workspaceMu.Lock()
	current := s.workspaceIdentityCurrentLocked(state.identity)
	s.workspaceMu.Unlock()
	return changes, !current, nil
}

// renameFileTargets maps each renamed file to its new location. A rename that
// cannot be expressed as two workspace paths, that does not move the file, or
// that gives one source two destinations is dropped: no import can then be
// proven to need a rewrite. A batch with a symbolic-link endpoint is withheld
// because canonical file identities cannot describe that directory-entry move.
func renameFileTargets(files []protocol.FileRename) map[string]string {
	renamed := make(map[string]string, len(files))
	ambiguous := make(map[string]struct{})
	for _, file := range files {
		oldURI, newURI := uri.URI(file.OldURI), uri.URI(file.NewURI)
		if !oldURI.IsFile() || !newURI.IsFile() {
			continue
		}
		// Inspect the directory entries before workspaceURIPath follows links.
		// Moving a link does not move its target; replacing a link likewise does
		// not replace its target. Ignoring just this operation would also give
		// other files in the batch an incorrect prospective lookup order.
		for _, path := range []string{oldURI.FsPath(), newURI.FsPath()} {
			if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
		}
		oldPath, oldOK := workspaceURIPath(oldURI)
		newPath, newOK := workspaceURIPath(newURI)
		if !oldOK || !newOK || sameWorkspacePath(oldPath, newPath) {
			continue
		}
		if _, exists := ambiguous[oldPath]; exists {
			continue
		}
		if existing, exists := renamed[oldPath]; exists && !sameWorkspacePath(existing, newPath) {
			delete(renamed, oldPath)
			ambiguous[oldPath] = struct{}{}
			continue
		}
		renamed[oldPath] = newPath
	}
	return renamed
}

// renameFileImporters returns every document whose imports may need rewriting:
// the recorded importers of each renamed file, and the renamed files
// themselves, whose relative imports move with them.
func renameFileImporters(state workspaceNavigationSnapshot, renamed map[string]string) []string {
	seen := make(map[string]struct{}, len(renamed))
	importers := make([]string, 0, len(renamed))
	add := func(path string) {
		if _, exists := seen[path]; exists {
			return
		}
		seen[path] = struct{}{}
		importers = append(importers, path)
	}
	for oldPath := range renamed {
		add(oldPath)
		for _, fact := range state.graph.Incoming(oldPath) {
			add(fact.Importer)
		}
	}
	sort.Strings(importers)
	return importers
}

func (s *Server) renameFileImporterChange(ctx context.Context, state workspaceNavigationSnapshot, encoding text.Encoding, importer string, renamed map[string]string) (protocol.DocumentChange, error) {
	if state.resolver == nil {
		return nil, nil
	}
	// A renamed importer is edited where it lives now, but a relative import
	// must be spelled for where the file will live afterwards.
	from := importer
	if destination, moved := renamed[importer]; moved {
		from = destination
	}
	snapshot, open, err := s.renameFileImporterSnapshot(state, importer)
	if err != nil || snapshot == nil {
		return nil, err
	}
	file := syntax.Parse(snapshot.Text())
	result := analysis.Analyze(file)
	edits := make([]protocol.TextDocumentEditElement, 0)
	valid := true
	var walk func([]syntax.Command, []syntax.Block, bool)
	walk = func(commands []syntax.Command, blocks []syntax.Block, deferred bool) {
		for index := range commands {
			if !valid {
				return
			}
			command := &commands[index]
			insideFunction := deferred || syntax.CommandInsideFunction(command, blocks)
			if command.Import != nil && !insideFunction {
				// A function-local import is deferred until the function runs,
				// so its path is not part of the script's load-time imports.
				nodeEdits, nodeValid := renameFileImportNodeEdits(state, snapshot, file, result, encoding, importer, from, command.Import, renamed)
				if !nodeValid {
					valid = false
					return
				}
				edits = append(edits, nodeEdits...)
			}
			if command.Embedded != nil {
				walk(command.Embedded.Commands, command.Embedded.Blocks, insideFunction)
			}
		}
	}
	walk(file.Commands, file.Blocks, false)
	if !valid || len(edits) == 0 {
		return nil, nil
	}
	sort.Slice(edits, func(i, j int) bool {
		left := edits[i].(*protocol.TextEdit).Range.Start
		right := edits[j].(*protocol.TextEdit).Range.Start
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Character < right.Character
	})
	if err := validateRenameBindings(ctx, snapshot, encoding, edits); err != nil {
		return nil, err
	}
	// Recheck after collecting edits and validating the proposed text. Closed
	// files have no client version to reject an already-stale range.
	if !open {
		content, readable := readRegularWorkspaceFile(importer, maxFileBytes)
		if !readable || string(content) != snapshot.Text() {
			return nil, protocol.ErrContentModified
		}
	}
	version, versioned := snapshot.Version()
	var versionPointer *int32
	if versioned {
		versionPointer = &version
	}
	return &protocol.TextDocumentEdit{
		TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri.File(importer)},
			Version:                versionPointer,
		},
		Edits: edits,
	}, nil
}

// renameFileImporterSnapshot returns the content the importer's edits must be
// computed from. An open document uses the client's own content; a closed
// document is read back and compared with the indexed source, so a concurrent
// write can never be edited from a stale reading.
func (s *Server) renameFileImporterSnapshot(state workspaceNavigationSnapshot, importer string) (*text.Snapshot, bool, error) {
	s.publishMu.Lock()
	open, _, found := s.openWorkspaceSnapshotLocked(importer)
	s.publishMu.Unlock()
	if found {
		return open, true, nil
	}
	source, indexed := state.index.Source(importer)
	if !indexed {
		return nil, false, nil
	}
	content, readable := readRegularWorkspaceFile(importer, maxFileBytes)
	if !readable || string(content) != source {
		return nil, false, protocol.ErrContentModified
	}
	return text.NewSnapshot(uri.File(importer).String(), 0, nil, source), false, nil
}

// renameFileImportNodeEdits returns the edits for one import command, or
// valid=false when the import cannot be rewritten without guessing.
func renameFileImportNodeEdits(state workspaceNavigationSnapshot, snapshot *text.Snapshot, file *syntax.File, result *analysis.FileAnalysis, encoding text.Encoding, importer, from string, node *syntax.Import, renamed map[string]string) ([]protocol.TextDocumentEditElement, bool) {
	raw := file.Text(node.PathSpan)
	resolution := state.resolver.ResolveImportPath(importer, raw, node.Autoload)
	if resolution.Dynamic || resolution.Path == "" {
		return nil, true
	}
	target := resolution.Path
	newTarget, targetMoved := renamed[target]
	if !targetMoved && sameWorkspacePath(from, importer) {
		// Neither the import nor the importer moves.
		return nil, true
	}
	if !targetMoved {
		newTarget = target
	}
	spec, ok := workspace.RewriteImportPath(from, raw, target, newTarget)
	if !ok {
		return nil, false
	}
	newRaw, ok := requoteImportPath(raw, spec)
	if !ok {
		return nil, false
	}
	// A spelling that fits the same lookup directory can still be shadowed
	// by another runtimepath entry, including a destination in this batch.
	prospective := state.resolver.ResolveImportPathAfterRenames(from, newRaw, node.Autoload, renamed)
	if !sameWorkspacePath(prospective.Path, newTarget) {
		return nil, false
	}
	if newRaw == raw {
		return nil, true
	}
	if node.Alias.Start < node.Alias.End {
		// An explicit alias keeps its own name, so only the literal changes.
		rangeValue, ok := protocolRange(snapshot, encoding, node.PathSpan)
		if !ok {
			return nil, false
		}
		return []protocol.TextDocumentEditElement{&protocol.TextEdit{Range: rangeValue, NewText: newRaw}}, true
	}
	return renameFileNamespaceEdits(snapshot, file, result, encoding, node, raw, newRaw)
}

// renameFileNamespaceEdits rewrites a filename-derived namespace together with
// its literal. The namespace is both the last path component and the import's
// own declaration span, so it is replaced as its own range instead of hiding
// inside a larger one; the remaining literal pieces are replaced separately and
// only when they actually change.
func renameFileNamespaceEdits(snapshot *text.Snapshot, file *syntax.File, result *analysis.FileAnalysis, encoding text.Encoding, node *syntax.Import, raw, newRaw string) ([]protocol.TextDocumentEditElement, bool) {
	oldName := workspace.ImportAlias(file, node)
	newName, ok := workspace.ImportPathName(newRaw)
	if !ok || oldName == "" {
		return nil, false
	}
	if !renameIdentifier.MatchString(newName) {
		// A filename such as "my-lib.vim" cannot name an importable script
		// without an explicit alias, so the derived namespace has no valid
		// replacement.
		return nil, false
	}
	declaration := analysis.ImportDeclarationSpan(file, node)
	if declaration.Start < node.PathSpan.Start || declaration.End > node.PathSpan.End || declaration.Start >= declaration.End {
		return nil, false
	}
	// Splitting the literal byte-for-byte requires a path without quote
	// escapes or double-quoted backslash escapes. Both quote styles work for
	// these plain literals, and requoteImportPath preserves that style.
	if len(raw) < 2 || (raw[0] != '\'' && raw[0] != '"') || strings.Count(raw, string(raw[0])) != 2 ||
		(raw[0] == '"' && strings.Contains(raw, "\\")) {
		return nil, false
	}
	extension := ".vim" + string(raw[0])
	if !strings.HasSuffix(raw, extension) || !strings.HasSuffix(newRaw, extension) {
		return nil, false
	}
	if file.Text(declaration) != oldName {
		return nil, false
	}
	nameEnd := len(newRaw) - len(extension)
	nameStart := nameEnd - len(newName)
	if nameStart < 1 {
		return nil, false
	}
	declarationStart := declaration.Start - node.PathSpan.Start
	declarationEnd := declaration.End - node.PathSpan.Start
	edits := make([]protocol.TextDocumentEditElement, 0, 3)
	for _, piece := range []struct {
		start, end   int
		old, updated string
	}{
		{0, declarationStart, raw[:declarationStart], newRaw[:nameStart]},
		{declarationStart, declarationEnd, oldName, newName},
		{declarationEnd, len(raw), raw[declarationEnd:], newRaw[nameEnd:]},
	} {
		if piece.old == piece.updated {
			continue
		}
		span := syntax.Span{Start: node.PathSpan.Start + piece.start, End: node.PathSpan.Start + piece.end}
		rangeValue, ok := protocolRange(snapshot, encoding, span)
		if !ok {
			return nil, false
		}
		edits = append(edits, &protocol.TextEdit{Range: rangeValue, NewText: piece.updated})
	}
	references := make([]syntax.Span, 0)
	seen := make(map[syntax.Span]struct{})
	for _, reference := range result.References {
		if reference == nil || reference.Declaration == nil || reference.Declaration.Span != declaration {
			continue
		}
		if reference.Span.Start >= node.PathSpan.Start && reference.Span.End <= node.PathSpan.End {
			// The declaration itself lives inside the literal.
			continue
		}
		if _, exists := seen[reference.Span]; exists {
			continue
		}
		seen[reference.Span] = struct{}{}
		// A differently spelled reference, such as the script-local s: form,
		// cannot be replaced by the new name alone.
		if file.Text(reference.Span) != oldName {
			return nil, false
		}
		references = append(references, reference.Span)
	}
	// Every textual occurrence outside the literal must be one of the
	// references being rewritten. A use site this analysis does not bind, such
	// as a type annotation or a script-local spelling, would otherwise be left
	// naming the old script.
	if countImportNameOccurrences(file.Source, oldName, node.PathSpan) != len(references) {
		return nil, false
	}
	for _, span := range references {
		rangeValue, ok := protocolRange(snapshot, encoding, span)
		if !ok {
			return nil, false
		}
		edits = append(edits, &protocol.TextEdit{Range: rangeValue, NewText: newName})
	}
	return edits, true
}

// requoteImportPath rewrites raw with value, preserving the literal's quoting.
// A value that cannot be spelled in the original quote style is rejected rather
// than escaped into a different filename.
func requoteImportPath(raw, value string) (string, bool) {
	if len(raw) < 2 || raw[0] != raw[len(raw)-1] {
		return "", false
	}
	switch raw[0] {
	case '\'':
		if strings.ContainsAny(value, "'\n\r") {
			return "", false
		}
		return "'" + value + "'", true
	case '"':
		if strings.ContainsAny(value, "\\\"\n\r") {
			return "", false
		}
		return "\"" + value + "\"", true
	}
	return "", false
}

// countImportNameOccurrences counts whole-identifier occurrences of name in
// source, ignoring those inside ignore.
func countImportNameOccurrences(source, name string, ignore syntax.Span) int {
	if name == "" {
		return 0
	}
	count := 0
	for index := 0; index+len(name) <= len(source); {
		found := strings.Index(source[index:], name)
		if found < 0 {
			break
		}
		start := index + found
		end := start + len(name)
		index = start + 1
		if start >= ignore.Start && end <= ignore.End {
			continue
		}
		if !importNameBoundary(source, start-1) || !importNameBoundary(source, end) {
			continue
		}
		count++
	}
	return count
}

func importNameBoundary(source string, index int) bool {
	if index < 0 || index >= len(source) {
		return true
	}
	character := source[index]
	return !(character == '_' || character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' || character >= '0' && character <= '9')
}
