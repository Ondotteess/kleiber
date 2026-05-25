package editor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Ondotteess/kleiber/internal/events"
	"github.com/Ondotteess/kleiber/internal/logging"
)

// engineEventPublishTimeout caps how long EditorEngine waits for
// subscribers to accept one event before giving up and logging a
// warning. Mutations on the buffer block synchronously until publish
// returns; if you have a slow subscriber that consistently misses
// this window, the editor will feel sluggish — fix the subscriber,
// don't raise the timeout.
const engineEventPublishTimeout = 5 * time.Second

// Sentinel errors returned by EditorEngine.
var (
	// ErrBufferNotFound is returned by methods that take a
	// BufferID when no such buffer is registered.
	ErrBufferNotFound = errors.New("editor: buffer not found")

	// ErrBufferUntitled is returned by Save when the buffer has
	// no associated path. Use SaveAs to assign one.
	ErrBufferUntitled = errors.New("editor: buffer has no path; use SaveAs")

	// ErrEmptySavePath is returned by SaveAs with an empty path.
	ErrEmptySavePath = errors.New("editor: empty save path")
)

// BufferID is an opaque handle for one buffer registered with an
// EditorEngine. IDs are monotonic per engine; they are not
// recycled when a buffer is closed.
type BufferID int64

// BufferRef is a lightweight snapshot of a registered buffer,
// returned by Buffers().
type BufferRef struct {
	ID    BufferID
	Path  string // empty for untitled
	Dirty bool
}

// BufferEvent is one of BufferOpened, BufferChanged, BufferClosed,
// BufferSaved. The set is sealed by an unexported method on each
// concrete type.
type BufferEvent interface {
	bufferEvent()
}

// BufferOpened is emitted by Open / NewBuffer when a buffer enters
// the engine's registry. Path is empty for untitled buffers.
type BufferOpened struct {
	ID   BufferID
	Path string
}

func (BufferOpened) bufferEvent() {}

// BufferChanged wraps a Change observed on a managed buffer. It is
// emitted for every Insert / Delete / Undo / Redo that mutates the
// buffer.
type BufferChanged struct {
	ID     BufferID
	Change Change
}

func (BufferChanged) bufferEvent() {}

// BufferClosed is emitted by Close after the buffer has been
// removed from the registry. Path reports the buffer's last known
// path (empty if it was untitled).
type BufferClosed struct {
	ID   BufferID
	Path string
}

func (BufferClosed) bufferEvent() {}

// BufferSaved is emitted by Save and SaveAs after the buffer's
// contents have been written to disk.
type BufferSaved struct {
	ID   BufferID
	Path string
}

func (BufferSaved) bufferEvent() {}

// EngineOptions configures NewEngine.
type EngineOptions struct {
	// Logger receives structured records. Nil maps to a discard
	// logger.
	Logger *slog.Logger
}

// EditorEngine is the orchestrator of open buffers. It owns the
// registry, performs file I/O, and bridges per-buffer Change
// observers onto a single typed event bus the rest of Kleiber
// subscribes to (UI, LSP didChange, etc.).
//
// EditorEngine is safe for concurrent use. Mutations on different
// buffers proceed in parallel; mutations on the same buffer are
// serialized by the buffer's internal mutex, and saves are serialized
// per buffer so two concurrent SaveAs calls cannot interleave
// their temp-rename dance.
//
// Slow subscribers: publish is synchronous with a five-second
// timeout. A truly stuck subscriber will make mutations block up
// to five seconds and then proceed (the event is dropped). Fix
// the subscriber if you see those warnings in the log.
type EditorEngine struct {
	logger *slog.Logger
	events *events.Topic[BufferEvent]

	nextID atomic.Int64

	mu      sync.RWMutex
	buffers map[BufferID]*managedBuffer
}

// managedBuffer is the engine's record for one Buffer.
type managedBuffer struct {
	id  BufferID
	buf *Buffer

	pathMu sync.Mutex
	path   string // empty for untitled

	cleanMu   sync.RWMutex
	cleanText string

	// writeMu serializes Save / SaveAs on this buffer so two
	// concurrent saves cannot interleave their atomic-rename
	// dance.
	writeMu sync.Mutex
}

// NewEngine constructs an EditorEngine. The returned engine has no
// registered buffers.
func NewEngine(opts EngineOptions) *EditorEngine {
	logger := opts.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	return &EditorEngine{
		logger:  logger,
		events:  events.NewTopic[BufferEvent]("editor.buffer", logger),
		buffers: map[BufferID]*managedBuffer{},
	}
}

// Events returns the typed topic that carries every BufferEvent
// emitted by the engine. Multiple subscribers may subscribe; each
// receives every event independently.
func (e *EditorEngine) Events() *events.Topic[BufferEvent] {
	return e.events
}

// Open reads path from disk, constructs a Buffer with its
// contents, and registers it with the engine.
//
// The buffer's initial text is the raw bytes of the file as-is;
// encoding detection / conversion is intentionally out of scope.
//
// Errors wrap fs.ErrNotExist when path does not exist.
func (e *EditorEngine) Open(ctx context.Context, path string) (BufferID, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, fmt.Errorf("editor: resolving %s: %w", path, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return 0, fmt.Errorf("editor: reading %s: %w", abs, err)
	}
	return e.register(NewBuffer(string(data)), abs), nil
}

// NewBuffer registers an untitled buffer initialized to text. Use
// SaveAs to give it a path.
func (e *EditorEngine) NewBuffer(text string) BufferID {
	return e.register(NewBuffer(text), "")
}

// register assigns a BufferID, wires the per-buffer observer that
// forwards Changes onto the engine's event topic, and publishes
// the BufferOpened event.
func (e *EditorEngine) register(buf *Buffer, path string) BufferID {
	id := BufferID(e.nextID.Add(1))
	mb := &managedBuffer{
		id:        id,
		buf:       buf,
		path:      path,
		cleanText: buf.Text(),
	}

	// Capture id so each managed buffer's changes are tagged
	// correctly on the wire.
	buf.Observe(func(ch Change) {
		e.mu.RLock()
		_, ok := e.buffers[id]
		e.mu.RUnlock()
		if !ok {
			return
		}
		e.publish(BufferChanged{ID: id, Change: ch})
	})

	e.mu.Lock()
	e.buffers[id] = mb
	e.mu.Unlock()

	e.publish(BufferOpened{ID: id, Path: path})
	return id
}

// Close removes the buffer with the given ID from the registry and
// publishes a BufferClosed event. Returns ErrBufferNotFound if no
// such buffer is registered.
//
// Close does not touch disk and does not affect the underlying
// *Buffer — callers that still hold the pointer can keep using it
// (but the engine will no longer publish events from it).
func (e *EditorEngine) Close(id BufferID) error {
	e.mu.Lock()
	mb, ok := e.buffers[id]
	if !ok {
		e.mu.Unlock()
		return fmt.Errorf("%w: id=%d", ErrBufferNotFound, id)
	}
	delete(e.buffers, id)
	e.mu.Unlock()

	mb.pathMu.Lock()
	path := mb.path
	mb.pathMu.Unlock()

	e.publish(BufferClosed{ID: id, Path: path})
	return nil
}

// Buffer returns the *Buffer for id. The returned pointer is the
// live buffer; mutations on it fire the engine's events through
// the observer the engine registered at Open / NewBuffer time.
//
// Callers should not mutate the buffer after the engine has Closed
// it (no observer will be there to publish events), but doing so
// is not flagged as an error.
func (e *EditorEngine) Buffer(id BufferID) (*Buffer, error) {
	e.mu.RLock()
	mb, ok := e.buffers[id]
	e.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: id=%d", ErrBufferNotFound, id)
	}
	return mb.buf, nil
}

// Path returns the on-disk path of the buffer with the given ID,
// or "" if it is untitled.
func (e *EditorEngine) Path(id BufferID) (string, error) {
	e.mu.RLock()
	mb, ok := e.buffers[id]
	e.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("%w: id=%d", ErrBufferNotFound, id)
	}
	mb.pathMu.Lock()
	defer mb.pathMu.Unlock()
	return mb.path, nil
}

// Buffers returns a snapshot of registered buffers, sorted by ID
// (open-order, since IDs are monotonic). The returned slice is a
// fresh copy; mutating it does not affect the engine.
func (e *EditorEngine) Buffers() []BufferRef {
	e.mu.RLock()
	refs := make([]BufferRef, 0, len(e.buffers))
	for id, mb := range e.buffers {
		mb.pathMu.Lock()
		path := mb.path
		mb.pathMu.Unlock()
		refs = append(refs, BufferRef{ID: id, Path: path, Dirty: mb.dirty()})
	}
	e.mu.RUnlock()
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	return refs
}

// Dirty reports whether the buffer differs from its last successful
// Open, Save, or SaveAs snapshot. Untitled buffers are dirty until
// SaveAs assigns an on-disk path.
func (e *EditorEngine) Dirty(id BufferID) (bool, error) {
	e.mu.RLock()
	mb, ok := e.buffers[id]
	e.mu.RUnlock()
	if !ok {
		return false, fmt.Errorf("%w: id=%d", ErrBufferNotFound, id)
	}
	return mb.dirty(), nil
}

// Save writes the buffer's current contents to its associated path
// atomically (temp file + rename). The buffer must already have a
// path; if not, Save returns ErrBufferUntitled and SaveAs must be
// used instead.
func (e *EditorEngine) Save(ctx context.Context, id BufferID) error {
	e.mu.RLock()
	mb, ok := e.buffers[id]
	e.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: id=%d", ErrBufferNotFound, id)
	}

	mb.writeMu.Lock()
	defer mb.writeMu.Unlock()

	mb.pathMu.Lock()
	path := mb.path
	mb.pathMu.Unlock()
	if path == "" {
		return ErrBufferUntitled
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	text := mb.buf.Text()
	if err := atomicWriteFile(path, []byte(text)); err != nil {
		return fmt.Errorf("editor: saving %s: %w", path, err)
	}
	mb.markClean(text)

	e.publish(BufferSaved{ID: id, Path: path})
	return nil
}

// SaveAs writes the buffer to path (overriding any current path)
// and updates the buffer's associated path on success.
func (e *EditorEngine) SaveAs(ctx context.Context, id BufferID, path string) error {
	if path == "" {
		return ErrEmptySavePath
	}
	e.mu.RLock()
	mb, ok := e.buffers[id]
	e.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: id=%d", ErrBufferNotFound, id)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("editor: resolving %s: %w", path, err)
	}

	mb.writeMu.Lock()
	defer mb.writeMu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	text := mb.buf.Text()
	if err := atomicWriteFile(abs, []byte(text)); err != nil {
		return fmt.Errorf("editor: saving %s: %w", abs, err)
	}

	mb.pathMu.Lock()
	mb.path = abs
	mb.pathMu.Unlock()
	mb.markClean(text)

	e.publish(BufferSaved{ID: id, Path: abs})
	return nil
}

func (mb *managedBuffer) dirty() bool {
	mb.pathMu.Lock()
	untitled := mb.path == ""
	mb.pathMu.Unlock()
	if untitled {
		return true
	}

	text := mb.buf.Text()
	mb.cleanMu.RLock()
	clean := mb.cleanText
	mb.cleanMu.RUnlock()
	return text != clean
}

func (mb *managedBuffer) markClean(text string) {
	mb.cleanMu.Lock()
	defer mb.cleanMu.Unlock()
	mb.cleanText = text
}

// publish sends ev to the engine's event topic with a bounded
// timeout (engineEventPublishTimeout). On timeout it logs a warning
// and returns; the event is dropped.
func (e *EditorEngine) publish(ev BufferEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), engineEventPublishTimeout)
	defer cancel()
	if err := e.events.Publish(ctx, ev); err != nil {
		e.logger.Warn("publishing buffer event", "err", err)
	}
}
