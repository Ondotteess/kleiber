package ui

import "testing"

func TestCommandPaletteState_OpenCloseMoveWraparound(t *testing.T) {
	palette := CommandPaletteState{}
	palette = palette.Opened(3)
	if !palette.Open || palette.SelectedIndex != 0 {
		t.Fatalf("opened palette = %+v, want open index 0", palette)
	}

	palette = palette.Move(1, 3)
	if palette.SelectedIndex != 1 {
		t.Fatalf("after move down index = %d, want 1", palette.SelectedIndex)
	}
	palette = palette.Move(3, 3)
	if palette.SelectedIndex != 1 {
		t.Fatalf("after full wrap index = %d, want 1", palette.SelectedIndex)
	}
	palette = palette.Move(-2, 3)
	if palette.SelectedIndex != 2 {
		t.Fatalf("after move up wrap index = %d, want 2", palette.SelectedIndex)
	}

	palette = palette.Closed()
	palette = palette.Move(1, 3)
	if palette.Open || palette.SelectedIndex != 2 {
		t.Fatalf("closed palette after move = %+v, want closed index retained", palette)
	}
}

func TestCommandPaletteState_EmptyCommandsSafe(t *testing.T) {
	palette := CommandPaletteState{SelectedIndex: 99}.Opened(0)
	if !palette.Open || palette.SelectedIndex != 0 {
		t.Fatalf("opened empty palette = %+v, want open index 0", palette)
	}
	palette = palette.Move(1, 0)
	if palette.SelectedIndex != 0 {
		t.Fatalf("empty move index = %d, want 0", palette.SelectedIndex)
	}
	snapshot := palette.Snapshot(nil)
	if !snapshot.Open || snapshot.SelectedIndex != 0 || len(snapshot.Commands) != 0 {
		t.Fatalf("empty snapshot = %+v, want open empty index 0", snapshot)
	}
	if _, ok := snapshot.Selected(); ok {
		t.Fatal("Selected ok = true for empty palette")
	}
}

func TestCommandPaletteSnapshot_DefensiveCommands(t *testing.T) {
	commands := []CommandItem{{Name: "editor.openFile"}, {Name: "project.refresh"}}
	palette := CommandPaletteState{Open: true, SelectedIndex: 1}

	snapshot := palette.Snapshot(commands)
	selected, ok := snapshot.Selected()
	if !ok || selected.Name != "project.refresh" {
		t.Fatalf("selected = %+v %v, want project.refresh", selected, ok)
	}
	snapshot.Commands[1].Name = "mutated"
	if commands[1].Name == "mutated" {
		t.Fatal("palette snapshot returned mutable command slice")
	}
}
