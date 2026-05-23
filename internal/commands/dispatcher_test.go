package commands

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func newFunc(name, desc string, fn func(ctx context.Context, args map[string]any) error) Func {
	return Func{NameStr: name, DescriptionStr: desc, Fn: fn}
}

func TestDispatcher_Register_AddsCommand(t *testing.T) {
	d := New(nil)
	cmd := newFunc("editor.save", "Save the active buffer", nil)
	if err := d.Register(cmd); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !d.Has("editor.save") {
		t.Error("Has(\"editor.save\") = false after Register")
	}
	if d.Len() != 1 {
		t.Errorf("Len() = %d, want 1", d.Len())
	}
}

func TestDispatcher_Register_Duplicate_Error(t *testing.T) {
	d := New(nil)
	cmd := newFunc("dup", "", nil)
	if err := d.Register(cmd); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := d.Register(cmd)
	if !errors.Is(err, ErrDuplicateName) {
		t.Errorf("err = %v, want ErrDuplicateName", err)
	}
}

func TestDispatcher_Register_Nil_Error(t *testing.T) {
	d := New(nil)
	if err := d.Register(nil); !errors.Is(err, ErrNilCommand) {
		t.Errorf("err = %v, want ErrNilCommand", err)
	}
}

func TestDispatcher_Register_EmptyName_Error(t *testing.T) {
	d := New(nil)
	if err := d.Register(newFunc("", "no name", nil)); !errors.Is(err, ErrEmptyName) {
		t.Errorf("err = %v, want ErrEmptyName", err)
	}
}

func TestDispatcher_Dispatch_ExecutesCommand(t *testing.T) {
	d := New(nil)
	var ran atomic.Bool
	cmd := newFunc("run", "", func(ctx context.Context, args map[string]any) error {
		ran.Store(true)
		if got, ok := args["x"].(int); !ok || got != 7 {
			t.Errorf("args[\"x\"] = %v, want 7", args["x"])
		}
		return nil
	})
	if err := d.Register(cmd); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := d.Dispatch(context.Background(), "run", map[string]any{"x": 7}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !ran.Load() {
		t.Error("command did not execute")
	}
}

func TestDispatcher_Dispatch_Unknown_Error(t *testing.T) {
	d := New(nil)
	err := d.Dispatch(context.Background(), "missing", nil)
	if !errors.Is(err, ErrUnknownCommand) {
		t.Errorf("err = %v, want ErrUnknownCommand", err)
	}
}

func TestDispatcher_Dispatch_PropagatesError(t *testing.T) {
	d := New(nil)
	boom := errors.New("boom")
	cmd := newFunc("fail", "", func(ctx context.Context, args map[string]any) error {
		return boom
	})
	if err := d.Register(cmd); err != nil {
		t.Fatalf("Register: %v", err)
	}
	err := d.Dispatch(context.Background(), "fail", nil)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want it to wrap boom", err)
	}
}

func TestDispatcher_Palette_SortedByName(t *testing.T) {
	d := New(nil)
	for _, n := range []string{"zebra", "alpha", "mango"} {
		if err := d.Register(newFunc(n, "", nil)); err != nil {
			t.Fatalf("Register(%q): %v", n, err)
		}
	}
	got := d.Palette()
	want := []string{"alpha", "mango", "zebra"}
	if len(got) != len(want) {
		t.Fatalf("Palette() len = %d, want %d", len(got), len(want))
	}
	for i, c := range got {
		if c.Name() != want[i] {
			t.Errorf("Palette()[%d].Name() = %q, want %q", i, c.Name(), want[i])
		}
	}
}

func TestDispatcher_Unregister_RemovesCommand(t *testing.T) {
	d := New(nil)
	if err := d.Register(newFunc("temp", "", nil)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	d.Unregister("temp")
	if d.Has("temp") {
		t.Error("command should be gone after Unregister")
	}
	// Idempotent.
	d.Unregister("temp")
}

func TestDispatcher_Concurrent(t *testing.T) {
	d := New(nil)
	const workers = 16
	const cmds = 50

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < cmds; i++ {
				name := commandName(w, i)
				if err := d.Register(newFunc(name, "", nil)); err != nil {
					t.Errorf("Register(%q): %v", name, err)
					return
				}
				if err := d.Dispatch(context.Background(), name, nil); err != nil {
					t.Errorf("Dispatch(%q): %v", name, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	if got := d.Len(); got != workers*cmds {
		t.Errorf("Len() = %d, want %d", got, workers*cmds)
	}
}

func commandName(worker, i int) string {
	// Avoid fmt to keep the hot loop minimal.
	const digits = "0123456789"
	buf := []byte("cmd-")
	if worker >= 10 {
		buf = append(buf, digits[worker/10])
	}
	buf = append(buf, digits[worker%10])
	buf = append(buf, '-')
	if i >= 100 {
		buf = append(buf, digits[i/100])
	}
	if i >= 10 {
		buf = append(buf, digits[(i/10)%10])
	}
	buf = append(buf, digits[i%10])
	return string(buf)
}
