package server

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
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
	"github.com/neoclide/vimls-go/internal/vimhelp"
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
	maxDiagnosticsPerDocument   = 1000
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
	contentID  text.ContentID
	configFile bool
	file       *syntax.File
	analysis   *analysis.FileAnalysis
}

type parseInFlightKey struct {
	uri        string
	contentID  text.ContentID
	configFile bool
}

type inFlightParse struct {
	source   string
	done     chan struct{}
	file     *syntax.File
	analysis *analysis.FileAnalysis
}

type publishedDiagnosticsState struct {
	hasDiagnostics bool
	hash           [32]byte
	hasHash        bool
	mustPublish    bool
	publishSeq     uint64
	lastVersion    int32
	hasLastVersion bool
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
	identity                 workspaceIdentity
	path                     string
	graph                    workspace.ImportGraphSnapshot
	targets                  map[string]importTargetSnapshot
	roots                    []string
	indexComplete            bool
	missingAutoloadFunctions map[string]bool
	missingGlobalFunctions   map[string]bool
	userCommandNames         []string
	augroupNames             []string
	globalDiagnostics        []syntax.Diagnostic
	ready                    bool
}

// serverTestHooks are test-only seams. Tests install them before starting the
// relevant work. Scheduling hooks run outside Server locks.
type serverTestHooks struct {
	afterAnalysisFinished        func(string)
	beforeParseSnapshotCacheMiss func(*text.Snapshot)
	beforeInFlightWait           func(*text.Snapshot)
	beforeAnalyze                func(*syntax.File)

	beforeWorkspaceWGWait       func()
	beforeWorkspaceRestoreRead  func(workspaceRestore)
	beforeWorkspaceRebuildDelay func()
	beforeWorkspaceIndexWait    func()
	workspaceIndexWaitTimeout   time.Duration
	workspaceProgressTimeout    time.Duration
	beforeWorkspaceBuild        func([]*text.Snapshot)
	afterWorkspaceIndexWorker   func()
	beforeShutdownReturn        func()

	beforeWatchedFileProcess func(string)
	beforeWatchedFileRead    func(string)
	beforeWatchedFileInstall func(string)
	beforeRuntimeHelpRead    func(context.Context, string)
	beforeRuntimeHelpParse   func(string)

	beforeWorkspaceIdentityCheck func()
	discoverWorkspaceFiles       func(context.Context, string, int) ([]string, bool, error)
	// replaceWorkspaceGraph runs while workspaceMu is held and must not reenter Server.
	replaceWorkspaceGraph func(*workspace.ImportGraph, string, []workspace.ImportFact) error
}

// Server mutexes use a partial, not total, lock order. When multiple Server
// mutexes are held, acquire them only in these directions:
//
//	runtimepathRunMu -> publishMu
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
	testHooks                   serverTestHooks
	diagnosticsSendMu           sync.Mutex
	publishMu                   sync.Mutex
	parsed                      map[string]parsedDocument
	parseInFlight               map[parseInFlightKey]*inFlightParse
	published                   map[string]publishedDiagnosticsState
	pullDiagnosticResults       map[string]pullDiagnosticResult
	workspaceDiagnosticReported map[string]string // publishMu; retained until removal is acknowledged
	documentChangesSupport      bool
	hierarchicalSymbolsSupport  bool
	nextDiagnosticResultID      uint64
	semanticTokenResults        map[string]semanticTokenResult
	nextSemanticTokenResultID   uint64
	initialRefreshPending       map[string]struct{}
	diagnosticRefreshGeneration uint64
	diagnosticRefreshRunning    bool
	workspaceMu                 sync.Mutex
	configurationMu             sync.Mutex
	configurationGeneration     uint64
	workspaceRoots              []string
	runtimePaths                []string
	runtimepathGeneration       uint64
	runtimepathRunMu            sync.Mutex // serializes runtime batches and workspace installation
	runtimepathIndexedPaths     []string
	runtimepathWorkspaceRoots   []string
	runtimepathWG               sync.WaitGroup
	runtimeHelp                 map[string]vimhelp.SymbolDocumentation
	runtimeHelpRoots            map[string][]string
	runtimeHelpFiles            map[string][]vimhelp.SymbolDocumentation
	runtimeHelpRunning          bool
	runtimeHelpRoot             string
	runtimeHelpCancel           context.CancelFunc
	runtimeHelpWG               sync.WaitGroup
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

	watchMu                       sync.Mutex
	watchDynamicRegistration      bool
	watchRelativePatterns         bool
	watchRegistered               bool
	initialized                   bool
	watchWG                       sync.WaitGroup
	watchedFilesRunning           bool
	watchedFilesDirty             bool
	workspaceConfiguration        bool
	excludeRuntimePathCompletions bool
	configFiles                   []string
	// Diagnostic maps are replaced, never mutated, while mu is held. Analysis
	// and publication may therefore snapshot their immutable map references.
	disabledDiagnostics map[string]struct{}
	overrideDiagnostics map[string]protocol.DiagnosticSeverity
	diagnosticMaxNumber int

	completionNow func() time.Time
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
		parseInFlight:         make(map[parseInFlightKey]*inFlightParse),
		published:             make(map[string]publishedDiagnosticsState),
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
		diagnosticMaxNumber:   maxDiagnosticsPerDocument,
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
	handler := s.lifecycleHandler(s.cancellationHandler(s.runtimepathHandler(protocol.ServerHandler(s, jsonrpc2.MethodNotFoundHandler))))
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
			if request.Method() == protocol.MethodWorkspaceDidChangeWatchedFiles {
				jsonrpc2.Async(ctx)
			}
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
		if request.Method() != protocol.MethodInitialize && request.Method() != protocol.MethodShutdown && request.Method() != MethodDidChangeRuntimepath {
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
		protocol.MethodTextDocumentTypeDefinition,
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
		protocol.MethodTextDocumentSemanticTokensRange,
		protocol.MethodTextDocumentCodeLens,
		protocol.MethodCodeLensResolve,
		protocol.MethodTextDocumentCodeAction,
		protocol.MethodTextDocumentInlayHint,
		protocol.MethodTextDocumentFormatting,
		protocol.MethodTextDocumentRangeFormatting,
		protocol.MethodTextDocumentRangesFormatting,
		protocol.MethodTextDocumentOnTypeFormatting,
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

func (s *Server) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	encoding, protocolEncoding := negotiatePositionEncoding(params.Capabilities.General)
	openClose := true
	includeText := true
	changeKind := protocol.TextDocumentSyncKindIncremental
	runtimePaths, _, runtimepathWarning := runtimepathFromOptions([]byte(params.InitializationOptions))
	runtimePaths = usableRuntimePaths(runtimePaths)
	if len(runtimePaths) == 0 {
		runtimePaths = defaultRuntimePaths(ctx)
	}
	configFiles, _, configFilesWarning := configFilesFromOptions([]byte(params.InitializationOptions))
	watchDynamic, watchRelative := watchedFilesCapabilities(params.Capabilities.Workspace)
	workspaceConfiguration := params.Capabilities.Workspace != nil && params.Capabilities.Workspace.Configuration != nil && *params.Capabilities.Workspace.Configuration
	workspaceProgress := params.Capabilities.Window != nil && params.Capabilities.Window.WorkDoneProgress != nil && *params.Capabilities.Window.WorkDoneProgress
	prepareRename := params.Capabilities.TextDocument != nil && params.Capabilities.TextDocument.Rename != nil && params.Capabilities.TextDocument.Rename.PrepareSupport != nil && *params.Capabilities.TextDocument.Rename.PrepareSupport
	rangeFormattingRanges := params.Capabilities.TextDocument != nil && params.Capabilities.TextDocument.RangeFormatting != nil && params.Capabilities.TextDocument.RangeFormatting.RangesSupport != nil && *params.Capabilities.TextDocument.RangeFormatting.RangesSupport
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
	for _, warning := range []string{runtimepathWarning, configFilesWarning} {
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
	s.configFiles = configFiles
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
	s.documentChangesSupport = params.Capabilities.Workspace != nil && params.Capabilities.Workspace.WorkspaceEdit != nil && params.Capabilities.Workspace.WorkspaceEdit.DocumentChanges != nil && *params.Capabilities.Workspace.WorkspaceEdit.DocumentChanges
	s.hierarchicalSymbolsSupport = params.Capabilities.TextDocument != nil && params.Capabilities.TextDocument.DocumentSymbol != nil && params.Capabilities.TextDocument.DocumentSymbol.HierarchicalDocumentSymbolSupport != nil && *params.Capabilities.TextDocument.DocumentSymbol.HierarchicalDocumentSymbolSupport
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
	var documentRangeFormattingProvider protocol.DocumentRangeFormattingProvider = protocol.Boolean(true)
	if rangeFormattingRanges {
		rangesFormattingRangesSupport := true
		documentRangeFormattingProvider = &protocol.DocumentRangeFormattingOptions{RangesSupport: &rangesFormattingRangesSupport}
	}
	var codeActionProvider protocol.CodeActionProvider
	if codeActionLiterals {
		codeActionProvider = &protocol.CodeActionOptions{CodeActionKinds: []protocol.CodeActionKind{protocol.CodeActionKindQuickFix}}
	}
	capabilities := protocol.ServerCapabilities{
		PositionEncoding:                protocolEncoding,
		DocumentFormattingProvider:      protocol.Boolean(true),
		DocumentRangeFormattingProvider: documentRangeFormattingProvider,
		DocumentOnTypeFormattingProvider: protocol.DocumentOnTypeFormattingOptions{
			FirstTriggerCharacter: "\n",
			MoreTriggerCharacter:  []string{"\\"},
		},
		ImplementationProvider:    protocol.Boolean(true),
		CallHierarchyProvider:     protocol.Boolean(true),
		TypeHierarchyProvider:     protocol.Boolean(true),
		DeclarationProvider:       protocol.Boolean(true),
		DefinitionProvider:        protocol.Boolean(true),
		TypeDefinitionProvider:    protocol.Boolean(true),
		ReferencesProvider:        protocol.Boolean(true),
		DocumentHighlightProvider: protocol.Boolean(true),
		HoverProvider:             protocol.Boolean(true),
		DocumentSymbolProvider:    protocol.Boolean(true),
		FoldingRangeProvider:      protocol.Boolean(true),
		SelectionRangeProvider:    protocol.Boolean(true),
		WorkspaceSymbolProvider:   protocol.Boolean(true),
		DocumentLinkProvider:      &protocol.DocumentLinkOptions{ResolveProvider: &documentLinkResolve},
		CompletionProvider:        &protocol.CompletionOptions{ResolveProvider: &completionResolve, TriggerCharacters: []string{".", ":", "&", "#", "<", "+", "\"", "'", "-"}},
		SignatureHelpProvider:     &protocol.SignatureHelpOptions{TriggerCharacters: []string{"(", ","}, RetriggerCharacters: []string{","}},
		RenameProvider:            renameProvider,
		SemanticTokensProvider: &protocol.SemanticTokensOptions{
			Legend: protocol.SemanticTokensLegend{TokenTypes: append([]string(nil), semanticTokenTypes...), TokenModifiers: append([]string(nil), semanticTokenModifiers...)},
			Range:  protocol.Boolean(true),
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
	hook := s.testHooks.beforeShutdownReturn
	s.publishMu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (s *Server) DidOpen(_ context.Context, params *protocol.DidOpenTextDocumentParams) error {
	document := params.TextDocument
	s.publishMu.Lock()
	snapshot := s.documents.Open(document.URI.String(), document.Version, document.Text)
	st := s.published[snapshot.URI()]
	st.mustPublish = true
	st.publishSeq++
	s.published[snapshot.URI()] = st
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
	if err == nil {
		st := s.published[params.TextDocument.URI.String()]
		st.mustPublish = true
		st.publishSeq++
		s.published[params.TextDocument.URI.String()] = st
	}
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
	if err == nil {
		st := s.published[params.TextDocument.URI.String()]
		st.mustPublish = true
		st.publishSeq++
		s.published[params.TextDocument.URI.String()] = st
	}
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
	delete(s.published, documentURI)
	s.publishMu.Unlock()
	if closed {
		s.restoreWorkspaceDocument(documentURI)
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
	s.configurationMu.Lock()
	s.configurationGeneration++
	generation := s.configurationGeneration
	s.configurationMu.Unlock()
	jsonrpc2.Async(ctx)
	section := "vim"
	values, err := client.Configuration(ctx, &protocol.ConfigurationParams{Items: []protocol.ConfigurationItem{{Section: &section}}})
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	return s.applyWorkspaceConfigurationGeneration(ctx, []byte(values[0]), generation)
}

func (s *Server) applyWorkspaceConfiguration(ctx context.Context, settings []byte) error {
	return s.applyWorkspaceConfigurationGeneration(ctx, settings, 0)
}

func (s *Server) applyWorkspaceConfigurationGeneration(ctx context.Context, settings []byte, generation uint64) error {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	if ctx.Err() != nil || s.analysisContext.Err() != nil || generation != 0 && generation != s.configurationGeneration {
		return nil
	}
	if generation == 0 {
		s.configurationGeneration++
	}
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
	disabled, overrides, maxNumber, diagnosticsWarning := diagnosticSettingsFromSettings(settings, s.disabledDiagnostics, s.overrideDiagnostics, s.diagnosticMaxNumber)
	diagnosticsChanged := !maps.Equal(disabled, s.disabledDiagnostics) || !maps.Equal(overrides, s.overrideDiagnostics) || maxNumber != s.diagnosticMaxNumber
	s.disabledDiagnostics = disabled
	s.overrideDiagnostics = overrides
	s.diagnosticMaxNumber = maxNumber
	s.mu.Unlock()
	var snapshots []*text.Snapshot
	if workspaceDelayChanged || diagnosticsChanged {
		if diagnosticsChanged {
			for uriKey, st := range s.published {
				st.mustPublish = true
				st.publishSeq++
				s.published[uriKey] = st
			}
		}
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

// IsConfigFile reports whether path represents a Vim configuration file.
func (s *Server) IsConfigFile(path string) bool {
	s.mu.Lock()
	patterns := append([]string(nil), s.configFiles...)
	s.mu.Unlock()
	s.workspaceMu.Lock()
	roots := append([]string(nil), s.workspaceRoots...)
	runtimeRoots := append([]string(nil), s.runtimePaths...)
	s.workspaceMu.Unlock()
	return workspace.IsConfigFile(path, patterns, roots, runtimeRoots)
}

func (s *Server) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) (protocol.DocumentSymbolResult, error) {
	s.mu.Lock()
	encoding, hierarchical := s.encoding, s.hierarchicalSymbolsSupport
	s.mu.Unlock()
	documentURI := params.TextDocument.URI.String()
	s.publishMu.Lock()
	snapshot, ok := s.documents.Snapshot(documentURI)
	s.publishMu.Unlock()
	if !ok || snapshot.ByteLen() > maxFileBytes {
		if !hierarchical {
			return protocol.SymbolInformationSlice{}, nil
		}
		return protocol.DocumentSymbolSlice{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, protocol.ErrRequestCancelled
	}
	file := s.parseSnapshot(snapshot)
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
	if !hierarchical {
		flat := make(protocol.SymbolInformationSlice, 0, len(result))
		var appendSymbols func([]protocol.DocumentSymbol, *string)
		appendSymbols = func(symbols []protocol.DocumentSymbol, container *string) {
			for _, symbol := range symbols {
				flat = append(flat, protocol.SymbolInformation{
					BaseSymbolInformation: protocol.BaseSymbolInformation{Name: symbol.Name, Kind: symbol.Kind, Tags: symbol.Tags, ContainerName: container},
					Location:              protocol.Location{URI: params.TextDocument.URI, Range: symbol.Range},
				})
				appendSymbols(symbol.Children, &symbol.Name)
			}
		}
		appendSymbols(result, nil)
		return flat, nil
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
	hook := s.testHooks.afterAnalysisFinished
	s.analysisMu.Unlock()
	if hook != nil {
		hook(documentURI)
	}
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
		raw, rawAnalysis := s.analyzeSnapshotContext(work.Context, work.Snapshot)
		if raw == nil || rawAnalysis == nil {
			return nil, workspaceIdentity{}, false
		}
		parsed := *raw
		parsed.Diagnostics = append([]syntax.Diagnostic(nil), raw.Diagnostics...)
		file = &parsed
		clonedAnalysis := *rawAnalysis
		clonedAnalysis.File = file
		fileAnalysis = &clonedAnalysis
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
	if ctx.Err() != nil {
		return nil, workspaceIdentity{}, false
	}
	// Workspace-dependent diagnostics are omitted when the captured index is
	// not ready. The local result remains useful, and index installation asks
	// capable pull clients to refresh it.
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
	file.Diagnostics = analysis.SuppressKnownAugroupEventDiagnostics(
		file,
		analysis.CombinedDiagnostics(file, &versionedAnalysis),
		workspaceSnapshot.augroupNames,
	)
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
	file, _ := s.analyzeSnapshot(snapshot)
	return file
}

// analyzeSnapshot returns the cached or parsed syntax tree and pure file analysis
// for an open snapshot.
func (s *Server) analyzeSnapshot(snapshot *text.Snapshot) (*syntax.File, *analysis.FileAnalysis) {
	return s.analyzeSnapshotContext(context.Background(), snapshot)
}

// configFileRoleForURI reports whether the document behind documentURI is a
// user configuration file. The decision is made at the analysis boundary from
// the document path; it never changes the AST.
func (s *Server) configFileRoleForURI(documentURI string) bool {
	path, ok := workspaceURIPath(uri.URI(documentURI))
	if !ok {
		return false
	}
	return s.IsConfigFile(path)
}

// analyzeWithRole returns the file-local analysis for one parsed file in the
// configuration-file mode selected for the document.
func analyzeWithRole(file *syntax.File, configFile bool) *analysis.FileAnalysis {
	if configFile {
		return analysis.AnalyzeConfigFile(file)
	}
	return analysis.Analyze(file)
}

// analyzeSnapshotContext returns the syntax tree and pure file analysis for snapshot,
// waiting for in-flight calculations or initiating a single parse+analysis pass.
// If ctx is cancelled while waiting on an in-flight calculation, it yields without
// disturbing the in-flight work.
func (s *Server) analyzeSnapshotContext(ctx context.Context, snapshot *text.Snapshot) (*syntax.File, *analysis.FileAnalysis) {
	if snapshot == nil || snapshot.ByteLen() > maxFileBytes {
		return nil, nil
	}
	documentURI := snapshot.URI()
	contentID := snapshot.ContentID()
	source := snapshot.Text()
	configFile := s.configFileRoleForURI(documentURI)

	s.publishMu.Lock()
	parsed := s.parsed[documentURI]
	if parsed.file != nil && parsed.contentID == contentID && parsed.configFile == configFile && parsed.file.Source == source {
		if parsed.analysis != nil {
			s.publishMu.Unlock()
			return parsed.file, parsed.analysis
		}
		file := parsed.file
		s.publishMu.Unlock()
		if hook := s.testHooks.beforeAnalyze; hook != nil {
			hook(file)
		}
		fileAnalysis := analyzeWithRole(file, configFile)
		s.publishMu.Lock()
		if cur := s.parsed[documentURI]; cur.file == file && cur.contentID == contentID && cur.configFile == configFile {
			cur.analysis = fileAnalysis
			s.parsed[documentURI] = cur
		}
		s.publishMu.Unlock()
		return file, fileAnalysis
	}

	key := parseInFlightKey{
		uri:        documentURI,
		contentID:  contentID,
		configFile: configFile,
	}

	for {
		if ctx.Err() != nil {
			s.publishMu.Unlock()
			return nil, nil
		}
		inFlight := s.parseInFlight[key]
		if inFlight != nil && inFlight.source == source {
			done := inFlight.done
			hook := s.testHooks.beforeInFlightWait
			s.publishMu.Unlock()
			if hook != nil {
				hook(snapshot)
			}
			select {
			case <-ctx.Done():
				return nil, nil
			case <-done:
			}
			s.publishMu.Lock()
			parsed = s.parsed[documentURI]
			if parsed.file != nil && parsed.contentID == contentID && parsed.configFile == configFile && parsed.file.Source == source {
				if parsed.analysis != nil {
					s.publishMu.Unlock()
					return parsed.file, parsed.analysis
				}
				file := parsed.file
				s.publishMu.Unlock()
				if hook := s.testHooks.beforeAnalyze; hook != nil {
					hook(file)
				}
				fileAnalysis := analyzeWithRole(file, configFile)
				s.publishMu.Lock()
				if cur := s.parsed[documentURI]; cur.file == file && cur.contentID == contentID && cur.configFile == configFile {
					cur.analysis = fileAnalysis
					s.parsed[documentURI] = cur
				}
				s.publishMu.Unlock()
				return file, fileAnalysis
			}
			if inFlight.file != nil && inFlight.analysis != nil {
				s.publishMu.Unlock()
				return inFlight.file, inFlight.analysis
			}
			continue
		}

		entry := &inFlightParse{
			source: source,
			done:   make(chan struct{}),
		}
		s.parseInFlight[key] = entry
		s.publishMu.Unlock()

		if hook := s.testHooks.beforeParseSnapshotCacheMiss; hook != nil {
			hook(snapshot)
		}
		file := syntax.Parse(source)
		if hook := s.testHooks.beforeAnalyze; hook != nil {
			hook(file)
		}
		fileAnalysis := analyzeWithRole(file, configFile)

		entry.file = file
		entry.analysis = fileAnalysis

		s.publishMu.Lock()
		if s.parseInFlight[key] == entry {
			delete(s.parseInFlight, key)
		}
		close(entry.done)

		if s.analysisContext.Err() == nil {
			current, ok := s.documents.Snapshot(documentURI)
			if ok && current == snapshot {
				s.parsed[documentURI] = parsedDocument{
					contentID:  contentID,
					configFile: configFile,
					file:       file,
					analysis:   fileAnalysis,
				}
			}
		}
		s.publishMu.Unlock()
		return file, fileAnalysis
	}
}

func (s *Server) publishSyntax(analysis workspace.Analysis, file *syntax.File, identity workspaceIdentity) bool {
	s.mu.Lock()
	encoding := s.encoding
	client := s.client
	diagnosticRelatedInformation := s.languageFeatures.diagnosticRelatedInformation
	overrides := s.overrideDiagnostics
	maxNumber := s.diagnosticMaxNumber
	pullDiagnostics := s.pullDiagnostics
	s.mu.Unlock()

	s.publishMu.Lock()
	if s.analysisContext.Err() != nil || !s.documents.IsCurrent(analysis) {
		s.publishMu.Unlock()
		return false
	}
	documentURI := analysis.Snapshot.URI()
	s.workspaceMu.Lock()
	if !s.workspaceIdentityCurrentLocked(identity) {
		s.workspaceMu.Unlock()
		s.publishMu.Unlock()
		s.startAnalysis(documentURI)
		return false
	}
	s.workspaceMu.Unlock()
	_, initialRefresh := s.initialRefreshPending[documentURI]
	initialRefresh = initialRefresh && analysis.Snapshot.ByteLen() <= maxFileBytes
	diagnostics := protocolDiagnostics(analysis.Snapshot, file, encoding, diagnosticRelatedInformation, overrides, maxNumber)
	if pullDiagnostics {
		s.installPullDiagnosticResultLocked(analysis, identity, diagnostics)
		delete(s.initialRefreshPending, documentURI)
		s.publishMu.Unlock()
		return initialRefresh
	}
	pubState := s.published[documentURI]
	curVersion, hasVersion := analysis.Snapshot.Version()
	diagHash := hashProtocolDiagnostics(diagnostics)

	if len(diagnostics) == 0 {
		if !pubState.hasDiagnostics {
			delete(s.initialRefreshPending, documentURI)
			s.publishMu.Unlock()
			return initialRefresh
		}
	} else {
		unchanged := pubState.hasDiagnostics &&
			!pubState.mustPublish &&
			pubState.hasHash &&
			pubState.hash == diagHash &&
			(!hasVersion || (pubState.hasLastVersion && pubState.lastVersion == curVersion))
		if unchanged {
			delete(s.initialRefreshPending, documentURI)
			s.publishMu.Unlock()
			return initialRefresh
		}
	}
	if client == nil {
		s.publishMu.Unlock()
		return false
	}
	startSeq := pubState.publishSeq
	params := &protocol.PublishDiagnosticsParams{URI: uri.URI(documentURI), Diagnostics: diagnostics}
	if version, ok := analysis.Snapshot.Version(); ok {
		params.Version = protocol.NewOptional(version)
	}
	s.publishMu.Unlock()

	s.diagnosticsSendMu.Lock()
	defer s.diagnosticsSendMu.Unlock()

	s.publishMu.Lock()
	if s.analysisContext.Err() != nil || !s.documents.IsCurrent(analysis) {
		s.publishMu.Unlock()
		return false
	}
	s.workspaceMu.Lock()
	workspaceCurrent := s.workspaceIdentityCurrentLocked(identity)
	s.workspaceMu.Unlock()
	if !workspaceCurrent {
		s.publishMu.Unlock()
		s.startAnalysis(documentURI)
		return false
	}
	s.publishMu.Unlock()

	if err := client.PublishDiagnostics(analysis.Context, params); err != nil {
		if analysis.Context.Err() == nil {
			s.logf("vimls: publish diagnostics for %s: %v", documentURI, err)
		}
		return false
	}

	s.publishMu.Lock()
	defer s.publishMu.Unlock()

	if s.analysisContext.Err() != nil {
		return false
	}
	_, documentOpen := s.documents.Snapshot(documentURI)
	analysisCurrent := s.documents.IsCurrent(analysis)
	s.workspaceMu.Lock()
	workspaceCurrent = s.workspaceIdentityCurrentLocked(identity)
	s.workspaceMu.Unlock()
	if !documentOpen {
		return false
	}

	currentPubState := s.published[documentURI]
	if hasVersion && currentPubState.hasLastVersion && currentPubState.lastVersion > curVersion {
		return false
	}

	newMustPublish := currentPubState.mustPublish && (currentPubState.publishSeq != startSeq)

	if len(diagnostics) == 0 {
		s.published[documentURI] = publishedDiagnosticsState{
			hasDiagnostics: false,
			mustPublish:    newMustPublish,
			publishSeq:     currentPubState.publishSeq,
			hasLastVersion: hasVersion,
			lastVersion:    curVersion,
		}
	} else {
		s.published[documentURI] = publishedDiagnosticsState{
			hasDiagnostics: true,
			hasHash:        true,
			hash:           diagHash,
			mustPublish:    newMustPublish,
			publishSeq:     currentPubState.publishSeq,
			hasLastVersion: hasVersion,
			lastVersion:    curVersion,
		}
	}
	// A successful send is client-visible even when an edit raced with it.
	// Record that send, then analyze the current state so it can be corrected.
	if !analysisCurrent || !workspaceCurrent {
		s.startAnalysis(documentURI)
		return false
	}
	delete(s.initialRefreshPending, documentURI)
	return initialRefresh
}

func protocolDiagnostics(snapshot *text.Snapshot, file *syntax.File, encoding text.Encoding, diagnosticRelatedInformation bool, overrides map[string]protocol.DiagnosticSeverity, maxNumber int) []protocol.Diagnostic {
	items := file.Diagnostics
	if len(items) > maxNumber {
		items = append([]syntax.Diagnostic(nil), items...)
		sort.Slice(items, func(left, right int) bool {
			leftSeverity := diagnosticProtocolSeverity(items[left])
			if severity, ok := overrides[items[left].Code]; ok {
				leftSeverity = severity
			}
			rightSeverity := diagnosticProtocolSeverity(items[right])
			if severity, ok := overrides[items[right].Code]; ok {
				rightSeverity = severity
			}
			if leftSeverity != rightSeverity {
				return leftSeverity < rightSeverity
			}
			if items[left].Span.Start != items[right].Span.Start {
				return items[left].Span.Start < items[right].Span.Start
			}
			if items[left].Span.End != items[right].Span.End {
				return items[left].Span.End < items[right].Span.End
			}
			if items[left].Code != items[right].Code {
				return items[left].Code < items[right].Code
			}
			return items[left].Message < items[right].Message
		})
		items = append(items[:maxNumber-1], syntax.Diagnostic{
			Code: "vimls/diagnostics-truncated", Message: "additional diagnostics were omitted",
			Span: syntax.Span{Start: snapshot.ByteLen(), End: snapshot.ByteLen()},
		})
	}
	diagnostics := make([]protocol.Diagnostic, 0, len(items))
	var relatedSnapshots map[string]*text.Snapshot
	for _, item := range items {
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
			Severity: diagnosticProtocolSeverity(item),
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
		if diagnosticRelatedInformation && item.Related.Message != "" && item.Related.Span.Start < item.Related.Span.End {
			relatedURI := item.Related.URI
			relatedSnapshot := snapshot
			if relatedURI != "" {
				if relatedSnapshots == nil {
					relatedSnapshots = make(map[string]*text.Snapshot)
				}
				relatedSnapshot = relatedSnapshots[item.Related.URI]
				if relatedSnapshot == nil {
					relatedSnapshot = text.NewSnapshot(item.Related.URI, 0, nil, item.Related.Source)
					relatedSnapshots[item.Related.URI] = relatedSnapshot
				}
			}
			relatedStart, startError := relatedSnapshot.Position(item.Related.Span.Start, encoding)
			relatedEnd, endError := relatedSnapshot.Position(item.Related.Span.End, encoding)
			if startError == nil && endError == nil {
				if relatedURI == "" {
					relatedURI = snapshot.URI()
				}
				diagnostic.RelatedInformation = []protocol.DiagnosticRelatedInformation{{
					Location: protocol.Location{URI: uri.URI(relatedURI), Range: protocol.Range{
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

func hashProtocolDiagnostics(diagnostics []protocol.Diagnostic) [32]byte {
	h := sha256.New()
	for _, d := range diagnostics {
		binary.Write(h, binary.LittleEndian, d.Range.Start.Line)
		binary.Write(h, binary.LittleEndian, d.Range.Start.Character)
		binary.Write(h, binary.LittleEndian, d.Range.End.Line)
		binary.Write(h, binary.LittleEndian, d.Range.End.Character)
		binary.Write(h, binary.LittleEndian, int32(d.Severity))
		if d.Code != nil {
			fmt.Fprintf(h, "%v", d.Code)
		}
		h.Write([]byte{0})
		if src, ok := d.Source.Get(); ok {
			h.Write([]byte(src))
		}
		h.Write([]byte{0})
		if d.Message != nil {
			fmt.Fprintf(h, "%v", d.Message)
		}
		h.Write([]byte{0})
		if tags := d.Tags.Slice(); len(tags) > 0 {
			for _, tag := range tags {
				binary.Write(h, binary.LittleEndian, int32(tag))
			}
		}
		for _, rel := range d.RelatedInformation {
			h.Write([]byte(rel.Location.URI))
			h.Write([]byte{0})
			binary.Write(h, binary.LittleEndian, rel.Location.Range.Start.Line)
			binary.Write(h, binary.LittleEndian, rel.Location.Range.Start.Character)
			binary.Write(h, binary.LittleEndian, rel.Location.Range.End.Line)
			binary.Write(h, binary.LittleEndian, rel.Location.Range.End.Character)
			h.Write([]byte(rel.Message))
			h.Write([]byte{0})
		}
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
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

// diagnosticProtocolSeverity returns the protocol severity of one diagnostic.
// An occurrence-level severity set by a role-aware analysis (such as the
// config-file mode) takes precedence; otherwise the code's registered default
// or a vim/E special case applies.
func diagnosticProtocolSeverity(item syntax.Diagnostic) protocol.DiagnosticSeverity {
	if item.Severity != nil {
		return protocolSeverity(*item.Severity)
	}
	return protocolDiagnosticSeverity(item.Code)
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
	s.diagnosticsSendMu.Lock()
	defer s.diagnosticsSendMu.Unlock()
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
		if hook := s.testHooks.beforeWorkspaceWGWait; hook != nil {
			hook()
		}
		s.workspaceWG.Wait()
		s.runtimepathWG.Wait()
		s.runtimeHelpWG.Wait()
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
