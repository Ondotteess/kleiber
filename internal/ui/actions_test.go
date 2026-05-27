package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Ondotteess/kleiber/internal/app"
	"github.com/Ondotteess/kleiber/internal/commands"
	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/project"
)

func TestNewController_NilInputsError(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, err := NewPresenter(session, PresenterOptions{})
	if err != nil {
		t.Fatalf("NewPresenter: %v", err)
	}
	defer presenter.Close()

	if _, err := NewController(nil, session, ControllerOptions{}); !errors.Is(err, ErrNilPresenter) {
		t.Fatalf("nil presenter err = %v, want ErrNilPresenter", err)
	}
	if _, err := NewController(presenter, nil, ControllerOptions{}); !errors.Is(err, ErrNilSession) {
		t.Fatalf("nil session err = %v, want ErrNilSession", err)
	}

	var controller *Controller
	if err := controller.OpenFile(context.Background(), "main.go"); !errors.Is(err, ErrNilController) {
		t.Fatalf("nil controller err = %v, want ErrNilController", err)
	}
}

func TestController_DispatchesThroughSession(t *testing.T) {
	session, err := app.NewSession(app.Options{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	presenter, err := NewPresenter(session, PresenterOptions{})
	if err != nil {
		t.Fatalf("NewPresenter: %v", err)
	}
	defer presenter.Close()

	controller, err := NewController(presenter, session, ControllerOptions{})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	err = controller.NewBuffer(context.Background(), "draft")
	if !errors.Is(err, commands.ErrUnknownCommand) {
		t.Fatalf("NewBuffer err = %v, want ErrUnknownCommand from dispatcher", err)
	}
}

func TestController_EditorActionsUpdatePresenterState(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, err := NewPresenter(session, PresenterOptions{})
	if err != nil {
		t.Fatalf("NewPresenter: %v", err)
	}
	defer presenter.Close()
	controller, err := NewController(presenter, session, ControllerOptions{})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	ctx := context.Background()

	if err := controller.NewBuffer(ctx, "hello"); err != nil {
		t.Fatalf("NewBuffer: %v", err)
	}
	waitForUpdate(t, presenter)
	mustRefresh(t, presenter)
	state := presenter.State()
	if len(state.Buffers) != 1 || !state.Buffers[0].Dirty {
		t.Fatalf("Buffers = %+v, want one dirty untitled buffer", state.Buffers)
	}
	bid := state.Buffers[0].ID

	if err := controller.NewView(ctx, bid); err != nil {
		t.Fatalf("NewView: %v", err)
	}
	mustRefresh(t, presenter)
	state = presenter.State()
	if len(state.Views) != 1 || state.Views[0].BufferID != bid {
		t.Fatalf("Views = %+v, want one view for buffer %d", state.Views, bid)
	}
	vid := state.Views[0].ID

	if err := controller.MoveCursor(ctx, vid, editor.Position{Line: 0, Column: 5}, false); err != nil {
		t.Fatalf("MoveCursor: %v", err)
	}
	if err := controller.InsertText(ctx, vid, " world"); err != nil {
		t.Fatalf("InsertText: %v", err)
	}
	if err := controller.Backspace(ctx, vid); err != nil {
		t.Fatalf("Backspace: %v", err)
	}
	if err := controller.MoveCursor(ctx, vid, editor.Position{Line: 0, Column: 5}, false); err != nil {
		t.Fatalf("MoveCursor before selection: %v", err)
	}
	if err := controller.MoveCursor(ctx, vid, editor.Position{Line: 0, Column: 10}, true); err != nil {
		t.Fatalf("MoveCursor extend selection: %v", err)
	}
	if err := controller.DeleteSelection(ctx, vid); err != nil {
		t.Fatalf("DeleteSelection: %v", err)
	}
	buf, err := session.Editor().Buffer(bid)
	if err != nil {
		t.Fatalf("Buffer: %v", err)
	}
	if got := buf.Text(); got != "hello" {
		t.Fatalf("buffer text = %q, want hello", got)
	}

	path := filepath.Join(t.TempDir(), "note.txt")
	if err := controller.SaveAsBuffer(ctx, bid, path); err != nil {
		t.Fatalf("SaveAsBuffer: %v", err)
	}
	assertFileContent(t, path, "hello")
	if err := controller.InsertText(ctx, vid, "!"); err != nil {
		t.Fatalf("InsertText after saveAs: %v", err)
	}
	if err := controller.SaveBuffer(ctx, bid); err != nil {
		t.Fatalf("SaveBuffer: %v", err)
	}
	assertFileContent(t, path, "hello!")

	if err := controller.CloseView(ctx, vid); err != nil {
		t.Fatalf("CloseView: %v", err)
	}
	mustRefresh(t, presenter)
	if got := presenter.State().Views; len(got) != 0 {
		t.Fatalf("Views after CloseView = %+v, want empty", got)
	}
	if err := controller.CloseBuffer(ctx, bid); err != nil {
		t.Fatalf("CloseBuffer: %v", err)
	}
	mustRefresh(t, presenter)
	if got := presenter.State().Buffers; len(got) != 0 {
		t.Fatalf("Buffers after CloseBuffer = %+v, want empty", got)
	}
}

func TestController_InvalidInputsReturnCommandErrors(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	presenter, err := NewPresenter(session, PresenterOptions{})
	if err != nil {
		t.Fatalf("NewPresenter: %v", err)
	}
	defer presenter.Close()
	controller, err := NewController(presenter, session, ControllerOptions{})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}

	cases := []struct {
		name string
		run  func() error
		want error
	}{
		{
			name: "empty open path",
			run:  func() error { return controller.OpenFile(context.Background(), "") },
			want: app.ErrCommandInvalidArg,
		},
		{
			name: "zero buffer id",
			run:  func() error { return controller.CloseBuffer(context.Background(), 0) },
			want: app.ErrCommandInvalidArg,
		},
		{
			name: "missing project",
			run:  func() error { return controller.RefreshProject(context.Background()) },
			want: app.ErrCommandProjectNil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestController_RefreshProjectUpdatesPresenterStateAndSignals(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/uicontroller\n\ngo 1.25\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")

	p, err := project.Open(context.Background(), root, project.Options{})
	if err != nil {
		t.Fatalf("project.Open: %v", err)
	}
	session := newRegisteredSession(t, app.Options{Project: p})
	presenter, err := NewPresenter(session, PresenterOptions{})
	if err != nil {
		t.Fatalf("NewPresenter: %v", err)
	}
	defer presenter.Close()
	controller, err := NewController(presenter, session, ControllerOptions{})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}

	if hasProjectPackage(presenter.State().Project.Packages, "example.test/uicontroller/pkg/extra") {
		t.Fatal("extra package visible before refresh")
	}
	writeFile(t, filepath.Join(root, "pkg", "extra", "extra.go"), "package extra\n\nfunc Extra() {}\n")

	if err := controller.RefreshProject(context.Background()); err != nil {
		t.Fatalf("RefreshProject: %v", err)
	}
	waitForUpdate(t, presenter)
	if !hasProjectPackage(presenter.State().Project.Packages, "example.test/uicontroller/pkg/extra") {
		t.Fatalf("presenter state missing refreshed package: %+v", presenter.State().Project.Packages)
	}
}

func mustRefresh(t *testing.T, presenter *Presenter) {
	t.Helper()
	if err := presenter.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	if got := string(data); got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}

func hasProjectPackage(pkgs []PackageItem, importPath string) bool {
	for _, pkg := range pkgs {
		if pkg.ImportPath == importPath {
			return true
		}
	}
	return false
}
