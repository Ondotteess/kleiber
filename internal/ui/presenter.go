package ui

import (
	"context"
	"sync"

	"github.com/Ondotteess/kleiber/internal/app"
	"github.com/Ondotteess/kleiber/internal/editor"
)

const (
	defaultPresenterEventBuffer  = 64
	defaultPresenterUpdateBuffer = 1
)

// PresenterOptions configures NewPresenter.
type PresenterOptions struct {
	// EventBuffer sizes the subscription to editor events.
	EventBuffer int

	// UpdateBuffer sizes the update notification channel. Defaults to one so
	// repeated editor events coalesce while the future UI is busy.
	UpdateBuffer int
}

// Presenter owns the current UI State snapshot and emits lightweight update
// signals when editor events indicate the state may be stale. It does not render
// anything and imports no gioui packages.
type Presenter struct {
	session *app.Session

	mu     sync.RWMutex
	state  State
	dirty  bool
	closed bool

	updates chan struct{}
	done    chan struct{}
	cancel  func()
	once    sync.Once
	wg      sync.WaitGroup
}

// NewPresenter builds an initial State, subscribes to editor events, and starts
// a small event loop that coalesces future update signals.
func NewPresenter(session *app.Session, opts PresenterOptions) (*Presenter, error) {
	if session == nil {
		return nil, ErrNilSession
	}
	state, err := BuildState(session)
	if err != nil {
		return nil, err
	}
	eventBuffer := opts.EventBuffer
	if eventBuffer <= 0 {
		eventBuffer = defaultPresenterEventBuffer
	}
	updateBuffer := opts.UpdateBuffer
	if updateBuffer <= 0 {
		updateBuffer = defaultPresenterUpdateBuffer
	}

	events, cancel := session.SubscribeEditorEvents(eventBuffer)
	p := &Presenter{
		session: session,
		state:   cloneState(state),
		updates: make(chan struct{}, updateBuffer),
		done:    make(chan struct{}),
		cancel:  cancel,
	}
	p.wg.Add(1)
	go p.run(events)
	return p, nil
}

// State returns the presenter's current defensive State snapshot.
func (p *Presenter) State() State {
	if p == nil {
		return State{}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return cloneState(p.state)
}

// Updates returns a channel that receives coalesced state-stale signals.
func (p *Presenter) Updates() <-chan struct{} {
	if p == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return p.updates
}

// Dirty reports whether editor events have arrived since the last successful
// Refresh. It is a hint for UI scheduling, not a rendering command.
func (p *Presenter) Dirty() bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dirty
}

// Refresh rebuilds the current State from app.Session snapshots.
func (p *Presenter) Refresh(ctx context.Context) error {
	if p == nil || p.session == nil {
		return ErrNilSession
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	state, err := BuildState(p.session)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.state = cloneState(state)
	p.dirty = false
	return nil
}

// Close unsubscribes from editor events and waits for the event loop to exit.
// Close is idempotent.
func (p *Presenter) Close() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		if p.cancel != nil {
			p.cancel()
		}
		close(p.done)
		p.wg.Wait()
		close(p.updates)
	})
}

func (p *Presenter) run(events <-chan editor.BufferEvent) {
	defer p.wg.Done()
	for {
		select {
		case <-p.done:
			return
		case _, ok := <-events:
			if !ok {
				return
			}
			p.markDirty()
		}
	}
}

func (p *Presenter) markDirty() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.dirty = true
	p.mu.Unlock()

	p.emitUpdate()
}

func (p *Presenter) emitUpdate() {
	if p == nil {
		return
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return
	}
	select {
	case p.updates <- struct{}{}:
	default:
	}
}
