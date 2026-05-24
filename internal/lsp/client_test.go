package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// testHandshakeTimeout caps any test waiting for the LSP handshake to
// complete. Net.Pipe transport is in-process and instantaneous in
// practice; the timeout exists to keep a buggy test from hanging CI.
const testHandshakeTimeout = 5 * time.Second

// testLogger returns a discard-only logger. Tests that need to inspect
// log output build their own *slog.Logger over a bytes.Buffer.
func testLogger(_ *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeServer is an in-process LSP server stub. It runs its own read
// goroutine and dispatches messages via per-method handlers registered
// by the test. The default Initialize/Shutdown handlers let typical
// tests focus on the interesting case instead of re-wiring boilerplate.
type fakeServer struct {
	t       *testing.T
	netConn net.Conn
	conn    *Conn

	mu              sync.Mutex
	requestHandlers map[string]func(*Request) *Response
	notifHandlers   map[string]func(*Notification)
	notifSeen       []*Notification
	respFromClient  chan *Response
	done            chan struct{}
}

func defaultInitializeResult(t *testing.T) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(InitializeResult{
		ServerInfo:   &ServerInfo{Name: "fake", Version: "0.0.0"},
		Capabilities: ServerCapabilities{},
	})
	if err != nil {
		t.Fatalf("marshal InitializeResult: %v", err)
	}
	return data
}

func newFakeServer(t *testing.T, netConn net.Conn) *fakeServer {
	t.Helper()
	s := &fakeServer{
		t:       t,
		netConn: netConn,
		conn: NewConn(ConnOptions{
			Reader: netConn,
			Writer: netConn,
			Closer: netConn,
			Logger: testLogger(t),
		}),
		requestHandlers: map[string]func(*Request) *Response{},
		notifHandlers:   map[string]func(*Notification){},
		respFromClient:  make(chan *Response, 8),
		done:            make(chan struct{}),
	}
	// Default handlers: complete the handshake/teardown unless the
	// test overrides them.
	s.Handle(MethodInitialize, func(req *Request) *Response {
		return &Response{ID: req.ID, Result: defaultInitializeResult(t)}
	})
	s.Handle(MethodShutdown, func(req *Request) *Response {
		return &Response{ID: req.ID, Result: json.RawMessage("null")}
	})
	return s
}

// Handle registers a request handler. Returning nil from fn suppresses
// the response (useful for "server never replies" cases).
func (s *fakeServer) Handle(method string, fn func(*Request) *Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestHandlers[method] = fn
}

// HandleNotification registers a side-effect for an incoming
// notification. The notification is also recorded in Notifications().
func (s *fakeServer) HandleNotification(method string, fn func(*Notification)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifHandlers[method] = fn
}

// Notifications returns a snapshot of every notification the server
// has received from the client so far.
func (s *fakeServer) Notifications() []*Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Notification(nil), s.notifSeen...)
}

// ResponsesFromClient delivers Responses the server reads from the
// client (i.e., the client's answer to a server-initiated Request).
// Buffered to avoid losing events if the test reads slowly.
func (s *fakeServer) ResponsesFromClient() <-chan *Response {
	return s.respFromClient
}

// Notify sends a notification from server to client.
func (s *fakeServer) Notify(method string, params any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return s.conn.Write(&Notification{Method: method, Params: p})
}

// SendRequest sends a request from server to client. The client should
// respond eventually; observe via ResponsesFromClient.
func (s *fakeServer) SendRequest(id ID, method string, params any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return s.conn.Write(&Request{ID: id, Method: method, Params: p})
}

// Run is the read+dispatch loop. Call in a goroutine after construction.
func (s *fakeServer) Run() {
	defer close(s.done)
	for {
		msg, err := s.conn.Read()
		if err != nil {
			return
		}
		switch m := msg.(type) {
		case *Request:
			s.mu.Lock()
			h := s.requestHandlers[m.Method]
			s.mu.Unlock()
			var resp *Response
			if h != nil {
				resp = h(m)
			} else {
				resp = &Response{
					ID: m.ID,
					Error: &ResponseError{
						Code:    ErrCodeMethodNotFound,
						Message: "fake server: " + m.Method,
					},
				}
			}
			if resp != nil {
				if err := s.conn.Write(resp); err != nil {
					return
				}
			}
		case *Notification:
			s.mu.Lock()
			s.notifSeen = append(s.notifSeen, m)
			h := s.notifHandlers[m.Method]
			s.mu.Unlock()
			if h != nil {
				h(m)
			}
		case *Response:
			select {
			case s.respFromClient <- m:
			default:
			}
		}
	}
}

// CloseAndWait closes the transport (forcing the client's read loop to
// EOF) and waits for the server's Run goroutine to exit.
func (s *fakeServer) CloseAndWait() {
	_ = s.netConn.Close()
	<-s.done
}

// connectedClient returns a Client wired to a fakeServer through an
// in-process net.Pipe. Handshake has already completed on return.
// Cleanup is registered via t.Cleanup, so the test does not need to
// call Stop/Close manually unless it wants to observe the teardown.
func connectedClient(t *testing.T) (*Client, *fakeServer) {
	t.Helper()
	clientNet, serverNet := net.Pipe()
	server := newFakeServer(t, serverNet)
	go server.Run()

	client := NewClient(ClientOptions{Logger: testLogger(t)})
	client.started.Store(true)

	clientConn := NewConn(ConnOptions{
		Reader: clientNet,
		Writer: clientNet,
		Closer: clientNet,
		Logger: testLogger(t),
	})

	ctx, cancel := context.WithTimeout(context.Background(), testHandshakeTimeout)
	defer cancel()
	if err := client.runWithConn(ctx, clientConn); err != nil {
		t.Fatalf("runWithConn: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Stop(ctx)
		server.CloseAndWait()
	})

	return client, server
}

// Tests ---------------------------------------------------------------

func TestClient_Start_Initialize_Succeeds(t *testing.T) {
	client, _ := connectedClient(t)
	if client.Pid() != 0 {
		t.Errorf("Pid = %d, want 0 (no subprocess in test transport)", client.Pid())
	}
}

func TestClient_Initialize_PropagatesServerError(t *testing.T) {
	clientNet, serverNet := net.Pipe()
	server := newFakeServer(t, serverNet)
	server.Handle(MethodInitialize, func(req *Request) *Response {
		return &Response{
			ID: req.ID,
			Error: &ResponseError{
				Code:    ErrCodeInternalError,
				Message: "boom",
			},
		}
	})
	go server.Run()
	t.Cleanup(server.CloseAndWait)

	client := NewClient(ClientOptions{Logger: testLogger(t)})
	client.started.Store(true)
	clientConn := NewConn(ConnOptions{
		Reader: clientNet,
		Writer: clientNet,
		Closer: clientNet,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.runWithConn(ctx, clientConn)
	if err == nil {
		t.Fatal("runWithConn: nil error, want server error propagated")
	}
	var respErr *ResponseError
	if !errors.As(err, &respErr) {
		t.Errorf("err = %v, want *ResponseError underlying", err)
	} else if respErr.Code != ErrCodeInternalError {
		t.Errorf("respErr.Code = %d, want %d", respErr.Code, ErrCodeInternalError)
	}
	if client.started.Load() {
		t.Error("started flag still true after failed handshake")
	}
}

func TestClient_DidOpen_SendsNotification(t *testing.T) {
	client, server := connectedClient(t)
	seen := make(chan *Notification, 1)
	server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		seen <- n
	})

	if err := client.DidOpen(context.Background(), "file:///x.go", "go", "package x\n"); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}

	select {
	case n := <-seen:
		var p DidOpenTextDocumentParams
		if err := json.Unmarshal(n.Params, &p); err != nil {
			t.Fatalf("unmarshal params: %v", err)
		}
		if p.TextDocument.URI != "file:///x.go" {
			t.Errorf("URI = %q, want file:///x.go", p.TextDocument.URI)
		}
		if p.TextDocument.LanguageID != "go" {
			t.Errorf("LanguageID = %q, want go", p.TextDocument.LanguageID)
		}
		if p.TextDocument.Version != 1 {
			t.Errorf("Version = %d, want 1", p.TextDocument.Version)
		}
		if p.TextDocument.Text != "package x\n" {
			t.Errorf("Text = %q, want %q", p.TextDocument.Text, "package x\n")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive didOpen notification in 2s")
	}
}

func TestClient_DidOpen_BeforeStart_Errors(t *testing.T) {
	c := NewClient(ClientOptions{Logger: testLogger(t)})
	err := c.DidOpen(context.Background(), "file:///x.go", "go", "")
	if !errors.Is(err, ErrClientNotStarted) {
		t.Errorf("err = %v, want ErrClientNotStarted", err)
	}
}

func TestClient_PublishDiagnostics_DeliveredToTopic(t *testing.T) {
	client, server := connectedClient(t)
	ch, cancel := client.Diagnostics().Subscribe(8)
	defer cancel()

	diag := PublishDiagnosticsParams{
		URI: "file:///x.go",
		Diagnostics: []Diagnostic{{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 0, Character: 5},
			},
			Severity: DiagnosticSeverityError,
			Message:  "undefined: foo",
		}},
	}
	if err := server.Notify(MethodPublishDiagnostics, diag); err != nil {
		t.Fatalf("server.Notify: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.URI != "file:///x.go" {
			t.Errorf("URI = %q", ev.URI)
		}
		if len(ev.Diagnostics) != 1 {
			t.Fatalf("got %d diagnostics, want 1", len(ev.Diagnostics))
		}
		if ev.Diagnostics[0].Message != "undefined: foo" {
			t.Errorf("Message = %q", ev.Diagnostics[0].Message)
		}
		if ev.Diagnostics[0].Severity != DiagnosticSeverityError {
			t.Errorf("Severity = %d", ev.Diagnostics[0].Severity)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive diagnostics event in 2s")
	}
}

func TestClient_Hover_ReturnsServerResult(t *testing.T) {
	client, server := connectedClient(t)
	server.Handle(MethodTextDocumentHover, func(req *Request) *Response {
		var p HoverParams
		_ = json.Unmarshal(req.Params, &p)
		result, _ := json.Marshal(Hover{
			Contents: MarkupContent{
				Kind:  MarkupKindPlainText,
				Value: "at line " + strconv.Itoa(p.Position.Line),
			},
		})
		return &Response{ID: req.ID, Result: result}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h, err := client.Hover(ctx, "file:///x.go", Position{Line: 7})
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if h == nil {
		t.Fatal("Hover = nil, want result")
	}
	if h.Contents.Value != "at line 7" {
		t.Errorf("Contents.Value = %q, want %q", h.Contents.Value, "at line 7")
	}
}

func TestClient_Hover_NullResult_ReturnsNil(t *testing.T) {
	client, server := connectedClient(t)
	server.Handle(MethodTextDocumentHover, func(req *Request) *Response {
		return &Response{ID: req.ID, Result: json.RawMessage("null")}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h, err := client.Hover(ctx, "file:///x.go", Position{})
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if h != nil {
		t.Errorf("Hover = %+v, want nil", h)
	}
}

func TestClient_Hover_RoutesByIDUnderConcurrency(t *testing.T) {
	client, server := connectedClient(t)

	server.Handle(MethodTextDocumentHover, func(req *Request) *Response {
		// Delay so the order of replies is not the order of requests.
		var p HoverParams
		_ = json.Unmarshal(req.Params, &p)
		delay := time.Duration(50-p.Position.Line) * time.Millisecond
		time.Sleep(delay)
		result, _ := json.Marshal(Hover{
			Contents: MarkupContent{
				Kind:  MarkupKindPlainText,
				Value: strconv.Itoa(p.Position.Line),
			},
		})
		return &Response{ID: req.ID, Result: result}
	})

	const n = 25
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h, err := client.Hover(ctx, "file:///x.go", Position{Line: i})
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = h.Contents.Value
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Errorf("Hover %d: %v", i, errs[i])
			continue
		}
		want := strconv.Itoa(i)
		if results[i] != want {
			t.Errorf("results[%d] = %q, want %q (response routed to wrong caller?)",
				i, results[i], want)
		}
	}
}

func TestClient_Hover_ContextCancel_RemovesPending(t *testing.T) {
	client, server := connectedClient(t)
	// Server never responds.
	server.Handle(MethodTextDocumentHover, func(req *Request) *Response { return nil })

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := client.Hover(ctx, "file:///x.go", Position{})
		errCh <- err
	}()

	// Wait a touch so the request makes it to the server's read loop
	// and registers in the pending map.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Hover did not return within 2s of ctx cancel")
	}

	client.pendingMu.Lock()
	n := len(client.pending)
	client.pendingMu.Unlock()
	if n != 0 {
		t.Errorf("pending map has %d entries after cancel, want 0", n)
	}
}

func TestClient_Stop_RunsShutdownExitSequence(t *testing.T) {
	clientNet, serverNet := net.Pipe()
	server := newFakeServer(t, serverNet)

	shutdownSeen := make(chan struct{})
	server.Handle(MethodShutdown, func(req *Request) *Response {
		close(shutdownSeen)
		return &Response{ID: req.ID, Result: json.RawMessage("null")}
	})
	exitSeen := make(chan struct{})
	server.HandleNotification(MethodExit, func(n *Notification) {
		close(exitSeen)
	})
	go server.Run()
	t.Cleanup(server.CloseAndWait)

	client := NewClient(ClientOptions{Logger: testLogger(t)})
	client.started.Store(true)
	clientConn := NewConn(ConnOptions{
		Reader: clientNet,
		Writer: clientNet,
		Closer: clientNet,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.runWithConn(ctx, clientConn); err != nil {
		t.Fatalf("runWithConn: %v", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := client.Stop(stopCtx); err != nil {
		t.Errorf("Stop: %v", err)
	}

	select {
	case <-shutdownSeen:
	default:
		t.Error("server did not see shutdown request")
	}
	select {
	case <-exitSeen:
	default:
		t.Error("server did not see exit notification")
	}
}

func TestClient_ServerRequest_RespondsMethodNotFound(t *testing.T) {
	client, server := connectedClient(t)
	_ = client

	if err := server.SendRequest(NewIntID(9999),
		MethodClientRegisterCap, json.RawMessage("null")); err != nil {
		t.Fatalf("SendRequest: %v", err)
	}

	select {
	case resp := <-server.ResponsesFromClient():
		if resp.Error == nil {
			t.Fatal("Response.Error = nil, want method-not-found")
		}
		if resp.Error.Code != ErrCodeMethodNotFound {
			t.Errorf("Error.Code = %d, want %d", resp.Error.Code, ErrCodeMethodNotFound)
		}
		if id, _ := resp.ID.Int64(); id != 9999 {
			t.Errorf("response ID = %d, want 9999", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not see client response within 2s")
	}
}

func TestClient_UnknownNotification_DoesNotPanic(t *testing.T) {
	_, server := connectedClient(t)
	if err := server.Notify("$/some-future-thing", map[string]int{"x": 1}); err != nil {
		t.Fatalf("server.Notify: %v", err)
	}
	// No assertion: the test passes if the read loop does not panic
	// or deadlock. Stop in t.Cleanup verifies the loop is still alive.
}

// Conn.Close coverage --------------------------------------------------

type countingCloser struct {
	calls int
	err   error
}

func (c *countingCloser) Close() error {
	c.calls++
	return c.err
}

func TestConn_Close_InvokesCloser(t *testing.T) {
	c := &countingCloser{}
	conn := NewConn(ConnOptions{
		Reader: io.NopCloser(nil),
		Writer: io.Discard,
		Closer: c,
	})
	if err := conn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if c.calls != 1 {
		t.Errorf("calls = %d, want 1", c.calls)
	}
}

func TestConn_Close_NilCloser_NoOp(t *testing.T) {
	conn := NewConn(ConnOptions{
		Reader: io.NopCloser(nil),
		Writer: io.Discard,
	})
	if err := conn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
