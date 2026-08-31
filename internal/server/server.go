package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sort"
	"sync"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/jsonrpc"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const (
	Name                      = "vimls"
	Version                   = "dev"
	maxFileBytes              = 4 << 20
	maxPendingRequests        = 128
	maxParallelAnalysis       = 4
	maxDiagnosticsPerDocument = 200
	maxWorkspaceFiles         = 20000
	maxIndexBytes             = 256 << 20
	maxWorkspaceSymbols       = 200
)

const MethodDidChangeRuntimepath = "vimls/didChangeRuntimepath"
const fileWatchRegistrationID = "vimls-watch-vim-files"

type state uint8

const (
	stateBeforeInitialize state = iota
	stateActive
	stateShutdown
)

type parsedDocument struct {
	revision uint64
	file     *syntax.File
}

type Server struct {
	protocol.UnimplementedServer

	input  io.Reader
	output io.Writer
	log    io.Writer
	logMu  sync.Mutex

	mu                  sync.Mutex
	state               state
	targetVersion       TargetVersion
	targetOverride      bool
	unresolvedSeverity  syntax.DiagnosticSeverity
	pendingWarning      string
	client              protocol.Client
	cancellations       map[jsonrpc2.ID]context.CancelFunc
	documents           *workspace.Documents
	encoding            text.Encoding
	exitOnce            sync.Once
	exitCode            chan int
	analysisMu          sync.Mutex
	analysisContext     context.Context
	analysisCancel      context.CancelFunc
	analysisStopped     bool
	analysisWG          sync.WaitGroup
	analysisWake        chan struct{}
	analysisPending     map[string]struct{}
	analysisRunning     map[string]struct{}
	analysisWorkers     int
	publishMu           sync.Mutex
	parsed              map[string]parsedDocument
	published           map[string]bool
	workspaceMu         sync.Mutex
	workspaceRoots      []string
	runtimePaths        []string
	workspaceIndex      *workspace.Index
	workspaceGraph      *workspace.ImportGraph
	workspaceGraphView  workspace.ImportGraphSnapshot
	workspaceResolver   *workspace.PathResolver
	workspaceFiles      map[string]struct{}
	workspacePending    map[string]struct{}
	workspaceDependents map[string]struct{}
	workspaceBuilt      bool
	workspaceRevision   uint64
	workspaceRunning    bool
	workspaceWG         sync.WaitGroup

	watchMu                  sync.Mutex
	watchDynamicRegistration bool
	watchRelativePatterns    bool
	watchRegistered          bool
	initialized              bool
	watchWG                  sync.WaitGroup
	workspaceConfiguration   bool
}

func New(input io.Reader, output, logOutput io.Writer) *Server {
	target, _ := ParseTargetVersion(DefaultTargetVersion)
	analysisContext, analysisCancel := context.WithCancel(context.Background())
	graph := workspace.NewImportGraph()
	return &Server{
		input:               input,
		output:              output,
		log:                 logOutput,
		targetVersion:       target,
		unresolvedSeverity:  defaultUnresolvedSeverity,
		cancellations:       make(map[jsonrpc2.ID]context.CancelFunc),
		documents:           workspace.NewDocuments(),
		encoding:            text.UTF16,
		exitCode:            make(chan int, 1),
		analysisContext:     analysisContext,
		analysisCancel:      analysisCancel,
		analysisWake:        make(chan struct{}, maxParallelAnalysis),
		analysisPending:     make(map[string]struct{}),
		analysisRunning:     make(map[string]struct{}),
		parsed:              make(map[string]parsedDocument),
		published:           make(map[string]bool),
		workspaceIndex:      workspace.NewIndex(maxWorkspaceFiles, maxIndexBytes),
		workspaceGraph:      graph,
		workspaceGraphView:  graph.Snapshot(),
		workspaceFiles:      make(map[string]struct{}),
		workspacePending:    make(map[string]struct{}),
		workspaceDependents: make(map[string]struct{}),
	}
}

func (s *Server) TargetVersion() TargetVersion {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.targetVersion
}

// Run serves one LSP session until exit, EOF, cancellation, or a transport error.
func (s *Server) Run(ctx context.Context) int {
	if ctx.Err() != nil {
		return 0
	}
	defer s.stopAnalysis()
	stream := jsonrpc.NewStream(s.input, s.output)
	conn := jsonrpc2.NewConn(stream, jsonrpc2.WithCodec(protocolCodec{}))
	s.mu.Lock()
	s.client = protocol.ClientDispatcher(conn)
	s.mu.Unlock()
	ctx = protocol.WithClient(ctx, s.client)
	handler := s.lifecycleHandler(s.cancellationHandler(protocol.ServerHandler(s, jsonrpc2.MethodNotFoundHandler)))
	conn.Go(ctx, handler)

	select {
	case code := <-s.exitCode:
		_ = stream.Close()
		return code
	case <-ctx.Done():
		_ = stream.Close()
		return 0
	case <-conn.Done():
		select {
		case code := <-s.exitCode:
			return code
		default:
		}
		if err := conn.Err(); err != nil {
			s.logf("vimls: connection error: %v", err)
			return 1
		}
		return 0
	}
}

func (s *Server) cancellationHandler(next jsonrpc2.Handler) jsonrpc2.Handler {
	return func(ctx context.Context, request *jsonrpc2.Request) (any, error) {
		if request.Method() == protocol.MethodCancelRequest {
			var params protocol.CancelParams
			if err := protocol.Unmarshal(request.Params(), &params); err != nil {
				return nil, jsonrpc2.ErrInvalidParams
			}
			var id jsonrpc2.ID
			switch value := params.ID.(type) {
			case protocol.Integer:
				id = jsonrpc2.NewNumberID(int64(value))
			case protocol.String:
				id = jsonrpc2.NewStringID(string(value))
			default:
				return nil, jsonrpc2.ErrInvalidParams
			}
			s.mu.Lock()
			cancel := s.cancellations[id]
			s.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			return nil, nil
		}
		if !request.IsCall() {
			return next(ctx, request)
		}

		base, cancel := context.WithCancel(jsonrpc2.DetachContext(ctx))
		requestCtx := valueContext{Context: base, values: ctx}
		id := request.ID()
		if !s.registerCancellation(id, cancel) {
			cancel()
			return nil, jsonrpc2.NewError(jsonrpc2.JSONRPCReservedErrorRangeEnd, "too many pending requests")
		}
		defer func() {
			s.mu.Lock()
			delete(s.cancellations, id)
			s.mu.Unlock()
			cancel()
		}()
		return next(requestCtx, request)
	}
}

func (s *Server) registerCancellation(id jsonrpc2.ID, cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cancellations) >= maxPendingRequests {
		return false
	}
	s.cancellations[id] = cancel
	return true
}

func (s *Server) lifecycleHandler(next jsonrpc2.Handler) jsonrpc2.Handler {
	return func(ctx context.Context, request *jsonrpc2.Request) (any, error) {
		method := request.Method()
		if method == protocol.MethodExit {
			if request.IsCall() {
				return nil, jsonrpc2.NewError(jsonrpc2.InvalidRequest, "exit must be a notification")
			}
			return next(ctx, request)
		}
		if method == protocol.MethodInitialize && !request.IsCall() {
			return nil, nil
		}
		if method == protocol.MethodInitialized && request.IsCall() {
			return nil, jsonrpc2.NewError(jsonrpc2.InvalidRequest, "initialized must be a notification")
		}
		if method == protocol.MethodShutdown && !request.IsCall() {
			return nil, nil
		}
		if method == protocol.MethodCancelRequest && request.IsCall() {
			return nil, jsonrpc2.NewError(jsonrpc2.InvalidRequest, "cancel must be a notification")
		}

		s.mu.Lock()
		current := s.state
		s.mu.Unlock()
		switch current {
		case stateBeforeInitialize:
			if method != protocol.MethodInitialize {
				if request.IsCall() {
					return nil, jsonrpc2.NewError(jsonrpc2.ServerNotInitialized, "Server not initialized")
				}
				return nil, nil
			}
		case stateActive:
			if method == protocol.MethodInitialize {
				return nil, jsonrpc2.NewError(jsonrpc2.InvalidRequest, "initialize may only be sent once")
			}
		case stateShutdown:
			if request.IsCall() {
				return nil, jsonrpc2.NewError(jsonrpc2.InvalidRequest, "server has shut down")
			}
			return nil, nil
		}
		if method == protocol.MethodInitialized && len(request.Params()) == 0 {
			return nil, s.Initialized(ctx, &protocol.InitializedParams{})
		}
		if method == MethodDidChangeRuntimepath {
			if request.IsCall() {
				return nil, jsonrpc2.NewError(jsonrpc2.InvalidRequest, "runtimepath changes must be notifications")
			}
			var params DidChangeRuntimepathParams
			if err := protocol.Unmarshal(request.Params(), &params); err != nil {
				return nil, jsonrpc2.ErrInvalidParams
			}
			return nil, s.DidChangeRuntimepath(ctx, &params)
		}
		if !implementedMethod(method) {
			if request.IsCall() {
				return nil, jsonrpc2.ErrMethodNotFound
			}
			return nil, nil
		}
		result, err := next(ctx, request)
		if !request.IsCall() && err != nil && errors.Is(err, jsonrpc2.ErrMethodNotFound) {
			return nil, nil
		}
		return result, err
	}
}

func implementedMethod(method string) bool {
	switch method {
	case protocol.MethodInitialize,
		protocol.MethodInitialized,
		protocol.MethodShutdown,
		protocol.MethodExit,
		protocol.MethodCancelRequest,
		protocol.MethodTextDocumentDidOpen,
		protocol.MethodTextDocumentDidChange,
		protocol.MethodTextDocumentDidSave,
		protocol.MethodTextDocumentDidClose,
		protocol.MethodTextDocumentDeclaration,
		protocol.MethodTextDocumentDefinition,
		protocol.MethodTextDocumentReferences,
		protocol.MethodTextDocumentDocumentHighlight,
		protocol.MethodTextDocumentHover,
		protocol.MethodTextDocumentDocumentSymbol,
		protocol.MethodTextDocumentFoldingRange,
		protocol.MethodTextDocumentSelectionRange,
		protocol.MethodTextDocumentDocumentLink,
		protocol.MethodTextDocumentCompletion,
		protocol.MethodCompletionItemResolve,
		protocol.MethodTextDocumentSignatureHelp,
		protocol.MethodTextDocumentPrepareRename,
		protocol.MethodTextDocumentRename,
		protocol.MethodTextDocumentSemanticTokensFull,
		protocol.MethodTextDocumentCodeAction,
		protocol.MethodTextDocumentInlayHint,
		protocol.MethodWorkspaceDidChangeConfiguration,
		protocol.MethodWorkspaceDidChangeWorkspaceFolders,
		protocol.MethodWorkspaceDidChangeWatchedFiles,
		protocol.MethodWorkspaceSymbol,
		MethodDidChangeRuntimepath:
		return true
	default:
		return false
	}
}

func (s *Server) Initialize(_ context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	encoding, protocolEncoding := negotiatePositionEncoding(params.Capabilities.General)
	openClose := true
	includeText := true
	changeKind := protocol.TextDocumentSyncKindIncremental
	targetVersion, targetOverride, targetWarning := targetVersionFromOptions([]byte(params.InitializationOptions))
	runtimePaths, runtimepathConfigured, runtimepathWarning := runtimepathFromOptions([]byte(params.InitializationOptions))
	unresolvedSeverity, unresolvedWarning := unresolvedSeverityFromOptions([]byte(params.InitializationOptions))
	if !runtimepathConfigured {
		runtimePaths = defaultRuntimePaths()
	}
	watchDynamic, watchRelative := watchedFilesCapabilities(params.Capabilities.Workspace)
	workspaceConfiguration := params.Capabilities.Workspace != nil && params.Capabilities.Workspace.Configuration != nil && *params.Capabilities.Workspace.Configuration
	prepareRename := params.Capabilities.TextDocument != nil && params.Capabilities.TextDocument.Rename != nil && params.Capabilities.TextDocument.Rename.PrepareSupport != nil && *params.Capabilities.TextDocument.Rename.PrepareSupport
	codeActionLiterals := params.Capabilities.TextDocument != nil && params.Capabilities.TextDocument.CodeAction != nil && params.Capabilities.TextDocument.CodeAction.CodeActionLiteralSupport.CodeActionKind.ValueSet != nil
	s.mu.Lock()
	s.targetVersion = targetVersion
	s.targetOverride = targetOverride
	s.unresolvedSeverity = unresolvedSeverity
	s.pendingWarning = targetWarning
	for _, warning := range []string{runtimepathWarning, unresolvedWarning} {
		if warning == "" {
			continue
		}
		if s.pendingWarning == "" {
			s.pendingWarning = warning
		} else {
			s.pendingWarning += "; " + warning
		}
	}
	s.encoding = encoding
	s.state = stateActive
	s.watchDynamicRegistration = watchDynamic
	s.watchRelativePatterns = watchRelative
	s.workspaceConfiguration = workspaceConfiguration
	s.mu.Unlock()
	s.setWorkspaceRoots(workspaceRootsFromInitialize(params))
	s.setRuntimePaths(runtimePaths)
	s.refreshWorkspaceResolver()
	workspaceFoldersSupported := true
	completionResolve := true
	documentLinkResolve := false
	renamePrepare := true
	var renameProvider protocol.RenameProvider = protocol.Boolean(true)
	if prepareRename {
		renameProvider = &protocol.RenameOptions{PrepareProvider: &renamePrepare}
	}
	var codeActionProvider protocol.CodeActionProvider
	if codeActionLiterals {
		codeActionProvider = &protocol.CodeActionOptions{CodeActionKinds: []protocol.CodeActionKind{protocol.CodeActionKindQuickFix}}
	}
	return &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			PositionEncoding:          protocolEncoding,
			DeclarationProvider:       protocol.Boolean(true),
			DefinitionProvider:        protocol.Boolean(true),
			ReferencesProvider:        protocol.Boolean(true),
			DocumentHighlightProvider: protocol.Boolean(true),
			HoverProvider:             protocol.Boolean(true),
			DocumentSymbolProvider:    protocol.Boolean(true),
			FoldingRangeProvider:      protocol.Boolean(true),
			SelectionRangeProvider:    protocol.Boolean(true),
			WorkspaceSymbolProvider:   protocol.Boolean(true),
			DocumentLinkProvider:      &protocol.DocumentLinkOptions{ResolveProvider: &documentLinkResolve},
			CompletionProvider:        &protocol.CompletionOptions{ResolveProvider: &completionResolve},
			SignatureHelpProvider:     &protocol.SignatureHelpOptions{TriggerCharacters: []string{"(", ","}, RetriggerCharacters: []string{","}},
			RenameProvider:            renameProvider,
			SemanticTokensProvider: &protocol.SemanticTokensOptions{
				Legend: protocol.SemanticTokensLegend{TokenTypes: append([]string(nil), semanticTokenTypes...), TokenModifiers: append([]string(nil), semanticTokenModifiers...)},
				Full:   protocol.Boolean(true),
			},
			CodeActionProvider: codeActionProvider,
			InlayHintProvider:  protocol.Boolean(true),
			Workspace: &protocol.WorkspaceOptions{WorkspaceFolders: &protocol.WorkspaceFoldersServerCapabilities{
				Supported: &workspaceFoldersSupported, ChangeNotifications: protocol.Boolean(true),
			}},
			TextDocumentSync: &protocol.TextDocumentSyncOptions{
				OpenClose: &openClose,
				Change:    &changeKind,
				Save:      &protocol.SaveOptions{IncludeText: &includeText},
			},
		},
		ServerInfo: protocol.ServerInfo{Name: Name, Version: protocol.NewOptional(Version)},
	}, nil
}

func (s *Server) Initialized(ctx context.Context, _ *protocol.InitializedParams) error {
	s.mu.Lock()
	s.initialized = true
	s.mu.Unlock()
	s.scheduleFileWatchRegistration()
	s.scheduleWorkspaceRebuild()
	if err := s.refreshWorkspaceConfiguration(ctx); err != nil {
		s.logf("vimls: request workspace configuration: %v", err)
	}
	s.mu.Lock()
	warning := s.pendingWarning
	s.pendingWarning = ""
	client := s.client
	s.mu.Unlock()
	if warning == "" || client == nil {
		return nil
	}
	err := client.LogMessage(ctx, &protocol.LogMessageParams{
		Type:    protocol.MessageTypeWarning,
		Message: warning,
	})
	if err != nil {
		s.logf("vimls: send configuration warning: %v", err)
	}
	return err
}

func watchedFilesCapabilities(workspaceCapabilities *protocol.WorkspaceClientCapabilities) (dynamic, relative bool) {
	if workspaceCapabilities == nil || workspaceCapabilities.DidChangeWatchedFiles == nil {
		return false, false
	}
	capabilities := workspaceCapabilities.DidChangeWatchedFiles
	return capabilities.DynamicRegistration != nil && *capabilities.DynamicRegistration,
		capabilities.RelativePatternSupport != nil && *capabilities.RelativePatternSupport
}

func (s *Server) Shutdown(context.Context) error {
	s.mu.Lock()
	s.state = stateShutdown
	s.mu.Unlock()
	return nil
}

func (s *Server) DidOpen(_ context.Context, params *protocol.DidOpenTextDocumentParams) error {
	document := params.TextDocument
	s.publishMu.Lock()
	snapshot := s.documents.Open(document.URI.String(), document.Version, document.Text)
	s.removeWorkspaceURI(snapshot.URI())
	s.publishMu.Unlock()
	s.startAnalysis(document.URI.String())
	return nil
}

func (s *Server) DidChange(_ context.Context, params *protocol.DidChangeTextDocumentParams) error {
	changes := make([]text.Change, 0, len(params.ContentChanges))
	for _, change := range params.ContentChanges {
		switch value := change.(type) {
		case *protocol.TextDocumentContentChangePartial:
			changes = append(changes, text.Change{
				Range: &text.Range{
					Start: fromProtocolPosition(value.Range.Start),
					End:   fromProtocolPosition(value.Range.End),
				},
				Text: value.Text,
			})
		case *protocol.TextDocumentContentChangeWholeDocument:
			changes = append(changes, text.Change{Text: value.Text})
		default:
			s.logf("vimls: ignored invalid content change for %s", params.TextDocument.URI)
			return nil
		}
	}
	s.mu.Lock()
	encoding := s.encoding
	s.mu.Unlock()
	s.publishMu.Lock()
	snapshot, err := s.documents.Change(params.TextDocument.URI.String(), params.TextDocument.Version, encoding, changes)
	if err == nil {
		s.removeWorkspaceURI(snapshot.URI())
	}
	s.publishMu.Unlock()
	if err != nil {
		s.logf("vimls: ignored content change for %s: %v", params.TextDocument.URI, err)
	} else {
		s.startAnalysis(params.TextDocument.URI.String())
	}
	return nil
}

func (s *Server) DidSave(_ context.Context, params *protocol.DidSaveTextDocumentParams) error {
	s.publishMu.Lock()
	snapshot, err := s.documents.Save(params.TextDocument.URI.String(), params.Text)
	if err == nil {
		s.removeWorkspaceURI(snapshot.URI())
	}
	s.publishMu.Unlock()
	if err != nil {
		s.logf("vimls: ignored save for %s: %v", params.TextDocument.URI, err)
	} else {
		s.startAnalysis(params.TextDocument.URI.String())
	}
	return nil
}

func (s *Server) DidClose(_ context.Context, params *protocol.DidCloseTextDocumentParams) error {
	documentURI := params.TextDocument.URI.String()
	s.analysisMu.Lock()
	delete(s.analysisPending, documentURI)
	s.analysisMu.Unlock()
	s.publishMu.Lock()
	s.documents.Close(documentURI)
	s.restoreWorkspaceDocument(documentURI)
	delete(s.parsed, documentURI)
	clearDiagnostics := s.published[documentURI]
	delete(s.published, documentURI)
	s.publishMu.Unlock()
	if clearDiagnostics {
		s.clearDiagnostics(documentURI)
	}
	return nil
}

func (s *Server) DidChangeConfiguration(ctx context.Context, params *protocol.DidChangeConfigurationParams) error {
	if len(params.Settings) == 0 || string(params.Settings) == "null" {
		return s.refreshWorkspaceConfiguration(ctx)
	}
	return s.applyWorkspaceConfiguration(ctx, []byte(params.Settings))
}

func (s *Server) refreshWorkspaceConfiguration(ctx context.Context) error {
	s.mu.Lock()
	supported := s.workspaceConfiguration
	client := s.client
	s.mu.Unlock()
	if !supported || client == nil {
		return nil
	}
	section := "vimls"
	values, err := client.Configuration(ctx, &protocol.ConfigurationParams{Items: []protocol.ConfigurationItem{{Section: &section}}})
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	return s.applyWorkspaceConfiguration(ctx, []byte(values[0]))
}

func (s *Server) applyWorkspaceConfiguration(ctx context.Context, settings []byte) error {
	s.mu.Lock()
	var targetWarning string
	if !s.targetOverride {
		s.targetVersion, targetWarning = targetVersionFromSettings(settings, s.targetVersion)
	}
	var unresolvedWarning string
	s.unresolvedSeverity, unresolvedWarning = unresolvedSeverityFromSettings(settings, s.unresolvedSeverity)
	s.mu.Unlock()
	s.publishMu.Lock()
	snapshots := s.documents.ConfigurationChanged()
	s.publishMu.Unlock()
	for _, snapshot := range snapshots {
		s.startAnalysis(snapshot.URI())
	}
	if targetWarning != "" && unresolvedWarning != "" {
		return s.sendWarning(ctx, targetWarning+"; "+unresolvedWarning)
	}
	if targetWarning != "" {
		return s.sendWarning(ctx, targetWarning)
	}
	if unresolvedWarning != "" {
		return s.sendWarning(ctx, unresolvedWarning)
	}
	return nil
}

func (s *Server) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) (protocol.DocumentSymbolResult, error) {
	documentURI := params.TextDocument.URI.String()
	s.publishMu.Lock()
	snapshot, ok := s.documents.Snapshot(documentURI)
	parsed := s.parsed[documentURI]
	s.publishMu.Unlock()
	if !ok || snapshot.ByteLen() > maxFileBytes {
		return protocol.DocumentSymbolSlice{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, protocol.ErrRequestCancelled
	}
	file := parsed.file
	if file == nil || parsed.revision != snapshot.Revision() {
		file = syntax.Parse(snapshot.Text())
	}
	s.mu.Lock()
	encoding := s.encoding
	s.mu.Unlock()
	result := make(protocol.DocumentSymbolSlice, 0)
	for _, symbol := range analysis.CollectSymbols(file) {
		if converted, valid := documentSymbol(snapshot, encoding, symbol); valid {
			result = append(result, converted)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, protocol.ErrRequestCancelled
	}
	current, currentOK := s.documents.Snapshot(documentURI)
	if !currentOK || current != snapshot {
		return nil, protocol.ErrContentModified
	}
	return result, nil
}

func documentSymbol(snapshot *text.Snapshot, encoding text.Encoding, symbol *analysis.Symbol) (protocol.DocumentSymbol, bool) {
	rangeValue, rangeOK := protocolRange(snapshot, encoding, symbol.Range)
	selection, selectionOK := protocolRange(snapshot, encoding, symbol.SelectionRange)
	if !rangeOK || !selectionOK || symbol.Name == "" {
		return protocol.DocumentSymbol{}, false
	}
	result := protocol.DocumentSymbol{
		Name: symbol.Name, Kind: protocolSymbolKind(symbol.Kind), Range: rangeValue, SelectionRange: selection,
	}
	if symbol.Deprecated {
		result.Tags = []protocol.SymbolTag{protocol.SymbolTagDeprecated}
	}
	if symbol.Detail != "" {
		detail := symbol.Detail
		result.Detail = &detail
	}
	for _, child := range symbol.Children {
		if converted, ok := documentSymbol(snapshot, encoding, child); ok {
			result.Children = append(result.Children, converted)
		}
	}
	return result, true
}

func protocolRange(snapshot *text.Snapshot, encoding text.Encoding, span syntax.Span) (protocol.Range, bool) {
	start, startError := snapshot.Position(span.Start, encoding)
	end, endError := snapshot.Position(span.End, encoding)
	if startError != nil || endError != nil {
		return protocol.Range{}, false
	}
	return protocol.Range{
		Start: protocol.Position{Line: uint32(start.Line), Character: uint32(start.Character)},
		End:   protocol.Position{Line: uint32(end.Line), Character: uint32(end.Character)},
	}, true
}

func protocolSymbolKind(kind analysis.SymbolKind) protocol.SymbolKind {
	switch kind {
	case analysis.SymbolKindImport:
		return protocol.SymbolKindModule
	case analysis.SymbolKindClass:
		return protocol.SymbolKindClass
	case analysis.SymbolKindInterface:
		return protocol.SymbolKindInterface
	case analysis.SymbolKindEnum:
		return protocol.SymbolKindEnum
	case analysis.SymbolKindEnumMember:
		return protocol.SymbolKindEnumMember
	case analysis.SymbolKindTypeAlias:
		return protocol.SymbolKindStruct
	case analysis.SymbolKindMethod:
		return protocol.SymbolKindMethod
	case analysis.SymbolKindConstructor:
		return protocol.SymbolKindConstructor
	case analysis.SymbolKindVariable:
		return protocol.SymbolKindVariable
	case analysis.SymbolKindConstant:
		return protocol.SymbolKindConstant
	default:
		return protocol.SymbolKindFunction
	}
}

func (s *Server) Exit(context.Context) error {
	s.mu.Lock()
	code := 1
	if s.state == stateShutdown {
		code = 0
	}
	s.mu.Unlock()
	s.exitOnce.Do(func() { s.exitCode <- code })
	return nil
}

func (s *Server) Request(context.Context, string, any) (any, error) {
	return nil, jsonrpc2.ErrMethodNotFound
}

func (s *Server) sendWarning(ctx context.Context, warning string) error {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return nil
	}
	err := client.LogMessage(ctx, &protocol.LogMessageParams{Type: protocol.MessageTypeWarning, Message: warning})
	if err != nil {
		s.logf("vimls: send warning: %v", err)
	}
	return err
}

func negotiatePositionEncoding(general *protocol.GeneralClientCapabilities) (text.Encoding, protocol.PositionEncodingKind) {
	if general != nil {
		for _, encoding := range general.PositionEncodings {
			switch encoding {
			case protocol.PositionEncodingKindUTF8:
				return text.UTF8, encoding
			case protocol.PositionEncodingKindUTF16:
				return text.UTF16, encoding
			case protocol.PositionEncodingKindUTF32:
				return text.UTF32, encoding
			}
		}
	}
	return text.UTF16, protocol.PositionEncodingKindUTF16
}

func fromProtocolPosition(position protocol.Position) text.Position {
	return text.Position{Line: int(position.Line), Character: int(position.Character)}
}

func (s *Server) startAnalysis(documentURI string) {
	s.analysisMu.Lock()
	if s.analysisStopped {
		s.analysisMu.Unlock()
		return
	}
	_, pending := s.analysisPending[documentURI]
	s.analysisPending[documentURI] = struct{}{}
	if s.analysisWorkers == 0 {
		s.analysisWorkers = analysisParallelism()
		s.analysisWG.Add(s.analysisWorkers)
		for range s.analysisWorkers {
			go s.analysisWorker()
		}
	}
	if !pending {
		s.wakeAnalysisLocked()
	}
	s.analysisMu.Unlock()

}

func analysisParallelism() int {
	parallelism := runtime.GOMAXPROCS(0)
	if parallelism < 1 {
		return 1
	}
	if parallelism > maxParallelAnalysis {
		return maxParallelAnalysis
	}
	return parallelism
}

func (s *Server) analysisWorker() {
	defer s.analysisWG.Done()
	for {
		select {
		case <-s.analysisContext.Done():
			return
		case <-s.analysisWake:
		}
		for {
			documentURI, ok := s.takePendingAnalysis()
			if !ok {
				break
			}
			s.analyzeDocument(documentURI)
			s.finishAnalysis(documentURI)
		}
	}
}

func (s *Server) takePendingAnalysis() (string, bool) {
	s.analysisMu.Lock()
	defer s.analysisMu.Unlock()
	if s.analysisStopped {
		return "", false
	}
	documentURI := ""
	for candidate := range s.analysisPending {
		if _, running := s.analysisRunning[candidate]; running {
			continue
		}
		if documentURI == "" || candidate < documentURI {
			documentURI = candidate
		}
	}
	if documentURI == "" {
		return "", false
	}
	delete(s.analysisPending, documentURI)
	s.analysisRunning[documentURI] = struct{}{}
	return documentURI, true
}

func (s *Server) finishAnalysis(documentURI string) {
	s.analysisMu.Lock()
	delete(s.analysisRunning, documentURI)
	if len(s.analysisPending) > 0 && !s.analysisStopped {
		s.wakeAnalysisLocked()
	}
	s.analysisMu.Unlock()
}

func (s *Server) wakeAnalysisLocked() {
	select {
	case s.analysisWake <- struct{}{}:
	default:
	}
}

func (s *Server) analyzeDocument(documentURI string) {
	work, ok := s.documents.BeginAnalysis(s.analysisContext, documentURI)
	if !ok || work.Context.Err() != nil {
		return
	}
	target := s.TargetVersion()
	if target.Latest {
		target, _ = ParseTargetVersion(MaximumTargetVersion)
	}
	var file *syntax.File
	var fileAnalysis *analysis.FileAnalysis
	if work.Snapshot.ByteLen() > maxFileBytes {
		file = &syntax.File{
			Source: work.Snapshot.Text(),
			Diagnostics: []syntax.Diagnostic{{
				Code: "vimls/file-too-large", Message: "file exceeds the 4 MiB analysis limit",
			}},
		}
	} else {
		file = syntax.Parse(work.Snapshot.Text())
		file.Diagnostics = append(file.Diagnostics, syntax.CompatibilityDiagnostics(file, syntax.Version{Major: target.Major, Minor: target.Minor, Patch: target.Patch})...)
		fileAnalysis = analysis.Analyze(file)
		versionedAnalysis := *fileAnalysis
		versionedAnalysis.Diagnostics = analysisDiagnosticsForTarget(file, fileAnalysis.Diagnostics, target)
		versionedAnalysis.Diagnostics = s.autoloadExportedFunctionDiagnostics(work.Snapshot.URI(), file, fileAnalysis, versionedAnalysis.Diagnostics)
		file.Diagnostics = analysis.CombinedDiagnostics(file, &versionedAnalysis)
		if work.Context.Err() != nil {
			return
		}
	}
	if work.Context.Err() != nil {
		return
	}
	if !s.prepareSyntax(work, file) {
		return
	}
	graphRevision, graphReady, importDiagnostics := s.workspaceImportDiagnostics(work.Snapshot.URI(), file, fileAnalysis)
	if !graphReady || work.Context.Err() != nil {
		return
	}
	file.Diagnostics = append(file.Diagnostics, importDiagnostics...)
	file.Diagnostics = append(file.Diagnostics, s.userCommandAbbreviationDiagnostics(file)...)
	file.Diagnostics = append(file.Diagnostics, s.globalNameConflictDiagnostics(work.Snapshot.URI(), file)...)
	sort.SliceStable(file.Diagnostics, func(left, right int) bool {
		if file.Diagnostics[left].Span.Start != file.Diagnostics[right].Span.Start {
			return file.Diagnostics[left].Span.Start < file.Diagnostics[right].Span.Start
		}
		return file.Diagnostics[left].Span.End < file.Diagnostics[right].Span.End
	})
	if len(file.Diagnostics) > maxDiagnosticsPerDocument {
		file.Diagnostics = append(file.Diagnostics[:maxDiagnosticsPerDocument-1], syntax.Diagnostic{
			Code: "vimls/diagnostics-truncated", Message: "additional diagnostics were omitted",
			Span: syntax.Span{Start: len(file.Source), End: len(file.Source)},
		})
	}
	s.publishSyntax(work, file, graphRevision)
}

func (s *Server) userCommandAbbreviationDiagnostics(file *syntax.File) []syntax.Diagnostic {
	s.workspaceMu.Lock()
	index := s.workspaceIndex
	ready := s.workspaceBuilt && len(s.workspacePending) == 0
	s.workspaceMu.Unlock()
	if !ready || index == nil || !index.Complete() {
		return nil
	}
	return analysis.UserCommandAbbreviationDiagnostics(file, index.UserCommandNames())
}

func (s *Server) globalNameConflictDiagnostics(documentURI string, file *syntax.File) []syntax.Diagnostic {
	path, ok := workspaceURIPath(uri.URI(documentURI))
	if !ok {
		return nil
	}
	s.workspaceMu.Lock()
	index := s.workspaceIndex
	ready := s.workspaceBuilt && len(s.workspacePending) == 0
	s.workspaceMu.Unlock()
	if !ready || index == nil {
		return nil
	}
	return index.GlobalNameConflictDiagnostics(path, file)
}

func (s *Server) autoloadExportedFunctionDiagnostics(documentURI string, file *syntax.File, result *analysis.FileAnalysis, diagnostics []syntax.Diagnostic) []syntax.Diagnostic {
	path, ok := workspaceURIPath(uri.URI(documentURI))
	if !ok || file == nil || result == nil || result.Root == nil {
		return diagnostics
	}
	s.workspaceMu.Lock()
	roots := workspaceIndexRoots(s.workspaceRoots, s.runtimePaths)
	s.workspaceMu.Unlock()
	autoload := false
	for _, root := range roots {
		if _, ok := workspaceAutoloadPath(path, root); ok {
			autoload = true
			break
		}
	}
	if !autoload {
		return diagnostics
	}
	versioned := diagnostics
	copied := false
	for index := range diagnostics {
		diagnostic := diagnostics[index]
		if diagnostic.Code != "vim/E1041" || !autoloadExportedDefVariableConflict(file, result, diagnostic.Span) {
			continue
		}
		if !copied {
			versioned = append([]syntax.Diagnostic(nil), diagnostics...)
			copied = true
		}
		name := file.Text(diagnostic.Span)
		versioned[index].Code = "vim/E707"
		versioned[index].Message = "Function name conflicts with variable: " + name
	}
	return versioned
}

func autoloadExportedDefVariableConflict(file *syntax.File, result *analysis.FileAnalysis, span syntax.Span) bool {
	var function *syntax.Command
	for index := range file.Commands {
		command := &file.Commands[index]
		if command.Canonical == "def" && command.Function != nil && command.Function.Name == span && serverCommandHasModifier(command, "export") {
			function = command
			break
		}
	}
	if function == nil {
		return false
	}
	name := file.Text(span)
	for _, declaration := range result.Root.Declarations {
		if declaration != nil && declaration.Span.Start < span.Start && declaration.Name == name &&
			(declaration.Kind == analysis.SymbolKindVariable || declaration.Kind == analysis.SymbolKindConstant) {
			return true
		}
	}
	return false
}

func serverCommandHasModifier(command *syntax.Command, name string) bool {
	for _, modifier := range command.Modifiers {
		if modifier.Name == name {
			return true
		}
	}
	return false
}

func analysisDiagnosticsForTarget(file *syntax.File, diagnostics []syntax.Diagnostic, target TargetVersion) []syntax.Diagnostic {
	if target.Major > 9 || target.Major == 9 && (target.Minor > 2 || target.Minor == 2 && target.Patch >= 507) {
		return diagnostics
	}
	versioned := append([]syntax.Diagnostic(nil), diagnostics...)
	for index := range versioned {
		if versioned[index].Code == "vim/E1406" {
			versioned[index].Code = "vim/E1369"
			versioned[index].Message = "Duplicate variable: " + file.Text(versioned[index].Span)
		}
	}
	return versioned
}

func (s *Server) prepareSyntax(analysis workspace.Analysis, file *syntax.File) bool {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if !s.documents.IsCurrent(analysis) {
		return false
	}
	documentURI := analysis.Snapshot.URI()
	parsedFile := *file
	parsedFile.Diagnostics = append([]syntax.Diagnostic(nil), file.Diagnostics...)
	s.parsed[documentURI] = parsedDocument{revision: analysis.Snapshot.Revision(), file: &parsedFile}
	dependents := s.replaceWorkspaceFile(documentURI, file)
	s.startWorkspaceDependents(dependents)
	return true
}

func (s *Server) publishSyntax(analysis workspace.Analysis, file *syntax.File, graphRevision uint64) {
	s.mu.Lock()
	encoding := s.encoding
	client := s.client
	unresolvedSeverity := s.unresolvedSeverity
	s.mu.Unlock()

	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if !s.documents.IsCurrent(analysis) {
		return
	}
	documentURI := analysis.Snapshot.URI()
	s.workspaceMu.Lock()
	currentGraphRevision := s.workspaceGraphView.Revision()
	s.workspaceMu.Unlock()
	if currentGraphRevision != graphRevision {
		s.startAnalysis(documentURI)
		return
	}
	diagnostics := make([]protocol.Diagnostic, 0, len(file.Diagnostics))
	for _, item := range file.Diagnostics {
		start, startError := analysis.Snapshot.Position(item.Span.Start, encoding)
		end, endError := analysis.Snapshot.Position(item.Span.End, encoding)
		if startError != nil || endError != nil {
			continue
		}
		diagnostic := protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: uint32(start.Line), Character: uint32(start.Character)},
				End:   protocol.Position{Line: uint32(end.Line), Character: uint32(end.Character)},
			},
			Severity: protocolDiagnosticSeverity(item.Code, unresolvedSeverity),
			Code:     protocol.String(item.Code),
			Source:   protocol.NewOptional(Name),
			Message:  protocol.String(item.Message),
		}
		if item.Code == "vimls/deprecated" {
			diagnostic.Tags = protocol.NewDiagnosticTags(protocol.DiagnosticTagDeprecated)
		} else if item.Code == "vimls/unused-variable" {
			diagnostic.Tags = protocol.NewDiagnosticTags(protocol.DiagnosticTagUnnecessary)
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	if len(diagnostics) == 0 && !s.published[documentURI] {
		return
	}
	if len(diagnostics) == 0 {
		delete(s.published, documentURI)
	} else {
		s.published[documentURI] = true
	}
	if client == nil {
		return
	}
	params := &protocol.PublishDiagnosticsParams{URI: uri.URI(documentURI), Diagnostics: diagnostics}
	if version, ok := analysis.Snapshot.Version(); ok {
		params.Version = protocol.NewOptional(version)
	}
	if err := client.PublishDiagnostics(analysis.Context, params); err != nil && analysis.Context.Err() == nil {
		s.logf("vimls: publish diagnostics for %s: %v", documentURI, err)
	}
}

func protocolDiagnosticSeverity(code string, unresolvedSeverity syntax.DiagnosticSeverity) protocol.DiagnosticSeverity {
	switch code {
	case "vim/E122", "vim/E174", "vim/E464", "vim/E705", "vim/E707":
		return protocol.DiagnosticSeverityWarning
	case "vim/E117", "vim/E121", "vim/E1001", "vim/E1089":
		return protocolSeverity(unresolvedSeverity)
	}
	definition, ok := syntax.LookupVimlsDiagnostic(code)
	if !ok {
		return protocol.DiagnosticSeverityError
	}
	return protocolSeverity(definition.Severity)
}

func protocolSeverity(severity syntax.DiagnosticSeverity) protocol.DiagnosticSeverity {
	switch severity {
	case syntax.DiagnosticWarning:
		return protocol.DiagnosticSeverityWarning
	case syntax.DiagnosticInformation:
		return protocol.DiagnosticSeverityInformation
	case syntax.DiagnosticHint:
		return protocol.DiagnosticSeverityHint
	default:
		return protocol.DiagnosticSeverityError
	}
}

func (s *Server) clearDiagnostics(documentURI string) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client != nil {
		if err := client.PublishDiagnostics(s.analysisContext, &protocol.PublishDiagnosticsParams{URI: uri.URI(documentURI), Diagnostics: []protocol.Diagnostic{}}); err != nil {
			s.logf("vimls: clear diagnostics for %s: %v", documentURI, err)
		}
	}
}

func (s *Server) stopAnalysis() {
	s.analysisMu.Lock()
	if s.analysisStopped {
		s.analysisMu.Unlock()
		return
	}
	s.analysisStopped = true
	s.analysisCancel()
	s.analysisMu.Unlock()
	s.analysisWG.Wait()
	// Synchronize with a rebuild that may have checked analysisContext just
	// before cancellation, so its WaitGroup.Add completes before Wait starts.
	waitGroupAddBarrier(&s.workspaceMu)
	s.workspaceWG.Wait()
	waitGroupAddBarrier(&s.watchMu)
	s.watchWG.Wait()
}

func waitGroupAddBarrier(mu *sync.Mutex) {
	mu.Lock()
	defer mu.Unlock()
}

func (s *Server) logf(format string, args ...any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if s.log != nil {
		_, _ = fmt.Fprintf(s.log, format+"\n", args...)
	}
}

type protocolCodec struct{}

func (protocolCodec) Marshal(value any) ([]byte, error) {
	return protocol.Marshal(value)
}

func (protocolCodec) Unmarshal(data []byte, value any) error {
	return protocol.Unmarshal(data, value)
}

type valueContext struct {
	context.Context
	values context.Context
}

func (c valueContext) Value(key any) any {
	return c.values.Value(key)
}
