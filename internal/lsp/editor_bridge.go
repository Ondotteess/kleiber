package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/logging"
)

const (
	// bridgeEngineEventBuffer sizes the bridge's subscription to the
	// editor's BufferEvent topic. Big enough to absorb a checkout-
	// style burst (hundreds of opened files) without dropping events.
	bridgeEngineEventBuffer = 256

	// bridgeDiagBuffer sizes the bridge's subscription to the LSP
	// client's DiagnosticsEvent topic. Lower than engine events:
	// gopls publishes one per document edit, and the buffer here only
	// needs to cover bursts during a typing storm.
	bridgeDiagBuffer = 64

	// bridgeRouteTimeout caps how long the bridge will wait when
	// routing an LSP notification or an engine event onward. Five
	// seconds matches the editor's own publishEvent timeout.
	bridgeRouteTimeout = 5 * time.Second
)

var (
	// ErrBridgeDocumentNotTracked is returned when a bridge operation
	// targets a buffer that was never opened with the language server.
	ErrBridgeDocumentNotTracked = errors.New("lsp: bridge document not tracked")

	// ErrBridgeBufferChangedDuringFormat is returned when a buffer
	// mutates while a formatting request is in flight.
	ErrBridgeBufferChangedDuringFormat = errors.New("lsp: bridge buffer changed during format")
)

// BridgeOptions configures NewBridge. Logger is the only field most
// callers need to override; the rest exist for tests that want
// deterministic timing.
type BridgeOptions struct {
	// Logger receives structured records. Nil means discard.
	Logger *slog.Logger
}

// Bridge connects an editor.EditorEngine to a *Client. It subscribes
// to engine BufferEvents, forwards them to the language server as
// textDocument/didOpen|didChange|didClose, and routes the server's
// textDocument/publishDiagnostics back to the engine as
// editor.BufferDiagnostics events.
//
// Lifecycle: NewBridge → ... → Close. NewBridge starts background
// goroutines; Close cancels them and waits for clean exit.
//
// Concurrency: the bridge owns its goroutines and synchronizes its
// internal document map with a mutex. Calls from the engine and the
// LSP client may race; the document map is the authoritative
// rendezvous point.
//
// Scope (v1):
//   - Only buffers with a non-empty path are forwarded. Untitled
//     buffers are skipped; they can be picked up after SaveAs in a
//     follow-up.
//   - Only .go files are forwarded. gopls rejects other extensions.
//   - Document sync is full-text per change. Incremental sync lands
//     when the editor exposes structured edits to subscribers.
type Bridge struct {
	logger *slog.Logger
	client *Client
	engine *editor.EditorEngine

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once

	mu    sync.Mutex
	docs  map[editor.BufferID]*managedDoc
	byURI map[DocumentURI]editor.BufferID
}

// managedDoc is the bridge's per-buffer record.
type managedDoc struct {
	uri     DocumentURI
	version atomic.Int32
}

// NewBridge constructs a Bridge and starts its background goroutines.
// The returned Bridge runs until Close is called or parent is canceled.
//
// The client must be Started (otherwise Did* calls error). The engine
// must be live. Neither is allowed to be nil.
func NewBridge(parent context.Context, opts BridgeOptions, client *Client, engine *editor.EditorEngine) *Bridge {
	logger := opts.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	ctx, cancel := context.WithCancel(parent)
	b := &Bridge{
		logger: logger,
		client: client,
		engine: engine,
		ctx:    ctx,
		cancel: cancel,
		docs:   map[editor.BufferID]*managedDoc{},
		byURI:  map[DocumentURI]editor.BufferID{},
	}

	engSub, engUnsub := engine.Events().Subscribe(bridgeEngineEventBuffer)
	diagSub, diagUnsub := client.Diagnostics().Subscribe(bridgeDiagBuffer)

	b.wg.Add(2)
	go b.runEngineLoop(engSub, engUnsub)
	go b.runDiagLoop(diagSub, diagUnsub)

	return b
}

// Close stops the bridge's background goroutines and waits for them
// to exit. Close is idempotent.
//
// Close does not stop the underlying client or engine; the bridge is
// purely a wiring layer.
func (b *Bridge) Close() {
	b.closeOnce.Do(func() {
		b.cancel()
		b.wg.Wait()
	})
}

// FormatBuffer requests LSP full-document formatting for a tracked
// buffer and applies the returned TextEdits to the editor buffer.
//
// The buffer must already be open in the bridge (non-empty path, .go
// extension, didOpen sent). Formatting edits are computed against the
// buffer snapshot current at request time; if the local buffer mutates
// before the server responds, the edits are rejected rather than
// applied to the wrong coordinates.
func (b *Bridge) FormatBuffer(ctx context.Context, id editor.BufferID, opts FormattingOptions) (int, error) {
	b.mu.Lock()
	doc, ok := b.docs[id]
	b.mu.Unlock()
	if !ok {
		return 0, fmt.Errorf("%w: id=%d", ErrBridgeDocumentNotTracked, id)
	}

	buf, err := b.engine.Buffer(id)
	if err != nil {
		return 0, err
	}
	seq := buf.Seq()

	reqCtx, cancel := context.WithTimeout(ctx, bridgeRouteTimeout)
	defer cancel()
	edits, err := b.client.Formatting(reqCtx, doc.uri, opts)
	if err != nil {
		return 0, err
	}
	if len(edits) == 0 {
		return 0, nil
	}
	if got := buf.Seq(); got != seq {
		return 0, fmt.Errorf("%w: id=%d before=%d after=%d",
			ErrBridgeBufferChangedDuringFormat, id, seq, got)
	}
	if err := ApplyTextEdits(buf, edits); err != nil {
		return 0, err
	}
	return len(edits), nil
}

// FormatAndSaveBuffer formats a tracked buffer through LSP and saves
// the resulting editor contents to the buffer's associated path.
//
// Formatting errors prevent the save; save errors are returned after
// successful formatting. The returned int is the number of formatting
// edits applied before saving.
func (b *Bridge) FormatAndSaveBuffer(ctx context.Context, id editor.BufferID, opts FormattingOptions) (int, error) {
	n, err := b.FormatBuffer(ctx, id, opts)
	if err != nil {
		return 0, err
	}
	if err := b.engine.Save(ctx, id); err != nil {
		return n, err
	}
	return n, nil
}

// runEngineLoop reads BufferEvents from the engine and forwards each
// to the language server. It exits when the bridge's context is
// canceled or the subscription channel closes (engine topic closed).
func (b *Bridge) runEngineLoop(sub <-chan editor.BufferEvent, unsub func()) {
	defer b.wg.Done()
	defer unsub()
	for {
		select {
		case <-b.ctx.Done():
			return
		case ev, ok := <-sub:
			if !ok {
				return
			}
			b.handleEngineEvent(ev)
		}
	}
}

// handleEngineEvent dispatches one engine event to its handler.
// BufferDiagnostics events (which the bridge itself publishes) are
// intentionally ignored to prevent feedback loops; other unknown
// events are also no-ops.
func (b *Bridge) handleEngineEvent(ev editor.BufferEvent) {
	switch e := ev.(type) {
	case editor.BufferOpened:
		b.onBufferOpened(e)
	case editor.BufferChanged:
		b.onBufferChanged(e)
	case editor.BufferClosed:
		b.onBufferClosed(e)
	}
}

// onBufferOpened forwards a BufferOpened to the language server as
// textDocument/didOpen. Untitled and non-Go buffers are skipped.
func (b *Bridge) onBufferOpened(e editor.BufferOpened) {
	if e.Path == "" {
		return
	}
	if !isGoFile(e.Path) {
		return
	}
	uri, err := DocumentURIFromPath(e.Path)
	if err != nil {
		b.logger.Warn("bridge: building URI",
			"path", e.Path, "err", err,
		)
		return
	}

	buf, err := b.engine.Buffer(e.ID)
	if err != nil {
		// Buffer was closed before we got to it. Nothing to do.
		b.logger.Debug("bridge: buffer disappeared before didOpen",
			"id", e.ID, "err", err,
		)
		return
	}
	text := buf.Text()

	doc := &managedDoc{uri: uri}
	doc.version.Store(1)

	b.mu.Lock()
	// Defensive: if a buffer with this ID is somehow already tracked,
	// release its URI mapping before overwriting.
	if old, ok := b.docs[e.ID]; ok {
		delete(b.byURI, old.uri)
	}
	b.docs[e.ID] = doc
	b.byURI[uri] = e.ID
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(b.ctx, bridgeRouteTimeout)
	defer cancel()
	if err := b.client.DidOpen(ctx, uri, "go", text); err != nil {
		b.logger.Warn("bridge: DidOpen failed",
			"uri", uri, "err", err,
		)
	}
}

// onBufferChanged forwards every BufferChanged event as a full-text
// textDocument/didChange. Buffers the bridge never opened are ignored.
func (b *Bridge) onBufferChanged(e editor.BufferChanged) {
	b.mu.Lock()
	doc, ok := b.docs[e.ID]
	b.mu.Unlock()
	if !ok {
		return
	}

	buf, err := b.engine.Buffer(e.ID)
	if err != nil {
		return
	}
	text := buf.Text()

	v := doc.version.Add(1)
	ctx, cancel := context.WithTimeout(b.ctx, bridgeRouteTimeout)
	defer cancel()
	if err := b.client.DidChange(ctx, doc.uri, int(v), text); err != nil {
		b.logger.Warn("bridge: DidChange failed",
			"uri", doc.uri, "err", err,
		)
	}
}

// onBufferClosed forwards BufferClosed as textDocument/didClose and
// removes the buffer from the bridge's tracking maps.
func (b *Bridge) onBufferClosed(e editor.BufferClosed) {
	b.mu.Lock()
	doc, ok := b.docs[e.ID]
	if ok {
		delete(b.docs, e.ID)
		delete(b.byURI, doc.uri)
	}
	b.mu.Unlock()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(b.ctx, bridgeRouteTimeout)
	defer cancel()
	if err := b.client.DidClose(ctx, doc.uri); err != nil {
		b.logger.Warn("bridge: DidClose failed",
			"uri", doc.uri, "err", err,
		)
	}
}

// runDiagLoop reads DiagnosticsEvents from the LSP client and routes
// them to the engine. Diagnostics for URIs the bridge never opened
// are dropped — they belong to a buffer the editor does not track.
func (b *Bridge) runDiagLoop(sub <-chan DiagnosticsEvent, unsub func()) {
	defer b.wg.Done()
	defer unsub()
	for {
		select {
		case <-b.ctx.Done():
			return
		case ev, ok := <-sub:
			if !ok {
				return
			}
			b.routeDiagnostics(ev)
		}
	}
}

// routeDiagnostics translates ev to editor.BufferDiagnostics and
// publishes it on the engine's event topic.
func (b *Bridge) routeDiagnostics(ev DiagnosticsEvent) {
	b.mu.Lock()
	id, ok := b.byURI[ev.URI]
	b.mu.Unlock()
	if !ok {
		b.logger.Debug("bridge: diagnostics for unknown URI",
			"uri", ev.URI,
		)
		return
	}

	diags := make([]editor.Diagnostic, len(ev.Diagnostics))
	for i, d := range ev.Diagnostics {
		diags[i] = lspDiagnosticToEditor(d)
	}

	ctx, cancel := context.WithTimeout(b.ctx, bridgeRouteTimeout)
	defer cancel()
	err := b.engine.Events().Publish(ctx, editor.BufferDiagnostics{
		ID:          id,
		Version:     ev.Version,
		Diagnostics: diags,
	})
	if err != nil {
		b.logger.Warn("bridge: publishing diagnostics",
			"id", id, "err", err,
		)
	}
}

// lspDiagnosticToEditor converts an LSP Diagnostic into the
// editor-native form. Ranges are copied as-is; the LSP convention is
// UTF-16 character columns and the editor convention is byte columns,
// so this mapping is *only* correct for ASCII positions today.
// Buffer-aware UTF-16 → byte conversion is a known follow-up tracked
// in components.md.
func lspDiagnosticToEditor(d Diagnostic) editor.Diagnostic {
	return editor.Diagnostic{
		Range:    lspRangeToEditor(d.Range),
		Severity: lspSeverityToEditor(d.Severity),
		Source:   d.Source,
		Code:     decodeDiagnosticCode(d.Code),
		Message:  d.Message,
	}
}

func lspRangeToEditor(r Range) editor.Range {
	return editor.Range{
		Start: editor.Position{Line: r.Start.Line, Column: r.Start.Character},
		End:   editor.Position{Line: r.End.Line, Column: r.End.Character},
	}
}

func lspSeverityToEditor(s DiagnosticSeverity) editor.DiagnosticSeverity {
	switch s {
	case DiagnosticSeverityError:
		return editor.DiagnosticSeverityError
	case DiagnosticSeverityWarning:
		return editor.DiagnosticSeverityWarning
	case DiagnosticSeverityInformation:
		return editor.DiagnosticSeverityInformation
	case DiagnosticSeverityHint:
		return editor.DiagnosticSeverityHint
	default:
		return editor.DiagnosticSeverityUnknown
	}
}

// decodeDiagnosticCode renders the spec-allowed (string|number)
// Diagnostic.Code as a plain string. Empty/null inputs return "".
func decodeDiagnosticCode(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	// Fallback: hand back the raw JSON literal so callers can at
	// least display *something*. Useful for non-standard servers.
	return string(raw)
}

// isGoFile reports whether path looks like a Go source file. Used to
// avoid sending non-Go content to gopls.
func isGoFile(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".go"
}

// docCount returns the number of buffers the bridge currently tracks.
// Package-internal — used by bridge tests to assert lifecycle state.
func (b *Bridge) docCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.docs)
}

// versionFor returns the current LSP version for the buffer, or 0 if
// the bridge does not track it. Package-internal — used by tests.
func (b *Bridge) versionFor(id editor.BufferID) int {
	b.mu.Lock()
	doc, ok := b.docs[id]
	b.mu.Unlock()
	if !ok {
		return 0
	}
	return int(doc.version.Load())
}

// uriFor returns the LSP URI for the buffer, or "" if the bridge does
// not track it. Package-internal — used by tests.
func (b *Bridge) uriFor(id editor.BufferID) DocumentURI {
	b.mu.Lock()
	doc, ok := b.docs[id]
	b.mu.Unlock()
	if !ok {
		return ""
	}
	return doc.uri
}
