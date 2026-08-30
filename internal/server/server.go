package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/chemzqm/vimls-go/internal/analysis"
	"github.com/chemzqm/vimls-go/internal/jsonrpc"
	"github.com/chemzqm/vimls-go/internal/syntax"
	"github.com/chemzqm/vimls-go/internal/text"
	"github.com/chemzqm/vimls-go/internal/workspace"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const (
	Name                      = "vimls"
	Version                   = "dev"
	maxFileBytes              = 4 << 20
	maxParallelAnalysis       = 4
	maxDiagnosticsPerDocument = 200
)

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

	mu              sync.Mutex
	state           state
	targetVersion   TargetVersion
	targetOverride  bool
	pendingWarning  string
	client          protocol.Client
	cancellations   map[jsonrpc2.ID]context.CancelFunc
	documents       *workspace.Documents
	encoding        text.Encoding
	exitOnce        sync.Once
	exitCode        chan int
	analysisMu      sync.Mutex
	analysisContext context.Context
	analysisCancel  context.CancelFunc
	analysisStopped bool
	analysisWG      sync.WaitGroup
	analysisWake    chan struct{}
	analysisPending map[string]struct{}
	analysisRunning map[string]struct{}
	analysisWorkers int
	publishMu       sync.Mutex
	parsed          map[string]parsedDocument
	published       map[string]bool
}

func New(input io.Reader, output, logOutput io.Writer) *Server {
	target, _ := ParseTargetVersion(DefaultTargetVersion)
	analysisContext, analysisCancel := context.WithCancel(context.Background())
	return &Server{
		input:           input,
		output:          output,
		log:             logOutput,
		targetVersion:   target,
		cancellations:   make(map[jsonrpc2.ID]context.CancelFunc),
		documents:       workspace.NewDocuments(),
		encoding:        text.UTF16,
		exitCode:        make(chan int, 1),
		analysisContext: analysisContext,
		analysisCancel:  analysisCancel,
		analysisWake:    make(chan struct{}, maxParallelAnalysis),
		analysisPending: make(map[string]struct{}),
		analysisRunning: make(map[string]struct{}),
		parsed:          make(map[string]parsedDocument),
		published:       make(map[string]bool),
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
		s.mu.Lock()
		s.cancellations[id] = cancel
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.cancellations, id)
			s.mu.Unlock()
			cancel()
		}()
		return next(requestCtx, request)
	}
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
		protocol.MethodTextDocumentDocumentSymbol,
		protocol.MethodWorkspaceDidChangeConfiguration:
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
	s.mu.Lock()
	s.targetVersion, s.targetOverride, s.pendingWarning = targetVersionFromOptions([]byte(params.InitializationOptions))
	s.encoding = encoding
	s.state = stateActive
	s.mu.Unlock()
	return &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			PositionEncoding:       protocolEncoding,
			DocumentSymbolProvider: protocol.Boolean(true),
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

func (s *Server) Shutdown(context.Context) error {
	s.mu.Lock()
	s.state = stateShutdown
	s.mu.Unlock()
	return nil
}

func (s *Server) DidOpen(_ context.Context, params *protocol.DidOpenTextDocumentParams) error {
	document := params.TextDocument
	s.publishMu.Lock()
	s.documents.Open(document.URI.String(), document.Version, document.Text)
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
	_, err := s.documents.Change(params.TextDocument.URI.String(), params.TextDocument.Version, encoding, changes)
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
	_, err := s.documents.Save(params.TextDocument.URI.String(), params.Text)
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
	s.mu.Lock()
	var warning string
	if !s.targetOverride {
		s.targetVersion, warning = targetVersionFromSettings([]byte(params.Settings), s.targetVersion)
	}
	s.mu.Unlock()
	s.publishMu.Lock()
	snapshots := s.documents.ConfigurationChanged()
	s.publishMu.Unlock()
	for _, snapshot := range snapshots {
		s.startAnalysis(snapshot.URI())
	}
	if warning != "" {
		return s.sendWarning(ctx, warning)
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
		file.Diagnostics = append(file.Diagnostics, analysis.Analyze(file).Diagnostics...)
		if len(file.Diagnostics) > maxDiagnosticsPerDocument {
			file.Diagnostics = append(file.Diagnostics[:maxDiagnosticsPerDocument-1], syntax.Diagnostic{
				Code: "vimls/diagnostics-truncated", Message: "additional diagnostics were omitted",
				Span: syntax.Span{Start: len(file.Source), End: len(file.Source)},
			})
		}
		if work.Context.Err() != nil {
			return
		}
	}
	if work.Context.Err() != nil {
		return
	}
	s.publishSyntax(work, file)
}

func (s *Server) publishSyntax(analysis workspace.Analysis, file *syntax.File) {
	s.mu.Lock()
	encoding := s.encoding
	client := s.client
	s.mu.Unlock()

	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if !s.documents.IsCurrent(analysis) {
		return
	}
	documentURI := analysis.Snapshot.URI()
	s.parsed[documentURI] = parsedDocument{revision: analysis.Snapshot.Revision(), file: file}
	diagnostics := make([]protocol.Diagnostic, 0, len(file.Diagnostics))
	for _, item := range file.Diagnostics {
		start, startError := analysis.Snapshot.Position(item.Span.Start, encoding)
		end, endError := analysis.Snapshot.Position(item.Span.End, encoding)
		if startError != nil || endError != nil {
			continue
		}
		diagnostics = append(diagnostics, protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: uint32(start.Line), Character: uint32(start.Character)},
				End:   protocol.Position{Line: uint32(end.Line), Character: uint32(end.Character)},
			},
			Severity: protocol.DiagnosticSeverityError,
			Code:     protocol.String(item.Code),
			Source:   protocol.NewOptional(Name),
			Message:  protocol.String(item.Message),
		})
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
}

func (s *Server) logf(format string, args ...any) {
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
