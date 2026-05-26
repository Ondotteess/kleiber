package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/Ondotteess/kleiber/internal/commands"
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
		"bufferID":     int(id),
		"tabSize":      int64(2),
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
