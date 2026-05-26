package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
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

	// ErrBridgeBufferChangedDuringCompletion is returned when a buffer
	// mutates while a completion request is in flight.
	ErrBridgeBufferChangedDuringCompletion = errors.New("lsp: bridge buffer changed during completion")

	// ErrBridgeBufferChangedDuringNavigation is returned when a buffer
	// mutates while a hover/definition/references request is in flight.
	ErrBridgeBufferChangedDuringNavigation = errors.New("lsp: bridge buffer changed during navigation")
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
//     buffers are skipped until SaveAs gives them a Go path.
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

// EditorHover is hover content translated to editor-native coordinates.
type EditorHover struct {
	Contents MarkupContent
	Range    *editor.Range
}

// EditorLocation is an LSP location translated to an editor byte range.
// BufferID is zero when the target file is not currently tracked by the
// bridge.
type EditorLocation struct {
	URI      DocumentURI
	Path     string
	BufferID editor.BufferID
	Range    editor.Range
}

// TrackedDocument is a full-text snapshot of one Go document the bridge has
// opened with gopls. It is intentionally heavier than a UI state snapshot: it
// exists so a future restart supervisor can replay didOpen against a fresh LSP
// client without reading stale content from disk.
type TrackedDocument struct {
	BufferID editor.BufferID
	URI      DocumentURI
	Path     string
	Version  int
	Text     string
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

// TrackedDocuments returns a sorted full-text snapshot of the Go documents the
// bridge currently tracks. Disappeared buffers are skipped; Close remains the
// lifecycle owner that eventually sends didClose and removes stale entries.
func (b *Bridge) TrackedDocuments() []TrackedDocument {
	refs := b.trackedDocumentRefs()
	out := make([]TrackedDocument, 0, len(refs))
	for _, ref := range refs {
		path, err := PathFromDocumentURI(ref.uri)
		if err != nil {
			b.logger.Warn("bridge: resolving tracked document URI",
				"uri", ref.uri, "err", err,
			)
			continue
		}
		buf, err := b.engine.Buffer(ref.id)
		if err != nil {
			b.logger.Debug("bridge: tracked buffer disappeared",
				"id", ref.id, "err", err,
			)
			continue
		}
		text, _ := bufferSnapshot(buf)
		out = append(out, TrackedDocument{
			BufferID: ref.id,
			URI:      ref.uri,
			Path:     path,
			Version:  ref.version,
			Text:     text,
		})
	}
	return out
}

// ReplayOpenDocuments re-sends didOpen for every currently tracked Go document
// using the latest editor buffer text. gopls treats didOpen as version 1, so
// successful replays reset the bridge's per-document version to 1 before future
// didChange messages are sent. This helper is a restart boundary, not an
// automatic restart loop.
func (b *Bridge) ReplayOpenDocuments(ctx context.Context) error {
	for _, doc := range b.TrackedDocuments() {
		reqCtx, cancel := context.WithTimeout(ctx, bridgeRouteTimeout)
		err := b.client.DidOpen(reqCtx, doc.URI, "go", doc.Text)
		cancel()
		if err != nil {
			return fmt.Errorf("lsp: replaying tracked document %s: %w", doc.URI, err)
		}
		b.resetVersionAfterReplay(doc.BufferID, doc.URI)
	}
	return nil
}

// HoverBuffer requests hover information for a tracked buffer at an
// editor byte-position. Any returned range is translated back into
// editor byte columns.
func (b *Bridge) HoverBuffer(ctx context.Context, id editor.BufferID, pos editor.Position) (*EditorHover, error) {
	doc, buf, text, seq, lspPos, err := b.trackedPosition(id, pos)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, bridgeRouteTimeout)
	defer cancel()
	hover, err := b.client.Hover(reqCtx, doc.uri, lspPos)
	if err != nil {
		return nil, err
	}
	if err := ensureBufferUnchanged(buf, id, seq, ErrBridgeBufferChangedDuringNavigation); err != nil {
		return nil, err
	}
	if hover == nil {
		return nil, nil
	}
	out := &EditorHover{Contents: hover.Contents}
	if hover.Range != nil {
		r, err := RangeToByteRange(text, *hover.Range)
		if err != nil {
			return nil, err
		}
		out.Range = &r
	}
	return out, nil
}

// CompleteBuffer requests LSP completion candidates for a tracked
// buffer at an editor byte-position.
//
// The buffer must already be open in the bridge. The supplied position
// is translated from editor byte columns to LSP UTF-16 character
// offsets against the current buffer snapshot. If the buffer mutates
// before the server responds, the result is rejected as stale.
func (b *Bridge) CompleteBuffer(ctx context.Context, id editor.BufferID, pos editor.Position) (*CompletionList, error) {
	doc, buf, _, seq, lspPos, err := b.trackedPosition(id, pos)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, bridgeRouteTimeout)
	defer cancel()
	list, err := b.client.Completion(reqCtx, doc.uri, lspPos)
	if err != nil {
		return nil, err
	}
	if err := ensureBufferUnchanged(buf, id, seq, ErrBridgeBufferChangedDuringCompletion); err != nil {
		return nil, err
	}
	return list, nil
}

// DefinitionBuffer requests definition locations for a tracked buffer
// at an editor byte-position and returns editor-native byte ranges.
func (b *Bridge) DefinitionBuffer(ctx context.Context, id editor.BufferID, pos editor.Position) ([]EditorLocation, error) {
	doc, buf, _, seq, lspPos, err := b.trackedPosition(id, pos)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, bridgeRouteTimeout)
	defer cancel()
	locations, err := b.client.Definition(reqCtx, doc.uri, lspPos)
	if err != nil {
		return nil, err
	}
	if err := ensureBufferUnchanged(buf, id, seq, ErrBridgeBufferChangedDuringNavigation); err != nil {
		return nil, err
	}
	return b.resolveLocations(locations)
}

// ReferencesBuffer requests reference locations for a tracked buffer at
// an editor byte-position and returns editor-native byte ranges.
func (b *Bridge) ReferencesBuffer(ctx context.Context, id editor.BufferID, pos editor.Position, includeDeclaration bool) ([]EditorLocation, error) {
	doc, buf, _, seq, lspPos, err := b.trackedPosition(id, pos)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, bridgeRouteTimeout)
	defer cancel()
	locations, err := b.client.References(reqCtx, doc.uri, lspPos, includeDeclaration)
	if err != nil {
		return nil, err
	}
	if err := ensureBufferUnchanged(buf, id, seq, ErrBridgeBufferChangedDuringNavigation); err != nil {
		return nil, err
	}
	return b.resolveLocations(locations)
}

func (b *Bridge) trackedPosition(id editor.BufferID, pos editor.Position) (*managedDoc, *editor.Buffer, string, int, Position, error) {
	b.mu.Lock()
	doc, ok := b.docs[id]
	b.mu.Unlock()
	if !ok {
		return nil, nil, "", 0, Position{}, fmt.Errorf("%w: id=%d", ErrBridgeDocumentNotTracked, id)
	}

	buf, err := b.engine.Buffer(id)
	if err != nil {
		return nil, nil, "", 0, Position{}, err
	}
	text, seq := bufferSnapshot(buf)
	lspPos, err := PositionFromBytePosition(text, pos.Line, pos.Column)
	if err != nil {
		return nil, nil, "", 0, Position{}, err
	}
	return doc, buf, text, seq, lspPos, nil
}

func bufferSnapshot(buf *editor.Buffer) (string, int) {
	for {
		seq := buf.Seq()
		text := buf.Text()
		if buf.Seq() == seq {
			return text, seq
		}
	}
}

func ensureBufferUnchanged(buf *editor.Buffer, id editor.BufferID, seq int, sentinel error) error {
	if got := buf.Seq(); got != seq {
		return fmt.Errorf("%w: id=%d before=%d after=%d", sentinel, id, seq, got)
	}
	return nil
}

type trackedDocumentRef struct {
	id      editor.BufferID
	uri     DocumentURI
	version int
}

func (b *Bridge) trackedDocumentRefs() []trackedDocumentRef {
	b.mu.Lock()
	refs := make([]trackedDocumentRef, 0, len(b.docs))
	for id, doc := range b.docs {
		refs = append(refs, trackedDocumentRef{
			id:      id,
			uri:     doc.uri,
			version: int(doc.version.Load()),
		})
	}
	b.mu.Unlock()
	sort.Slice(refs, func(i, j int) bool { return refs[i].id < refs[j].id })
	return refs
}

func (b *Bridge) resetVersionAfterReplay(id editor.BufferID, uri DocumentURI) {
	b.mu.Lock()
	doc, ok := b.docs[id]
	if ok && doc.uri == uri {
		doc.version.Store(1)
	}
	b.mu.Unlock()
}

func (b *Bridge) resolveLocations(locations []Location) ([]EditorLocation, error) {
	if len(locations) == 0 {
		return nil, nil
	}
	out := make([]EditorLocation, len(locations))
	for i, loc := range locations {
		resolved, err := b.resolveLocation(loc)
		if err != nil {
			return nil, err
		}
		out[i] = resolved
	}
	return out, nil
}

func (b *Bridge) resolveLocation(loc Location) (EditorLocation, error) {
	text, path, id, err := b.textForURI(loc.URI)
	if err != nil {
		return EditorLocation{}, err
	}
	r, err := RangeToByteRange(text, loc.Range)
	if err != nil {
		return EditorLocation{}, err
	}
	return EditorLocation{
		URI:      loc.URI,
		Path:     path,
		BufferID: id,
		Range:    r,
	}, nil
}

func (b *Bridge) textForURI(uri DocumentURI) (string, string, editor.BufferID, error) {
	path, err := PathFromDocumentURI(uri)
	if err != nil {
		return "", "", 0, err
	}

	b.mu.Lock()
	id, ok := b.byURI[uri]
	b.mu.Unlock()
	if ok {
		if buf, err := b.engine.Buffer(id); err == nil {
			text, _ := bufferSnapshot(buf)
			return text, path, id, nil
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", 0, fmt.Errorf("lsp: reading location target %s: %w", path, err)
	}
	return string(data), path, 0, nil
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
	case editor.BufferSaved:
		b.onBufferSaved(e)
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
	b.openDocument(e.ID, e.Path)
}

func (b *Bridge) openDocument(id editor.BufferID, path string) {
	uri, err := DocumentURIFromPath(path)
	if err != nil {
		b.logger.Warn("bridge: building URI",
			"path", path, "err", err,
		)
		return
	}

	buf, err := b.engine.Buffer(id)
	if err != nil {
		// Buffer was closed before we got to it. Nothing to do.
		b.logger.Debug("bridge: buffer disappeared before didOpen",
			"id", id, "err", err,
		)
		return
	}
	text := buf.Text()

	doc := &managedDoc{uri: uri}
	doc.version.Store(1)

	b.mu.Lock()
	// Defensive: if a buffer with this ID is somehow already tracked,
	// release its URI mapping before overwriting.
	if old, ok := b.docs[id]; ok {
		delete(b.byURI, old.uri)
	}
	b.docs[id] = doc
	b.byURI[uri] = id
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

// onBufferSaved reconciles bridge tracking after SaveAs. A plain save
// for an already tracked Go file is a no-op; SaveAs can move a tracked
// document to a new URI, turn an untitled Go buffer into a tracked
// document, or move a tracked Go buffer out of LSP scope.
func (b *Bridge) onBufferSaved(e editor.BufferSaved) {
	newIsGo := isGoFile(e.Path)
	var newURI DocumentURI
	if newIsGo {
		uri, err := DocumentURIFromPath(e.Path)
		if err != nil {
			b.logger.Warn("bridge: building URI after save",
				"path", e.Path, "err", err,
			)
			return
		}
		newURI = uri
	}

	b.mu.Lock()
	doc, tracked := b.docs[e.ID]
	b.mu.Unlock()

	switch {
	case !tracked && newIsGo:
		b.openDocument(e.ID, e.Path)
	case !tracked:
		return
	case !newIsGo:
		b.closeDocument(e.ID)
	case doc.uri != newURI:
		b.closeDocument(e.ID)
		b.openDocument(e.ID, e.Path)
	}
}

// onBufferClosed forwards BufferClosed as textDocument/didClose and
// removes the buffer from the bridge's tracking maps.
func (b *Bridge) onBufferClosed(e editor.BufferClosed) {
	b.closeDocument(e.ID)
}

func (b *Bridge) closeDocument(id editor.BufferID) {
	b.mu.Lock()
	doc, ok := b.docs[id]
	if ok {
		delete(b.docs, id)
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
	var currentVersion int
	if ok {
		if doc, tracked := b.docs[id]; tracked {
			currentVersion = int(doc.version.Load())
		}
	}
	b.mu.Unlock()
	if !ok {
		b.logger.Debug("bridge: diagnostics for unknown URI",
			"uri", ev.URI,
		)
		return
	}
	if ev.Version != nil && *ev.Version > 0 && currentVersion > 0 && *ev.Version < currentVersion {
		b.logger.Debug("bridge: dropping stale diagnostics",
			"uri", ev.URI,
			"diagnosticVersion", *ev.Version,
			"currentVersion", currentVersion,
		)
		return
	}

	text := ""
	if buf, err := b.engine.Buffer(id); err == nil {
		text, _ = bufferSnapshot(buf)
	}

	diags := make([]editor.Diagnostic, len(ev.Diagnostics))
	for i, d := range ev.Diagnostics {
		diags[i] = lspDiagnosticToEditor(text, d)
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
// editor-native form. When text is available, ranges are converted from
// LSP UTF-16 character columns into editor byte columns. If conversion
// fails (for example, a stale diagnostic races with local edits), the
// raw LSP coordinates are retained so the caller can still display the
// diagnostic instead of dropping it.
func lspDiagnosticToEditor(text string, d Diagnostic) editor.Diagnostic {
	r := lspRangeToEditor(d.Range)
	if text != "" {
		if converted, err := RangeToByteRange(text, d.Range); err == nil {
			r = converted
		}
	}
	return editor.Diagnostic{
		Range:    r,
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
