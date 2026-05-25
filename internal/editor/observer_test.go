package editor

import (
	"sync"
	"testing"
	"time"
)

func TestChangeKind_String(t *testing.T) {
	cases := []struct {
		k    ChangeKind
		want string
	}{
		{ChangeKindInsert, "insert"},
		{ChangeKindDelete, "delete"},
		{ChangeKindUndo, "undo"},
		{ChangeKindRedo, "redo"},
		{ChangeKind(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.k.String(); got != tc.want {
			t.Errorf("ChangeKind(%d).String() = %q, want %q", int(tc.k), got, tc.want)
		}
	}
}

func TestBuffer_Observe_FiresOnInsert(t *testing.T) {
	b := NewBuffer("abc")
	var got []Change
	b.Observe(func(c Change) { got = append(got, c) })

	if _, err := b.Insert(Position{0, 1}, "X"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Kind != ChangeKindInsert {
		t.Errorf("Kind = %v, want Insert", got[0].Kind)
	}
	if got[0].Edit.Inserted != "X" {
		t.Errorf("Edit.Inserted = %q, want %q", got[0].Edit.Inserted, "X")
	}
	if got[0].Seq != b.Seq() {
		t.Errorf("Change.Seq = %d, Buffer.Seq() = %d", got[0].Seq, b.Seq())
	}
}

func TestBuffer_Observe_FiresOnDelete(t *testing.T) {
	b := NewBuffer("abcdef")
	var got []Change
	b.Observe(func(c Change) { got = append(got, c) })

	if _, err := b.Delete(Range{Position{0, 1}, Position{0, 4}}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Kind != ChangeKindDelete {
		t.Errorf("Kind = %v, want Delete", got[0].Kind)
	}
	if got[0].Edit.Removed != "bcd" {
		t.Errorf("Edit.Removed = %q, want %q", got[0].Edit.Removed, "bcd")
	}
}

func TestBuffer_Observe_FiresOnUndoRedo(t *testing.T) {
	b := NewBuffer("ab")
	if _, err := b.Insert(Position{0, 2}, "c"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var got []Change
	b.Observe(func(c Change) { got = append(got, c) })

	if _, ok := b.Undo(); !ok {
		t.Fatal("Undo: false")
	}
	if _, ok := b.Redo(); !ok {
		t.Fatal("Redo: false")
	}

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Kind != ChangeKindUndo {
		t.Errorf("got[0].Kind = %v, want Undo", got[0].Kind)
	}
	if got[1].Kind != ChangeKindRedo {
		t.Errorf("got[1].Kind = %v, want Redo", got[1].Kind)
	}
}

func TestBuffer_Observe_DoesNotFireOnNoOp(t *testing.T) {
	b := NewBuffer("abc")
	var calls int
	b.Observe(func(_ Change) { calls++ })

	// Empty insert is a no-op.
	if _, err := b.Insert(Position{0, 1}, ""); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// Empty range delete is a no-op.
	if _, err := b.Delete(Range{Position{0, 1}, Position{0, 1}}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if calls != 0 {
		t.Errorf("calls = %d, want 0 (no-ops must not notify observers)", calls)
	}
}

func TestBuffer_Observe_MultipleObservers_AllCalled(t *testing.T) {
	b := NewBuffer("a")
	var mu sync.Mutex
	var calls1, calls2 int
	b.Observe(func(_ Change) {
		mu.Lock()
		calls1++
		mu.Unlock()
	})
	b.Observe(func(_ Change) {
		mu.Lock()
		calls2++
		mu.Unlock()
	})

	if _, err := b.Insert(Position{0, 1}, "b"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if calls1 != 1 || calls2 != 1 {
		t.Errorf("calls = (%d, %d), want (1, 1)", calls1, calls2)
	}
}

func TestBuffer_Observe_NilObserver_Ignored(t *testing.T) {
	b := NewBuffer("a")
	// Must not panic.
	b.Observe(nil)
	if _, err := b.Insert(Position{0, 1}, "b"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if b.Text() != "ab" {
		t.Errorf("Text = %q", b.Text())
	}
}

func TestBuffer_Observe_FailedInsertDoesNotNotify(t *testing.T) {
	b := NewBuffer("abc")
	var calls int
	b.Observe(func(_ Change) { calls++ })

	// Out-of-bounds insert; should error.
	if _, err := b.Insert(Position{99, 0}, "X"); err == nil {
		t.Fatal("Insert returned nil error for out-of-bounds")
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0 (failed insert must not notify)", calls)
	}
}

func TestBuffer_Observe_CallbackCanReadBuffer(t *testing.T) {
	b := NewBuffer("a")
	seen := make(chan string, 1)
	b.Observe(func(_ Change) {
		seen <- b.Text()
	})

	if _, err := b.Insert(Position{0, 1}, "b"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	select {
	case got := <-seen:
		if got != "ab" {
			t.Errorf("observer saw Text() = %q, want %q", got, "ab")
		}
	case <-time.After(time.Second):
		t.Fatal("observer callback could not read buffer")
	}
}
