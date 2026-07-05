package ui

import (
	"context"
	"testing"
	"time"

	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/lsp"
)

// publishDiagnostics pushes a BufferDiagnostics event through the engine's
// topic, the same path the LSP bridge uses.
func publishDiagnostics(t *testing.T, engine *editor.EditorEngine, ev editor.BufferDiagnostics) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := engine.Events().Publish(ctx, ev); err != nil {
		t.Fatalf("publish diagnostics: %v", err)
	}
}

func waitForDiagCount(t *testing.T, c *LSPController, id editor.BufferID, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.DiagnosticCount(id) == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("DiagnosticCount(%d) = %d, want %d", id, c.DiagnosticCount(id), want)
}

func TestLSPController_StoresDiagnostics(t *testing.T) {
	engine := editor.NewEngine(editor.EngineOptions{})
	c := NewLSPController(nil, engine)
	defer c.Close()

	id := editor.BufferID(1)
	publishDiagnostics(t, engine, editor.BufferDiagnostics{
		ID: id,
		Diagnostics: []editor.Diagnostic{
			{Message: "undeclared name: x", Severity: editor.DiagnosticSeverityError},
			{Message: "unused variable", Severity: editor.DiagnosticSeverityWarning},
		},
	})

	waitForDiagCount(t, c, id, 2)
	got := c.Diagnostics(id)
	if len(got) != 2 || got[0].Message != "undeclared name: x" {
		t.Errorf("Diagnostics = %+v, want the two published entries", got)
	}
}

func TestLSPController_EmptyDiagnostics_ClearsBuffer(t *testing.T) {
	engine := editor.NewEngine(editor.EngineOptions{})
	c := NewLSPController(nil, engine)
	defer c.Close()

	id := editor.BufferID(3)
	publishDiagnostics(t, engine, editor.BufferDiagnostics{
		ID:          id,
		Diagnostics: []editor.Diagnostic{{Message: "boom"}},
	})
	waitForDiagCount(t, c, id, 1)

	// An empty publish clears the buffer's diagnostics.
	publishDiagnostics(t, engine, editor.BufferDiagnostics{ID: id})
	waitForDiagCount(t, c, id, 0)
}

func TestLSPController_NilSupervisor_RequestsNotReady(t *testing.T) {
	engine := editor.NewEngine(editor.EngineOptions{})
	c := NewLSPController(nil, engine)
	defer c.Close()

	if c.State() != lsp.ServerStateStopped {
		t.Errorf("State = %v, want stopped with nil supervisor", c.State())
	}
	ctx := context.Background()
	if _, err := c.Complete(ctx, 1, editor.Position{}); err != lsp.ErrServerNotReady {
		t.Errorf("Complete err = %v, want ErrServerNotReady", err)
	}
	if _, err := c.Hover(ctx, 1, editor.Position{}); err != lsp.ErrServerNotReady {
		t.Errorf("Hover err = %v, want ErrServerNotReady", err)
	}
	if _, err := c.Definition(ctx, 1, editor.Position{}); err != lsp.ErrServerNotReady {
		t.Errorf("Definition err = %v, want ErrServerNotReady", err)
	}
	if _, err := c.References(ctx, 1, editor.Position{}); err != lsp.ErrServerNotReady {
		t.Errorf("References err = %v, want ErrServerNotReady", err)
	}
	if _, err := c.Format(ctx, 1, lsp.FormattingOptions{}); err != lsp.ErrServerNotReady {
		t.Errorf("Format err = %v, want ErrServerNotReady", err)
	}
}

func TestLSPController_Close_Idempotent(t *testing.T) {
	engine := editor.NewEngine(editor.EngineOptions{})
	c := NewLSPController(nil, engine)
	c.Close()
	c.Close() // must not panic or hang
}

func TestLSPController_Diagnostics_DefensiveCopy(t *testing.T) {
	engine := editor.NewEngine(editor.EngineOptions{})
	c := NewLSPController(nil, engine)
	defer c.Close()

	id := editor.BufferID(7)
	publishDiagnostics(t, engine, editor.BufferDiagnostics{
		ID:          id,
		Diagnostics: []editor.Diagnostic{{Message: "one"}},
	})
	waitForDiagCount(t, c, id, 1)

	got := c.Diagnostics(id)
	got[0].Message = "mutated"
	if c.Diagnostics(id)[0].Message != "one" {
		t.Error("Diagnostics did not return a defensive copy")
	}
}
