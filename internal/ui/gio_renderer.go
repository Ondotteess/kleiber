//go:build gio

package ui

import (
	"context"
	"errors"
	"image/color"
	"sync"

	gioapp "gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const (
	defaultGioWidthDP  = 960
	defaultGioHeightDP = 640
)

// GioRenderer lays out a read-only snapshot from Shell. It does not implement
// editor text rendering, command invocation, or file tree interactions.
type GioRenderer struct {
	shell *Shell
	theme *material.Theme
	list  widget.List
}

// NewGioRenderer constructs a renderer over a Shell state source.
func NewGioRenderer(shell *Shell) (*GioRenderer, error) {
	if shell == nil {
		return nil, ErrNilShell
	}
	return &GioRenderer{
		shell: shell,
		theme: material.NewTheme(),
		list:  widget.List{List: layout.List{Axis: layout.Vertical}},
	}, nil
}

// Layout draws the current Shell snapshot into the Gio layout context.
func (r *GioRenderer) Layout(gtx layout.Context) layout.Dimensions {
	if r == nil || r.shell == nil {
		return layout.Dimensions{}
	}
	model := BuildGioRenderModel(r.shell.Snapshot())
	return r.LayoutModel(gtx, model)
}

// LayoutModel draws a prebuilt render model. Tests cover the model builder;
// this method remains small so GUI smoke can stay manual.
func (r *GioRenderer) LayoutModel(gtx layout.Context, model GioRenderModel) layout.Dimensions {
	if r == nil || r.theme == nil {
		return layout.Dimensions{}
	}
	return layout.UniformInset(unit.Dp(18)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return material.List(r.theme, &r.list).Layout(gtx, len(model.Lines), func(gtx layout.Context, index int) layout.Dimensions {
			line := model.Lines[index]
			return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return r.layoutLine(gtx, line)
			})
		})
	})
}

func (r *GioRenderer) layoutLine(gtx layout.Context, line GioRenderLine) layout.Dimensions {
	switch line.Role {
	case GioRenderLineTitle:
		label := material.H5(r.theme, line.Text)
		label.Color = color.NRGBA{R: 34, G: 39, B: 46, A: 255}
		return label.Layout(gtx)
	case GioRenderLineSection:
		label := material.Subtitle1(r.theme, line.Text)
		label.Color = color.NRGBA{R: 23, G: 92, B: 138, A: 255}
		return label.Layout(gtx)
	case GioRenderLineMuted:
		label := material.Body2(r.theme, line.Text)
		label.Color = color.NRGBA{R: 96, G: 108, B: 118, A: 255}
		return label.Layout(gtx)
	default:
		return material.Body2(r.theme, line.Text).Layout(gtx)
	}
}

// RunGioWindow runs the window event loop. The caller is responsible for
// invoking gioapp.Main from main, per Gio's platform contract.
func RunGioWindow(ctx context.Context, shell *Shell, opts GioWindowOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if shell == nil {
		return ErrNilShell
	}
	renderer, err := NewGioRenderer(shell)
	if err != nil {
		return err
	}

	title := opts.Title
	if title == "" {
		title = shell.Snapshot().Title
	}
	if title == "" {
		title = defaultShellTitle
	}
	width := opts.WidthDP
	if width <= 0 {
		width = defaultGioWidthDP
	}
	height := opts.HeightDP
	if height <= 0 {
		height = defaultGioHeightDP
	}

	w := new(gioapp.Window)
	w.Option(
		gioapp.Title(title),
		gioapp.Size(unit.Dp(width), unit.Dp(height)),
	)
	var ops op.Ops

	stopUpdates, waitUpdates := watchGioUpdates(ctx, w, shell.Updates())
	defer func() {
		close(stopUpdates)
		waitUpdates()
	}()

	for {
		event := w.Event()
		switch event := event.(type) {
		case gioapp.DestroyEvent:
			shell.Close()
			if event.Err != nil && !errors.Is(event.Err, context.Canceled) {
				return event.Err
			}
			return nil
		case gioapp.FrameEvent:
			if shell.Dirty() {
				_ = shell.Refresh(ctx)
			}
			gtx := gioapp.NewContext(&ops, event)
			renderer.Layout(gtx)
			event.Frame(&ops)
		}
	}
}

func watchGioUpdates(ctx context.Context, w *gioapp.Window, updates <-chan struct{}) (chan<- struct{}, func()) {
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				w.Perform(system.ActionClose)
				return
			case _, ok := <-updates:
				if !ok {
					return
				}
				w.Invalidate()
			case <-stop:
				return
			}
		}
	}()
	return stop, wg.Wait
}
