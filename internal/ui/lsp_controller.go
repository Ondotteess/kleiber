package ui

import (
	"context"
	"sync"

	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/lsp"
)

const lspControllerEventBuffer = 64

// LSPController is the UI's window onto the language server. It keeps a
// live per-buffer diagnostics store fed by the editor engine's
// BufferDiagnostics events (which the LSP bridge publishes), exposes the
// server's health for a status bar, and forwards completion/hover/
// definition requests to the current bridge. It imports no gioui packages
// so it stays unit-testable.
//
// The supervisor may be nil (no language server): requests then report
// lsp.ErrServerNotReady and the state reads as stopped.
type LSPController struct {
	supervisor *lsp.Supervisor
	engine     *editor.EditorEngine

	mu    sync.RWMutex
	diags map[editor.BufferID][]editor.Diagnostic

	cancel func()
	stop   chan struct{}
	wg     sync.WaitGroup
	once   sync.Once
}

// NewLSPController subscribes to the engine's diagnostic events and starts
// a goroutine that maintains the diagnostics store until Close. engine is
// required; supervisor may be nil.
func NewLSPController(supervisor *lsp.Supervisor, engine *editor.EditorEngine) *LSPController {
	c := &LSPController{
		supervisor: supervisor,
		engine:     engine,
		diags:      map[editor.BufferID][]editor.Diagnostic{},
		stop:       make(chan struct{}),
	}
	if engine != nil {
		events, cancel := engine.Events().Subscribe(lspControllerEventBuffer)
		c.cancel = cancel
		c.wg.Add(1)
		go c.run(events)
	}
	return c
}

// run consumes engine events, recording the latest diagnostics per buffer
// and dropping a buffer's entry when it closes. It exits on Close via the
// stop channel: the events bus does not close subscriber channels, so
// ranging over the channel alone would never return.
func (c *LSPController) run(events <-chan editor.BufferEvent) {
	defer c.wg.Done()
	for {
		select {
		case <-c.stop:
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			c.handleEvent(ev)
		}
	}
}

func (c *LSPController) handleEvent(ev editor.BufferEvent) {
	switch e := ev.(type) {
	case editor.BufferDiagnostics:
		c.mu.Lock()
		if len(e.Diagnostics) == 0 {
			delete(c.diags, e.ID)
		} else {
			c.diags[e.ID] = append([]editor.Diagnostic(nil), e.Diagnostics...)
		}
		c.mu.Unlock()
	case editor.BufferClosed:
		c.mu.Lock()
		delete(c.diags, e.ID)
		c.mu.Unlock()
	}
}

// Diagnostics returns a copy of the current diagnostics for a buffer, or
// nil when there are none.
func (c *LSPController) Diagnostics(id editor.BufferID) []editor.Diagnostic {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.diags[id]
	if !ok {
		return nil
	}
	return append([]editor.Diagnostic(nil), d...)
}

// DiagnosticCount reports how many diagnostics a buffer currently has.
func (c *LSPController) DiagnosticCount(id editor.BufferID) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.diags[id])
}

// State returns the language server's lifecycle state, or
// ServerStateStopped when there is no supervisor.
func (c *LSPController) State() lsp.ServerState {
	if c.supervisor == nil {
		return lsp.ServerStateStopped
	}
	return c.supervisor.State()
}

// Supervisor returns the underlying supervisor (may be nil).
func (c *LSPController) Supervisor() *lsp.Supervisor { return c.supervisor }

// bridge returns the current LSP bridge, or nil when no session is ready.
func (c *LSPController) bridge() *lsp.Bridge {
	if c.supervisor == nil {
		return nil
	}
	return c.supervisor.Bridge()
}

// Complete requests completion candidates at pos in the given buffer.
func (c *LSPController) Complete(ctx context.Context, id editor.BufferID, pos editor.Position) (*lsp.CompletionList, error) {
	b := c.bridge()
	if b == nil {
		return nil, lsp.ErrServerNotReady
	}
	return b.CompleteBuffer(ctx, id, pos)
}

// Hover requests hover information at pos in the given buffer.
func (c *LSPController) Hover(ctx context.Context, id editor.BufferID, pos editor.Position) (*lsp.EditorHover, error) {
	b := c.bridge()
	if b == nil {
		return nil, lsp.ErrServerNotReady
	}
	return b.HoverBuffer(ctx, id, pos)
}

// Definition requests definition locations at pos in the given buffer.
func (c *LSPController) Definition(ctx context.Context, id editor.BufferID, pos editor.Position) ([]lsp.EditorLocation, error) {
	b := c.bridge()
	if b == nil {
		return nil, lsp.ErrServerNotReady
	}
	return b.DefinitionBuffer(ctx, id, pos)
}

// References requests all reference locations for the symbol at pos in
// the given buffer, including the declaration itself.
func (c *LSPController) References(ctx context.Context, id editor.BufferID, pos editor.Position) ([]lsp.EditorLocation, error) {
	b := c.bridge()
	if b == nil {
		return nil, lsp.ErrServerNotReady
	}
	return b.ReferencesBuffer(ctx, id, pos, true)
}

// Format formats the given buffer in place via the language server and
// reports how many text edits were applied.
func (c *LSPController) Format(ctx context.Context, id editor.BufferID, opts lsp.FormattingOptions) (int, error) {
	b := c.bridge()
	if b == nil {
		return 0, lsp.ErrServerNotReady
	}
	return b.FormatBuffer(ctx, id, opts)
}

// Close stops the diagnostics subscription and waits for its goroutine.
// It does not stop the supervisor, whose lifecycle the caller owns. Close
// is idempotent.
func (c *LSPController) Close() {
	c.once.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		close(c.stop)
		c.wg.Wait()
	})
}
