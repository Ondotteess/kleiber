package editor

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEngine_NewView_RegistersAndPublishes(t *testing.T) {
	e := newTestEngine(t)
	sub, cancel := e.Events().Subscribe(8)
	defer cancel()

	bid := e.NewBuffer("hello")
	<-sub // BufferOpened

	vid, err := e.NewView(bid)
	if err != nil {
		t.Fatalf("NewView: %v", err)
	}
	if vid == 0 {
		t.Error("ViewID is zero")
	}

	select {
	case ev := <-sub:
		opened, ok := ev.(ViewOpened)
		if !ok {
			t.Fatalf("got %T, want ViewOpened", ev)
		}
		if opened.ID != vid {
			t.Errorf("ViewOpened.ID = %v, want %v", opened.ID, vid)
		}
		if opened.BufferID != bid {
			t.Errorf("ViewOpened.BufferID = %v, want %v", opened.BufferID, bid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive ViewOpened in 2s")
	}
}

func TestEngine_NewView_UnknownBuffer_Errors(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.NewView(BufferID(9999))
	if !errors.Is(err, ErrBufferNotFound) {
		t.Errorf("err = %v, want ErrBufferNotFound", err)
	}
}

func TestEngine_View_ReturnsLiveView(t *testing.T) {
	e := newTestEngine(t)
	bid := e.NewBuffer("hello")
	vid, err := e.NewView(bid)
	if err != nil {
		t.Fatalf("NewView: %v", err)
	}
	v, err := e.View(vid)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if v.Buffer().Text() != "hello" {
		t.Errorf("buffer text = %q, want hello", v.Buffer().Text())
	}
}

func TestEngine_View_UnknownID_Errors(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.View(ViewID(9999))
	if !errors.Is(err, ErrViewNotFound) {
		t.Errorf("err = %v, want ErrViewNotFound", err)
	}
}

func TestEngine_CloseView_PublishesAndRemoves(t *testing.T) {
	e := newTestEngine(t)
	sub, cancel := e.Events().Subscribe(8)
	defer cancel()

	bid := e.NewBuffer("hello")
	<-sub // BufferOpened
	vid, err := e.NewView(bid)
	if err != nil {
		t.Fatalf("NewView: %v", err)
	}
	<-sub // ViewOpened

	if err := e.CloseView(vid); err != nil {
		t.Fatalf("CloseView: %v", err)
	}
	ev := <-sub
	closed, ok := ev.(ViewClosed)
	if !ok {
		t.Fatalf("got %T, want ViewClosed", ev)
	}
	if closed.ID != vid {
		t.Errorf("ViewClosed.ID = %v, want %v", closed.ID, vid)
	}

	if _, err := e.View(vid); !errors.Is(err, ErrViewNotFound) {
		t.Errorf("View after CloseView: err = %v, want ErrViewNotFound", err)
	}
}

func TestEngine_CloseView_UnknownID_Errors(t *testing.T) {
	e := newTestEngine(t)
	if err := e.CloseView(ViewID(9999)); !errors.Is(err, ErrViewNotFound) {
		t.Errorf("err = %v, want ErrViewNotFound", err)
	}
}

func TestEngine_Views_FilterByBuffer(t *testing.T) {
	e := newTestEngine(t)
	bid1 := e.NewBuffer("a")
	bid2 := e.NewBuffer("b")
	v1, _ := e.NewView(bid1)
	v2, _ := e.NewView(bid1)
	v3, _ := e.NewView(bid2)

	all := e.Views(0)
	if len(all) != 3 {
		t.Fatalf("Views(0) len = %d, want 3", len(all))
	}
	if all[0].ID != v1 || all[1].ID != v2 || all[2].ID != v3 {
		t.Errorf("Views(0) IDs = %v %v %v, want %v %v %v",
			all[0].ID, all[1].ID, all[2].ID, v1, v2, v3)
	}

	onBid1 := e.Views(bid1)
	if len(onBid1) != 2 {
		t.Fatalf("Views(bid1) len = %d, want 2", len(onBid1))
	}
	for _, r := range onBid1 {
		if r.BufferID != bid1 {
			t.Errorf("Views(bid1) returned view on %v", r.BufferID)
		}
	}
}

func TestEngine_Close_CleansUpViews(t *testing.T) {
	e := newTestEngine(t)
	sub, cancel := e.Events().Subscribe(16)
	defer cancel()

	bid := e.NewBuffer("x")
	<-sub // BufferOpened
	v1, _ := e.NewView(bid)
	<-sub // ViewOpened (v1)
	v2, _ := e.NewView(bid)
	<-sub // ViewOpened (v2)

	if err := e.Close(bid); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Expect ViewClosed (v1), ViewClosed (v2), BufferClosed — in that order.
	events := drainEvents(t, sub, 3, 2*time.Second)
	if vc, ok := events[0].(ViewClosed); !ok || vc.ID != v1 {
		t.Errorf("events[0] = %T %+v, want ViewClosed{ID:%d}", events[0], events[0], v1)
	}
	if vc, ok := events[1].(ViewClosed); !ok || vc.ID != v2 {
		t.Errorf("events[1] = %T %+v, want ViewClosed{ID:%d}", events[1], events[1], v2)
	}
	if _, ok := events[2].(BufferClosed); !ok {
		t.Errorf("events[2] = %T, want BufferClosed", events[2])
	}

	if _, err := e.View(v1); !errors.Is(err, ErrViewNotFound) {
		t.Errorf("View(v1) after Close: err = %v, want ErrViewNotFound", err)
	}
}

func TestEngine_View_SelectionChangePublished(t *testing.T) {
	e := newTestEngine(t)
	sub, cancel := e.Events().Subscribe(16)
	defer cancel()

	bid := e.NewBuffer("hello")
	<-sub // BufferOpened
	vid, _ := e.NewView(bid)
	<-sub // ViewOpened

	v, _ := e.View(vid)
	if err := v.MoveTo(Position{0, 3}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}

	ev := <-sub
	sc, ok := ev.(ViewSelectionChanged)
	if !ok {
		t.Fatalf("got %T, want ViewSelectionChanged", ev)
	}
	if sc.ID != vid {
		t.Errorf("ID = %v, want %v", sc.ID, vid)
	}
	if !sc.Change.New.Cursor.Equal(Position{0, 3}) {
		t.Errorf("New.Cursor = %v, want 0:3", sc.Change.New.Cursor)
	}
	if sc.Change.Cause != SelectionCauseMove {
		t.Errorf("Cause = %v, want SelectionCauseMove", sc.Change.Cause)
	}
}

func TestEngine_ViewIDs_AreMonotonic(t *testing.T) {
	e := newTestEngine(t)
	bid := e.NewBuffer("x")
	a, _ := e.NewView(bid)
	b, _ := e.NewView(bid)
	c, _ := e.NewView(bid)
	if !(a < b && b < c) {
		t.Errorf("ViewIDs %d %d %d not monotonic", a, b, c)
	}
}

// Regression: closing a buffer that has views should not deadlock or
// race with a concurrent buffer mutation.
func TestEngine_Close_WithActiveView_Safe(t *testing.T) {
	e := newTestEngine(t)
	bid := e.NewBuffer("hello")
	_, _ = e.NewView(bid)

	// Save first so dirty tracking has a clean baseline; not actually
	// necessary, but exercises the same paths a real editor would hit.
	dir := t.TempDir()
	if err := e.SaveAs(context.Background(), bid, dir+"/x"); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}

	if err := e.Close(bid); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
