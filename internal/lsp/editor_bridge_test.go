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

func TestBridge_UntitledSaveAsGo_SendsDidOpen(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	id := f.engine.NewBuffer("package saved\n")
	path := filepath.Join(t.TempDir(), "saved.go")
	if err := f.engine.SaveAs(context.Background(), id, path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	n := waitForNotification(t, openSeen, bridgeRouteWait)
	var p DidOpenTextDocumentParams
	if err := json.Unmarshal(n.Params, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	wantURI, _ := DocumentURIFromPath(path)
	if p.TextDocument.URI != wantURI {
		t.Errorf("URI = %q, want %q", p.TextDocument.URI, wantURI)
	}
	if p.TextDocument.Version != 1 {
		t.Errorf("Version = %d, want 1", p.TextDocument.Version)
	}
	if p.TextDocument.Text != "package saved\n" {
		t.Errorf("Text = %q", p.TextDocument.Text)
	}
	if got := f.bridge.uriFor(id); got != wantURI {
		t.Errorf("bridge tracking URI = %q, want %q", got, wantURI)
	}
}

func TestBridge_SaveTrackedGo_NoExtraDidOpen(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 2)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	path := writeGoFile(t, "package x\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	if err := f.engine.Save(context.Background(), id); err != nil {
		t.Fatalf("Save: %v", err)
	}

	select {
	case n := <-openSeen:
		t.Fatalf("unexpected extra didOpen after Save: %s", n.Params)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestBridge_SaveAsTrackedGoToDifferentGo_ReopensDocument(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 2)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})
	closeSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidClose, func(n *Notification) {
		closeSeen <- n
	})

	oldPath := writeGoFile(t, "package x\n")
	id, err := f.engine.Open(context.Background(), oldPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)
	oldURI, _ := DocumentURIFromPath(oldPath)

	newPath := filepath.Join(t.TempDir(), "renamed.go")
	if err := f.engine.SaveAs(context.Background(), id, newPath); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	closeN := waitForNotification(t, closeSeen, bridgeRouteWait)
	var closeP DidCloseTextDocumentParams
	if err := json.Unmarshal(closeN.Params, &closeP); err != nil {
		t.Fatalf("unmarshal close: %v", err)
	}
	if closeP.TextDocument.URI != oldURI {
		t.Errorf("closed URI = %q, want %q", closeP.TextDocument.URI, oldURI)
	}

	openN := waitForNotification(t, openSeen, bridgeRouteWait)
	var openP DidOpenTextDocumentParams
	if err := json.Unmarshal(openN.Params, &openP); err != nil {
		t.Fatalf("unmarshal open: %v", err)
	}
	newURI, _ := DocumentURIFromPath(newPath)
	if openP.TextDocument.URI != newURI {
		t.Errorf("opened URI = %q, want %q", openP.TextDocument.URI, newURI)
	}
	if openP.TextDocument.Version != 1 {
		t.Errorf("opened version = %d, want reset to 1", openP.TextDocument.Version)
	}
	if got := f.bridge.uriFor(id); got != newURI {
		t.Errorf("bridge tracking URI = %q, want %q", got, newURI)
	}
	if got := f.bridge.versionFor(id); got != 1 {
		t.Errorf("bridge version = %d, want reset to 1", got)
	}
}

func TestBridge_SaveAsTrackedGoToNonGo_ClosesAndUntracks(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 2)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})
	closeSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidClose, func(n *Notification) {
		closeSeen <- n
	})

	path := writeGoFile(t, "package x\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)
	oldURI, _ := DocumentURIFromPath(path)

	if err := f.engine.SaveAs(context.Background(), id, filepath.Join(t.TempDir(), "notes.txt")); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	closeN := waitForNotification(t, closeSeen, bridgeRouteWait)
	var closeP DidCloseTextDocumentParams
	if err := json.Unmarshal(closeN.Params, &closeP); err != nil {
		t.Fatalf("unmarshal close: %v", err)
	}
	if closeP.TextDocument.URI != oldURI {
		t.Errorf("closed URI = %q, want %q", closeP.TextDocument.URI, oldURI)
	}
	if got := f.bridge.uriFor(id); got != "" {
		t.Errorf("bridge tracking URI = %q, want empty", got)
	}

	select {
	case n := <-openSeen:
		t.Fatalf("unexpected didOpen after SaveAs to non-Go: %s", n.Params)
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

// --- tracked document snapshots -----------------------------------

func TestBridge_TrackedDocuments_ListsTrackedGoOnly(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	goPath := writeGoFile(t, "package x\n")
	goID, err := f.engine.Open(context.Background(), goPath)
	if err != nil {
		t.Fatalf("Open go file: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	mdPath := filepath.Join(t.TempDir(), "README.md")
	if err := os.WriteFile(mdPath, []byte("# docs\n"), 0o644); err != nil {
		t.Fatalf("WriteFile markdown: %v", err)
	}
	if _, err := f.engine.Open(context.Background(), mdPath); err != nil {
		t.Fatalf("Open markdown: %v", err)
	}
	f.engine.NewBuffer("package draft\n")

	docs := f.bridge.TrackedDocuments()
	if len(docs) != 1 {
		t.Fatalf("TrackedDocuments len = %d, want 1: %+v", len(docs), docs)
	}
	wantURI, _ := DocumentURIFromPath(goPath)
	if docs[0].BufferID != goID {
		t.Errorf("BufferID = %v, want %v", docs[0].BufferID, goID)
	}
	if docs[0].URI != wantURI {
		t.Errorf("URI = %q, want %q", docs[0].URI, wantURI)
	}
	if filepath.Clean(docs[0].Path) != filepath.Clean(goPath) {
		t.Errorf("Path = %q, want %q", docs[0].Path, goPath)
	}
	if docs[0].Text != "package x\n" {
		t.Errorf("Text = %q, want package x", docs[0].Text)
	}
	if docs[0].Version != 1 {
		t.Errorf("Version = %d, want 1", docs[0].Version)
	}
}

func TestBridge_TrackedDocuments_UsesCurrentEditorText(t *testing.T) {
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
	if _, err := buf.Insert(editor.Position{Line: 0, Column: 0}, "// current\n"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	waitForNotification(t, changeSeen, bridgeRouteWait)

	docs := f.bridge.TrackedDocuments()
	if len(docs) != 1 {
		t.Fatalf("TrackedDocuments len = %d, want 1", len(docs))
	}
	if docs[0].Text != "// current\npackage x\n" {
		t.Errorf("Text = %q, want current editor text", docs[0].Text)
	}
	if docs[0].Version != 2 {
		t.Errorf("Version = %d, want 2 after one change", docs[0].Version)
	}
}

func TestBridge_ReplayOpenDocuments_SendsCurrentTextAndResetsVersion(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 2)
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
	if _, err := buf.Insert(editor.Position{Line: 0, Column: 0}, "// replay\n"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	waitForNotification(t, changeSeen, bridgeRouteWait)
	if got := f.bridge.versionFor(id); got != 2 {
		t.Fatalf("version before replay = %d, want 2", got)
	}

	if err := f.bridge.ReplayOpenDocuments(context.Background()); err != nil {
		t.Fatalf("ReplayOpenDocuments: %v", err)
	}

	n := waitForNotification(t, openSeen, bridgeRouteWait)
	var p DidOpenTextDocumentParams
	if err := json.Unmarshal(n.Params, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.TextDocument.Version != 1 {
		t.Errorf("replayed version = %d, want 1", p.TextDocument.Version)
	}
	if p.TextDocument.Text != "// replay\npackage x\n" {
		t.Errorf("replayed text = %q, want current editor text", p.TextDocument.Text)
	}
	if got := f.bridge.versionFor(id); got != 1 {
		t.Errorf("bridge version after replay = %d, want reset to 1", got)
	}
}

// --- hover/navigation -----------------------------------------------

func TestBridge_HoverBuffer_ConvertsPositionAndRange(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	line := "func main() { _ = \"\u00e9\"; fmt.Println() }"
	startByte := strings.Index(line, "Println")
	if startByte < 0 {
		t.Fatal("test line does not contain Println")
	}
	endByte := startByte + len("Println")
	pos := editor.Position{Line: 2, Column: startByte + 2}
	startCharacter := startByte - 1 // \u00e9 is two UTF-8 bytes but one UTF-16 code unit.
	endCharacter := endByte - 1

	f.server.Handle(MethodTextDocumentHover, func(req *Request) *Response {
		var p HoverParams
		_ = json.Unmarshal(req.Params, &p)
		if p.Position != (Position{Line: 2, Character: pos.Column - 1}) {
			t.Errorf("Position = %+v, want line 2 character %d", p.Position, pos.Column-1)
		}
		result, _ := json.Marshal(Hover{
			Contents: MarkupContent{Kind: MarkupKindPlainText, Value: "fmt.Println"},
			Range: &Range{
				Start: Position{Line: 2, Character: startCharacter},
				End:   Position{Line: 2, Character: endCharacter},
			},
		})
		return &Response{ID: req.ID, Result: result}
	})

	path := writeGoFile(t, "package main\n\n"+line+"\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	got, err := f.bridge.HoverBuffer(context.Background(), id, pos)
	if err != nil {
		t.Fatalf("HoverBuffer: %v", err)
	}
	if got == nil {
		t.Fatal("HoverBuffer = nil, want hover")
	}
	if got.Contents.Value != "fmt.Println" {
		t.Errorf("Contents.Value = %q, want fmt.Println", got.Contents.Value)
	}
	if got.Range == nil {
		t.Fatal("Range = nil, want converted range")
	}
	if got.Range.Start.Column != startByte || got.Range.End.Column != endByte {
		t.Errorf("Range = %+v, want byte columns %d..%d", *got.Range, startByte, endByte)
	}
}

func TestBridge_DefinitionBuffer_ConvertsTrackedLocation(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	line := "func main() { _ = \"\u00e9\"; target() }"
	startByte := strings.Index(line, "target")
	if startByte < 0 {
		t.Fatal("test line does not contain target")
	}
	endByte := startByte + len("target")
	startCharacter := startByte - 1
	endCharacter := endByte - 1
	var wantURI DocumentURI
	f.server.Handle(MethodTextDocumentDefinition, func(req *Request) *Response {
		var p DefinitionParams
		_ = json.Unmarshal(req.Params, &p)
		if p.Position != (Position{Line: 2, Character: startCharacter + 2}) {
			t.Errorf("Position = %+v, want translated editor position", p.Position)
		}
		result, _ := json.Marshal(Location{
			URI: wantURI,
			Range: Range{
				Start: Position{Line: 2, Character: startCharacter},
				End:   Position{Line: 2, Character: endCharacter},
			},
		})
		return &Response{ID: req.ID, Result: result}
	})

	path := writeGoFile(t, "package main\n\n"+line+"\n")
	var err error
	wantURI, err = DocumentURIFromPath(path)
	if err != nil {
		t.Fatalf("DocumentURIFromPath: %v", err)
	}
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	got, err := f.bridge.DefinitionBuffer(context.Background(), id, editor.Position{
		Line:   2,
		Column: startByte + 2,
	})
	if err != nil {
		t.Fatalf("DefinitionBuffer: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("DefinitionBuffer len = %d, want 1", len(got))
	}
	if got[0].BufferID != id {
		t.Errorf("BufferID = %v, want %v", got[0].BufferID, id)
	}
	if filepath.Clean(got[0].Path) != filepath.Clean(path) {
		t.Errorf("Path = %q, want %q", got[0].Path, path)
	}
	if got[0].Range.Start.Column != startByte || got[0].Range.End.Column != endByte {
		t.Errorf("Range = %+v, want byte columns %d..%d", got[0].Range, startByte, endByte)
	}
}

func TestBridge_ReferencesBuffer_ConvertsUntrackedLocationFromDisk(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	refLine := "var _ = \"\u00e9\"; target()"
	startByte := strings.Index(refLine, "target")
	if startByte < 0 {
		t.Fatal("test line does not contain target")
	}
	endByte := startByte + len("target")
	refPath := writeGoFile(t, "package ref\n\n"+refLine+"\n")
	refURI, err := DocumentURIFromPath(refPath)
	if err != nil {
		t.Fatalf("DocumentURIFromPath: %v", err)
	}

	f.server.Handle(MethodTextDocumentReferences, func(req *Request) *Response {
		var p ReferenceParams
		_ = json.Unmarshal(req.Params, &p)
		if !p.Context.IncludeDeclaration {
			t.Error("IncludeDeclaration = false, want true")
		}
		result, _ := json.Marshal([]Location{{
			URI: refURI,
			Range: Range{
				Start: Position{Line: 2, Character: startByte - 1},
				End:   Position{Line: 2, Character: endByte - 1},
			},
		}})
		return &Response{ID: req.ID, Result: result}
	})

	path := writeGoFile(t, "package main\n\nfunc main() { target() }\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	got, err := f.bridge.ReferencesBuffer(context.Background(), id, editor.Position{
		Line:   2,
		Column: strings.Index("func main() { target() }", "target"),
	}, true)
	if err != nil {
		t.Fatalf("ReferencesBuffer: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ReferencesBuffer len = %d, want 1", len(got))
	}
	if got[0].BufferID != 0 {
		t.Errorf("BufferID = %v, want 0 for untracked file", got[0].BufferID)
	}
	if filepath.Clean(got[0].Path) != filepath.Clean(refPath) {
		t.Errorf("Path = %q, want %q", got[0].Path, refPath)
	}
	if got[0].Range.Start.Column != startByte || got[0].Range.End.Column != endByte {
		t.Errorf("Range = %+v, want byte columns %d..%d", got[0].Range, startByte, endByte)
	}
}

func TestBridge_Navigation_UntrackedBuffer_Errors(t *testing.T) {
	f := newBridgeFixture(t)

	id := f.engine.NewBuffer("package x\n")
	_, err := f.bridge.HoverBuffer(context.Background(), id, editor.Position{})
	if !errors.Is(err, ErrBridgeDocumentNotTracked) {
		t.Errorf("err = %v, want ErrBridgeDocumentNotTracked", err)
	}
}

func TestBridge_DefinitionBuffer_BufferChangedDuringRequest_Errors(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	requestSeen := make(chan struct{})
	release := make(chan struct{})
	f.server.Handle(MethodTextDocumentDefinition, func(req *Request) *Response {
		close(requestSeen)
		<-release
		return &Response{ID: req.ID, Result: json.RawMessage("null")}
	})

	path := writeGoFile(t, "package x\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	errCh := make(chan error, 1)
	go func() {
		_, err := f.bridge.DefinitionBuffer(context.Background(), id, editor.Position{Line: 0, Column: 7})
		errCh <- err
	}()

	select {
	case <-requestSeen:
	case <-time.After(bridgeRouteWait):
		t.Fatal("definition request was not observed")
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
		if !errors.Is(err, ErrBridgeBufferChangedDuringNavigation) {
			t.Fatalf("err = %v, want ErrBridgeBufferChangedDuringNavigation", err)
		}
	case <-time.After(bridgeRouteWait):
		t.Fatal("DefinitionBuffer did not return")
	}
}

// --- completions ----------------------------------------------------

func TestBridge_CompleteBuffer_ConvertsBytePosition(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	line := "func main() { _ = \"\u00e9\"; fmt.Pr }"
	pos := editor.Position{Line: 2, Column: strings.Index(line, "Pr") + len("Pr")}
	wantCharacter := pos.Column - 1 // \u00e9 is two UTF-8 bytes but one UTF-16 code unit.
	f.server.Handle(MethodTextDocumentCompletion, func(req *Request) *Response {
		var p CompletionParams
		_ = json.Unmarshal(req.Params, &p)
		if p.TextDocument.URI == "" {
			t.Error("completion URI is empty")
		}
		if p.Position != (Position{Line: 2, Character: wantCharacter}) {
			t.Errorf("Position = %+v, want line 2 character %d", p.Position, wantCharacter)
		}
		return &Response{ID: req.ID, Result: json.RawMessage(`[
			{"label":"Println","kind":3,"detail":"func"}
		]`)}
	})

	path := writeGoFile(t, "package main\n\n"+line+"\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	got, err := f.bridge.CompleteBuffer(context.Background(), id, pos)
	if err != nil {
		t.Fatalf("CompleteBuffer: %v", err)
	}
	if got == nil {
		t.Fatal("CompleteBuffer = nil, want list")
	}
	if len(got.Items) != 1 || got.Items[0].Label != "Println" {
		t.Fatalf("completion items = %+v, want Println", got.Items)
	}
}

func TestBridge_CompleteBuffer_UntrackedBuffer_Errors(t *testing.T) {
	f := newBridgeFixture(t)

	id := f.engine.NewBuffer("package x\n")
	_, err := f.bridge.CompleteBuffer(context.Background(), id, editor.Position{})
	if !errors.Is(err, ErrBridgeDocumentNotTracked) {
		t.Errorf("err = %v, want ErrBridgeDocumentNotTracked", err)
	}
}

func TestBridge_CompleteBuffer_BufferChangedDuringRequest_Errors(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	requestSeen := make(chan struct{})
	release := make(chan struct{})
	f.server.Handle(MethodTextDocumentCompletion, func(req *Request) *Response {
		close(requestSeen)
		<-release
		return &Response{ID: req.ID, Result: json.RawMessage(`[{"label":"main"}]`)}
	})

	path := writeGoFile(t, "package x\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	errCh := make(chan error, 1)
	go func() {
		_, err := f.bridge.CompleteBuffer(context.Background(), id, editor.Position{Line: 0, Column: 7})
		errCh <- err
	}()

	select {
	case <-requestSeen:
	case <-time.After(bridgeRouteWait):
		t.Fatal("completion request was not observed")
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
		if !errors.Is(err, ErrBridgeBufferChangedDuringCompletion) {
			t.Fatalf("err = %v, want ErrBridgeBufferChangedDuringCompletion", err)
		}
	case <-time.After(bridgeRouteWait):
		t.Fatal("CompleteBuffer did not return")
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

func TestBridge_Diagnostics_ConvertsUTF16RangeToByteColumns(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})

	line := "var _ = \"a\U0001F600b\""
	startByte := strings.Index(line, "b")
	if startByte < 0 {
		t.Fatal("test line does not contain b")
	}
	startCharacter := startByte - 2 // \U0001F600 is four UTF-8 bytes but two UTF-16 code units.
	path := writeGoFile(t, "package main\n\n"+line+"\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	sub, cancel := f.engine.Events().Subscribe(8)
	defer cancel()

	uri, _ := DocumentURIFromPath(path)
	diag := PublishDiagnosticsParams{
		URI: uri,
		Diagnostics: []Diagnostic{{
			Range: Range{
				Start: Position{Line: 2, Character: startCharacter},
				End:   Position{Line: 2, Character: startCharacter + 1},
			},
			Severity: DiagnosticSeverityWarning,
			Message:  "check range",
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
			if len(bd.Diagnostics) != 1 {
				t.Fatalf("Diagnostics len = %d, want 1", len(bd.Diagnostics))
			}
			r := bd.Diagnostics[0].Range
			if r.Start.Column != startByte || r.End.Column != startByte+1 {
				t.Fatalf("Range = %+v, want byte columns %d..%d", r, startByte, startByte+1)
			}
			return
		case <-deadline:
			t.Fatal("did not receive BufferDiagnostics within deadline")
		}
	}
}

func TestBridge_Diagnostics_DropsStaleVersion(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})
	changeSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidChange, func(n *Notification) {
		changeSeen <- n
	})

	path := writeGoFile(t, "package main\n\nvar _ = \"\u00e9\"\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	buf, err := f.engine.Buffer(id)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if _, err := buf.Insert(editor.Position{Line: 2, Column: 0}, "var _ = \"fresh\"\n"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	waitForNotification(t, changeSeen, bridgeRouteWait)
	if got := f.bridge.versionFor(id); got != 2 {
		t.Fatalf("bridge version = %d, want 2", got)
	}

	sub, cancel := f.engine.Events().Subscribe(8)
	defer cancel()

	uri, _ := DocumentURIFromPath(path)
	stale := 1
	if err := f.server.Notify(MethodPublishDiagnostics, PublishDiagnosticsParams{
		URI:     uri,
		Version: &stale,
		Diagnostics: []Diagnostic{{
			Range: Range{
				Start: Position{Line: 2, Character: 0},
				End:   Position{Line: 2, Character: 3},
			},
			Severity: DiagnosticSeverityWarning,
			Message:  "stale",
		}},
	}); err != nil {
		t.Fatalf("server.Notify: %v", err)
	}

	select {
	case ev := <-sub:
		if _, ok := ev.(editor.BufferDiagnostics); ok {
			t.Fatalf("unexpected stale BufferDiagnostics: %+v", ev)
		}
	case <-time.After(200 * time.Millisecond):
	}
}

func TestBridge_Diagnostics_CurrentVersionRoutesAfterEdit(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})
	changeSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidChange, func(n *Notification) {
		changeSeen <- n
	})

	path := writeGoFile(t, "package main\n\nvar _ = \"\u00e9\"\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitForNotification(t, openSeen, bridgeRouteWait)

	buf, err := f.engine.Buffer(id)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if _, err := buf.Insert(editor.Position{Line: 2, Column: 0}, "var _ = \"fresh\"\n"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	waitForNotification(t, changeSeen, bridgeRouteWait)

	sub, cancel := f.engine.Events().Subscribe(8)
	defer cancel()

	uri, _ := DocumentURIFromPath(path)
	current := f.bridge.versionFor(id)
	if err := f.server.Notify(MethodPublishDiagnostics, PublishDiagnosticsParams{
		URI:     uri,
		Version: &current,
		Diagnostics: []Diagnostic{{
			Range: Range{
				Start: Position{Line: 2, Character: 0},
				End:   Position{Line: 2, Character: 3},
			},
			Severity: DiagnosticSeverityWarning,
			Message:  "current",
		}},
	}); err != nil {
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
			if bd.Version == nil || *bd.Version != current {
				t.Errorf("Version = %v, want %d", bd.Version, current)
			}
			if len(bd.Diagnostics) != 1 || bd.Diagnostics[0].Message != "current" {
				t.Fatalf("Diagnostics = %+v, want current diagnostic", bd.Diagnostics)
			}
			return
		case <-deadline:
			t.Fatal("did not receive current BufferDiagnostics")
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
