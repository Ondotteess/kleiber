package ui

import (
	"strings"
	"testing"

	"github.com/Ondotteess/kleiber/internal/editor"
)

func TestBuildGioRenderModel_EmptyState(t *testing.T) {
	model := BuildGioRenderModel(ShellState{Title: "Kleiber Test"})

	if model.Title != "Kleiber Test" {
		t.Fatalf("Title = %q, want Kleiber Test", model.Title)
	}
	text := renderModelText(model)
	for _, want := range []string{
		"read-only experimental UI | pre-alpha | gopls not auto-started",
		"window: Ctrl+P palette | Up/Down navigate | Enter pending/no-op | Esc closes palette/quit | F5/Ctrl+R/Command+R refresh | Ctrl+Q/Command+Q quit",
		"summary: commands 0 | buffers 0 | views 0",
		"Project",
		"Buffers",
		"Commands",
		"Editor",
		"Editor widget pending",
		"File tree interaction pending",
		"Command palette execution pending",
		"No commands registered",
		"No open buffers",
		"No project opened",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("render model missing %q:\n%s", want, text)
		}
	}
}

func TestExperimentalUIShortcutSummary_CoversPaletteContract(t *testing.T) {
	for _, want := range []string{
		"Ctrl+P/Command+P opens palette",
		"Up/Down navigate palette",
		"Enter pending/no-op (command execution pending)",
		"Escape closes palette before quit",
		"F5/Ctrl+R/Command+R refresh",
		"Ctrl+Q/Command+Q quit",
	} {
		if !strings.Contains(ExperimentalUIShortcutSummary, want) {
			t.Fatalf("shortcut summary missing %q: %s", want, ExperimentalUIShortcutSummary)
		}
	}
}

func TestBuildGioRenderModel_CommandPaletteOpen(t *testing.T) {
	commands := []CommandItem{
		{Name: "editor.openFile", Description: "Open file"},
		{Name: "project.refresh", Description: "Refresh project"},
	}
	palette := CommandPaletteState{Open: true, SelectedIndex: 1}.Snapshot(commands)
	model := BuildGioRenderModel(ShellState{
		State:   State{Commands: commands},
		Palette: palette,
	})

	text := renderModelText(model)
	for _, want := range []string{
		"Command Palette",
		"Ctrl+P open | Up/Down navigate | Enter execution pending | Escape close",
		"  editor.openFile - Open file",
		"> project.refresh - Refresh project",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("render model missing %q:\n%s", want, text)
		}
	}
	assertBefore(t, text, "Command Palette", "Project")
}

func TestBuildGioRenderModel_CommandPaletteEmpty(t *testing.T) {
	model := BuildGioRenderModel(ShellState{
		Palette: CommandPaletteState{Open: true}.Snapshot(nil),
	})

	text := renderModelText(model)
	if !strings.Contains(text, "Command Palette") || !strings.Contains(text, "No commands registered") {
		t.Fatalf("render model missing empty palette state:\n%s", text)
	}
}

func TestBuildGioRenderModel_RefreshErrorStatus(t *testing.T) {
	model := BuildGioRenderModel(ShellState{
		Title:        "Kleiber Test",
		RefreshError: "project refresh failed",
	})

	text := renderModelText(model)
	if !strings.Contains(text, "status: refresh failed: project refresh failed") {
		t.Fatalf("render model missing refresh error:\n%s", text)
	}
}

func TestBuildGioRenderModel_StateSummary(t *testing.T) {
	model := BuildGioRenderModel(ShellState{
		Title: "Kleiber",
		State: State{
			Commands: []CommandItem{
				{Name: "editor.openFile", Description: "Open file"},
				{Name: "project.refresh", Description: "Refresh project metadata"},
			},
			Buffers: []BufferItem{
				{ID: editor.BufferID(1), DisplayName: "main.go", Path: "C:/repo/main.go", Dirty: true},
			},
			Views: []ViewItem{{ID: editor.ViewID(1), BufferID: editor.BufferID(1)}},
			Project: ProjectState{
				Open: true,
				Root: "C:/repo",
				Modules: []ModuleItem{
					{Path: "example.test/app", RelDir: ".", GoVersion: "1.25"},
				},
				Packages: []PackageItem{
					{
						ImportPath: "example.test/app",
						Files:      []FileItem{{RelPath: "main.go"}},
						TestFiles:  []FileItem{{RelPath: "main_test.go"}},
					},
				},
			},
		},
	})

	text := renderModelText(model)
	for _, want := range []string{
		"summary: commands 2 | buffers 1 | views 1",
		"editor.openFile - Open file",
		"#1 main.go (dirty) - C:/repo/main.go",
		"root: C:/repo",
		"modules: 1 | packages: 1",
		"module: . (go 1.25)",
		"package: example.test/app (files: 1, tests: 1)",
		"file: main.go",
		"test: main_test.go",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("render model missing %q:\n%s", want, text)
		}
	}
}

func TestBuildGioRenderModel_UntitledDirtyBuffer(t *testing.T) {
	model := BuildGioRenderModel(ShellState{
		State: State{
			Buffers: []BufferItem{
				{ID: editor.BufferID(7), DisplayName: "Untitled", Dirty: true},
			},
		},
	})

	text := renderModelText(model)
	if !strings.Contains(text, "#7 Untitled (dirty) - untitled") {
		t.Fatalf("render model missing untitled dirty marker:\n%s", text)
	}
}

func TestBuildGioRenderModel_LongLinesCompacted(t *testing.T) {
	longCommand := "editor." + strings.Repeat("veryLongCommandName", 20)
	longPath := "C:/" + strings.Repeat("deep/", 50) + "main.go"

	model := BuildGioRenderModel(ShellState{
		State: State{
			Commands: []CommandItem{{Name: longCommand, Description: strings.Repeat("description ", 30)}},
			Buffers:  []BufferItem{{ID: editor.BufferID(1), DisplayName: "main.go", Path: longPath}},
		},
	})

	for _, line := range model.Lines {
		if got := len([]rune(line.Text)); got > maxGioLineRunes {
			t.Fatalf("line length = %d, want <= %d: %q", got, maxGioLineRunes, line.Text)
		}
	}
	text := renderModelText(model)
	if !strings.Contains(text, "...") {
		t.Fatalf("render model did not compact long lines:\n%s", text)
	}
}

func TestBuildGioRenderModel_DeterministicOrdering(t *testing.T) {
	state := ShellState{
		State: State{
			Commands: []CommandItem{
				{Name: "z.command"},
				{Name: "a.command"},
			},
			Buffers: []BufferItem{
				{ID: editor.BufferID(3), DisplayName: "third.go"},
				{ID: editor.BufferID(1), DisplayName: "first.go"},
			},
			Project: ProjectState{
				Open: true,
				Root: "C:/repo",
				Modules: []ModuleItem{
					{Path: "example.test/b", RelDir: "b"},
					{Path: "example.test/a", RelDir: "a"},
				},
				Packages: []PackageItem{
					{ImportPath: "example.test/b", Files: []FileItem{{RelPath: "b.go"}}},
					{ImportPath: "example.test/a", Files: []FileItem{{RelPath: "a.go"}}},
				},
			},
		},
	}

	first := renderModelText(BuildGioRenderModel(state))
	second := renderModelText(BuildGioRenderModel(state))
	if first != second {
		t.Fatalf("render model is not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	assertBefore(t, first, "module: a", "module: b")
	assertBefore(t, first, "package: example.test/a", "package: example.test/b")
	assertBefore(t, first, "#1 first.go", "#3 third.go")
	assertBefore(t, first, "a.command", "z.command")
}

func renderModelText(model GioRenderModel) string {
	var b strings.Builder
	for _, line := range model.Lines {
		b.WriteString(line.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func assertBefore(t *testing.T, text, left, right string) {
	t.Helper()
	leftIndex := strings.Index(text, left)
	rightIndex := strings.Index(text, right)
	if leftIndex < 0 || rightIndex < 0 {
		t.Fatalf("missing %q or %q in:\n%s", left, right, text)
	}
	if leftIndex >= rightIndex {
		t.Fatalf("%q should appear before %q in:\n%s", left, right, text)
	}
}
