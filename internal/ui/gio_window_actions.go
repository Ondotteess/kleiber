//go:build gio

package ui

import "gioui.org/io/key"

func windowActionFromGioKey(ev key.Event, paletteOpen bool) WindowAction {
	return WindowActionForKeyStrokeWithPalette(WindowKeyStroke{
		Name:    gioWindowKeyName(ev.Name),
		Press:   ev.State == key.Press,
		Ctrl:    ev.Modifiers.Contain(key.ModCtrl),
		Command: ev.Modifiers.Contain(key.ModCommand),
		Shift:   ev.Modifiers.Contain(key.ModShift),
		Alt:     ev.Modifiers.Contain(key.ModAlt),
		Super:   ev.Modifiers.Contain(key.ModSuper),
	}, paletteOpen)
}

func gioWindowKeyName(name key.Name) string {
	switch name {
	case key.NameF5:
		return WindowKeyF5
	case key.NameEscape:
		return WindowKeyEscape
	case key.NameUpArrow:
		return WindowKeyUp
	case key.NameDownArrow:
		return WindowKeyDown
	case key.NameReturn, key.NameEnter:
		return WindowKeyEnter
	case key.Name("P"):
		return WindowKeyP
	case key.Name("R"):
		return WindowKeyR
	case key.Name("Q"):
		return WindowKeyQ
	default:
		return string(name)
	}
}
