//go:build gio

package ui

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/Ondotteess/kleiber/internal/syntax"
)

// ideMonoTypeface is the monospace face used across the IDE window. Go Mono
// ships with gofont.Collection, so no external font files are required.
const ideMonoTypeface = "Go Mono"

// ideTextSizeSp is the base editor text size in scale-independent pixels.
const ideTextSizeSp = 13

// cellSampleGlyphs is the reference string measured to derive the monospace
// cell width. Ten glyphs are laid out and the advance is averaged to reduce
// rounding error from a single-glyph measurement.
const cellSampleGlyphs = "MMMMMMMMMM"

// IDETheme bundles the Gio material theme with the dark palette and metric
// helpers used by the IDE widgets. It owns no mutable rendering state, so a
// single instance is shared across widgets and frames.
type IDETheme struct {
	// Theme is the underlying material theme with its shaper and monospace
	// face already configured.
	Theme *material.Theme

	// Background fills the editor and window body.
	Background color.NRGBA
	// Panel fills side panels (file tree) and the tab strip.
	Panel color.NRGBA
	// Gutter is the line-number gutter background.
	Gutter color.NRGBA
	// GutterText is the muted color of line numbers.
	GutterText color.NRGBA
	// LineHighlight is the subtle fill behind the caret's line.
	LineHighlight color.NRGBA
	// Selection is the translucent fill behind selected text.
	Selection color.NRGBA
	// Caret is the color of the caret bar.
	Caret color.NRGBA
	// Foreground is the default text color.
	Foreground color.NRGBA
	// Muted is a dimmed color for secondary UI text.
	Muted color.NRGBA
	// Divider colors the 1px separators between regions.
	Divider color.NRGBA
	// ActiveTab fills the currently selected tab.
	ActiveTab color.NRGBA
	// AccentBar underlines the active tab.
	Accent color.NRGBA

	// cellWidthCache caches the measured cell width keyed by the metric and
	// text size it was measured at, so the off-screen sample is laid out at
	// most once per unique rendering context.
	cellWidthCache map[cellMetricKey]int
	// lineHeightCache caches the measured single-line height per metric.
	lineHeightCache map[cellMetricKey]int
}

// cellMetricKey identifies the rendering context a cached metric was
// measured under. The pixel-per-dp and pixel-per-sp ratios fully determine
// the shaped size for a fixed text size.
type cellMetricKey struct {
	pxPerDp float32
	pxPerSp float32
}

// NewIDETheme constructs the dark IDE theme with a configured monospace
// shaper. The returned theme is ready to hand to the IDE widgets.
func NewIDETheme() *IDETheme {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	th.Face = font.Typeface(ideMonoTypeface)
	th.TextSize = unit.Sp(ideTextSizeSp)

	return &IDETheme{
		Theme:           th,
		Background:      rgb(0x1e1e1e),
		Panel:           rgb(0x252526),
		Gutter:          rgb(0x1e1e1e),
		GutterText:      rgb(0x858585),
		LineHighlight:   rgba(0xffffff, 0x14),
		Selection:       rgba(0x264f78, 0x99),
		Caret:           rgb(0xaeafad),
		Foreground:      rgb(0xd4d4d4),
		Muted:           rgb(0x858585),
		Divider:         rgb(0x333333),
		ActiveTab:       rgb(0x1e1e1e),
		Accent:          rgb(0x569cd6),
		cellWidthCache:  map[cellMetricKey]int{},
		lineHeightCache: map[cellMetricKey]int{},
	}
}

// MonoFont returns the monospace font descriptor for direct label styling.
func (t *IDETheme) MonoFont() font.Font {
	return font.Font{Typeface: font.Typeface(ideMonoTypeface)}
}

// ColorForKind maps a syntax kind to a palette color. Unknown kinds fall
// back to the default foreground.
func (t *IDETheme) ColorForKind(kind syntax.Kind) color.NRGBA {
	switch kind {
	case syntax.KindKeyword:
		return rgb(0x569cd6)
	case syntax.KindString:
		return rgb(0xce9178)
	case syntax.KindComment:
		return rgb(0x6a9955)
	case syntax.KindNumber:
		return rgb(0xb5cea8)
	case syntax.KindBuiltin:
		return rgb(0x4ec9b0)
	case syntax.KindOperator:
		return rgb(0xd4d4d4)
	case syntax.KindIdentifier:
		return rgb(0xd4d4d4)
	default:
		return t.Foreground
	}
}

// CellWidth returns the pixel advance of one monospace cell in the current
// rendering context. It lays out a reference string off-screen once per
// unique metric and caches the result. The value is always at least one
// pixel so callers never divide the caret into a zero-width column.
func (t *IDETheme) CellWidth(gtx layout.Context) int {
	key := cellMetricKey{pxPerDp: gtx.Metric.PxPerDp, pxPerSp: gtx.Metric.PxPerSp}
	if w, ok := t.cellWidthCache[key]; ok {
		return w
	}
	dims := t.measure(gtx, cellSampleGlyphs)
	width := dims.Size.X / len(cellSampleGlyphs)
	if width < 1 {
		width = 1
	}
	t.cellWidthCache[key] = width
	return width
}

// LineHeight returns the pixel height of a single line of editor text in the
// current rendering context, cached per metric.
func (t *IDETheme) LineHeight(gtx layout.Context) int {
	key := cellMetricKey{pxPerDp: gtx.Metric.PxPerDp, pxPerSp: gtx.Metric.PxPerSp}
	if h, ok := t.lineHeightCache[key]; ok {
		return h
	}
	dims := t.measure(gtx, cellSampleGlyphs)
	height := dims.Size.Y
	if height < 1 {
		height = 1
	}
	t.lineHeightCache[key] = height
	return height
}

// measure lays out sample text off-screen (inside a discarded op.Record) and
// returns its dimensions. Recording ensures the measurement produces no
// visible paint operations in the caller's frame.
func (t *IDETheme) measure(gtx layout.Context, sample string) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	label := material.Label(t.Theme, t.Theme.TextSize, sample)
	label.Font = t.MonoFont()
	label.MaxLines = 1
	measureGtx := gtx
	measureGtx.Constraints.Min = image.Point{}
	measureGtx.Constraints.Max = image.Point{X: 1 << 20, Y: 1 << 20}
	dims := label.Layout(measureGtx)
	macro.Stop()
	return dims
}

// rgb builds an opaque NRGBA from a 0xRRGGBB literal.
func rgb(hex uint32) color.NRGBA {
	return color.NRGBA{
		R: uint8(hex >> 16),
		G: uint8(hex >> 8),
		B: uint8(hex),
		A: 0xff,
	}
}

// rgba builds an NRGBA from a 0xRRGGBB literal and an explicit alpha byte.
func rgba(hex uint32, alpha uint8) color.NRGBA {
	c := rgb(hex)
	c.A = alpha
	return c
}
