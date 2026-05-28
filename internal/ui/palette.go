package ui

// CommandPaletteState is the dependency-free interaction state for the
// experimental command palette. It stores only UI navigation metadata; command
// execution remains a future controller concern.
type CommandPaletteState struct {
	Open          bool
	SelectedIndex int
}

// CommandPaletteSnapshot is the defensive palette model exposed through
// ShellState and consumed by renderers.
type CommandPaletteSnapshot struct {
	Open          bool
	SelectedIndex int
	Commands      []CommandItem
}

// Opened returns palette state opened against the current command count.
func (p CommandPaletteState) Opened(commandCount int) CommandPaletteState {
	p.Open = true
	p.SelectedIndex = clampPaletteIndex(p.SelectedIndex, commandCount)
	return p
}

// Closed returns palette state with the palette hidden. The selected index is
// retained so reopening can preserve nearby context when commands are stable.
func (p CommandPaletteState) Closed() CommandPaletteState {
	p.Open = false
	return p
}

// Move returns state with a wrapped selection movement. Movement is ignored
// while the palette is closed or when there are no commands.
func (p CommandPaletteState) Move(delta, commandCount int) CommandPaletteState {
	if !p.Open || commandCount <= 0 || delta == 0 {
		p.SelectedIndex = clampPaletteIndex(p.SelectedIndex, commandCount)
		return p
	}
	p.SelectedIndex = wrapPaletteIndex(p.SelectedIndex+delta, commandCount)
	return p
}

// Snapshot returns a defensive command-palette model with a clamped selection.
func (p CommandPaletteState) Snapshot(commands []CommandItem) CommandPaletteSnapshot {
	out := CommandPaletteSnapshot{
		Open:          p.Open,
		SelectedIndex: clampPaletteIndex(p.SelectedIndex, len(commands)),
		Commands:      cloneCommands(commands),
	}
	return out
}

// Selected returns the selected command when the palette is open and non-empty.
func (p CommandPaletteSnapshot) Selected() (CommandItem, bool) {
	if !p.Open || len(p.Commands) == 0 {
		return CommandItem{}, false
	}
	index := clampPaletteIndex(p.SelectedIndex, len(p.Commands))
	return p.Commands[index], true
}

func clampPaletteIndex(index, count int) int {
	if count <= 0 {
		return 0
	}
	if index < 0 {
		return 0
	}
	if index >= count {
		return count - 1
	}
	return index
}

func wrapPaletteIndex(index, count int) int {
	if count <= 0 {
		return 0
	}
	index %= count
	if index < 0 {
		index += count
	}
	return index
}
