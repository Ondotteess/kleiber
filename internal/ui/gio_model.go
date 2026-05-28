package ui

import (
	"fmt"
	"sort"
	"strings"
)

const (
	maxGioPreviewItems = 12
	maxGioLineRunes    = 160
)

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
	GioRenderLineStatus  GioRenderLineRole = "status"
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
			{Text: "read-only experimental UI | pre-alpha | gopls not auto-started", Role: GioRenderLineStatus},
			{Text: "window: Ctrl+P palette | Up/Down navigate | Enter pending/no-op | Esc closes palette/quit | F5/Ctrl+R/Command+R refresh | Ctrl+Q/Command+Q quit", Role: GioRenderLineMuted},
			{Text: fmt.Sprintf("summary: commands %d | buffers %d | views %d", len(state.Commands), len(state.Buffers), len(state.Views)), Role: GioRenderLineMuted},
		},
	}
	if snapshot.RefreshError != "" {
		model.Lines = append(model.Lines, GioRenderLine{
			Text: compactGioLine("status: refresh failed: " + snapshot.RefreshError),
			Role: GioRenderLineStatus,
		})
	}
	model.addCommandPalette(snapshot.Palette)
	model.addProject(state.Project)
	model.addBuffers(state.Buffers)
	model.addCommands(state.Commands)
	model.addEditorPlaceholder()
	return model
}

func (m *GioRenderModel) addCommandPalette(palette CommandPaletteSnapshot) {
	if !palette.Open {
		return
	}
	m.addSection("Command Palette")
	m.addMuted("Ctrl+P open | Up/Down navigate | Enter execution pending | Escape close")
	commands := sortedCommands(palette.Commands)
	if len(commands) == 0 {
		m.addMuted("No commands registered")
		return
	}
	selected := clampPaletteIndex(palette.SelectedIndex, len(commands))
	start := 0
	if selected >= maxGioPreviewItems {
		start = selected - maxGioPreviewItems + 1
	}
	if start > 0 {
		m.addMuted(fmt.Sprintf("...%d commands before", start))
	}
	for i := start; i < len(commands) && i < start+maxGioPreviewItems; i++ {
		cmd := commands[i]
		marker := "  "
		if i == selected {
			marker = "> "
		}
		line := marker + cmd.Name
		if cmd.Description != "" {
			line += " - " + cmd.Description
		}
		m.addBody(line)
	}
	if remaining := len(commands) - (start + maxGioPreviewItems); remaining > 0 {
		m.addMuted(fmt.Sprintf("...and %d more commands", remaining))
	}
}

func (m *GioRenderModel) addCommands(commands []CommandItem) {
	m.addSection("Commands")
	commands = sortedCommands(commands)
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
	buffers = sortedBuffers(buffers)
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
		name := buf.DisplayName
		if name == "" {
			name = "Untitled"
		}
		line := fmt.Sprintf("#%d %s (%s)", buf.ID, name, dirty)
		if buf.Path != "" {
			line += " - " + buf.Path
		} else {
			line += " - untitled"
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
	modules := sortedModules(project.Modules)
	packages := sortedPackages(project.Packages)
	m.addBody(fmt.Sprintf("modules: %d | packages: %d", len(modules), len(packages)))
	for i, module := range modules {
		if i >= maxGioPreviewItems {
			m.addMuted(fmt.Sprintf("...and %d more modules", len(modules)-i))
			break
		}
		label := module.RelDir
		if label == "" {
			label = module.Path
		}
		detail := label
		if module.GoVersion != "" {
			detail += " (go " + module.GoVersion + ")"
		}
		m.addBody("module: " + detail)
	}
	for i, pkg := range packages {
		if i >= maxGioPreviewItems {
			m.addMuted(fmt.Sprintf("...and %d more packages", len(packages)-i))
			return
		}
		m.addBody(fmt.Sprintf("package: %s (files: %d, tests: %d)", pkg.ImportPath, len(pkg.Files), len(pkg.TestFiles)))
		m.addFiles("file", pkg.Files)
		m.addFiles("test", pkg.TestFiles)
	}
}

func (m *GioRenderModel) addFiles(label string, files []FileItem) {
	files = sortedFiles(files)
	for i, file := range files {
		if i >= 2 {
			m.addMuted(fmt.Sprintf("  ...and %d more %s files", len(files)-i, label))
			return
		}
		path := file.RelPath
		if path == "" {
			path = file.DisplayName
		}
		m.addMuted("  " + label + ": " + path)
	}
}

func (m *GioRenderModel) addEditorPlaceholder() {
	m.addSection("Editor")
	m.addMuted("Editor widget pending")
	m.addMuted("File tree interaction pending")
	m.addMuted("Command palette execution pending")
}

func (m *GioRenderModel) addSection(text string) {
	m.Lines = append(m.Lines, GioRenderLine{Text: compactGioLine(text), Role: GioRenderLineSection})
}

func (m *GioRenderModel) addBody(text string) {
	m.Lines = append(m.Lines, GioRenderLine{Text: compactGioLine(text), Role: GioRenderLineBody})
}

func (m *GioRenderModel) addMuted(text string) {
	m.Lines = append(m.Lines, GioRenderLine{Text: compactGioLine(text), Role: GioRenderLineMuted})
}

func sortedCommands(commands []CommandItem) []CommandItem {
	out := append([]CommandItem(nil), commands...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Description < out[j].Description
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func sortedBuffers(buffers []BufferItem) []BufferItem {
	out := append([]BufferItem(nil), buffers...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sortedModules(modules []ModuleItem) []ModuleItem {
	out := append([]ModuleItem(nil), modules...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].RelDir == out[j].RelDir {
			return out[i].Path < out[j].Path
		}
		return out[i].RelDir < out[j].RelDir
	})
	return out
}

func sortedPackages(packages []PackageItem) []PackageItem {
	out := append([]PackageItem(nil), packages...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ImportPath == out[j].ImportPath {
			return out[i].RelDir < out[j].RelDir
		}
		return out[i].ImportPath < out[j].ImportPath
	})
	return out
}

func sortedFiles(files []FileItem) []FileItem {
	out := append([]FileItem(nil), files...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].RelPath == out[j].RelPath {
			return out[i].Path < out[j].Path
		}
		return out[i].RelPath < out[j].RelPath
	})
	return out
}

func compactGioLine(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	text = strings.TrimRight(text, " \t\r\n")
	runes := []rune(text)
	if len(runes) <= maxGioLineRunes {
		return text
	}
	return string(runes[:maxGioLineRunes-3]) + "..."
}
