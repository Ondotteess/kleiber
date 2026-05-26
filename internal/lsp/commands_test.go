package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"

	"github.com/Ondotteess/kleiber/internal/commands"
	"github.com/Ondotteess/kleiber/internal/config"
	"github.com/Ondotteess/kleiber/internal/editor"
)

func TestRegisterBridgeCommands_AddsCommands(t *testing.T) {
	f := newBridgeFixture(t)
	d := commands.New(nil)

	if err := RegisterBridgeCommands(d, f.bridge); err != nil {
		t.Fatalf("RegisterBridgeCommands: %v", err)
	}
	if !d.Has(CommandFormatBuffer) {
		t.Errorf("dispatcher missing %s", CommandFormatBuffer)
	}
	if !d.Has(CommandFormatAndSaveBuffer) {
		t.Errorf("dispatcher missing %s", CommandFormatAndSaveBuffer)
	}
}

func TestRegisterBridgeCommands_NilInputsError(t *testing.T) {
	if err := RegisterBridgeCommands(nil, nil); !errors.Is(err, ErrCommandDispatcherNil) {
		t.Errorf("nil dispatcher err = %v, want ErrCommandDispatcherNil", err)
	}
	d := commands.New(nil)
	if err := RegisterBridgeCommands(d, nil); !errors.Is(err, ErrCommandBridgeNil) {
		t.Errorf("nil bridge err = %v, want ErrCommandBridgeNil", err)
	}
}

func TestRegisterSaveCommand_AddsCommand(t *testing.T) {
	f := newBridgeFixture(t)
	d := commands.New(nil)

	if err := RegisterSaveCommand(d, f.engine, f.bridge, config.Default()); err != nil {
		t.Fatalf("RegisterSaveCommand: %v", err)
	}
	if !d.Has(CommandSaveBuffer) {
		t.Errorf("dispatcher missing %s", CommandSaveBuffer)
	}
}

func TestRegisterSaveCommand_NilInputsError(t *testing.T) {
	if err := RegisterSaveCommand(nil, nil, nil, config.Default()); !errors.Is(err, ErrCommandDispatcherNil) {
		t.Errorf("nil dispatcher err = %v, want ErrCommandDispatcherNil", err)
	}
	d := commands.New(nil)
	if err := RegisterSaveCommand(d, nil, nil, config.Default()); !errors.Is(err, ErrCommandEngineNil) {
		t.Errorf("nil engine err = %v, want ErrCommandEngineNil", err)
	}
}

func TestBridgeCommands_FormatAndSave_DispatchesToBridge(t *testing.T) {
	f := newBridgeFixture(t)
	d := commands.New(nil)
	if err := RegisterBridgeCommands(d, f.bridge); err != nil {
		t.Fatalf("RegisterBridgeCommands: %v", err)
	}

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})
	f.server.Handle(MethodTextDocumentFormatting, func(req *Request) *Response {
		var p DocumentFormattingParams
		_ = json.Unmarshal(req.Params, &p)
		if p.Options.TabSize != 2 {
			t.Errorf("TabSize = %d, want 2", p.Options.TabSize)
		}
		if !p.Options.InsertSpaces {
			t.Error("InsertSpaces = false, want true")
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

	err = d.Dispatch(context.Background(), CommandFormatAndSaveBuffer, map[string]any{
		"bufferID":     json.Number(strconv.FormatInt(int64(id), 10)),
		"tabSize":      float64(2),
		"insertSpaces": true,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); got != "package x\n\nfunc main() {}\n" {
		t.Errorf("file content = %q", got)
	}
}

func TestSaveBuffer_FormatOnSaveDisabled_PlainSave(t *testing.T) {
	f := newBridgeFixture(t)

	path := writeGoFile(t, "package x\n")
	id, err := f.engine.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	buf, err := f.engine.Buffer(id)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if _, err := buf.Insert(editorEnd(buf), "\nfunc  main( ){}\n"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	cfg := config.Default()
	if err := SaveBuffer(context.Background(), f.engine, f.bridge, cfg, id); err != nil {
		t.Fatalf("SaveBuffer: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); got != "package x\n\nfunc  main( ){}\n" {
		t.Errorf("file content = %q, want unformatted save", got)
	}
}

func TestSaveBuffer_FormatOnSaveEnabled_FormatsTrackedGoBuffer(t *testing.T) {
	f := newBridgeFixture(t)

	openSeen := make(chan *Notification, 1)
	f.server.HandleNotification(MethodTextDocumentDidOpen, func(n *Notification) {
		openSeen <- n
	})
	formatSeen := make(chan struct{}, 1)
	f.server.Handle(MethodTextDocumentFormatting, func(req *Request) *Response {
		formatSeen <- struct{}{}
		var p DocumentFormattingParams
		_ = json.Unmarshal(req.Params, &p)
		if p.Options.TabSize != 2 {
			t.Errorf("TabSize = %d, want 2", p.Options.TabSize)
		}
		if !p.Options.InsertSpaces {
			t.Error("InsertSpaces = false, want true")
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

	cfg := config.Default()
	cfg.Editor.FormatOnSave = true
	cfg.Editor.TabSize = 2
	cfg.Editor.InsertSpaces = true
	if err := SaveBuffer(context.Background(), f.engine, f.bridge, cfg, id); err != nil {
		t.Fatalf("SaveBuffer: %v", err)
	}

	select {
	case <-formatSeen:
	default:
		t.Fatal("format request was not observed")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); got != "package x\n\nfunc main() {}\n" {
		t.Errorf("file content = %q, want formatted save", got)
	}
}

func TestSaveBuffer_FormatErrorPreventsSave(t *testing.T) {
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
	if _, err := buf.Insert(editorEnd(buf), "\nfunc  main( ){}\n"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	cfg := config.Default()
	cfg.Editor.FormatOnSave = true
	err = SaveBuffer(context.Background(), f.engine, f.bridge, cfg, id)
	var respErr *ResponseError
	if !errors.As(err, &respErr) {
		t.Fatalf("err = %v, want *ResponseError", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); got != "package  x\n" {
		t.Errorf("file content = %q, want original content on formatting error", got)
	}
}

func TestBridgeCommands_MissingBufferIDErrors(t *testing.T) {
	f := newBridgeFixture(t)
	d := commands.New(nil)
	if err := RegisterBridgeCommands(d, f.bridge); err != nil {
		t.Fatalf("RegisterBridgeCommands: %v", err)
	}

	err := d.Dispatch(context.Background(), CommandFormatBuffer, nil)
	if !errors.Is(err, ErrCommandMissingArg) {
		t.Errorf("err = %v, want ErrCommandMissingArg", err)
	}
}

func editorEnd(buf *editor.Buffer) editor.Position {
	text := buf.Text()
	line, col := 0, 0
	for _, r := range text {
		if r == '\n' {
			line++
			col = 0
			continue
		}
		col += len(string(r))
	}
	return editor.Position{Line: line, Column: col}
}

func TestBridgeCommands_InvalidOptionsError(t *testing.T) {
	f := newBridgeFixture(t)
	d := commands.New(nil)
	if err := RegisterBridgeCommands(d, f.bridge); err != nil {
		t.Fatalf("RegisterBridgeCommands: %v", err)
	}

	err := d.Dispatch(context.Background(), CommandFormatBuffer, map[string]any{
		"bufferID": 1,
		"tabSize":  0,
	})
	if !errors.Is(err, ErrCommandInvalidArg) {
		t.Errorf("err = %v, want ErrCommandInvalidArg", err)
	}
}

func TestBridgeCommands_RejectNonIntegralJSONNumbers(t *testing.T) {
	f := newBridgeFixture(t)
	d := commands.New(nil)
	if err := RegisterBridgeCommands(d, f.bridge); err != nil {
		t.Fatalf("RegisterBridgeCommands: %v", err)
	}

	cases := []struct {
		name string
		args map[string]any
	}{
		{
			name: "fractional float bufferID",
			args: map[string]any{"bufferID": float64(1.5)},
		},
		{
			name: "negative number bufferID",
			args: map[string]any{"bufferID": json.Number("-1")},
		},
		{
			name: "overflow number bufferID",
			args: map[string]any{"bufferID": json.Number("9223372036854775808")},
		},
		{
			name: "fractional number tabSize",
			args: map[string]any{"bufferID": 1, "tabSize": json.Number("2.5")},
		},
		{
			name: "negative float tabSize",
			args: map[string]any{"bufferID": 1, "tabSize": float64(-2)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := d.Dispatch(context.Background(), CommandFormatBuffer, tc.args)
			if !errors.Is(err, ErrCommandInvalidArg) {
				t.Fatalf("err = %v, want ErrCommandInvalidArg", err)
			}
		})
	}
}
