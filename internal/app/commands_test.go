package app

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
	"github.com/Ondotteess/kleiber/internal/config"
	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/lsp"
	"github.com/Ondotteess/kleiber/internal/project"
)

type fakeFormatter struct {
	calls int
	opts  lsp.FormattingOptions
	fn    func(context.Context, editor.BufferID, lsp.FormattingOptions) (int, error)
}

func (f *fakeFormatter) FormatAndSaveBuffer(ctx context.Context, id editor.BufferID, opts lsp.FormattingOptions) (int, error) {
	f.calls++
	f.opts = opts
	if f.fn != nil {
		return f.fn(ctx, id, opts)
	}
	return 0, nil
}

func TestNewSession_DefaultsCoreDependencies(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if s.Dispatcher() == nil {
		t.Fatal("Dispatcher is nil")
	}
	if s.Editor() == nil {
		t.Fatal("Editor is nil")
	}
	if got := s.Config().Editor.TabSize; got != config.Default().Editor.TabSize {
		t.Fatalf("Config.Editor.TabSize = %d, want default", got)
	}
	if _, ok := s.ProjectSnapshot(); ok {
		t.Fatal("ProjectSnapshot ok = true without project")
	}
}

func TestNewSession_UsesInjectedDependencies(t *testing.T) {
	cfg := config.Default()
	cfg.Editor.FormatOnSave = true
	d := commands.New(nil)
	e := editor.NewEngine(editor.EngineOptions{})

	s, err := NewSession(Options{
		Config:     &cfg,
		Dispatcher: d,
		Editor:     e,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if s.Dispatcher() != d {
		t.Fatal("NewSession did not use injected dispatcher")
	}
	if s.Editor() != e {
		t.Fatal("NewSession did not use injected editor")
	}
	if !s.Config().Editor.FormatOnSave {
		t.Fatal("NewSession did not use injected config")
	}
}

func TestSession_RegisterCommands_AddsExpectedCommandsAndDuplicateErrors(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := s.RegisterCommands(); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}
	for _, name := range []string{
		CommandOpenFile,
		CommandNewBuffer,
		CommandCloseBuffer,
		CommandSaveBuffer,
		CommandSaveAsBuffer,
		CommandNewView,
		CommandCloseView,
		CommandMoveCursor,
		CommandInsertText,
		CommandBackspace,
		CommandDeleteSelection,
		CommandProjectRefresh,
	} {
		if !s.Dispatcher().Has(name) {
			t.Errorf("dispatcher missing %s", name)
		}
	}
	if err := s.RegisterCommands(); !errors.Is(err, commands.ErrDuplicateName) {
		t.Fatalf("second RegisterCommands err = %v, want ErrDuplicateName", err)
	}
}

func TestSessionCommands_OpenFile_PublishesBufferOpened(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	writeFile(t, path, "package main\n")

	s := newRegisteredSession(t, Options{})
	sub, cancel := s.Editor().Events().Subscribe(8)
	defer cancel()

	if err := s.Dispatcher().Dispatch(context.Background(), CommandOpenFile, map[string]any{"path": path}); err != nil {
		t.Fatalf("Dispatch openFile: %v", err)
	}

	select {
	case ev := <-sub:
		opened, ok := ev.(editor.BufferOpened)
		if !ok {
			t.Fatalf("got %T, want BufferOpened", ev)
		}
		if opened.Path == "" {
			t.Fatal("BufferOpened.Path is empty")
		}
		buf, err := s.Editor().Buffer(opened.ID)
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

func TestSessionCommands_NewBufferAndCloseBuffer(t *testing.T) {
	s := newRegisteredSession(t, Options{})

	if err := s.Dispatcher().Dispatch(context.Background(), CommandNewBuffer, map[string]any{"text": "draft"}); err != nil {
		t.Fatalf("Dispatch newBuffer: %v", err)
	}
	refs := s.Editor().Buffers()
	if len(refs) != 1 {
		t.Fatalf("Buffers len = %d, want 1", len(refs))
	}
	if !refs[0].Dirty || refs[0].Path != "" {
		t.Fatalf("BufferRef = %+v, want dirty untitled", refs[0])
	}
	buf, err := s.Editor().Buffer(refs[0].ID)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if got := buf.Text(); got != "draft" {
		t.Errorf("buffer text = %q, want draft", got)
	}

	if err := s.Dispatcher().Dispatch(context.Background(), CommandCloseBuffer, map[string]any{
		"bufferID": json.Number(strconv.FormatInt(int64(refs[0].ID), 10)),
	}); err != nil {
		t.Fatalf("Dispatch closeBuffer: %v", err)
	}
	if _, err := s.Editor().Buffer(refs[0].ID); !errors.Is(err, editor.ErrBufferNotFound) {
		t.Errorf("Buffer after close err = %v, want ErrBufferNotFound", err)
	}
}

func TestSessionCommands_SaveAsBuffer_WritesFile(t *testing.T) {
	s := newRegisteredSession(t, Options{})
	id := s.Editor().NewBuffer("hello")

	path := filepath.Join(t.TempDir(), "out.txt")
	if err := s.Dispatcher().Dispatch(context.Background(), CommandSaveAsBuffer, map[string]any{
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

func TestSession_SaveBuffer_FormatOnSaveDisabled_PlainSave(t *testing.T) {
	formatter := &fakeFormatter{}
	s := newRegisteredSession(t, Options{Formatter: formatter})
	path := writeGoFile(t, "package x\n")
	id, err := s.Editor().Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	buf, err := s.Editor().Buffer(id)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if _, err := buf.Insert(editorEnd(buf), "\nfunc  main( ){}\n"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := s.Dispatcher().Dispatch(context.Background(), CommandSaveBuffer, map[string]any{"bufferID": id}); err != nil {
		t.Fatalf("Dispatch saveBuffer: %v", err)
	}
	if formatter.calls != 0 {
		t.Fatalf("formatter calls = %d, want 0", formatter.calls)
	}
	assertFile(t, path, "package x\n\nfunc  main( ){}\n")
}

func TestSession_SaveBuffer_FormatOnSaveEnabled_UsesFormatter(t *testing.T) {
	cfg := config.Default()
	cfg.Editor.FormatOnSave = true
	cfg.Editor.TabSize = 2
	cfg.Editor.InsertSpaces = true

	var s *Session
	formatter := &fakeFormatter{
		fn: func(ctx context.Context, id editor.BufferID, opts lsp.FormattingOptions) (int, error) {
			buf, err := s.Editor().Buffer(id)
			if err != nil {
				return 0, err
			}
			if _, err := buf.Delete(editor.Range{
				Start: editor.Position{Line: 0, Column: 0},
				End:   editorEnd(buf),
			}); err != nil {
				return 0, err
			}
			if _, err := buf.Insert(editor.Position{}, "package x\n\nfunc main() {}\n"); err != nil {
				return 0, err
			}
			return 1, s.Editor().Save(ctx, id)
		},
	}
	s = newRegisteredSession(t, Options{Config: &cfg, Formatter: formatter})
	path := writeGoFile(t, "package  x\n\nfunc main( ){}\n")
	id, err := s.Editor().Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := s.Dispatcher().Dispatch(context.Background(), CommandSaveBuffer, map[string]any{"bufferID": int64(id)}); err != nil {
		t.Fatalf("Dispatch saveBuffer: %v", err)
	}
	if formatter.calls != 1 {
		t.Fatalf("formatter calls = %d, want 1", formatter.calls)
	}
	if formatter.opts.TabSize != 2 || !formatter.opts.InsertSpaces {
		t.Fatalf("formatter opts = %+v, want tabSize=2 insertSpaces=true", formatter.opts)
	}
	assertFile(t, path, "package x\n\nfunc main() {}\n")
}

func TestSession_SaveBuffer_FormatErrorPreventsSave(t *testing.T) {
	cfg := config.Default()
	cfg.Editor.FormatOnSave = true
	formatErr := errors.New("format failed")
	formatter := &fakeFormatter{
		fn: func(ctx context.Context, id editor.BufferID, opts lsp.FormattingOptions) (int, error) {
			return 0, formatErr
		},
	}
	s := newRegisteredSession(t, Options{Config: &cfg, Formatter: formatter})
	path := writeGoFile(t, "package  x\n")
	id, err := s.Editor().Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	buf, err := s.Editor().Buffer(id)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if _, err := buf.Insert(editorEnd(buf), "\nfunc  main( ){}\n"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	err = s.Dispatcher().Dispatch(context.Background(), CommandSaveBuffer, map[string]any{"bufferID": id})
	if !errors.Is(err, formatErr) {
		t.Fatalf("Dispatch saveBuffer err = %v, want formatErr", err)
	}
	assertFile(t, path, "package  x\n")
}

func TestSession_SaveBuffer_FormatOnSaveFallbacks(t *testing.T) {
	cfg := config.Default()
	cfg.Editor.FormatOnSave = true

	t.Run("no formatter saves go file plainly", func(t *testing.T) {
		s := newRegisteredSession(t, Options{Config: &cfg})
		path := writeGoFile(t, "package x\n")
		id, err := s.Editor().Open(context.Background(), path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		buf, err := s.Editor().Buffer(id)
		if err != nil {
			t.Fatalf("Buffer: %v", err)
		}
		if _, err := buf.Insert(editorEnd(buf), "\nvar X = 1\n"); err != nil {
			t.Fatalf("Insert: %v", err)
		}
		if err := s.Dispatcher().Dispatch(context.Background(), CommandSaveBuffer, map[string]any{"bufferID": id}); err != nil {
			t.Fatalf("Dispatch saveBuffer: %v", err)
		}
		assertFile(t, path, "package x\n\nvar X = 1\n")
	})

	t.Run("untracked go file saves plainly", func(t *testing.T) {
		formatter := &fakeFormatter{
			fn: func(ctx context.Context, id editor.BufferID, opts lsp.FormattingOptions) (int, error) {
				return 0, lsp.ErrBridgeDocumentNotTracked
			},
		}
		s := newRegisteredSession(t, Options{Config: &cfg, Formatter: formatter})
		path := writeGoFile(t, "package x\n")
		id, err := s.Editor().Open(context.Background(), path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		buf, err := s.Editor().Buffer(id)
		if err != nil {
			t.Fatalf("Buffer: %v", err)
		}
		if _, err := buf.Insert(editorEnd(buf), "\nvar X = 1\n"); err != nil {
			t.Fatalf("Insert: %v", err)
		}
		if err := s.Dispatcher().Dispatch(context.Background(), CommandSaveBuffer, map[string]any{"bufferID": id}); err != nil {
			t.Fatalf("Dispatch saveBuffer: %v", err)
		}
		if formatter.calls != 1 {
			t.Fatalf("formatter calls = %d, want 1", formatter.calls)
		}
		assertFile(t, path, "package x\n\nvar X = 1\n")
	})

	t.Run("non go file skips formatter", func(t *testing.T) {
		formatter := &fakeFormatter{}
		s := newRegisteredSession(t, Options{Config: &cfg, Formatter: formatter})
		path := filepath.Join(t.TempDir(), "note.txt")
		writeFile(t, path, "hello")
		id, err := s.Editor().Open(context.Background(), path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		buf, err := s.Editor().Buffer(id)
		if err != nil {
			t.Fatalf("Buffer: %v", err)
		}
		if _, err := buf.Insert(editor.Position{Line: 0, Column: 5}, " world"); err != nil {
			t.Fatalf("Insert: %v", err)
		}
		if err := s.Dispatcher().Dispatch(context.Background(), CommandSaveBuffer, map[string]any{"bufferID": id}); err != nil {
			t.Fatalf("Dispatch saveBuffer: %v", err)
		}
		if formatter.calls != 0 {
			t.Fatalf("formatter calls = %d, want 0", formatter.calls)
		}
		assertFile(t, path, "hello world")
	})
}

func TestSessionCommands_ViewMoveAndEdit(t *testing.T) {
	s := newRegisteredSession(t, Options{})
	bid := s.Editor().NewBuffer("hello")

	if err := s.Dispatcher().Dispatch(context.Background(), CommandNewView, map[string]any{"bufferID": int64(bid)}); err != nil {
		t.Fatalf("Dispatch newView: %v", err)
	}
	views := s.Editor().Views(bid)
	if len(views) != 1 {
		t.Fatalf("Views len = %d, want 1", len(views))
	}
	vid := views[0].ID

	if err := s.Dispatcher().Dispatch(context.Background(), CommandMoveCursor, map[string]any{
		"viewID": json.Number(strconv.FormatInt(int64(vid), 10)),
		"line":   float64(0),
		"column": float64(5),
	}); err != nil {
		t.Fatalf("Dispatch moveCursor: %v", err)
	}
	if err := s.Dispatcher().Dispatch(context.Background(), CommandInsertText, map[string]any{
		"viewID": vid,
		"text":   " world",
	}); err != nil {
		t.Fatalf("Dispatch insertText: %v", err)
	}
	buf, err := s.Editor().Buffer(bid)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if got := buf.Text(); got != "hello world" {
		t.Fatalf("buffer text = %q, want hello world", got)
	}

	if err := s.Dispatcher().Dispatch(context.Background(), CommandBackspace, map[string]any{"viewID": int64(vid)}); err != nil {
		t.Fatalf("Dispatch backspace: %v", err)
	}
	if got := buf.Text(); got != "hello worl" {
		t.Fatalf("buffer text after backspace = %q, want hello worl", got)
	}

	if err := s.Dispatcher().Dispatch(context.Background(), CommandMoveCursor, map[string]any{
		"viewID": vid,
		"line":   0,
		"column": 5,
	}); err != nil {
		t.Fatalf("Dispatch moveCursor reset: %v", err)
	}
	if err := s.Dispatcher().Dispatch(context.Background(), CommandMoveCursor, map[string]any{
		"viewID":          vid,
		"line":            0,
		"column":          10,
		"extendSelection": true,
	}); err != nil {
		t.Fatalf("Dispatch moveCursor extend: %v", err)
	}
	if err := s.Dispatcher().Dispatch(context.Background(), CommandDeleteSelection, map[string]any{"viewID": vid}); err != nil {
		t.Fatalf("Dispatch deleteSelection: %v", err)
	}
	if got := buf.Text(); got != "hello" {
		t.Fatalf("buffer text after deleteSelection = %q, want hello", got)
	}

	if err := s.Dispatcher().Dispatch(context.Background(), CommandCloseView, map[string]any{"viewID": vid}); err != nil {
		t.Fatalf("Dispatch closeView: %v", err)
	}
	if _, err := s.Editor().View(vid); !errors.Is(err, editor.ErrViewNotFound) {
		t.Errorf("View after close err = %v, want ErrViewNotFound", err)
	}
}

func TestSessionCommands_InvalidArgsError(t *testing.T) {
	s := newRegisteredSession(t, Options{})

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
			err := s.Dispatcher().Dispatch(context.Background(), tc.command, tc.args)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSession_ProjectRefresh_CommandUpdatesSnapshot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/apprefresh\n\ngo 1.25\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")

	p, err := project.Open(context.Background(), root, project.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := newRegisteredSession(t, Options{Project: p})
	if hasPackage(s.Project().Snapshot().Packages, "example.test/apprefresh/pkg/extra") {
		t.Fatal("new package visible before command refresh")
	}

	extraDir := filepath.Join(root, "pkg", "extra")
	if err := os.MkdirAll(extraDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extraDir: %v", err)
	}
	writeFile(t, filepath.Join(extraDir, "extra.go"), "package extra\n\nfunc Extra() {}\n")

	if err := s.Dispatcher().Dispatch(context.Background(), CommandProjectRefresh, nil); err != nil {
		t.Fatalf("Dispatch project.refresh: %v", err)
	}
	snap, ok := s.ProjectSnapshot()
	if !ok {
		t.Fatal("ProjectSnapshot ok = false")
	}
	if !hasPackage(snap.Packages, "example.test/apprefresh/pkg/extra") {
		t.Fatalf("Snapshot missing new package after refresh: %+v", snap.Packages)
	}
}

func TestSession_ProjectRefresh_NoProjectErrors(t *testing.T) {
	s := newRegisteredSession(t, Options{})

	err := s.Dispatcher().Dispatch(context.Background(), CommandProjectRefresh, nil)
	if !errors.Is(err, ErrCommandProjectNil) {
		t.Fatalf("Dispatch project.refresh err = %v, want ErrCommandProjectNil", err)
	}
}

func newRegisteredSession(t *testing.T, opts Options) *Session {
	t.Helper()
	s, err := NewSession(opts)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := s.RegisterCommands(); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}
	return s
}

func writeGoFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.go")
	writeFile(t, path, contents)
	return path
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	if got := string(data); got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}

func hasPackage(pkgs []project.Package, importPath string) bool {
	for _, pkg := range pkgs {
		if pkg.ImportPath == importPath {
			return true
		}
	}
	return false
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
