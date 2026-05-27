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
		"pre-alpha experimental UI",
		"commands: 0  buffers: 0  views: 0",
		"Editor widget pending",
		"No commands registered",
		"No open buffers",
		"No project opened",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("render model missing %q:\n%s", want, text)
		}
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
					{Path: "example.test/app", RelDir: "."},
				},
				Packages: []PackageItem{
					{ImportPath: "example.test/app"},
				},
			},
		},
	})

	text := renderModelText(model)
	for _, want := range []string{
		"commands: 2  buffers: 1  views: 1",
		"editor.openFile - Open file",
		"#1 main.go (dirty) - C:/repo/main.go",
		"root: C:/repo",
		"module: .",
		"package: example.test/app",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("render model missing %q:\n%s", want, text)
		}
	}
}

func renderModelText(model GioRenderModel) string {
	var b strings.Builder
	for _, line := range model.Lines {
		b.WriteString(line.Text)
		b.WriteByte('\n')
	}
	return b.String()
}
