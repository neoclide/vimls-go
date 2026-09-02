package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"runtime"
	"sort"
	"sync"
	"time"

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
	Name                        = "vimls"
	maxFileBytes                = 4 << 20
	maxPendingRequests          = 128
	maxParallelAnalysis         = 6
	maxDiagnosticsPerDocument   = 200
	maxWorkspaceFiles           = 20000
	maxIndexBytes               = 256 << 20
	maxWorkspaceSymbols         = 200
	maxRelationshipFactsPerFile = 1 << 15
	maxRelationshipFacts        = 1 << 18
)

var Version = "dev"

const MethodDidChangeRuntimepath = "vimls/didChangeRuntimepath"
const fileWatchRegistrationID = "vimls-watch-vim-files"

type state uint8

const (
	stateBeforeInitialize state = iota
	stateActive
	stateShutdown
)

type parsedDocument struct {
	contentID text.ContentID
	file      *syntax.File
}

// semanticTokenResult is immutable after installation. The latest result for
// each URI is retained only as the base for a subsequent delta request.
type semanticTokenResult struct {
	data     []uint32
	resultID string
}

// workspaceIdentity names the post-install workspace state consumed by one
// document analysis. It is captured and checked only while workspaceMu is held.
type workspaceIdentity struct {
	generation    uint64
	index         *workspace.Index
	indexRevision uint64
	graphRevision uint64
}

type workspaceAnalysisSnapshot struct {
	identity          workspaceIdentity
	path              string
	graph             workspace.ImportGraphSnapshot
	targets           map[string]importTargetSnapshot
	roots             []string
	indexComplete     bool
	userCommandNames  []string
	globalDiagnostics []syntax.Diagnostic
	ready             bool
}

// Server mutexes use a partial, not total, lock order. When multiple Server
// mutexes are held, acquire them only in these directions:
//
//	publishMu -> mu
//	publishMu -> workspaceMu -> analysisMu
//	publishMu -> analysisMu
//	watchMu   -> mu
//	watchMu   -> workspaceMu
//
// No order is defined between publishMu and watchMu or between mu and
// workspaceMu; do not hold either unordered pair together. logMu is terminal:
// code holding logMu must not acquire another Server mutex, although logf may
// be called while another Server mutex is held.
type Server struct {
	protocol.UnimplementedServer

	input  io.Reader
	output io.Writer
	log    io.Writer
	logMu  sync.Mutex

	mu                       sync.Mutex
	state                    state
	pendingWarning           string
	client                   protocol.Client
	workspaceProgress        bool
	pullDiagnostics          bool
	diagnosticRefreshSupport bool

	semanticTokensRefreshSupport    bool
	semanticTokensRefreshGeneration uint64
	semanticTokensRefreshRunning    bool

	inlayHintRefreshSupport    bool
	inlayHintRefreshGeneration uint64
	inlayHintRefreshRunning    bool

	codeLensRefreshSupport    bool
	codeLensRefreshGeneration uint64
	codeLensRefreshRunning    bool

	workspaceProgressID         uint64
	cancellations               map[jsonrpc2.ID]context.CancelFunc
	documents                   *workspace.Documents
	encoding                    text.Encoding
	completion                  completionCapabilities
	languageFeatures            languageFeatureCapabilities
	exitOnce                    sync.Once
	stopOnce                    sync.Once
	exitCode                    chan int
	analysisMu                  sync.Mutex
	analysisContext             context.Context
	analysisCancel              context.CancelFunc
	analysisStopped             bool
	analysisWG                  sync.WaitGroup
	analysisWake                chan struct{}
	analysisPending             map[string]struct{}
	analysisRunning             map[string]struct{}
	analysisWorkers             int
	publishMu                   sync.Mutex
	parsed                      map[string]parsedDocument
	published                   map[string]bool
	pullDiagnosticResults       map[string]pullDiagnosticResult
	nextDiagnosticResultID      uint64
	semanticTokenResults        map[string]semanticTokenResult
	nextSemanticTokenResultID   uint64
	initialRefreshPending       map[string]struct{}
	diagnosticRefreshGeneration uint64
	diagnosticRefreshRunning    bool
	workspaceMu                 sync.Mutex
	workspaceRoots              []string
	runtimePaths                []string
	workspaceIndex              *workspace.Index
	workspaceGraph              *workspace.ImportGraph
	workspaceGraphView          workspace.ImportGraphSnapshot
	workspaceResolver           *workspace.PathResolver
	workspaceFiles              map[string]struct{}
	workspacePending            map[string]struct{}
	workspaceDependents         map[string]struct{}
	workspaceBuilt              bool
	workspaceRevision           uint64
	workspaceRunning            bool
	workspaceChanged            chan struct{}
	workspaceWake               chan struct{}
	workspaceDelay              time.Duration
	workspaceWG                 sync.WaitGroup
	hierarchyLimit              int

	// The following hooks are test-only synchronization seams. They are set
	// before use and are always called outside server locks.
	beforeParseSnapshotCacheMissForTest func(*text.Snapshot)
	beforeAnalyzeForTest                func(*syntax.File)
	beforeWorkspaceRestoreReadForTest   func(workspaceRestore)
	beforeWorkspaceRebuildDelayForTest  func()
	beforeWorkspaceIndexWaitForTest     func()
	beforeWorkspaceBuildForTest         func([]*text.Snapshot)

	watchMu                       sync.Mutex
	watchDynamicRegistration      bool
	watchRelativePatterns         bool
	watchRegistered               bool
	initialized                   bool
	watchWG                       sync.WaitGroup
	workspaceConfiguration        bool
	excludeRuntimePathCompletions bool
	// Diagnostic maps are replaced, never mutated, while mu is held. Analysis
	// and publication may therefore snapshot their immutable map references.
	disabledDiagnostics map[string]struct{}
	overrideDiagnostics map[string]protocol.DiagnosticSeverity

	beforeWorkspaceIdentityCheck func()
	completionNow                func() time.Time
}

func New(input io.Reader, output, logOutput io.Writer) *Server {
	analysisContext, analysisCancel := context.WithCancel(context.Background())
	graph := workspace.NewImportGraph()
	return &Server{
		input:                 input,
		output:                output,
		log:                   logOutput,
		cancellations:         make(map[jsonrpc2.ID]context.CancelFunc),
		documents:             workspace.NewDocuments(),
		encoding:              text.UTF16,
		exitCode:              make(chan int, 1),
		analysisContext:       analysisContext,
		analysisCancel:        analysisCancel,
		analysisWake:          make(chan struct{}, maxParallelAnalysis),
		analysisPending:       make(map[string]struct{}),
		pullDiagnosticResults: make(map[string]pullDiagnosticResult),
		semanticTokenResults:  make(map[string]semanticTokenResult),
		initialRefreshPending: make(map[string]struct{}),
		analysisRunning:       make(map[string]struct{}),
		parsed:                make(map[string]parsedDocument),
		published:             make(map[string]bool),
		workspaceIndex:        newWorkspaceIndex(),
		workspaceGraph:        graph,
		workspaceGraphView:    graph.Snapshot(),
		workspaceFiles:        make(map[string]struct{}),
		workspacePending:      make(map[string]struct{}),
		workspaceDependents:   make(map[string]struct{}),
		workspaceChanged:      make(chan struct{}),
		workspaceWake:         make(chan struct{}, 1),
		workspaceDelay:        defaultWorkspaceRebuildDebounce,
		disabledDiagnostics:   make(map[string]struct{}),
		overrideDiagnostics:   make(map[string]protocol.DiagnosticSeverity),
		hierarchyLimit:        maxHierarchyResults,
		completionNow:         time.Now,
	}
}

func newWorkspaceIndex() *workspace.Index {
	return workspace.NewIndex(maxWorkspaceFiles, maxIndexBytes, maxRelationshipFactsPerFile, maxRelationshipFacts)
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
			return next(valueContext{Context: jsonrpc2.DetachContext(ctx), values: ctx}, request)
		}

		base, cancel := context.WithCancel(jsonrpc2.DetachContext(ctx))
		requestCtx := valueContext{Context: base, values: ctx}
		id := request.ID()
		if err := s.registerCancellation(id, cancel); err != nil {
			cancel()
			return nil, err
		}
		// Register first so a following $/cancelRequest cannot be handled before this request.
		// Keep lifecycle calls ordered: later input may depend on initialize or shutdown completing.
		if request.Method() != protocol.MethodInitialize && request.Method() != protocol.MethodShutdown {
			jsonrpc2.Async(ctx)
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

func (s *Server) registerCancellation(id jsonrpc2.ID, cancel context.CancelFunc) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.cancellations[id]; exists {
		return jsonrpc2.NewError(jsonrpc2.InvalidRequest, fmt.Sprintf("duplicate request ID: %v", id))
	}
	if len(s.cancellations) >= maxPendingRequests {
		return jsonrpc2.NewError(jsonrpc2.JSONRPCReservedErrorRangeEnd, "too many pending requests")
	}
	s.cancellations[id] = cancel
	return nil
}

func (s *Server) lifecycleHandler(next jsonrpc2.Handler) jsonrpc2.Handler {
	return func(ctx context.Context, request *jsonrpc2.Request) (any, error) {
		ctx = valueContext{Context: jsonrpc2.DetachContext(ctx), values: ctx}
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
			var params *DidChangeRuntimepathParams
			if err := protocol.Unmarshal(request.Params(), &params); err != nil {
				return nil, jsonrpc2.ErrInvalidParams
			}
			return nil, s.DidChangeRuntimepath(ctx, params)
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
		protocol.MethodTextDocumentDiagnostic,
		protocol.MethodWorkspaceDiagnostic,
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
		protocol.MethodTextDocumentSemanticTokensFullDelta,
		protocol.MethodTextDocumentCodeLens,
		protocol.MethodCodeLensResolve,
		protocol.MethodTextDocumentCodeAction,
		protocol.MethodTextDocumentInlayHint,
		protocol.MethodTextDocumentFormatting,
		protocol.MethodTextDocumentRangeFormatting,
		protocol.MethodTextDocumentImplementation,
		protocol.MethodTextDocumentPrepareCallHierarchy,
		protocol.MethodCallHierarchyIncomingCalls,
		protocol.MethodCallHierarchyOutgoingCalls,
		protocol.MethodTextDocumentPrepareTypeHierarchy,
		protocol.MethodTypeHierarchySupertypes,
		protocol.MethodTypeHierarchySubtypes,
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
	runtimePaths, runtimepathConfigured, runtimepathWarning := runtimepathFromOptions([]byte(params.InitializationOptions))
	if !runtimepathConfigured {
		runtimePaths = defaultRuntimePaths()
	}
	runtimePaths = usableRuntimePaths(runtimePaths)
	watchDynamic, watchRelative := watchedFilesCapabilities(params.Capabilities.Workspace)
	workspaceConfiguration := params.Capabilities.Workspace != nil && params.Capabilities.Workspace.Configuration != nil && *params.Capabilities.Workspace.Configuration
	workspaceProgress := params.Capabilities.Window != nil && params.Capabilities.Window.WorkDoneProgress != nil && *params.Capabilities.Window.WorkDoneProgress
	prepareRename := params.Capabilities.TextDocument != nil && params.Capabilities.TextDocument.Rename != nil && params.Capabilities.TextDocument.Rename.PrepareSupport != nil && *params.Capabilities.TextDocument.Rename.PrepareSupport
	codeActionLiterals := params.Capabilities.TextDocument != nil && params.Capabilities.TextDocument.CodeAction != nil && params.Capabilities.TextDocument.CodeAction.CodeActionLiteralSupport.CodeActionKind.ValueSet != nil
	completion := completionCapabilitiesFromClient(params.Capabilities.TextDocument)
	languageFeatures := languageFeatureCapabilitiesFromClient(params.Capabilities.TextDocument)
	pullDiagnostics := params.Capabilities.TextDocument != nil && params.Capabilities.TextDocument.Diagnostic != nil
	if pullDiagnostics {
		languageFeatures = languageFeatureCapabilitiesFromDiagnostic(params.Capabilities.TextDocument.Diagnostic, languageFeatures)
	}
	diagnosticRefreshSupport := params.Capabilities.Workspace != nil && params.Capabilities.Workspace.Diagnostics != nil && params.Capabilities.Workspace.Diagnostics.RefreshSupport != nil && *params.Capabilities.Workspace.Diagnostics.RefreshSupport
	semanticTokensRefreshSupport := params.Capabilities.Workspace != nil && params.Capabilities.Workspace.SemanticTokens != nil && params.Capabilities.Workspace.SemanticTokens.RefreshSupport != nil && *params.Capabilities.Workspace.SemanticTokens.RefreshSupport
	inlayHintRefreshSupport := params.Capabilities.Workspace != nil && params.Capabilities.Workspace.InlayHint != nil && params.Capabilities.Workspace.InlayHint.RefreshSupport != nil && *params.Capabilities.Workspace.InlayHint.RefreshSupport
	codeLensRefreshSupport := params.Capabilities.Workspace != nil && params.Capabilities.Workspace.CodeLens != nil && params.Capabilities.Workspace.CodeLens.RefreshSupport != nil && *params.Capabilities.Workspace.CodeLens.RefreshSupport
	s.mu.Lock()
	s.pendingWarning = ""
	for _, warning := range []string{runtimepathWarning} {
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
	s.completion = completion
	s.languageFeatures = languageFeatures
	s.state = stateActive
	s.watchDynamicRegistration = watchDynamic
	s.watchRelativePatterns = watchRelative
	s.workspaceConfiguration = workspaceConfiguration
	s.workspaceProgress = workspaceProgress
	s.pullDiagnostics = pullDiagnostics
	s.diagnosticRefreshSupport = diagnosticRefreshSupport
	s.semanticTokensRefreshSupport = semanticTokensRefreshSupport
	s.inlayHintRefreshSupport = inlayHintRefreshSupport
	s.codeLensRefreshSupport = codeLensRefreshSupport
	s.mu.Unlock()
	s.workspaceMu.Lock()
	s.workspaceDelay = defaultWorkspaceRebuildDebounce
	s.workspaceMu.Unlock()
	s.setWorkspaceRoots(workspaceRootsFromInitialize(params))
	s.setRuntimePaths(runtimePaths)
	s.refreshWorkspaceResolver()
	workspaceFoldersSupported := true
	completionResolve := true
	documentLinkResolve := false
	semanticTokensDelta := true
	codeLensResolve := true
	renamePrepare := true
	var renameProvider protocol.RenameProvider = protocol.Boolean(true)
	if prepareRename {
		renameProvider = &protocol.RenameOptions{PrepareProvider: &renamePrepare}
	}
	var codeActionProvider protocol.CodeActionProvider
	if codeActionLiterals {
		codeActionProvider = &protocol.CodeActionOptions{CodeActionKinds: []protocol.CodeActionKind{protocol.CodeActionKindQuickFix}}
	}
	capabilities := protocol.ServerCapabilities{
		PositionEncoding:                protocolEncoding,
		DocumentFormattingProvider:      protocol.Boolean(true),
		DocumentRangeFormattingProvider: protocol.Boolean(true),
		ImplementationProvider:          protocol.Boolean(true),
		CallHierarchyProvider:           protocol.Boolean(true),
		TypeHierarchyProvider:           protocol.Boolean(true),
		DeclarationProvider:             protocol.Boolean(true),
		DefinitionProvider:              protocol.Boolean(true),
		ReferencesProvider:              protocol.Boolean(true),
		DocumentHighlightProvider:       protocol.Boolean(true),
		HoverProvider:                   protocol.Boolean(true),
		DocumentSymbolProvider:          protocol.Boolean(true),
		FoldingRangeProvider:            protocol.Boolean(true),
		SelectionRangeProvider:          protocol.Boolean(true),
		WorkspaceSymbolProvider:         protocol.Boolean(true),
		DocumentLinkProvider:            &protocol.DocumentLinkOptions{ResolveProvider: &documentLinkResolve},
		CompletionProvider:              &protocol.CompletionOptions{ResolveProvider: &completionResolve, TriggerCharacters: []string{".", ":", "&", "#", "<", "\"", "'"}},
		SignatureHelpProvider:           &protocol.SignatureHelpOptions{TriggerCharacters: []string{"(", ","}, RetriggerCharacters: []string{","}},
		RenameProvider:                  renameProvider,
		SemanticTokensProvider: &protocol.SemanticTokensOptions{
			Legend: protocol.SemanticTokensLegend{TokenTypes: append([]string(nil), semanticTokenTypes...), TokenModifiers: append([]string(nil), semanticTokenModifiers...)},
			Full:   &protocol.SemanticTokensFullDelta{Delta: &semanticTokensDelta},
		},
		CodeLensProvider:   &protocol.CodeLensOptions{ResolveProvider: &codeLensResolve},
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
	}
	if pullDiagnostics {
		capabilities.DiagnosticProvider = &protocol.DiagnosticOptions{InterFileDependencies: true, WorkspaceDiagnostics: true}
	}
	return &protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo:   protocol.ServerInfo{Name: Name, Version: protocol.NewOptional(Version)},
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
	s.stopAnalysis()
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	return nil
}

func (s *Server) DidOpen(_ context.Context, params *protocol.DidOpenTextDocumentParams) error {
	document := params.TextDocument
	s.publishMu.Lock()
	snapshot := s.documents.Open(document.URI.String(), document.Version, document.Text)
	var dependents []string
	if snapshot.ByteLen() > maxFileBytes {
		dependents = s.replaceWorkspaceFile(snapshot.URI(), nil)
	} else {
		s.removeWorkspaceURI(snapshot.URI())
		s.initialRefreshPending[snapshot.URI()] = struct{}{}
	}
	delete(s.parsed, snapshot.URI())
	delete(s.semanticTokenResults, snapshot.URI())
	s.publishMu.Unlock()
	s.startAnalysis(document.URI.String())
	s.startWorkspaceDependents(dependents)
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
	snapshot, changed, err := s.documents.Change(params.TextDocument.URI.String(), params.TextDocument.Version, encoding, changes)
	var dependents []string
	if err == nil && changed {
		if snapshot.ByteLen() > maxFileBytes {
			dependents = s.replaceWorkspaceFile(snapshot.URI(), nil)
			delete(s.parsed, snapshot.URI())
		} else {
			s.removeWorkspaceURI(snapshot.URI())
		}
	}
	s.publishMu.Unlock()
	if err != nil {
		s.logf("vimls: ignored content change for %s: %v", params.TextDocument.URI, err)
	} else {
		s.startAnalysis(params.TextDocument.URI.String())
		s.startWorkspaceDependents(dependents)
	}
	return nil
}

func (s *Server) DidSave(_ context.Context, params *protocol.DidSaveTextDocumentParams) error {
	s.publishMu.Lock()
	snapshot, changed, err := s.documents.Save(params.TextDocument.URI.String(), params.Text)
	var dependents []string
	if err == nil && changed {
		if snapshot.ByteLen() > maxFileBytes {
			dependents = s.replaceWorkspaceFile(snapshot.URI(), nil)
			delete(s.parsed, snapshot.URI())
		} else {
			s.removeWorkspaceURI(snapshot.URI())
		}
	}
	s.publishMu.Unlock()
	if err != nil {
		s.logf("vimls: ignored save for %s: %v", params.TextDocument.URI, err)
	} else if changed {
		s.startAnalysis(params.TextDocument.URI.String())
		s.startWorkspaceDependents(dependents)
	}
	return nil
}

func (s *Server) DidClose(_ context.Context, params *protocol.DidCloseTextDocumentParams) error {
	documentURI := params.TextDocument.URI.String()
	s.analysisMu.Lock()
	delete(s.analysisPending, documentURI)
	s.analysisMu.Unlock()
	s.publishMu.Lock()
	closed := s.documents.Close(documentURI)
	delete(s.parsed, documentURI)
	delete(s.pullDiagnosticResults, documentURI)
	delete(s.semanticTokenResults, documentURI)
	delete(s.initialRefreshPending, documentURI)
	clearDiagnostics := s.published[documentURI]
	delete(s.published, documentURI)
	s.publishMu.Unlock()
	if closed {
		s.restoreWorkspaceDocument(documentURI)
	}
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
	// Configuration is a server-to-client request. Release the connection read
	// loop first so it can receive the client's response while this handler waits.
	jsonrpc2.Async(ctx)
	section := "vim"
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
	excludeRuntimePath, excludeRuntimePathWarning := excludeRuntimePathFromSettings(settings, s.excludeRuntimePathCompletions)
	s.excludeRuntimePathCompletions = excludeRuntimePath
	s.mu.Unlock()
	s.workspaceMu.Lock()
	workspaceDelay, workspaceDelayWarning := workspaceRebuildDebounceFromSettings(settings, s.workspaceDelay)
	workspaceDelayChanged := workspaceDelay != s.workspaceDelay
	if workspaceDelayChanged {
		s.workspaceDelay = workspaceDelay
		if s.workspaceRunning {
			select {
			case s.workspaceWake <- struct{}{}:
			default:
			}
		}
	}
	s.workspaceMu.Unlock()
	s.publishMu.Lock()
	s.mu.Lock()
	disabled, overrides, diagnosticsWarning := diagnosticSettingsFromSettings(settings, s.disabledDiagnostics, s.overrideDiagnostics)
	diagnosticsChanged := !maps.Equal(disabled, s.disabledDiagnostics) || !maps.Equal(overrides, s.overrideDiagnostics)
	s.disabledDiagnostics = disabled
	s.overrideDiagnostics = overrides
	s.mu.Unlock()
	var snapshots []*text.Snapshot
	if workspaceDelayChanged || diagnosticsChanged {
		snapshots = s.documents.ConfigurationChanged()
	}
	s.publishMu.Unlock()
	for _, snapshot := range snapshots {
		s.startAnalysis(snapshot.URI())
	}
	warning := ""
	for _, next := range []string{workspaceDelayWarning, diagnosticsWarning, excludeRuntimePathWarning} {
		if next == "" {
			continue
		}
		if warning != "" {
			warning += "; "
		}
		warning += next
	}
	if warning != "" {
		if err := s.sendWarning(ctx, warning); err != nil {
			return err
		}
	}
	if diagnosticsChanged {
		s.scheduleDiagnosticRefresh()
	}
	return nil
}

func (s *Server) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) (protocol.DocumentSymbolResult, error) {
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
	documentURI := params.TextDocument.URI.String()
	s.publishMu.Lock()
	snapshot, ok := s.documents.Snapshot(documentURI)
	s.publishMu.Unlock()
	if !ok || snapshot.ByteLen() > maxFileBytes {
		return protocol.DocumentSymbolSlice{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, protocol.ErrRequestCancelled
	}
	file := s.parseSnapshot(snapshot)
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
	s.mu.Lock()
	shutdown := s.state == stateShutdown
	s.mu.Unlock()
	if shutdown {
		return
	}
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
	file, identity, ok := s.computeDocumentDiagnostics(work)
	if ok && s.publishSyntax(work, file, identity) {
		s.scheduleSemanticTokensRefresh()
		s.scheduleInlayHintRefresh()
		s.scheduleCodeLensRefresh()
	}
}

// computeDocumentDiagnostics is the single raw diagnostic path for background
// publication and document pull requests. It deliberately keeps protocol work
// at the server boundary.
func (s *Server) computeDocumentDiagnostics(work workspace.Analysis) (*syntax.File, workspaceIdentity, bool) {
	disabledDiagnostics := s.disabledDiagnosticsSnapshot()
	var file *syntax.File
	var fileAnalysis *analysis.FileAnalysis
	if work.Snapshot.ByteLen() > maxFileBytes {
		file = &syntax.File{
			Diagnostics: []syntax.Diagnostic{{
				Code: "vimls/file-too-large", Message: "file exceeds the 4 MiB analysis limit",
			}},
		}
	} else {
		raw := s.parseSnapshot(work.Snapshot)
		if raw == nil {
			return nil, workspaceIdentity{}, false
		}
		parsed := *raw
		parsed.Diagnostics = append([]syntax.Diagnostic(nil), raw.Diagnostics...)
		file = &parsed
		if s.beforeAnalyzeForTest != nil {
			s.beforeAnalyzeForTest(file)
		}
		fileAnalysis = analysis.Analyze(file)
		if work.Context.Err() != nil {
			return nil, workspaceIdentity{}, false
		}
	}
	if work.Context.Err() != nil {
		return nil, workspaceIdentity{}, false
	}
	workspaceSnapshot, ok := s.prepareSyntax(work, file, fileAnalysis)
	if !ok {
		return nil, workspaceIdentity{}, false
	}
	return s.composeDocumentDiagnostics(work.Context, work.Snapshot, file, fileAnalysis, workspaceSnapshot, disabledDiagnostics)
}

func (s *Server) composeDocumentDiagnostics(ctx context.Context, snapshot *text.Snapshot, file *syntax.File, fileAnalysis *analysis.FileAnalysis, workspaceSnapshot workspaceAnalysisSnapshot, disabledDiagnostics map[string]struct{}) (*syntax.File, workspaceIdentity, bool) {
	if !workspaceSnapshot.ready || ctx.Err() != nil {
		return nil, workspaceIdentity{}, false
	}
	if snapshot.ByteLen() > maxFileBytes {
		file.Diagnostics = filterDisabledDiagnostics(file.Diagnostics, disabledDiagnostics)
		return file, workspaceSnapshot.identity, true
	}
	versionedAnalysis := *fileAnalysis
	autoload := false
	for _, root := range workspaceSnapshot.roots {
		if _, ok := workspaceAutoloadPath(workspaceSnapshot.path, root); ok {
			autoload = true
			break
		}
	}
	versionedAnalysis.Diagnostics = analysis.AutoloadExportedDefDiagnostics(file, fileAnalysis, autoload, versionedAnalysis.Diagnostics)
	file.Diagnostics = analysis.CombinedDiagnostics(file, &versionedAnalysis)
	file.Diagnostics = append(file.Diagnostics, s.workspaceImportDiagnostics(workspaceSnapshot, file, fileAnalysis)...)
	if workspaceSnapshot.indexComplete {
		file.Diagnostics = append(file.Diagnostics, analysis.UserCommandAbbreviationDiagnostics(file, workspaceSnapshot.userCommandNames)...)
	}
	file.Diagnostics = append(file.Diagnostics, workspaceSnapshot.globalDiagnostics...)
	file.Diagnostics = filterDisabledDiagnostics(file.Diagnostics, disabledDiagnostics)
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
	return file, workspaceSnapshot.identity, true
}

func (s *Server) prepareSyntax(work workspace.Analysis, file *syntax.File, result *analysis.FileAnalysis) (workspaceAnalysisSnapshot, bool) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if s.analysisContext.Err() != nil || !s.documents.IsCurrent(work) {
		return workspaceAnalysisSnapshot{}, false
	}
	documentURI := work.Snapshot.URI()
	if work.Snapshot.ByteLen() > maxFileBytes {
		file = nil
		result = nil
	}
	workspaceSnapshot, dependents := s.replaceWorkspaceFileWithAnalysisSnapshot(documentURI, file, result)
	s.startWorkspaceDependents(dependents)
	return workspaceSnapshot, true
}

// parseSnapshot returns the immutable syntax tree for an open snapshot. Its
// cache contains parser output only; callers that add diagnostics must work on
// a separate File header and diagnostics slice.
func (s *Server) parseSnapshot(snapshot *text.Snapshot) *syntax.File {
	if snapshot == nil || snapshot.ByteLen() > maxFileBytes {
		return nil
	}
	documentURI := snapshot.URI()
	contentID := snapshot.ContentID()
	source := snapshot.Text()
	s.publishMu.Lock()
	parsed := s.parsed[documentURI]
	s.publishMu.Unlock()
	if parsed.file != nil && parsed.contentID == contentID && parsed.file.Source == source {
		return parsed.file
	}
	if s.beforeParseSnapshotCacheMissForTest != nil {
		s.beforeParseSnapshotCacheMissForTest(snapshot)
	}

	file := syntax.Parse(source)
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if s.analysisContext.Err() != nil {
		return file
	}
	current, ok := s.documents.Snapshot(documentURI)
	if !ok || current != snapshot {
		return file
	}
	parsed = s.parsed[documentURI]
	if parsed.file != nil && parsed.contentID == contentID && parsed.file.Source == source {
		return parsed.file
	}
	s.parsed[documentURI] = parsedDocument{contentID: contentID, file: file}
	return file
}

func (s *Server) publishSyntax(analysis workspace.Analysis, file *syntax.File, identity workspaceIdentity) bool {
	s.mu.Lock()
	encoding := s.encoding
	client := s.client
	diagnosticRelatedInformation := s.languageFeatures.diagnosticRelatedInformation
	overrides := s.overrideDiagnostics
	pullDiagnostics := s.pullDiagnostics
	s.mu.Unlock()

	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if s.analysisContext.Err() != nil || !s.documents.IsCurrent(analysis) {
		return false
	}
	documentURI := analysis.Snapshot.URI()
	s.workspaceMu.Lock()
	if !s.workspaceIdentityCurrentLocked(identity) {
		s.workspaceMu.Unlock()
		s.startAnalysis(documentURI)
		return false
	}
	s.workspaceMu.Unlock()
	_, initialRefresh := s.initialRefreshPending[documentURI]
	delete(s.initialRefreshPending, documentURI)
	initialRefresh = initialRefresh && analysis.Snapshot.ByteLen() <= maxFileBytes
	diagnostics := protocolDiagnostics(analysis.Snapshot, file, encoding, diagnosticRelatedInformation, overrides)
	if pullDiagnostics {
		s.installPullDiagnosticResultLocked(analysis, identity, diagnostics)
		return initialRefresh
	}
	if len(diagnostics) == 0 && !s.published[documentURI] {
		return initialRefresh
	}
	if len(diagnostics) == 0 {
		delete(s.published, documentURI)
	} else {
		s.published[documentURI] = true
	}
	if client == nil {
		return initialRefresh
	}
	params := &protocol.PublishDiagnosticsParams{URI: uri.URI(documentURI), Diagnostics: diagnostics}
	if version, ok := analysis.Snapshot.Version(); ok {
		params.Version = protocol.NewOptional(version)
	}
	if err := client.PublishDiagnostics(analysis.Context, params); err != nil && analysis.Context.Err() == nil {
		s.logf("vimls: publish diagnostics for %s: %v", documentURI, err)
	}
	return initialRefresh
}

func protocolDiagnostics(snapshot *text.Snapshot, file *syntax.File, encoding text.Encoding, diagnosticRelatedInformation bool, overrides map[string]protocol.DiagnosticSeverity) []protocol.Diagnostic {
	diagnostics := make([]protocol.Diagnostic, 0, len(file.Diagnostics))
	var relatedSnapshots map[string]*text.Snapshot
	for _, item := range file.Diagnostics {
		start, startError := snapshot.Position(item.Span.Start, encoding)
		end, endError := snapshot.Position(item.Span.End, encoding)
		if startError != nil || endError != nil {
			continue
		}
		diagnostic := protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: uint32(start.Line), Character: uint32(start.Character)},
				End:   protocol.Position{Line: uint32(end.Line), Character: uint32(end.Character)},
			},
			Severity: protocolDiagnosticSeverity(item.Code),
			Code:     protocol.String(item.Code),
			Source:   protocol.NewOptional(Name),
			Message:  protocol.String(item.Message),
		}
		if severity, ok := overrides[item.Code]; ok {
			diagnostic.Severity = severity
		}
		switch item.Code {
		case "vimls/deprecated":
			diagnostic.Tags = protocol.NewDiagnosticTags(protocol.DiagnosticTagDeprecated)
		case "vimls/unused-variable":
			diagnostic.Tags = protocol.NewDiagnosticTags(protocol.DiagnosticTagUnnecessary)
		}
		if diagnosticRelatedInformation && item.Related.URI != "" {
			if relatedSnapshots == nil {
				relatedSnapshots = make(map[string]*text.Snapshot)
			}
			relatedSnapshot := relatedSnapshots[item.Related.URI]
			if relatedSnapshot == nil {
				relatedSnapshot = text.NewSnapshot(item.Related.URI, 0, nil, item.Related.Source)
				relatedSnapshots[item.Related.URI] = relatedSnapshot
			}
			relatedStart, startError := relatedSnapshot.Position(item.Related.Span.Start, encoding)
			relatedEnd, endError := relatedSnapshot.Position(item.Related.Span.End, encoding)
			if startError == nil && endError == nil {
				diagnostic.RelatedInformation = []protocol.DiagnosticRelatedInformation{{
					Location: protocol.Location{URI: uri.URI(item.Related.URI), Range: protocol.Range{
						Start: protocol.Position{Line: uint32(relatedStart.Line), Character: uint32(relatedStart.Character)},
						End:   protocol.Position{Line: uint32(relatedEnd.Line), Character: uint32(relatedEnd.Character)},
					}},
					Message: item.Related.Message,
				}}
			}
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	return diagnostics
}

func (s *Server) disabledDiagnosticsSnapshot() map[string]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.disabledDiagnostics
}

func filterDisabledDiagnostics(diagnostics []syntax.Diagnostic, disabled map[string]struct{}) []syntax.Diagnostic {
	if len(disabled) == 0 {
		return diagnostics
	}
	filtered := diagnostics[:0]
	for _, diagnostic := range diagnostics {
		if _, ok := disabled[diagnostic.Code]; !ok {
			filtered = append(filtered, diagnostic)
		}
	}
	return filtered
}

func protocolDiagnosticSeverity(code string) protocol.DiagnosticSeverity {
	switch code {
	case "vim/E122", "vim/E174", "vim/E464", "vim/E705", "vim/E707":
		return protocol.DiagnosticSeverityWarning
	case "vim/E117", "vim/E121", "vim/E1001", "vim/E1089":
		return protocol.DiagnosticSeverityWarning
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
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if s.analysisContext.Err() != nil {
		return
	}
	s.mu.Lock()
	client := s.client
	pullDiagnostics := s.pullDiagnostics
	s.mu.Unlock()
	if client != nil && !pullDiagnostics {
		if err := client.PublishDiagnostics(s.analysisContext, &protocol.PublishDiagnosticsParams{URI: uri.URI(documentURI), Diagnostics: []protocol.Diagnostic{}}); err != nil {
			s.logf("vimls: clear diagnostics for %s: %v", documentURI, err)
		}
	}
}

func (s *Server) cancelAnalysis() {
	s.workspaceMu.Lock()
	s.analysisMu.Lock()
	if !s.analysisStopped {
		s.analysisStopped = true
		s.analysisCancel()
	}
	clear(s.analysisPending)
	s.analysisMu.Unlock()
	s.workspaceMu.Unlock()
}

func (s *Server) stopAnalysis() {
	s.stopOnce.Do(func() {
		s.cancelAnalysis()
		s.analysisWG.Wait()
		// Synchronize with a rebuild that may have checked analysisContext just
		// before cancellation, so its WaitGroup.Add completes before Wait starts.
		waitGroupAddBarrier(&s.workspaceMu)
		s.workspaceWG.Wait()
		waitGroupAddBarrier(&s.watchMu)
		s.watchWG.Wait()
	})
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
