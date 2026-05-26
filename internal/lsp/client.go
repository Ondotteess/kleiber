package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Ondotteess/kleiber/internal/events"
	"github.com/Ondotteess/kleiber/internal/logging"
)

// shutdownRequestTimeout caps how long Stop waits for the server to ack
// the shutdown request. gopls answers in well under a second; two
// seconds is generous for slow CI.
const shutdownRequestTimeout = 2 * time.Second

// diagnosticsPublishTimeout caps how long the read loop will wait for
// subscribers of the diagnostics topic to accept an event. If
// subscribers are slow, we drop the event and log a warning rather than
// wedge the entire LSP message pump.
const diagnosticsPublishTimeout = 5 * time.Second

// ErrClientAlreadyStarted is returned by Start when the Client has
// already been started in its lifetime.
var ErrClientAlreadyStarted = errors.New("lsp: client already started")

// ErrClientNotStarted is returned by methods called before Start (or
// after a failed Start).
var ErrClientNotStarted = errors.New("lsp: client not started")

// ErrConnectionClosed is returned by an outstanding call when the read
// loop terminates (typically because gopls exited) before a response
// arrives.
var ErrConnectionClosed = errors.New("lsp: connection closed before response")

// DiagnosticsEvent is the typed event delivered to subscribers of
// Client.Diagnostics() whenever the server publishes diagnostics for a
// document.
type DiagnosticsEvent struct {
	URI         DocumentURI
	Version     *int
	Diagnostics []Diagnostic
}

// ClientOptions configures NewClient. Most fields have sensible
// defaults; the only one a caller normally must supply for end-to-end
// use is WorkspaceFolders (or RootURI for legacy roots).
type ClientOptions struct {
	// Logger receives structured records. Nil means discard.
	Logger *slog.Logger

	// ClientName is the editor name reported to the server in
	// initialize. Defaults to "kleiber".
	ClientName string

	// ClientVersion is the editor version reported to the server.
	// Defaults to "dev".
	ClientVersion string

	// GoplsPath is the absolute path to the gopls binary. If empty,
	// Start uses exec.LookPath("gopls").
	GoplsPath string

	// GoplsArgs are extra command-line arguments. gopls's default
	// invocation (no args) is sufficient for LSP-over-stdio, so most
	// callers leave this nil.
	GoplsArgs []string

	// RootURI is the (deprecated) workspace root. Set this when
	// driving older code paths in gopls; for new code prefer
	// WorkspaceFolders.
	RootURI DocumentURI

	// WorkspaceFolders is the list of project roots sent during
	// initialize. For Kleiber this is typically one folder per Go
	// module visible in the open project.
	WorkspaceFolders []WorkspaceFolder
}

// Client is a high-level LSP client wired to gopls.
//
// Lifecycle: NewClient → Start (handshake) → DidOpen/Hover/Completion/... → Stop.
// A Client is one-shot: after Stop returns, do not call Start again.
//
// Concurrency: methods are safe for concurrent use. Internally the
// Client owns a single read-loop goroutine that demuxes server
// messages; responses are routed to their callers via a correlation
// table.
type Client struct {
	logger *slog.Logger
	opts   ClientOptions

	process *Process
	conn    *Conn

	nextID atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan *Response

	diagnostics *events.Topic[DiagnosticsEvent]

	started      atomic.Bool
	stopping     atomic.Bool
	readLoopDone chan struct{}
}

// NewClient constructs a Client. It performs no I/O; call Start to
// spawn gopls and run the initialize handshake.
func NewClient(opts ClientOptions) *Client {
	logger := opts.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	if opts.ClientName == "" {
		opts.ClientName = "kleiber"
	}
	if opts.ClientVersion == "" {
		opts.ClientVersion = "dev"
	}
	return &Client{
		logger:      logger,
		opts:        opts,
		pending:     map[int64]chan *Response{},
		diagnostics: events.NewTopic[DiagnosticsEvent]("lsp.diagnostics", logger),
	}
}

// Diagnostics returns the typed topic that receives every
// textDocument/publishDiagnostics notification the server emits.
// Subscribers should treat each event as authoritative for the
// document at its version — diagnostics replace, not accumulate.
func (c *Client) Diagnostics() *events.Topic[DiagnosticsEvent] {
	return c.diagnostics
}

// Pid returns the supervised gopls subprocess's PID, or 0 if Start has
// not yet run.
func (c *Client) Pid() int {
	if c.process == nil {
		return 0
	}
	return c.process.Pid()
}

// Start spawns gopls, opens stdio, runs the initialize/initialized
// handshake, and leaves the Client ready to accept document and
// language requests. On failure it cleans up the subprocess so callers
// do not need to call Stop after Start returns an error.
func (c *Client) Start(ctx context.Context) error {
	if !c.started.CompareAndSwap(false, true) {
		return ErrClientAlreadyStarted
	}

	binary, err := c.resolveBinary()
	if err != nil {
		c.started.Store(false)
		return err
	}

	proc, err := Start(ctx, ProcessOptions{
		Binary: binary,
		Args:   c.opts.GoplsArgs,
		Logger: c.logger,
		Name:   "gopls",
	})
	if err != nil {
		c.started.Store(false)
		return fmt.Errorf("lsp: starting gopls: %w", err)
	}
	c.process = proc
	conn := NewConn(ConnOptions{
		Reader: proc.Stdout(),
		Writer: proc.Stdin(),
		Logger: c.logger,
	})

	if err := c.runWithConn(ctx, conn); err != nil {
		return err
	}

	c.logger.Info("lsp client started",
		"binary", binary,
		"pid", proc.Pid(),
	)
	return nil
}

// runWithConn brings up the read loop and handshake over an
// already-constructed Conn. Production code reaches it via Start; tests
// call it directly to inject an in-memory transport without spawning
// gopls. Assumes c.started has been CAS'd to true by the caller.
func (c *Client) runWithConn(ctx context.Context, conn *Conn) error {
	c.conn = conn
	c.readLoopDone = make(chan struct{})
	go c.readLoop()

	if err := c.handshake(ctx); err != nil {
		c.cleanupAfterStartFailure()
		return err
	}
	return nil
}

// handshake performs initialize → initialized.
func (c *Client) handshake(ctx context.Context) error {
	pid := os.Getpid()
	params := InitializeParams{
		ProcessID:        &pid,
		ClientInfo:       &ClientInfo{Name: c.opts.ClientName, Version: c.opts.ClientVersion},
		RootURI:          rootURIOrNil(c.opts.RootURI),
		Capabilities:     defaultClientCapabilities(),
		WorkspaceFolders: c.opts.WorkspaceFolders,
	}

	rawResult, err := c.call(ctx, MethodInitialize, params)
	if err != nil {
		return fmt.Errorf("lsp: initialize: %w", err)
	}
	var result InitializeResult
	if jerr := json.Unmarshal(rawResult, &result); jerr != nil {
		c.logger.Warn("decoding InitializeResult", "err", jerr)
	} else if result.ServerInfo != nil {
		c.logger.Info("server initialized",
			"name", result.ServerInfo.Name,
			"version", result.ServerInfo.Version,
		)
	}

	if err := c.notify(MethodInitialized, InitializedParams{}); err != nil {
		return fmt.Errorf("lsp: sending initialized: %w", err)
	}
	return nil
}

// Stop performs the LSP shutdown sequence (shutdown request → exit
// notification) and tears down the subprocess.
//
// Concurrent Stop calls coalesce on the underlying Process.Stop; the
// first does the work, the rest block until it returns.
func (c *Client) Stop(ctx context.Context) error {
	if !c.started.Load() {
		return nil
	}
	if !c.stopping.CompareAndSwap(false, true) {
		// Another goroutine is already stopping. Wait for it.
		<-c.readLoopDone
		return nil
	}
	defer c.diagnostics.Close()

	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownRequestTimeout)
	if _, err := c.call(shutdownCtx, MethodShutdown, nil); err != nil &&
		!errors.Is(err, ErrConnectionClosed) {
		c.logger.Warn("shutdown request", "err", err)
	}
	cancel()

	if err := c.notify(MethodExit, nil); err != nil {
		c.logger.Debug("exit notification", "err", err)
	}

	var procErr error
	if c.process != nil {
		procErr = c.process.Stop(ctx)
	}
	// Force-close the conn so any read still in flight returns and
	// the loop exits. In production the closer is nil (Process owns
	// the pipes), in tests it releases the in-memory transport.
	_ = c.conn.Close()
	<-c.readLoopDone
	return procErr
}

// DidOpen tells the server that the editor has opened a document.
// languageID is the LSP language identifier ("go" for Go files); text
// is the full buffer contents at version 1.
func (c *Client) DidOpen(ctx context.Context, uri DocumentURI, languageID, text string) error {
	if !c.started.Load() {
		return ErrClientNotStarted
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.notify(MethodTextDocumentDidOpen, DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: languageID,
			Version:    1,
			Text:       text,
		},
	})
}

// DidChange tells the server that a document's full text changed.
// version is the editor's monotonically increasing document version.
func (c *Client) DidChange(ctx context.Context, uri DocumentURI, version int, text string) error {
	if !c.started.Load() {
		return ErrClientNotStarted
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.notify(MethodTextDocumentDidChange, DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{
			URI:     uri,
			Version: version,
		},
		ContentChanges: []TextDocumentContentChangeEvent{{
			Text: text,
		}},
	})
}

// DidClose tells the server that the editor closed a document.
func (c *Client) DidClose(ctx context.Context, uri DocumentURI) error {
	if !c.started.Load() {
		return ErrClientNotStarted
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.notify(MethodTextDocumentDidClose, DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
}

// Hover requests hover information for a position in a document. A nil
// *Hover with nil error means the server has nothing to show (e.g.,
// the position is in whitespace).
func (c *Client) Hover(ctx context.Context, uri DocumentURI, pos Position) (*Hover, error) {
	if !c.started.Load() {
		return nil, ErrClientNotStarted
	}
	raw, err := c.call(ctx, MethodTextDocumentHover, HoverParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	})
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var h Hover
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, fmt.Errorf("lsp: decoding hover: %w", err)
	}
	return &h, nil
}

// Completion requests completion candidates at pos. A nil *CompletionList
// with nil error means the server has no candidates for that position.
func (c *Client) Completion(ctx context.Context, uri DocumentURI, pos Position) (*CompletionList, error) {
	if !c.started.Load() {
		return nil, ErrClientNotStarted
	}
	raw, err := c.call(ctx, MethodTextDocumentCompletion, CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	})
	if err != nil {
		return nil, err
	}
	return decodeCompletionList(raw)
}

// Definition requests the locations that define the symbol at pos. A nil
// slice with nil error means the server has no definition for that position.
func (c *Client) Definition(ctx context.Context, uri DocumentURI, pos Position) ([]Location, error) {
	if !c.started.Load() {
		return nil, ErrClientNotStarted
	}
	raw, err := c.call(ctx, MethodTextDocumentDefinition, DefinitionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	})
	if err != nil {
		return nil, err
	}
	return decodeLocations(raw, MethodTextDocumentDefinition, true)
}

// References requests references to the symbol at pos. includeDeclaration
// controls whether the symbol's declaration is included in the result.
func (c *Client) References(ctx context.Context, uri DocumentURI, pos Position, includeDeclaration bool) ([]Location, error) {
	if !c.started.Load() {
		return nil, ErrClientNotStarted
	}
	raw, err := c.call(ctx, MethodTextDocumentReferences, ReferenceParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
		Context:      ReferenceContext{IncludeDeclaration: includeDeclaration},
	})
	if err != nil {
		return nil, err
	}
	return decodeLocations(raw, MethodTextDocumentReferences, false)
}

// Formatting requests full-document formatting edits for uri. A nil
// slice with nil error means the server had no edits to apply.
func (c *Client) Formatting(ctx context.Context, uri DocumentURI, opts FormattingOptions) ([]TextEdit, error) {
	if !c.started.Load() {
		return nil, ErrClientNotStarted
	}
	raw, err := c.call(ctx, MethodTextDocumentFormatting, DocumentFormattingParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Options:      opts,
	})
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var edits []TextEdit
	if err := json.Unmarshal(raw, &edits); err != nil {
		return nil, fmt.Errorf("lsp: decoding formatting edits: %w", err)
	}
	return edits, nil
}

func decodeCompletionList(raw json.RawMessage) (*CompletionList, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var list CompletionList
	if err := json.Unmarshal(raw, &list); err == nil && list.Items != nil {
		return &list, nil
	}

	var items []CompletionItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("lsp: decoding completion result: %w", err)
	}
	return &CompletionList{Items: items}, nil
}

func decodeLocations(raw json.RawMessage, method string, allowSingle bool) ([]Location, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var locations []Location
	if err := json.Unmarshal(raw, &locations); err == nil {
		return locations, nil
	} else if !allowSingle {
		return nil, fmt.Errorf("lsp: decoding %s locations: %w", method, err)
	}

	var location Location
	if err := json.Unmarshal(raw, &location); err != nil {
		return nil, fmt.Errorf("lsp: decoding %s location: %w", method, err)
	}
	return []Location{location}, nil
}

// call sends a Request and blocks until the matching Response arrives,
// the read loop terminates, or ctx fires.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan *Response, 1)

	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	rawParams, err := encodeParams(params)
	if err != nil {
		c.removePending(id)
		return nil, fmt.Errorf("lsp: encoding %s params: %w", method, err)
	}

	req := &Request{ID: NewIntID(id), Method: method, Params: rawParams}
	if err := c.conn.Write(req); err != nil {
		c.removePending(id)
		return nil, fmt.Errorf("lsp: writing %s request: %w", method, err)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, ErrConnectionClosed
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	}
}

// notify sends a Notification (fire-and-forget). It is safe for
// concurrent use; the underlying Conn serializes writes.
func (c *Client) notify(method string, params any) error {
	raw, err := encodeParams(params)
	if err != nil {
		return fmt.Errorf("lsp: encoding %s params: %w", method, err)
	}
	if err := c.conn.Write(&Notification{Method: method, Params: raw}); err != nil {
		return fmt.Errorf("lsp: writing %s notification: %w", method, err)
	}
	return nil
}

// readLoop demultiplexes server messages until the underlying Conn
// returns an error (typically EOF on subprocess exit).
func (c *Client) readLoop() {
	defer close(c.readLoopDone)
	for {
		msg, err := c.conn.Read()
		if err != nil {
			c.logger.Debug("lsp read loop ended", "err", err)
			c.failAllPending()
			return
		}
		switch m := msg.(type) {
		case *Response:
			c.routeResponse(m)
		case *Notification:
			c.dispatchNotification(m)
		case *Request:
			c.dispatchRequest(m)
		}
	}
}

// dispatchRequest answers server-to-client requests. LSP lets servers ask the
// client for capabilities/configuration at runtime; unsupported methods receive
// method-not-found so the server can degrade cleanly.
func (c *Client) dispatchRequest(req *Request) {
	switch req.Method {
	case MethodWorkspaceConfiguration:
		c.respondWorkspaceConfiguration(req)
	case MethodWindowShowMessageReq:
		c.respondShowMessageRequest(req)
	default:
		c.respondMethodNotFound(req)
	}
}

// routeResponse hands a Response to its waiting caller, if any.
func (c *Client) routeResponse(resp *Response) {
	id, ok := resp.ID.Int64()
	if !ok {
		c.logger.Warn("response with non-integer ID; dropping")
		return
	}
	c.pendingMu.Lock()
	ch, exists := c.pending[id]
	if exists {
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
	if !exists {
		c.logger.Debug("response for unknown ID; dropping", "id", id)
		return
	}
	// Buffered channel of 1; the send is non-blocking.
	ch <- resp
}

// dispatchNotification routes server-pushed events to the appropriate
// handler. Unknown methods are logged at Debug and ignored.
func (c *Client) dispatchNotification(n *Notification) {
	switch n.Method {
	case MethodPublishDiagnostics:
		c.handlePublishDiagnostics(n.Params)
	case MethodWindowLogMessage, MethodWindowShowMessage:
		c.logger.Info("server window message",
			"method", n.Method,
			"params", string(n.Params),
		)
	default:
		c.logger.Debug("unhandled notification", "method", n.Method)
	}
}

// handlePublishDiagnostics decodes diagnostics and publishes them on
// the typed Topic. Slow subscribers cannot wedge the read loop: we
// timeout the Publish after diagnosticsPublishTimeout.
func (c *Client) handlePublishDiagnostics(params json.RawMessage) {
	var p PublishDiagnosticsParams
	if err := json.Unmarshal(params, &p); err != nil {
		c.logger.Warn("decoding publishDiagnostics", "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), diagnosticsPublishTimeout)
	defer cancel()
	event := DiagnosticsEvent{
		URI:         p.URI,
		Version:     p.Version,
		Diagnostics: p.Diagnostics,
	}
	if err := c.diagnostics.Publish(ctx, event); err != nil {
		c.logger.Warn("publishing diagnostics event", "err", err, "uri", p.URI)
	}
}

// respondWorkspaceConfiguration answers workspace/configuration with no
// configured values. The result must contain one entry per requested item.
func (c *Client) respondWorkspaceConfiguration(req *Request) {
	var params ConfigurationParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			if werr := c.conn.Write(&Response{
				ID: req.ID,
				Error: &ResponseError{
					Code:    ErrCodeInvalidParams,
					Message: fmt.Sprintf("invalid workspace/configuration params: %v", err),
				},
			}); werr != nil {
				c.logger.Warn("responding workspace/configuration invalid params", "err", werr)
			}
			return
		}
	}
	settings := make([]json.RawMessage, len(params.Items))
	result, err := json.Marshal(settings)
	if err != nil {
		if werr := c.conn.Write(&Response{
			ID: req.ID,
			Error: &ResponseError{
				Code:    ErrCodeInternalError,
				Message: fmt.Sprintf("encoding workspace/configuration result: %v", err),
			},
		}); werr != nil {
			c.logger.Warn("responding workspace/configuration encode error", "err", werr)
		}
		return
	}
	if err := c.conn.Write(&Response{ID: req.ID, Result: result}); err != nil {
		c.logger.Warn("responding workspace/configuration", "err", err)
	}
}

// respondShowMessageRequest acknowledges a server prompt without selecting an
// action. UI wiring can replace this with a real modal/action picker later.
func (c *Client) respondShowMessageRequest(req *Request) {
	var params ShowMessageRequestParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			if werr := c.conn.Write(&Response{
				ID: req.ID,
				Error: &ResponseError{
					Code:    ErrCodeInvalidParams,
					Message: fmt.Sprintf("invalid window/showMessageRequest params: %v", err),
				},
			}); werr != nil {
				c.logger.Warn("responding window/showMessageRequest invalid params", "err", werr)
			}
			return
		}
	}
	c.logger.Info("server show-message request",
		"type", params.Type,
		"message", params.Message,
	)
	if err := c.conn.Write(&Response{ID: req.ID, Result: json.RawMessage("null")}); err != nil {
		c.logger.Warn("responding window/showMessageRequest", "err", err)
	}
}

// respondMethodNotFound answers a server-to-client request with a
// MethodNotFound error. We do not yet handle most server-issued methods
// (client/registerCapability) so this is the safe default.
func (c *Client) respondMethodNotFound(req *Request) {
	err := c.conn.Write(&Response{
		ID: req.ID,
		Error: &ResponseError{
			Code:    ErrCodeMethodNotFound,
			Message: fmt.Sprintf("client does not implement %s", req.Method),
		},
	})
	if err != nil {
		c.logger.Warn("responding method-not-found", "method", req.Method, "err", err)
	}
}

// failAllPending closes every pending response channel so blocked
// callers observe ErrConnectionClosed.
func (c *Client) failAllPending() {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
}

// removePending discards a single pending entry, used when call's ctx
// fires before a Response arrives.
func (c *Client) removePending(id int64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

// resolveBinary returns either the explicitly configured path or the
// first "gopls" on $PATH.
func (c *Client) resolveBinary() (string, error) {
	if c.opts.GoplsPath != "" {
		return c.opts.GoplsPath, nil
	}
	bin, err := exec.LookPath("gopls")
	if err != nil {
		return "", fmt.Errorf("lsp: locating gopls: %w", err)
	}
	return bin, nil
}

// cleanupAfterStartFailure kills the subprocess and drains the read
// loop so a failed Start leaves no goroutines or processes behind.
func (c *Client) cleanupAfterStartFailure() {
	if c.process != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = c.process.Stop(ctx)
		cancel()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	if c.readLoopDone != nil {
		<-c.readLoopDone
	}
	c.diagnostics.Close()
	c.started.Store(false)
}

// encodeParams returns the JSON encoding of params, or nil if params
// is nil (which produces an LSP message with no "params" field).
func encodeParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	return json.Marshal(params)
}

// rootURIOrNil returns &uri if uri is non-empty, else nil. The pointer
// distinguishes "absent" (no rootUri field) from "explicitly null"
// (rootUri: null) in the marshaled JSON.
func rootURIOrNil(uri DocumentURI) *DocumentURI {
	if uri == "" {
		return nil
	}
	return &uri
}

// defaultClientCapabilities returns the minimal capability bundle
// Kleiber's v1 LSP client supports. Extending it is safe (additive);
// trimming it is a breaking change to be discussed in an ADR.
func defaultClientCapabilities() ClientCapabilities {
	return ClientCapabilities{
		Workspace: &WorkspaceClientCapabilities{
			WorkspaceFolders: true,
		},
		TextDocument: &TextDocumentClientCapabilities{
			Synchronization:    &TextDocumentSyncClientCapabilities{},
			PublishDiagnostics: &PublishDiagnosticsClientCapabilities{VersionSupport: true},
			Hover: &HoverClientCapabilities{
				ContentFormat: []MarkupKind{MarkupKindPlainText, MarkupKindMarkdown},
			},
			Completion: &CompletionClientCapabilities{
				CompletionItem: &CompletionItemClientCapabilities{
					DocumentationFormat:  []MarkupKind{MarkupKindPlainText, MarkupKindMarkdown},
					DeprecatedSupport:    true,
					PreselectSupport:     true,
					InsertReplaceSupport: true,
					LabelDetailsSupport:  true,
				},
			},
			Definition: &DefinitionClientCapabilities{},
			References: &ReferenceClientCapabilities{},
			Formatting: &FormattingClientCapabilities{},
		},
	}
}
