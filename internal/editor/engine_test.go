package editor

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testEngineLogger(_ *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestEngine(t *testing.T) *EditorEngine {
	t.Helper()
	return NewEngine(EngineOptions{Logger: testEngineLogger(t)})
}

// drainEvents reads exactly want events from sub, failing the test
// if they do not arrive within timeout.
func drainEvents(t *testing.T, sub <-chan BufferEvent, want int, timeout time.Duration) []BufferEvent {
	t.Helper()
	events := make([]BufferEvent, 0, want)
	deadline := time.After(timeout)
	for len(events) < want {
		select {
		case ev := <-sub:
			events = append(events, ev)
		case <-deadline:
			t.Fatalf("drainEvents timed out after %v; got %d/%d", timeout, len(events), want)
		}
	}
	return events
}

// --- Open / NewBuffer ------------------------------------------------

func TestEngine_Open_ReadsFileAndAssignsID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	e := newTestEngine(t)
	id, err := e.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if id == 0 {
		t.Error("BufferID is zero; expected non-zero monotonic ID")
	}
	buf, err := e.Buffer(id)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if buf.Text() != "hello world" {
		t.Errorf("Text() = %q, want %q", buf.Text(), "hello world")
	}
	gotPath, err := e.Path(id)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if gotPath == "" {
		t.Error("Path is empty after Open; expected absolute path")
	}
}

func TestEngine_Open_NonexistentFile_Errors(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.Open(context.Background(), filepath.Join(t.TempDir(), "nope.txt"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want it to wrap fs.ErrNotExist", err)
	}
}

func TestEngine_Open_PublishesBufferOpened(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	e := newTestEngine(t)
	sub, cancel := e.Events().Subscribe(8)
	defer cancel()

	id, err := e.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	select {
	case ev := <-sub:
		opened, ok := ev.(BufferOpened)
		if !ok {
			t.Fatalf("got %T, want BufferOpened", ev)
		}
		if opened.ID != id {
			t.Errorf("BufferOpened.ID = %v, want %v", opened.ID, id)
		}
		if opened.Path == "" {
			t.Error("BufferOpened.Path is empty; expected absolute path")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive BufferOpened in 2s")
	}
}

func TestEngine_NewBuffer_AssignsID_NoPath(t *testing.T) {
	e := newTestEngine(t)
	id := e.NewBuffer("hello")
	if id == 0 {
		t.Error("ID is zero")
	}
	path, err := e.Path(id)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if path != "" {
		t.Errorf("Path = %q, want empty (untitled)", path)
	}
}

func TestEngine_IDsAreMonotonic(t *testing.T) {
	e := newTestEngine(t)
	a := e.NewBuffer("a")
	b := e.NewBuffer("b")
	c := e.NewBuffer("c")
	if !(a < b && b < c) {
		t.Errorf("IDs %d %d %d not monotonic", a, b, c)
	}
}

// --- Close ----------------------------------------------------------

func TestEngine_Close_RemovesAndPublishes(t *testing.T) {
	e := newTestEngine(t)
	sub, cancel := e.Events().Subscribe(8)
	defer cancel()

	id := e.NewBuffer("x")
	<-sub // BufferOpened

	if err := e.Close(id); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ev := <-sub
	if _, ok := ev.(BufferClosed); !ok {
		t.Errorf("got %T, want BufferClosed", ev)
	}

	if _, err := e.Buffer(id); !errors.Is(err, ErrBufferNotFound) {
		t.Errorf("Buffer after Close: err = %v, want ErrBufferNotFound", err)
	}
}

func TestEngine_Close_StopsForwardingBufferChanges(t *testing.T) {
	e := newTestEngine(t)
	sub, cancel := e.Events().Subscribe(8)
	defer cancel()

	id := e.NewBuffer("x")
	<-sub // BufferOpened

	buf, err := e.Buffer(id)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if err := e.Close(id); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-sub // BufferClosed

	if _, err := buf.Insert(Position{0, 1}, "y"); err != nil {
		t.Fatalf("Insert after Close: %v", err)
	}

	select {
	case ev := <-sub:
		t.Fatalf("unexpected event after Close: %T", ev)
	default:
	}
}

func TestEngine_Close_UnknownID_Errors(t *testing.T) {
	e := newTestEngine(t)
	err := e.Close(BufferID(9999))
	if !errors.Is(err, ErrBufferNotFound) {
		t.Errorf("err = %v, want ErrBufferNotFound", err)
	}
}

// --- Save / SaveAs --------------------------------------------------

func TestEngine_Save_Untitled_Errors(t *testing.T) {
	e := newTestEngine(t)
	id := e.NewBuffer("x")
	err := e.Save(context.Background(), id)
	if !errors.Is(err, ErrBufferUntitled) {
		t.Errorf("err = %v, want ErrBufferUntitled", err)
	}
}

func TestEngine_SaveAs_WritesAndUpdatesPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	e := newTestEngine(t)
	sub, cancel := e.Events().Subscribe(8)
	defer cancel()

	id := e.NewBuffer("v1")
	<-sub // BufferOpened

	if err := e.SaveAs(context.Background(), id, path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	ev := <-sub
	saved, ok := ev.(BufferSaved)
	if !ok {
		t.Fatalf("got %T, want BufferSaved", ev)
	}
	if saved.Path == "" {
		t.Errorf("BufferSaved.Path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "v1" {
		t.Errorf("file content = %q, want v1", data)
	}

	gotPath, err := e.Path(id)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if gotPath == "" {
		t.Error("Path is empty after SaveAs")
	}
}

func TestEngine_SaveAs_EmptyPath_Errors(t *testing.T) {
	e := newTestEngine(t)
	id := e.NewBuffer("x")
	err := e.SaveAs(context.Background(), id, "")
	if !errors.Is(err, ErrEmptySavePath) {
		t.Errorf("err = %v, want ErrEmptySavePath", err)
	}
}

func TestEngine_Save_AfterSaveAs_WritesNewContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")

	e := newTestEngine(t)
	id := e.NewBuffer("hello")
	if err := e.SaveAs(context.Background(), id, path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	buf, _ := e.Buffer(id)
	if _, err := buf.Insert(Position{0, 5}, " world"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := e.Save(context.Background(), id); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("file = %q, want %q", data, "hello world")
	}
}

func TestEngine_SaveAs_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "deep", "nested", "file.txt")

	e := newTestEngine(t)
	id := e.NewBuffer("hello")
	if err := e.SaveAs(context.Background(), id, nested); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestEngine_Save_UnknownID_Errors(t *testing.T) {
	e := newTestEngine(t)
	err := e.Save(context.Background(), BufferID(9999))
	if !errors.Is(err, ErrBufferNotFound) {
		t.Errorf("err = %v, want ErrBufferNotFound", err)
	}
}

func TestEngine_Dirty_OpenTracksMutationsAndUndo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	e := newTestEngine(t)
	id, err := e.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if dirty, err := e.Dirty(id); err != nil || dirty {
		t.Fatalf("Dirty after Open = %v, %v; want false, nil", dirty, err)
	}

	buf, _ := e.Buffer(id)
	if _, err := buf.Insert(Position{0, 5}, " world"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if dirty, err := e.Dirty(id); err != nil || !dirty {
		t.Fatalf("Dirty after edit = %v, %v; want true, nil", dirty, err)
	}

	if _, ok := buf.Undo(); !ok {
		t.Fatal("Undo: false")
	}
	if dirty, err := e.Dirty(id); err != nil || dirty {
		t.Fatalf("Dirty after undo to clean text = %v, %v; want false, nil", dirty, err)
	}
}

func TestEngine_Dirty_UntitledUntilSaveAs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "draft.txt")

	e := newTestEngine(t)
	id := e.NewBuffer("draft")
	if dirty, err := e.Dirty(id); err != nil || !dirty {
		t.Fatalf("Dirty for untitled = %v, %v; want true, nil", dirty, err)
	}

	if err := e.SaveAs(context.Background(), id, path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if dirty, err := e.Dirty(id); err != nil || dirty {
		t.Fatalf("Dirty after SaveAs = %v, %v; want false, nil", dirty, err)
	}

	buf, _ := e.Buffer(id)
	if _, err := buf.Insert(Position{0, 5}, "!"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if dirty, err := e.Dirty(id); err != nil || !dirty {
		t.Fatalf("Dirty after edit = %v, %v; want true, nil", dirty, err)
	}

	if err := e.Save(context.Background(), id); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if dirty, err := e.Dirty(id); err != nil || dirty {
		t.Fatalf("Dirty after Save = %v, %v; want false, nil", dirty, err)
	}
}

func TestEngine_Dirty_UnknownID_Errors(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.Dirty(BufferID(9999))
	if !errors.Is(err, ErrBufferNotFound) {
		t.Errorf("err = %v, want ErrBufferNotFound", err)
	}
}

func TestEngine_Buffers_IncludesDirty(t *testing.T) {
	e := newTestEngine(t)
	id := e.NewBuffer("draft")
	refs := e.Buffers()
	if len(refs) != 1 {
		t.Fatalf("Buffers len = %d, want 1", len(refs))
	}
	if refs[0].ID != id || !refs[0].Dirty {
		t.Errorf("Buffers()[0] = %+v, want ID %d and Dirty true", refs[0], id)
	}
}

// --- Mutation events ------------------------------------------------

func TestEngine_Mutation_PublishesBufferChanged(t *testing.T) {
	e := newTestEngine(t)
	sub, cancel := e.Events().Subscribe(8)
	defer cancel()

	id := e.NewBuffer("a")
	<-sub // BufferOpened

	buf, _ := e.Buffer(id)
	if _, err := buf.Insert(Position{0, 1}, "b"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	ev := <-sub
	bc, ok := ev.(BufferChanged)
	if !ok {
		t.Fatalf("got %T, want BufferChanged", ev)
	}
	if bc.ID != id {
		t.Errorf("ID = %v, want %v", bc.ID, id)
	}
	if bc.Change.Kind != ChangeKindInsert {
		t.Errorf("Kind = %v, want Insert", bc.Change.Kind)
	}
	if bc.Change.Edit.Inserted != "b" {
		t.Errorf("Inserted = %q", bc.Change.Edit.Inserted)
	}
}

func TestEngine_Events_OrderedPerBuffer(t *testing.T) {
	e := newTestEngine(t)
	sub, cancel := e.Events().Subscribe(64)
	defer cancel()

	id := e.NewBuffer("")
	buf, _ := e.Buffer(id)

	const n = 10
	for i := 0; i < n; i++ {
		if _, err := buf.Insert(Position{0, i}, "x"); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	events := drainEvents(t, sub, 1+n, 2*time.Second)
	if _, ok := events[0].(BufferOpened); !ok {
		t.Errorf("events[0] = %T, want BufferOpened", events[0])
	}
	var lastSeq int
	for i := 1; i <= n; i++ {
		bc, ok := events[i].(BufferChanged)
		if !ok {
			t.Fatalf("events[%d] = %T, want BufferChanged", i, events[i])
		}
		if bc.Change.Seq <= lastSeq {
			t.Errorf("events[%d] Seq = %d, not strictly greater than %d",
				i, bc.Change.Seq, lastSeq)
		}
		lastSeq = bc.Change.Seq
	}
}

func TestEngine_ConcurrentMutationsOnDifferentBuffers(t *testing.T) {
	e := newTestEngine(t)
	sub, cancel := e.Events().Subscribe(1024)
	defer cancel()

	const nBuffers = 4
	const nInserts = 20

	ids := make([]BufferID, nBuffers)
	for i := range ids {
		ids[i] = e.NewBuffer("")
	}

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id BufferID) {
			defer wg.Done()
			buf, _ := e.Buffer(id)
			for i := 0; i < nInserts; i++ {
				if _, err := buf.Insert(Position{0, i}, "x"); err != nil {
					t.Errorf("Insert: %v", err)
					return
				}
			}
		}(id)
	}
	wg.Wait()

	total := nBuffers + nBuffers*nInserts
	events := drainEvents(t, sub, total, 5*time.Second)

	// Per-buffer Seq must be strictly monotonic in event order.
	seqByID := map[BufferID]int{}
	for _, ev := range events {
		bc, ok := ev.(BufferChanged)
		if !ok {
			continue
		}
		if bc.Change.Seq <= seqByID[bc.ID] {
			t.Errorf("buffer %d Seq %d not greater than previous %d",
				bc.ID, bc.Change.Seq, seqByID[bc.ID])
		}
		seqByID[bc.ID] = bc.Change.Seq
	}

	// Each buffer accumulated exactly nInserts changes.
	changes := map[BufferID]int{}
	for _, ev := range events {
		if bc, ok := ev.(BufferChanged); ok {
			changes[bc.ID]++
		}
	}
	for _, id := range ids {
		if changes[id] != nInserts {
			t.Errorf("buffer %d received %d changes, want %d", id, changes[id], nInserts)
		}
	}
}

// --- Buffers / Buffer / Path ----------------------------------------

func TestEngine_Buffers_SortedByID(t *testing.T) {
	e := newTestEngine(t)
	id1 := e.NewBuffer("a")
	id2 := e.NewBuffer("b")
	id3 := e.NewBuffer("c")

	refs := e.Buffers()
	if len(refs) != 3 {
		t.Fatalf("Buffers len = %d, want 3", len(refs))
	}
	if refs[0].ID != id1 || refs[1].ID != id2 || refs[2].ID != id3 {
		t.Errorf("ID order = %v %v %v, want %v %v %v",
			refs[0].ID, refs[1].ID, refs[2].ID, id1, id2, id3)
	}
}

func TestEngine_Buffer_UnknownID_Errors(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.Buffer(BufferID(9999))
	if !errors.Is(err, ErrBufferNotFound) {
		t.Errorf("err = %v, want ErrBufferNotFound", err)
	}
}

func TestEngine_Path_UnknownID_Errors(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.Path(BufferID(9999))
	if !errors.Is(err, ErrBufferNotFound) {
		t.Errorf("err = %v, want ErrBufferNotFound", err)
	}
}

// --- atomicWriteFile ------------------------------------------------

func TestAtomicWriteFile_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := atomicWriteFile(path, []byte("v2")); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "v2" {
		t.Errorf("content = %q, want v2", data)
	}
}

func TestAtomicWriteFile_EmptyPath_Errors(t *testing.T) {
	err := atomicWriteFile("", []byte("x"))
	if err == nil {
		t.Error("err is nil, want non-nil")
	}
}

func TestAtomicWriteFile_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c.txt")
	if err := atomicWriteFile(nested, []byte("data")); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	data, err := os.ReadFile(nested)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "data" {
		t.Errorf("content = %q, want data", data)
	}
}

// --- Save events do not include the changed event for SaveAs -------

func TestEngine_NewBuffer_PublishesBufferOpened(t *testing.T) {
	e := newTestEngine(t)
	sub, cancel := e.Events().Subscribe(8)
	defer cancel()

	id := e.NewBuffer("hello")
	select {
	case ev := <-sub:
		opened, ok := ev.(BufferOpened)
		if !ok {
			t.Fatalf("got %T, want BufferOpened", ev)
		}
		if opened.ID != id {
			t.Errorf("ID = %v, want %v", opened.ID, id)
		}
		if opened.Path != "" {
			t.Errorf("Path = %q, want empty (untitled)", opened.Path)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive BufferOpened in 1s")
	}
}
