//go:build gio

package ui

import (
	"testing"

	"gioui.org/io/key"
)

func TestWindowActionFromGioKey(t *testing.T) {
	tests := []struct {
		name string
		ev   key.Event
		want WindowAction
	}{
		{
			name: "f5 refresh",
			ev:   key.Event{Name: key.NameF5, State: key.Press},
			want: WindowActionRefresh,
		},
		{
			name: "ctrl r refresh",
			ev:   key.Event{Name: key.Name("R"), State: key.Press, Modifiers: key.ModCtrl},
			want: WindowActionRefresh,
		},
		{
			name: "command r refresh",
			ev:   key.Event{Name: key.Name("R"), State: key.Press, Modifiers: key.ModCommand},
			want: WindowActionRefresh,
		},
		{
			name: "ctrl p opens palette",
			ev:   key.Event{Name: key.Name("P"), State: key.Press, Modifiers: key.ModCtrl},
			want: WindowActionOpenPalette,
		},
		{
			name: "command p opens palette",
			ev:   key.Event{Name: key.Name("P"), State: key.Press, Modifiers: key.ModCommand},
			want: WindowActionOpenPalette,
		},
		{
			name: "escape quit",
			ev:   key.Event{Name: key.NameEscape, State: key.Press},
			want: WindowActionQuit,
		},
		{
			name: "ctrl q quit",
			ev:   key.Event{Name: key.Name("Q"), State: key.Press, Modifiers: key.ModCtrl},
			want: WindowActionQuit,
		},
		{
			name: "command q quit",
			ev:   key.Event{Name: key.Name("Q"), State: key.Press, Modifiers: key.ModCommand},
			want: WindowActionQuit,
		},
		{
			name: "plain r ignored",
			ev:   key.Event{Name: key.Name("R"), State: key.Press},
			want: WindowActionNone,
		},
		{
			name: "plain q ignored",
			ev:   key.Event{Name: key.Name("Q"), State: key.Press},
			want: WindowActionNone,
		},
		{
			name: "unrelated no-mod key ignored",
			ev:   key.Event{Name: key.Name("X"), State: key.Press},
			want: WindowActionNone,
		},
		{
			name: "up ignored when palette closed",
			ev:   key.Event{Name: key.NameUpArrow, State: key.Press},
			want: WindowActionNone,
		},
		{
			name: "down ignored when palette closed",
			ev:   key.Event{Name: key.NameDownArrow, State: key.Press},
			want: WindowActionNone,
		},
		{
			name: "return ignored when palette closed",
			ev:   key.Event{Name: key.NameReturn, State: key.Press},
			want: WindowActionNone,
		},
		{
			name: "repeat press maps refresh",
			ev:   key.Event{Name: key.NameF5, State: key.Press},
			want: WindowActionRefresh,
		},
		{
			name: "release ignored",
			ev:   key.Event{Name: key.NameF5, State: key.Release},
			want: WindowActionNone,
		},
		{
			name: "shifted shortcut ignored",
			ev:   key.Event{Name: key.Name("R"), State: key.Press, Modifiers: key.ModCtrl | key.ModShift},
			want: WindowActionNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := windowActionFromGioKey(tt.ev, false); got != tt.want {
				t.Fatalf("windowActionFromGioKey(%+v) = %s, want %s", tt.ev, got, tt.want)
			}
		})
	}
}

func TestWindowActionFromGioKey_WithPaletteOpen(t *testing.T) {
	tests := []struct {
		name string
		ev   key.Event
		want WindowAction
	}{
		{
			name: "escape closes palette",
			ev:   key.Event{Name: key.NameEscape, State: key.Press},
			want: WindowActionClosePalette,
		},
		{
			name: "up moves selection",
			ev:   key.Event{Name: key.NameUpArrow, State: key.Press},
			want: WindowActionPaletteUp,
		},
		{
			name: "down moves selection",
			ev:   key.Event{Name: key.NameDownArrow, State: key.Press},
			want: WindowActionPaletteDown,
		},
		{
			name: "return accept no-op",
			ev:   key.Event{Name: key.NameReturn, State: key.Press},
			want: WindowActionPaletteAccept,
		},
		{
			name: "enter accept no-op",
			ev:   key.Event{Name: key.NameEnter, State: key.Press},
			want: WindowActionPaletteAccept,
		},
		{
			name: "ctrl q still quits",
			ev:   key.Event{Name: key.Name("Q"), State: key.Press, Modifiers: key.ModCtrl},
			want: WindowActionQuit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := windowActionFromGioKey(tt.ev, true); got != tt.want {
				t.Fatalf("windowActionFromGioKey(%+v, true) = %s, want %s", tt.ev, got, tt.want)
			}
		})
	}
}
