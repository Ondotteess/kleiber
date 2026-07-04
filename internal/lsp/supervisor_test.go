package lsp

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/Ondotteess/kleiber/internal/editor"
)

// fakeGopls is a controllable connectFunc source for supervisor tests. It
// hands out clients wired to in-process fake servers over net.Pipe, so a
// test can "crash" the current session by closing its server, and can
// script the next N connect attempts to fail.
type fakeGopls struct {
	t      *testing.T
	engine *editor.EditorEngine

	mu       sync.Mutex
	servers  []*fakeServer
	failNext int
	failErr  error
	connects int
}

func newFakeGopls(t *testing.T, engine *editor.EditorEngine) *fakeGopls {
	t.Helper()
	return &fakeGopls{t: t, engine: engine}
}

func (f *fakeGopls) connect(ctx context.Context) (*Client, *Bridge, error) {
	f.mu.Lock()
	f.connects++
	if f.failNext > 0 {
		f.failNext--
		err := f.failErr
		f.mu.Unlock()
		return nil, nil, err
	}
	f.mu.Unlock()

	clientNet, serverNet := net.Pipe()
	server := newFakeServer(f.t, serverNet)
	go server.Run()

	client := NewClient(ClientOptions{Logger: testLogger(f.t)})
	client.started.Store(true)
	conn := NewConn(ConnOptions{
		Reader: clientNet,
		Writer: clientNet,
		Closer: clientNet,
		Logger: testLogger(f.t),
	})
	hctx, cancel := context.WithTimeout(ctx, testHandshakeTimeout)
	defer cancel()
	if err := client.runWithConn(hctx, conn); err != nil {
		server.CloseAndWait()
		return nil, nil, err
	}

	bridge := NewBridge(ctx, BridgeOptions{Logger: testLogger(f.t)}, client, f.engine)

	f.mu.Lock()
	f.servers = append(f.servers, server)
	f.mu.Unlock()
	return client, bridge, nil
}

// crashCurrent closes the most recently created fake server so the live
// client's read loop dies, simulating a gopls crash.
func (f *fakeGopls) crashCurrent() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.servers) > 0 {
		f.servers[len(f.servers)-1].CloseAndWait()
	}
}

func (f *fakeGopls) connectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connects
}

// stateCollector drains a supervisor's state topic into a slice so tests
// can wait for conditions without racing on the publish path.
type stateCollector struct {
	mu     sync.Mutex
	events []StateEvent
	done   chan struct{}
}

func collectStates(t *testing.T, s *Supervisor) *stateCollector {
	t.Helper()
	sub, cancel := s.States().Subscribe(64)
	topicDone := s.States().Done()
	c := &stateCollector{done: make(chan struct{})}
	stop := make(chan struct{})
	go func() {
		defer close(c.done)
		for {
			select {
			case ev := <-sub:
				c.mu.Lock()
				c.events = append(c.events, ev)
				c.mu.Unlock()
			case <-topicDone:
				return
			case <-stop:
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		cancel()
		<-c.done
	})
	return c
}

// waitFor polls until pred is satisfied by the collected events or the
// deadline passes.
func (c *stateCollector) waitFor(t *testing.T, pred func([]StateEvent) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		snapshot := append([]StateEvent(nil), c.events...)
		c.mu.Unlock()
		if pred(snapshot) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	c.mu.Lock()
	got := append([]StateEvent(nil), c.events...)
	c.mu.Unlock()
	t.Fatalf("timed out waiting for state condition; saw %v", got)
}

func countState(events []StateEvent, state ServerState) int {
	n := 0
	for _, ev := range events {
		if ev.State == state {
			n++
		}
	}
	return n
}

// instantSleep is a sleep seam that never actually waits, so backoff does
// not slow the tests. It honors cancellation.
func instantSleep(ctx context.Context, _ time.Duration) bool {
	return ctx.Err() == nil
}

func newTestSupervisor(t *testing.T, opts SupervisorOptions) *Supervisor {
	t.Helper()
	opts.Logger = testLogger(t)
	if opts.sleep == nil {
		opts.sleep = instantSleep
	}
	s, err := NewSupervisor(opts)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	return s
}

func TestSupervisor_Start_BecomesReady(t *testing.T) {
	engine := editor.NewEngine(editor.EngineOptions{})
	fake := newFakeGopls(t, engine)
	s := newTestSupervisor(t, SupervisorOptions{Engine: engine, connect: fake.connect})
	states := collectStates(t, s)

	s.Start()
	states.waitFor(t, func(evs []StateEvent) bool { return countState(evs, ServerStateReady) == 1 })

	if got := s.State(); got != ServerStateReady {
		t.Fatalf("State = %v, want ready", got)
	}
	if s.Bridge() == nil {
		t.Fatal("Bridge is nil after ready")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := s.State(); got != ServerStateStopped {
		t.Fatalf("State after Stop = %v, want stopped", got)
	}
}

func TestSupervisor_Crash_Restarts(t *testing.T) {
	engine := editor.NewEngine(editor.EngineOptions{})
	fake := newFakeGopls(t, engine)
	s := newTestSupervisor(t, SupervisorOptions{Engine: engine, connect: fake.connect})
	states := collectStates(t, s)

	s.Start()
	states.waitFor(t, func(evs []StateEvent) bool { return countState(evs, ServerStateReady) == 1 })

	fake.crashCurrent()

	states.waitFor(t, func(evs []StateEvent) bool { return countState(evs, ServerStateReady) == 2 })
	if got := s.State(); got != ServerStateReady {
		t.Fatalf("State after restart = %v, want ready", got)
	}
	if s.Bridge() == nil {
		t.Fatal("Bridge is nil after restart")
	}
	if fake.connectCount() < 2 {
		t.Fatalf("connectCount = %d, want >= 2", fake.connectCount())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestSupervisor_PermanentError_FailsWithoutRetry(t *testing.T) {
	engine := editor.NewEngine(editor.EngineOptions{})
	fake := newFakeGopls(t, engine)
	fake.failNext = 100
	fake.failErr = exec.ErrNotFound
	s := newTestSupervisor(t, SupervisorOptions{Engine: engine, connect: fake.connect})
	states := collectStates(t, s)

	s.Start()
	states.waitFor(t, func(evs []StateEvent) bool { return countState(evs, ServerStateFailed) == 1 })

	if got := s.State(); got != ServerStateFailed {
		t.Fatalf("State = %v, want failed", got)
	}
	if got := fake.connectCount(); got != 1 {
		t.Fatalf("connectCount = %d, want 1 (no retry on permanent error)", got)
	}
}

func TestSupervisor_RepeatedStartError_GivesUp(t *testing.T) {
	engine := editor.NewEngine(editor.EngineOptions{})
	fake := newFakeGopls(t, engine)
	fake.failNext = 100
	fake.failErr = errors.New("gopls start boom")
	s := newTestSupervisor(t, SupervisorOptions{
		Engine:      engine,
		connect:     fake.connect,
		MaxRestarts: 2,
	})
	states := collectStates(t, s)

	s.Start()
	states.waitFor(t, func(evs []StateEvent) bool { return countState(evs, ServerStateFailed) == 1 })

	// attempts: fail(1) -> restart, fail(2) -> restart, fail(3) -> give up.
	if got := fake.connectCount(); got != 3 {
		t.Fatalf("connectCount = %d, want 3 (MaxRestarts=2)", got)
	}
}

func TestSupervisor_Stop_Idempotent(t *testing.T) {
	engine := editor.NewEngine(editor.EngineOptions{})
	fake := newFakeGopls(t, engine)
	s := newTestSupervisor(t, SupervisorOptions{Engine: engine, connect: fake.connect})
	_ = collectStates(t, s)

	s.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if got := s.Bridge(); got != nil {
		t.Fatalf("Bridge after Stop = %v, want nil", got)
	}
}

func TestSupervisor_FormatAndSaveBuffer_NotReady(t *testing.T) {
	engine := editor.NewEngine(editor.EngineOptions{})
	s := newTestSupervisor(t, SupervisorOptions{Engine: engine, connect: newFakeGopls(t, engine).connect})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// Never started: no bridge, so formatting reports not-ready rather
	// than panicking on a nil bridge.
	if _, err := s.FormatAndSaveBuffer(ctx, editor.BufferID(1), FormattingOptions{}); !errors.Is(err, ErrServerNotReady) {
		t.Fatalf("FormatAndSaveBuffer err = %v, want ErrServerNotReady", err)
	}
}

func TestNewSupervisor_NilEngine(t *testing.T) {
	if _, err := NewSupervisor(SupervisorOptions{}); !errors.Is(err, ErrSupervisorNoEngine) {
		t.Fatalf("NewSupervisor err = %v, want ErrSupervisorNoEngine", err)
	}
}
