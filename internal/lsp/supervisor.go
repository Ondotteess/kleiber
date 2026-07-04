package lsp

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/events"
	"github.com/Ondotteess/kleiber/internal/logging"
)

// Supervisor restart policy defaults. A crashed gopls is restarted with
// exponential backoff between baseBackoff and maxBackoff; after
// maxRestarts consecutive rapid failures the supervisor gives up and
// reports ServerStateFailed. A session that stays ready past
// stableThreshold resets the failure count, so an isolated crash after
// hours of use does not count toward the cap.
const (
	defaultMaxRestarts     = 5
	defaultBaseBackoff     = 500 * time.Millisecond
	defaultMaxBackoff      = 30 * time.Second
	defaultStableThreshold = 30 * time.Second
	supervisorStopTimeout  = 5 * time.Second
)

// ErrSupervisorNoEngine is returned by NewSupervisor when Options.Engine
// is nil; the supervisor mirrors editor buffers onto gopls and cannot
// operate without an engine.
var ErrSupervisorNoEngine = errors.New("lsp: supervisor requires an editor engine")

// ErrServerNotReady is returned by request helpers when no gopls session
// is currently ready (starting, restarting, or failed).
var ErrServerNotReady = errors.New("lsp: language server not ready")

// ServerState is the lifecycle state of the supervised gopls process.
type ServerState int

// Server lifecycle states. Do not renumber: the zero value is the
// pre-start "stopped" state so a freshly built Supervisor reads sensibly.
const (
	ServerStateStopped ServerState = iota
	ServerStateStarting
	ServerStateReady
	ServerStateRestarting
	ServerStateFailed
)

// String returns a lower-case label suitable for logs and a status bar.
func (s ServerState) String() string {
	switch s {
	case ServerStateStopped:
		return "stopped"
	case ServerStateStarting:
		return "starting"
	case ServerStateReady:
		return "ready"
	case ServerStateRestarting:
		return "restarting"
	case ServerStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// StateEvent is published on Supervisor.States whenever the supervised
// server changes state. Attempt is the consecutive-failure count (0 when
// ready); Err carries the reason for Restarting and Failed.
type StateEvent struct {
	State   ServerState
	Pid     int
	Attempt int
	Err     string
}

// connectFunc starts one gopls session and returns a live client and the
// bridge wired to it and to the engine. The supervisor calls it for the
// initial start and every restart. Returning an error triggers the
// backoff-and-retry path.
type connectFunc func(ctx context.Context) (*Client, *Bridge, error)

// SupervisorOptions configures NewSupervisor.
type SupervisorOptions struct {
	// Engine is the editor engine whose Go buffers are mirrored onto
	// gopls. Required.
	Engine *editor.EditorEngine

	// Client is the template applied to every gopls client the
	// supervisor spawns (binary path, workspace folders, ...). Its
	// Logger is overridden with the supervisor's logger.
	Client ClientOptions

	// Bridge is the template applied to every Bridge the supervisor
	// builds. Its Logger is overridden with the supervisor's logger.
	Bridge BridgeOptions

	// Logger receives structured records. Nil means discard.
	Logger *slog.Logger

	// MaxRestarts caps consecutive rapid restarts before the supervisor
	// reports ServerStateFailed. Zero uses the default.
	MaxRestarts int

	// BaseBackoff and MaxBackoff bound the exponential restart delay.
	// Zero uses the defaults.
	BaseBackoff, MaxBackoff time.Duration

	// StableThreshold is how long a session must stay ready before a
	// later crash resets the failure count. Zero uses the default.
	StableThreshold time.Duration

	// connect and sleep are test seams. Production leaves them nil.
	connect connectFunc
	sleep   func(ctx context.Context, d time.Duration) bool
}

// Supervisor owns the lifecycle of a gopls client and its editor bridge,
// restarting them when gopls crashes and mirroring the engine's open Go
// buffers onto each fresh session. It publishes state transitions so a
// status bar can reflect the language server's health, and it satisfies
// app.BufferFormatter by delegating to the current bridge.
type Supervisor struct {
	engine     *editor.EditorEngine
	clientOpts ClientOptions
	bridgeOpts BridgeOptions
	logger     *slog.Logger

	maxRestarts     int
	baseBackoff     time.Duration
	maxBackoff      time.Duration
	stableThreshold time.Duration

	connect connectFunc
	sleep   func(ctx context.Context, d time.Duration) bool

	states *events.Topic[StateEvent]

	ctx    context.Context
	cancel context.CancelFunc

	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup

	mu     sync.RWMutex
	client *Client
	bridge *Bridge
	state  ServerState
}

// NewSupervisor constructs a Supervisor. It performs no I/O; call Start
// to spawn the first gopls session.
func NewSupervisor(opts SupervisorOptions) (*Supervisor, error) {
	if opts.Engine == nil {
		return nil, ErrSupervisorNoEngine
	}
	logger := opts.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	s := &Supervisor{
		engine:          opts.Engine,
		clientOpts:      opts.Client,
		bridgeOpts:      opts.Bridge,
		logger:          logger,
		maxRestarts:     orDefaultInt(opts.MaxRestarts, defaultMaxRestarts),
		baseBackoff:     orDefaultDuration(opts.BaseBackoff, defaultBaseBackoff),
		maxBackoff:      orDefaultDuration(opts.MaxBackoff, defaultMaxBackoff),
		stableThreshold: orDefaultDuration(opts.StableThreshold, defaultStableThreshold),
		connect:         opts.connect,
		sleep:           opts.sleep,
		states:          events.NewTopic[StateEvent]("lsp.serverState", logger),
	}
	if s.connect == nil {
		s.connect = s.defaultConnect
	}
	if s.sleep == nil {
		s.sleep = sleepUntil
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	return s, nil
}

// States returns the topic that receives a StateEvent on every server
// lifecycle transition. Subscribe before Start to catch the first
// Starting event.
func (s *Supervisor) States() *events.Topic[StateEvent] {
	return s.states
}

// State returns the current server state as a defensive read.
func (s *Supervisor) State() ServerState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Bridge returns the bridge for the current session, or nil when no
// session is ready (before the first start, during a restart, or after
// failure). Callers must tolerate nil and re-read on each use rather
// than caching, because a restart swaps the bridge.
func (s *Supervisor) Bridge() *Bridge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bridge
}

// Start launches the supervision loop. It returns immediately; watch
// States (or poll State) for readiness. Calling Start more than once is
// a no-op.
func (s *Supervisor) Start() {
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go s.run()
	})
}

// Stop tears down the current session and the supervision loop, then
// waits for both to finish. It is idempotent. The passed context bounds
// how long the caller is willing to wait; the graceful gopls shutdown
// itself is separately bounded.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.cancel()
	})
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		s.states.Close()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// FormatAndSaveBuffer satisfies app.BufferFormatter by delegating to the
// current bridge. It returns ErrServerNotReady when no session is ready.
func (s *Supervisor) FormatAndSaveBuffer(ctx context.Context, id editor.BufferID, opts FormattingOptions) (int, error) {
	bridge := s.Bridge()
	if bridge == nil {
		return 0, ErrServerNotReady
	}
	return bridge.FormatAndSaveBuffer(ctx, id, opts)
}

// run is the supervision loop: connect, serve until crash or shutdown,
// then restart with backoff or give up.
func (s *Supervisor) run() {
	defer s.wg.Done()

	attempt := 0
	backoff := s.baseBackoff

	for {
		if s.ctx.Err() != nil {
			s.setState(StateEvent{State: ServerStateStopped})
			return
		}
		s.setState(StateEvent{State: ServerStateStarting, Attempt: attempt})

		client, bridge, err := s.connect(s.ctx)
		if err != nil {
			if s.ctx.Err() != nil {
				s.setState(StateEvent{State: ServerStateStopped})
				return
			}
			if isPermanentStartError(err) {
				s.logger.Error("gopls unavailable; not retrying", "err", err)
				s.setState(StateEvent{State: ServerStateFailed, Attempt: attempt, Err: err.Error()})
				return
			}
			attempt++
			if attempt > s.maxRestarts {
				s.logger.Error("gopls failed to start; giving up", "attempts", attempt, "err", err)
				s.setState(StateEvent{State: ServerStateFailed, Attempt: attempt, Err: err.Error()})
				return
			}
			s.logger.Warn("gopls failed to start; will retry", "attempt", attempt, "err", err)
			s.setState(StateEvent{State: ServerStateRestarting, Attempt: attempt, Err: err.Error()})
			if !s.sleep(s.ctx, backoff) {
				s.setState(StateEvent{State: ServerStateStopped})
				return
			}
			backoff = nextBackoff(backoff, s.maxBackoff)
			continue
		}

		s.mu.Lock()
		s.client, s.bridge, s.state = client, bridge, ServerStateReady
		s.mu.Unlock()
		s.setState(StateEvent{State: ServerStateReady, Pid: client.Pid()})
		readyAt := time.Now()

		select {
		case <-s.ctx.Done():
			s.shutdown(client, bridge)
			s.setState(StateEvent{State: ServerStateStopped})
			return
		case <-client.Done():
			intentional := client.Stopping() || s.ctx.Err() != nil
			s.shutdown(client, bridge)
			if intentional {
				s.setState(StateEvent{State: ServerStateStopped})
				return
			}
			if time.Since(readyAt) >= s.stableThreshold {
				attempt = 0
				backoff = s.baseBackoff
			}
			attempt++
			if attempt > s.maxRestarts {
				s.logger.Error("gopls keeps crashing; giving up", "attempts", attempt)
				s.setState(StateEvent{State: ServerStateFailed, Attempt: attempt, Err: "gopls exited repeatedly"})
				return
			}
			s.logger.Warn("gopls exited unexpectedly; restarting", "attempt", attempt)
			s.setState(StateEvent{State: ServerStateRestarting, Attempt: attempt, Err: "gopls exited"})
			if !s.sleep(s.ctx, backoff) {
				s.setState(StateEvent{State: ServerStateStopped})
				return
			}
			backoff = nextBackoff(backoff, s.maxBackoff)
			continue
		}
	}
}

// shutdown closes the bridge, stops the client, and clears the current
// session pointers. It is safe for both the crash path (client already
// dead; Stop reaps the process) and the graceful path.
func (s *Supervisor) shutdown(client *Client, bridge *Bridge) {
	bridge.Close()
	ctx, cancel := context.WithTimeout(context.Background(), supervisorStopTimeout)
	defer cancel()
	if err := client.Stop(ctx); err != nil {
		s.logger.Warn("stopping gopls", "err", err)
	}
	s.mu.Lock()
	s.client, s.bridge = nil, nil
	s.mu.Unlock()
}

// defaultConnect is the production connectFunc: spawn gopls, run the
// handshake, wire a bridge, and mirror the engine's open Go buffers.
func (s *Supervisor) defaultConnect(ctx context.Context) (*Client, *Bridge, error) {
	clientOpts := s.clientOpts
	clientOpts.Logger = s.logger
	client := NewClient(clientOpts)
	if err := client.Start(ctx); err != nil {
		return nil, nil, err
	}

	bridgeOpts := s.bridgeOpts
	bridgeOpts.Logger = s.logger
	// Parent ctx is the supervisor's, so a supervisor Stop is a backstop
	// that tears down any bridge the restart path did not already close.
	bridge := NewBridge(s.ctx, bridgeOpts, client, s.engine)
	bridge.OpenExistingBuffers(ctx)
	return client, bridge, nil
}

// setState records the new state and publishes it, dropping the event if
// subscribers are saturated rather than stalling the loop.
func (s *Supervisor) setState(ev StateEvent) {
	s.mu.Lock()
	s.state = ev.State
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), diagnosticsPublishTimeout)
	defer cancel()
	if err := s.states.Publish(ctx, ev); err != nil && !errors.Is(err, events.ErrTopicClosed) {
		s.logger.Debug("publishing server state", "state", ev.State, "err", err)
	}
}

// isPermanentStartError reports whether a start failure is not worth
// retrying — chiefly gopls missing from PATH, which no amount of backoff
// will fix.
func isPermanentStartError(err error) bool {
	return errors.Is(err, exec.ErrNotFound)
}

// nextBackoff doubles d, capped at max.
func nextBackoff(d, max time.Duration) time.Duration {
	next := d * 2
	if next > max {
		return max
	}
	return next
}

// sleepUntil sleeps for d or until ctx is canceled. It returns true if
// the full duration elapsed, false if ctx fired first.
func sleepUntil(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func orDefaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func orDefaultDuration(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}
