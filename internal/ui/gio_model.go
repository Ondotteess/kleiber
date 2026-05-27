package ui

import "fmt"

const maxGioPreviewItems = 12

// GioRenderModel is the minimal read-only data a Gio renderer lays out. It is
// intentionally presentation-oriented but still independent from Gio packages
// so it can be tested without opening a window.
type GioRenderModel struct {
	Title string
	Lines []GioRenderLine
}

// GioRenderLine is one row in the experimental read-only UI.
type GioRenderLine struct {
	Text string
	Role GioRenderLineRole
}

// GioRenderLineRole classifies rows for the renderer.
type GioRenderLineRole string

const (
	GioRenderLineTitle   GioRenderLineRole = "title"
	GioRenderLineSection GioRenderLineRole = "section"
	GioRenderLineBody    GioRenderLineRole = "body"
	GioRenderLineMuted   GioRenderLineRole = "muted"
)

// BuildGioRenderModel converts ShellState into the first experimental
// window-readable model. It performs no I/O and does not expose commands.
func BuildGioRenderModel(snapshot ShellState) GioRenderModel {
	title := snapshot.Title
	if title == "" {
		title = defaultShellTitle
	}
	state := snapshot.State
	model := GioRenderModel{
		Title: title,
		Lines: []GioRenderLine{
			{Text: title, Role: GioRenderLineTitle},
			{Text: "pre-alpha experimental UI", Role: GioRenderLineMuted},
			{Text: fmt.Sprintf("commands: %d  buffers: %d  views: %d", len(state.Commands), len(state.Buffers), len(state.Views)), Role: GioRenderLineBody},
			{Text: "Editor widget pending", Role: GioRenderLineMuted},
		},
	}
	model.addCommands(state.Commands)
	model.addBuffers(state.Buffers)
	model.addProject(state.Project)
	return model
}

func (m *GioRenderModel) addCommands(commands []CommandItem) {
	m.addSection("Commands")
	if len(commands) == 0 {
		m.addMuted("No commands registered")
		return
	}
	for i, cmd := range commands {
		if i >= maxGioPreviewItems {
			m.addMuted(fmt.Sprintf("...and %d more commands", len(commands)-i))
			return
		}
		line := cmd.Name
		if cmd.Description != "" {
			line += " - " + cmd.Description
		}
		m.addBody(line)
	}
}

func (m *GioRenderModel) addBuffers(buffers []BufferItem) {
	m.addSection("Buffers")
	if len(buffers) == 0 {
		m.addMuted("No open buffers")
		return
	}
	for i, buf := range buffers {
		if i >= maxGioPreviewItems {
			m.addMuted(fmt.Sprintf("...and %d more buffers", len(buffers)-i))
			return
		}
		dirty := "clean"
		if buf.Dirty {
			dirty = "dirty"
		}
		line := fmt.Sprintf("#%d %s (%s)", buf.ID, buf.DisplayName, dirty)
		if buf.Path != "" {
			line += " - " + buf.Path
		}
		m.addBody(line)
	}
}

func (m *GioRenderModel) addProject(project ProjectState) {
	m.addSection("Project")
	if !project.Open {
		m.addMuted("No project opened")
		return
	}
	m.addBody("root: " + project.Root)
	m.addBody(fmt.Sprintf("modules: %d  packages: %d", len(project.Modules), len(project.Packages)))
	for i, module := range project.Modules {
		if i >= maxGioPreviewItems {
			m.addMuted(fmt.Sprintf("...and %d more modules", len(project.Modules)-i))
			break
		}
		label := module.RelDir
		if label == "" {
			label = module.Path
		}
		m.addBody("module: " + label)
	}
	for i, pkg := range project.Packages {
		if i >= maxGioPreviewItems {
			m.addMuted(fmt.Sprintf("...and %d more packages", len(project.Packages)-i))
			return
		}
		m.addBody("package: " + pkg.ImportPath)
	}
}

func (m *GioRenderModel) addSection(text string) {
	m.Lines = append(m.Lines, GioRenderLine{Text: text, Role: GioRenderLineSection})
}

func (m *GioRenderModel) addBody(text string) {
	m.Lines = append(m.Lines, GioRenderLine{Text: text, Role: GioRenderLineBody})
}

func (m *GioRenderModel) addMuted(text string) {
	m.Lines = append(m.Lines, GioRenderLine{Text: text, Role: GioRenderLineMuted})
}
