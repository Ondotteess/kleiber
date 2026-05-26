package editor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Ondotteess/kleiber/internal/commands"
)

func TestRegisterCommands_AddsExpectedCommands(t *testing.T) {
	d := commands.New(nil)
	e := newTestEngine(t)

	if err := RegisterCommands(d, e); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}
	for _, name := range []string{
		CommandOpenFile,
		CommandNewBuffer,
		CommandCloseBuffer,
		CommandSaveAsBuffer,
		CommandNewView,
		CommandCloseView,
		CommandMoveCursor,
		CommandInsertText,
		CommandBackspace,
		CommandDeleteSelection,
	} {
		if !d.Has(name) {
			t.Errorf("dispatcher missing %s", name)
		}
	}
}

func TestRegisterCommands_NilInputsError(t *testing.T) {
	if err := RegisterCommands(nil, nil); !errors.Is(err, ErrCommandDispatcherNil) {
		t.Errorf("nil dispatcher err = %v, want ErrCommandDispatcherNil", err)
	}
	d := commands.New(nil)
	if err := RegisterCommands(d, nil); !errors.Is(err, ErrCommandEngineNil) {
		t.Errorf("nil engine err = %v, want ErrCommandEngineNil", err)
	}
}

func TestEditorCommands_OpenFile_PublishesBufferOpened(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	e := newTestEngine(t)
	sub, cancel := e.Events().Subscribe(8)
	defer cancel()
	d := commands.New(nil)
	if err := RegisterCommands(d, e); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}

	if err := d.Dispatch(context.Background(), CommandOpenFile, map[string]any{"path": path}); err != nil {
		t.Fatalf("Dispatch openFile: %v", err)
	}

	select {
	case ev := <-sub:
		opened, ok := ev.(BufferOpened)
		if !ok {
			t.Fatalf("got %T, want BufferOpened", ev)
		}
		if opened.Path == "" {
			t.Fatal("BufferOpened.Path is empty")
		}
		buf, err := e.Buffer(opened.ID)
		if err != nil {
			t.Fatalf("Buffer: %v", err)
		}
		if got := buf.Text(); got != "package main\n" {
			t.Errorf("buffer text = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive BufferOpened")
	}
}

func TestEditorCommands_NewBufferAndCloseBuffer(t *testing.T) {
	e := newTestEngine(t)
	d := commands.New(nil)
	if err := RegisterCommands(d, e); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}

	if err := d.Dispatch(context.Background(), CommandNewBuffer, map[string]any{"text": "draft"}); err != nil {
		t.Fatalf("Dispatch newBuffer: %v", err)
	}
	refs := e.Buffers()
	if len(refs) != 1 {
		t.Fatalf("Buffers len = %d, want 1", len(refs))
	}
	if !refs[0].Dirty || refs[0].Path != "" {
		t.Fatalf("BufferRef = %+v, want dirty untitled", refs[0])
	}
	buf, err := e.Buffer(refs[0].ID)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if got := buf.Text(); got != "draft" {
		t.Errorf("buffer text = %q, want draft", got)
	}

	if err := d.Dispatch(context.Background(), CommandCloseBuffer, map[string]any{
		"bufferID": json.Number(strconv.FormatInt(int64(refs[0].ID), 10)),
	}); err != nil {
		t.Fatalf("Dispatch closeBuffer: %v", err)
	}
	if _, err := e.Buffer(refs[0].ID); !errors.Is(err, ErrBufferNotFound) {
		t.Errorf("Buffer after close err = %v, want ErrBufferNotFound", err)
	}
}

func TestEditorCommands_SaveAsBuffer_WritesFile(t *testing.T) {
	e := newTestEngine(t)
	id := e.NewBuffer("hello")
	d := commands.New(nil)
	if err := RegisterCommands(d, e); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}

	path := filepath.Join(t.TempDir(), "out.txt")
	if err := d.Dispatch(context.Background(), CommandSaveAsBuffer, map[string]any{
		"bufferID": float64(id),
		"path":     path,
	}); err != nil {
		t.Fatalf("Dispatch saveAsBuffer: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("file content = %q, want hello", data)
	}
}

func TestEditorCommands_ViewMoveAndEdit(t *testing.T) {
	e := newTestEngine(t)
	bid := e.NewBuffer("hello")
	d := commands.New(nil)
	if err := RegisterCommands(d, e); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}

	if err := d.Dispatch(context.Background(), CommandNewView, map[string]any{"bufferID": int64(bid)}); err != nil {
		t.Fatalf("Dispatch newView: %v", err)
	}
	views := e.Views(bid)
	if len(views) != 1 {
		t.Fatalf("Views len = %d, want 1", len(views))
	}
	vid := views[0].ID

	if err := d.Dispatch(context.Background(), CommandMoveCursor, map[string]any{
		"viewID": json.Number(strconv.FormatInt(int64(vid), 10)),
		"line":   float64(0),
		"column": float64(5),
	}); err != nil {
		t.Fatalf("Dispatch moveCursor: %v", err)
	}
	if err := d.Dispatch(context.Background(), CommandInsertText, map[string]any{
		"viewID": vid,
		"text":   " world",
	}); err != nil {
		t.Fatalf("Dispatch insertText: %v", err)
	}
	buf, err := e.Buffer(bid)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if got := buf.Text(); got != "hello world" {
		t.Fatalf("buffer text = %q, want hello world", got)
	}

	if err := d.Dispatch(context.Background(), CommandBackspace, map[string]any{"viewID": int64(vid)}); err != nil {
		t.Fatalf("Dispatch backspace: %v", err)
	}
	if got := buf.Text(); got != "hello worl" {
		t.Fatalf("buffer text after backspace = %q, want hello worl", got)
	}

	if err := d.Dispatch(context.Background(), CommandMoveCursor, map[string]any{
		"viewID": vid,
		"line":   0,
		"column": 5,
	}); err != nil {
		t.Fatalf("Dispatch moveCursor reset: %v", err)
	}
	if err := d.Dispatch(context.Background(), CommandMoveCursor, map[string]any{
		"viewID":          vid,
		"line":            0,
		"column":          10,
		"extendSelection": true,
	}); err != nil {
		t.Fatalf("Dispatch moveCursor extend: %v", err)
	}
	if err := d.Dispatch(context.Background(), CommandDeleteSelection, map[string]any{"viewID": vid}); err != nil {
		t.Fatalf("Dispatch deleteSelection: %v", err)
	}
	if got := buf.Text(); got != "hello" {
		t.Fatalf("buffer text after deleteSelection = %q, want hello", got)
	}

	if err := d.Dispatch(context.Background(), CommandCloseView, map[string]any{"viewID": vid}); err != nil {
		t.Fatalf("Dispatch closeView: %v", err)
	}
	if _, err := e.View(vid); !errors.Is(err, ErrViewNotFound) {
		t.Errorf("View after close err = %v, want ErrViewNotFound", err)
	}
}

func TestEditorCommands_InvalidArgsError(t *testing.T) {
	e := newTestEngine(t)
	d := commands.New(nil)
	if err := RegisterCommands(d, e); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}

	cases := []struct {
		name    string
		command string
		args    map[string]any
		want    error
	}{
		{
			name:    "missing path",
			command: CommandOpenFile,
			args:    nil,
			want:    ErrCommandMissingArg,
		},
		{
			name:    "fractional bufferID",
			command: CommandCloseBuffer,
			args:    map[string]any{"bufferID": float64(1.5)},
			want:    ErrCommandInvalidArg,
		},
		{
			name:    "negative bufferID",
			command: CommandCloseBuffer,
			args:    map[string]any{"bufferID": json.Number("-1")},
			want:    ErrCommandInvalidArg,
		},
		{
			name:    "overflow viewID",
			command: CommandCloseView,
			args:    map[string]any{"viewID": json.Number("9223372036854775808")},
			want:    ErrCommandInvalidArg,
		},
		{
			name:    "wrong text type",
			command: CommandInsertText,
			args:    map[string]any{"viewID": 1, "text": 42},
			want:    ErrCommandInvalidArg,
		},
		{
			name:    "negative cursor line",
			command: CommandMoveCursor,
			args:    map[string]any{"viewID": 1, "line": -1, "column": 0},
			want:    ErrCommandInvalidArg,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := d.Dispatch(context.Background(), tc.command, tc.args)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
