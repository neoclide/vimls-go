package server

import (
	"context"

	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const CommandUpdateImportsOnRename = "vimls.updateImportsOnRename"

// ExecuteCommand updates imports after a client has observed a file rename.
// Its single argument is RenameFilesParams; the files have already moved.
func (s *Server) ExecuteCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (protocol.LSPAny, error) {
	if params.Command != CommandUpdateImportsOnRename {
		return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, "unknown command: "+params.Command)
	}
	var renames protocol.RenameFilesParams
	if len(params.Arguments) != 1 || protocol.Unmarshal(params.Arguments[0], &renames) != nil || len(renames.Files) == 0 {
		return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, "expected one argument with a nonempty files array of oldUri/newUri pairs")
	}
	for _, file := range renames.Files {
		if !uri.URI(file.OldURI).IsFile() || !uri.URI(file.NewURI).IsFile() {
			return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, "rename paths must be file URIs")
		}
	}
	s.mu.Lock()
	client, supported := s.client, s.applyEditSupport
	s.mu.Unlock()
	if !supported || client == nil {
		return nil, jsonrpc2.NewError(jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed), "client does not support workspace/applyEdit")
	}
	// A pending client apply request must also end when the server shuts down.
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.analysisContext, cancel)
	defer cancel()
	defer stop()
	edit, err := s.renameFileWorkspaceEdit(ctx, renames.Files, true)
	if err != nil {
		return nil, err
	}
	if len(edit.DocumentChanges) == 0 && len(edit.Changes) == 0 {
		return nil, nil
	}
	if ctx.Err() != nil || s.analysisContext.Err() != nil {
		return nil, protocol.ErrRequestCancelled
	}
	label := "Update Vim imports after file rename"
	result, err := client.ApplyEdit(ctx, &protocol.ApplyWorkspaceEditParams{Label: &label, Edit: *edit})
	if ctx.Err() != nil {
		return nil, protocol.ErrRequestCancelled
	}
	if err != nil {
		return nil, err
	}
	encoded, err := protocol.Marshal(result)
	return protocol.LSPAny(encoded), err
}
