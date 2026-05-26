package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ondotteess/kleiber/internal/editor"
)

// bridgeRouteWait is the deadline for one round-trip through the
// bridge (engine event → fakeServer notification or fakeServer notify
// → engine BufferDiagnostics). Two seconds is generous given net.Pipe
// transport completes in well under a millisecond.
const bridgeRouteWait = 2 * time.Second

// newBridgeFixture wires an EditorEngine + Client (via connectedClient)
// + Bridge with shared cleanup. Returns the engine, the fakeServer (for
// driving notifications back), the bridge, and a "subscribe to engine
// events" helper.
type bridgeFixture struct {
	t      *testing.T
	engine *editor.EditorEngine
	client *Client
	server *fakeServer
	bridge *Bridge
}

func newBridgeFixture(t *testing.T) *bridgeFixture {
	t.Helper()
	client, server := connectedClient(t)
	engine := editor.NewEngine(editor.EngineOptions{Logger: testLogger(t)})
	bridge := NewBridge(context.Background(), BridgeOptions{Logger: testLogger(t)},
		client, engine)
	t.Cleanup(bridge.Close)
	return &bridgeFixture{
		t:      t,
		engine: engine,
		client: client,
		server: server,
		bridge: bridge,
	}
}

// writeGoFile creates a temp .go file with content and returns its
// absolute path. Files live for the duration of the test.
func writeGoFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.go")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// waitForNotification blocks until ch yields exactly one notification
// or the deadline fires. Returns the notification on success.
func waitForNotification(t *testing.T, ch <-chan *Notification, deadline time.Duration) *Notification {
	t.Helper()
	select {
	case n := <-ch:
		return n
	case <-time.After(deadline):
		t.Fatalf("did not receive notification within %v", deadline)
		return nil
	}
}

// --- didOpen --------------------------------------------------------

func TestBridge_BufferOpened_SendsDidOpen(t *testing.T) {
	f := newBridgeFixture(t)

	seen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		seen <- n
	})

	path := writeGoFile(t, "package x\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	n := waitForNotification(t, seen, bridgeRouteWait)
	var p DidOpenTextDocumentParams
	if err := json.Unmarshal(n.Params, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.TextDocument.LanguageID != "go" {
		t.Errorf("LanguageID = %q, want go", p.TextDocument.LanguageID)
	}
	if p.TextDocument.Version != 1 {
		t.Errorf("Version = %d, want 1", p.TextDocument.Version)
	}
	if p.TextDocument.Text != "package x\n" {
		t.Errorf("Text = %q", p.TextDocument.Text)
	}

	wantURI, err := DocumentURIFromPath(path)
	if err != nil {
		t.Fatalf("DocumentURIFromPath: %v", err)
	}
	if p.TextDocument.URI != wantURI {
		t.Errorf("URI = %q, want %q", p.TextDocument.URI, wantURI)
	}
	if got := f.bridge.uriFor(id); got != wantURI {
		t.Errorf("bridge tracking URI = %q, want %q", got, wantURI)
	}
}

func TestBridge_UntitledBuffer_NoDidOpen(t *testing.T) {
	f := newBridgeFixture(t)

	seen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		seen <- n
	})

	id := f.engine.NewBuffer("package x\n")

	select {
	case n := <-seen:
		t.Fatalf("unexpected didOpen for untitled buffer: %s", n.Params)
	case <-time.After(200 * time.Millisecond):
	}
	if f.bridge.uriFor(id) != "" {
		t.Errorf("bridge tracking URI for untitled buffer: %q", f.bridge.uriFor(id))
	}
}

func TestBridge_NonGoFile_NoDidOpen(t *testing.T) {
	f := newBridgeFixture(t)

	seen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		seen <- n
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	if err := os.WriteFile(path, []byte("# x\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := f.engine.Open(context.Background(), path); err != nil {
		t.Fatalf("Open: %v", err)
	}

	select {
	case n := <-seen:
		t.Fatalf("unexpected didOpen for non-Go file: %s", n.Params)
	case <-time.After(200 * time.Millisecond):
	}
}

// --- didChange ------------------------------------------------------

func TestBridge_BufferChanged_SendsFullTextDidChange(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})
	changeSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidChange, func(n *Notification) {
		changeSeen <- n
	})

	path := writeGoFile(t, "package x\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	buf, err := f.engine.Buffer(id)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if _, err := buf.Insert(editor.Position{Line: 0, Column: 9}, "var A = 1\n"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	n := waitForNotification(t, changeSeen, bridgeRouteWait)
	var p DidChangeTextDocumentParams
	if err := json.Unmarshal(n.Params, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.TextDocument.Version != 2 {
		t.Errorf("Version = %d, want 2 (1 for open, 2 after edit)", p.TextDocument.Version)
	}
	if len(p.ContentChanges) != 1 {
		t.Fatalf("ContentChanges len = %d, want 1", len(p.ContentChanges))
	}
	if !strings.Contains(p.ContentChanges[0].Text, "var A = 1") {
		t.Errorf("Text = %q, want it to contain 'var A = 1'", p.ContentChanges[0].Text)
	}
	if v := f.bridge.versionFor(id); v != 2 {
		t.Errorf("bridge versionFor = %d, want 2", v)
	}
}

func TestBridge_BufferChanged_MonotonicVersion(t *testing.T) {
	f := newBridgeFixture(t)

	versions := make(chan int, 8)
	f.server.HandleNotification(MethodTextDocumentDidChange, func(n *Notification) {
		var p DidChangeTextDocumentParams
		if err := json.Unmarshal(n.Params, &p); err == nil {
			versions <- p.TextDocument.Version
		}
	})

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	path := writeGoFile(t, "package x\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	buf, _ := f.engine.Buffer(id)
	const edits = 5
	for i := 0; i < edits; i++ {
		if _, err := buf.Insert(editor.Position{Line: 0, Column: 0}, "x"); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	got := make([]int, 0, edits)
	deadline := time.After(bridgeRouteWait)
	for len(got) < edits {
		select {
		case v := <-versions:
			got = append(got, v)
		case <-deadline:
			t.Fatalf("only %d/%d didChange seen", len(got), edits)
		}
	}
	want := []int{2, 3, 4, 5, 6}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("didChange[%d].Version = %d, want %d", i, got[i], v)
		}
	}
}

// --- formatting -----------------------------------------------------

func TestBridge_FormatBuffer_RequestsAndAppliesEdits(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})
	changeSeen := make(chan *Notification, 4)
	f.server.HandleNotification(MethodTextDocumentDidChange, func(n *Notification) {
		changeSeen <- n
	})
	f.server.Handle(MethodTextDocumentFormatting, func(req *Request) *Response {
		var p DocumentFormattingParams
		_ = json.Unmarshal(req.Params, &p)
		if p.TextDocument.URI == "" {
			t.Error("formatting URI is empty")
		}
		if p.Options.TabSize != 4 {
			t.Errorf("TabSize = %d, want 4", p.Options.TabSize)
		}
		result, _ := json.Marshal([]TextEdit{{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 3, Character: 0},
			},
			NewText: "package x\n\nfunc main() {}\n",
		}})
		return &Response{ID: req.ID, Result: result}
	})

	path := writeGoFile(t, "package  x\n\nfunc main( ){}\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	n, err := f.bridge.FormatBuffer(context.Background(), id, FormattingOptions{
		TabSize:      4,
		InsertSpaces: false,
	})
	if err != nil {
		t.Fatalf("FormatBuffer: %v", err)
	}
	if n != 1 {
		t.Errorf("FormatBuffer applied %d edits, want 1", n)
	}
	buf, err := f.engine.Buffer(id)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if got := buf.Text(); got != "package x\n\nfunc main() {}\n" {
		t.Errorf("buffer text = %q", got)
	}

	deadline := time.After(bridgeRouteWait)
	for {
		select {
		case msg := <-changeSeen:
			var p DidChangeTextDocumentParams
			if err := json.Unmarshal(msg.Params, &p); err != nil {
				t.Fatalf("unmarshal didChange: %v", err)
			}
			if len(p.ContentChanges) == 1 && p.ContentChanges[0].Text == "package x\n\nfunc main() {}\n" {
				return
			}
		case <-deadline:
			t.Fatal("did not observe formatted didChange")
		}
	}
}

func TestBridge_FormatBuffer_UntrackedBuffer_Errors(t *testing.T) {
	f := newBridgeFixture(t)

	id := f.engine.NewBuffer("package x\n")
	_, err := f.bridge.FormatBuffer(context.Background(), id, FormattingOptions{TabSize: 4})
	if !errors.Is(err, ErrBridgeDocumentNotTracked) {
		t.Errorf("err = %v, want ErrBridgeDocumentNotTracked", err)
	}
}

func TestBridge_FormatBuffer_BufferChangedDuringRequest_Errors(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	requestSeen := make(chan struct{})
	release := make(chan struct{})
	f.server.Handle(MethodTextDocumentFormatting, func(req *Request) *Response {
		close(requestSeen)
		<-release
		result, _ := json.Marshal([]TextEdit{{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 1, Character: 0},
			},
			NewText: "package x\n",
		}})
		return &Response{ID: req.ID, Result: result}
	})

	path := writeGoFile(t, "package  x\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	errCh := make(chan error, 1)
	go func() {
		_, err := f.bridge.FormatBuffer(context.Background(), id, FormattingOptions{TabSize: 4})
		errCh <- err
	}()

	select {
	case <-requestSeen:
	case <-time.After(bridgeRouteWait):
		t.Fatal("formatting request was not observed")
	}

	buf, err := f.engine.Buffer(id)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if _, err := buf.Insert(editor.Position{Line: 0, Column: 0}, "// changed\n"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	close(release)

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrBridgeBufferChangedDuringFormat) {
			t.Fatalf("err = %v, want ErrBridgeBufferChangedDuringFormat", err)
		}
	case <-time.After(bridgeRouteWait):
		t.Fatal("FormatBuffer did not return")
	}
	if got := buf.Text(); got != "// changed\npackage  x\n" {
		t.Errorf("buffer text = %q", got)
	}
}

func TestBridge_FormatAndSaveBuffer_FormatsThenWritesFile(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})
	f.server.Handle(MethodTextDocumentFormatting, func(req *Request) *Response {
		result, _ := json.Marshal([]TextEdit{{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 3, Character: 0},
			},
			NewText: "package x\n\nfunc main() {}\n",
		}})
		return &Response{ID: req.ID, Result: result}
	})

	path := writeGoFile(t, "package  x\n\nfunc main( ){}\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	n, err := f.bridge.FormatAndSaveBuffer(context.Background(), id, FormattingOptions{TabSize: 4})
	if err != nil {
		t.Fatalf("FormatAndSaveBuffer: %v", err)
	}
	if n != 1 {
		t.Errorf("edits = %d, want 1", n)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); got != "package x\n\nfunc main() {}\n" {
		t.Errorf("file content = %q", got)
	}
	if dirty, err := f.engine.Dirty(id); err != nil || dirty {
		t.Fatalf("Dirty after format+save = %v, %v; want false, nil", dirty, err)
	}
}

func TestBridge_FormatAndSaveBuffer_FormatErrorSkipsSave(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})
	f.server.Handle(MethodTextDocumentFormatting, func(req *Request) *Response {
		return &Response{
			ID: req.ID,
			Error: &ResponseError{
				Code:    ErrCodeInternalError,
				Message: "format failed",
			},
		}
	})

	path := writeGoFile(t, "package  x\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	buf, err := f.engine.Buffer(id)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if _, err := buf.Insert(editor.Position{Line: 0, Column: 0}, "// dirty\n"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	_, err = f.bridge.FormatAndSaveBuffer(context.Background(), id, FormattingOptions{TabSize: 4})
	var respErr *ResponseError
	if !errors.As(err, &respErr) {
		t.Fatalf("err = %v, want *ResponseError", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); got != "package  x\n" {
		t.Errorf("file content = %q, want original on disk", got)
	}
}

// --- didClose -------------------------------------------------------

func TestBridge_BufferClosed_SendsDidCloseAndForgets(t *testing.T) {
	f := newBridgeFixture(t)

	closeSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidClose, func(n *Notification) {
		closeSeen <- n
	})
	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	path := writeGoFile(t, "package x\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)
	if f.bridge.docCount() != 1 {
		t.Fatalf("docCount = %d, want 1 after Open", f.bridge.docCount())
	}

	if err := f.engine.Close(id); err != nil {
		t.Fatalf("engine.Close: %v", err)
	}

	n := waitForNotification(t, closeSeen, bridgeRouteWait)
	var p DidCloseTextDocumentParams
	if err := json.Unmarshal(n.Params, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	wantURI, _ := DocumentURIFromPath(path)
	if p.TextDocument.URI != wantURI {
		t.Errorf("URI = %q, want %q", p.TextDocument.URI, wantURI)
	}

	// Wait a tick for the bridge's internal map mutation to land
	// (didClose is sent on the same goroutine, so this is paranoid).
	deadline := time.Now().Add(bridgeRouteWait)
	for f.bridge.docCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.bridge.docCount() != 0 {
		t.Errorf("docCount = %d after Close, want 0", f.bridge.docCount())
	}
}

// --- diagnostics --------------------------------------------------

func TestBridge_DiagnosticsFromServer_RoutedToEngine(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	path := writeGoFile(t, "package x\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	sub, cancel := f.engine.Events().Subscribe(8)
	defer cancel()

	uri, _ := DocumentURIFromPath(path)
	v := 1
	diag := PublishDiagnosticsParams{
		URI:     uri,
		Version: &v,
		Diagnostics: []Diagnostic{{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 0, Character: 7},
			},
			Severity: DiagnosticSeverityError,
			Source:   "compiler",
			Code:     json.RawMessage(`"E001"`),
			Message:  "expected package name",
		}},
	}
	if err := f.server.Notify(MethodPublishDiagnostics, diag); err != nil {
		t.Fatalf("server.Notify: %v", err)
	}

	deadline := time.After(bridgeRouteWait)
	for {
		select {
		case ev := <-sub:
			bd, ok := ev.(editor.BufferDiagnostics)
			if !ok {
				continue
			}
			if bd.ID != id {
				t.Errorf("ID = %v, want %v", bd.ID, id)
			}
			if bd.Version == nil || *bd.Version != 1 {
				t.Errorf("Version = %v, want *1", bd.Version)
			}
			if len(bd.Diagnostics) != 1 {
				t.Fatalf("Diagnostics len = %d, want 1", len(bd.Diagnostics))
			}
			d := bd.Diagnostics[0]
			if d.Severity != editor.DiagnosticSeverityError {
				t.Errorf("Severity = %v, want Error", d.Severity)
			}
			if d.Source != "compiler" {
				t.Errorf("Source = %q, want %q", d.Source, "compiler")
			}
			if d.Code != "E001" {
				t.Errorf("Code = %q, want %q", d.Code, "E001")
			}
			if d.Message != "expected package name" {
				t.Errorf("Message = %q", d.Message)
			}
			if d.Range.Start.Column != 0 || d.Range.End.Column != 7 {
				t.Errorf("Range = %v", d.Range)
			}
			return
		case <-deadline:
			t.Fatal("did not receive BufferDiagnostics within deadline")
		}
	}
}

func TestBridge_DiagnosticsForUnknownURI_Dropped(t *testing.T) {
	f := newBridgeFixture(t)

	sub, cancel := f.engine.Events().Subscribe(8)
	defer cancel()

	diag := PublishDiagnosticsParams{
		URI: DocumentURI("file:///nowhere/x.go"),
		Diagnostics: []Diagnostic{{
			Severity: DiagnosticSeverityWarning,
			Message:  "stale",
		}},
	}
	if err := f.server.Notify(MethodPublishDiagnostics, diag); err != nil {
		t.Fatalf("server.Notify: %v", err)
	}

	select {
	case ev := <-sub:
		if _, ok := ev.(editor.BufferDiagnostics); ok {
			t.Fatalf("unexpected BufferDiagnostics for unknown URI: %+v", ev)
		}
	case <-time.After(200 * time.Millisecond):
	}
}

// --- diagnostic code shapes ---------------------------------------

func TestDecodeDiagnosticCode(t *testing.T) {
	cases := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{"empty", nil, ""},
		{"null", json.RawMessage("null"), ""},
		{"string", json.RawMessage(`"SA4006"`), "SA4006"},
		{"number", json.RawMessage(`42`), "42"},
		{"object", json.RawMessage(`{"value":"x"}`), `{"value":"x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeDiagnosticCode(tc.raw)
			if got != tc.want {
				t.Errorf("decodeDiagnosticCode(%s) = %q, want %q",
					tc.raw, got, tc.want)
			}
		})
	}
}

// --- isGoFile -----------------------------------------------------

func TestIsGoFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"x.go", true},
		{"/a/b/c.go", true},
		{"x.md", false},
		{"x", false},
		{"", false},
		{"go", false},   // bare name, no extension
		{"x.Go", false}, // case-sensitive; gopls also rejects this
		{"x.go.bak", false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := isGoFile(tc.path); got != tc.want {
				t.Errorf("isGoFile(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// --- Close ---------------------------------------------------------

func TestBridge_Close_StopsRouting(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	path := writeGoFile(t, "package x\n")
	if _, err := f.engine.Open(context.Background(), path); err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	f.bridge.Close()
	// Idempotent.
	f.bridge.Close()

	// After Close, edits should not produce notifications.
	changeSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidChange, func(n *Notification) {
		changeSeen <- n
	})
	buf, err := f.engine.Buffer(1)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if _, err := buf.Insert(editor.Position{Line: 0, Column: 0}, "y"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	select {
	case n := <-changeSeen:
		t.Fatalf("unexpected didChange after Close: %s", n.Params)
	case <-time.After(200 * time.Millisecond):
	}
}
