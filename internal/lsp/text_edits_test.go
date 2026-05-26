package lsp

import (
	"errors"
	"testing"

	"github.com/Ondotteess/kleiber/internal/editor"
)

func TestApplyTextEdits_ReplacesWholeDocument(t *testing.T) {
	buf := editor.NewBuffer("package  x\n\nfunc main( ){}\n")
	err := ApplyTextEdits(buf, []TextEdit{{
		Range: Range{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: 3, Character: 0},
		},
		NewText: "package x\n\nfunc main() {}\n",
	}})
	if err != nil {
		t.Fatalf("ApplyTextEdits: %v", err)
	}
	if got := buf.Text(); got != "package x\n\nfunc main() {}\n" {
		t.Errorf("Text() = %q", got)
	}
}

func TestApplyTextEdits_AppliesFromEndToStart(t *testing.T) {
	buf := editor.NewBuffer("one two three")
	edits := []TextEdit{
		{
			Range: Range{
				Start: Position{Line: 0, Character: len("one ")},
				End:   Position{Line: 0, Character: len("one two")},
			},
			NewText: "2",
		},
		{
			Range: Range{
				Start: Position{Line: 0, Character: len("one two ")},
				End:   Position{Line: 0, Character: len("one two three")},
			},
			NewText: "3",
		},
	}
	if err := ApplyTextEdits(buf, edits); err != nil {
		t.Fatalf("ApplyTextEdits: %v", err)
	}
	if got := buf.Text(); got != "one 2 3" {
		t.Errorf("Text() = %q, want %q", got, "one 2 3")
	}
}

func TestApplyTextEdits_ConvertsUTF16Ranges(t *testing.T) {
	buf := editor.NewBuffer("name := \"😀\"\n")
	err := ApplyTextEdits(buf, []TextEdit{{
		Range: Range{
			Start: Position{Line: 0, Character: len("name := \"")},
			End:   Position{Line: 0, Character: len("name := \"") + 2},
		},
		NewText: "ok",
	}})
	if err != nil {
		t.Fatalf("ApplyTextEdits: %v", err)
	}
	if got := buf.Text(); got != "name := \"ok\"\n" {
		t.Errorf("Text() = %q", got)
	}
}

func TestApplyTextEdits_RejectsOverlappingEdits(t *testing.T) {
	buf := editor.NewBuffer("abcdef")
	err := ApplyTextEdits(buf, []TextEdit{
		{
			Range: Range{
				Start: Position{Line: 0, Character: 1},
				End:   Position{Line: 0, Character: 4},
			},
			NewText: "x",
		},
		{
			Range: Range{
				Start: Position{Line: 0, Character: 3},
				End:   Position{Line: 0, Character: 5},
			},
			NewText: "y",
		},
	})
	if !errors.Is(err, ErrOverlappingTextEdits) {
		t.Fatalf("err = %v, want ErrOverlappingTextEdits", err)
	}
	if got := buf.Text(); got != "abcdef" {
		t.Errorf("buffer mutated after rejected edits: %q", got)
	}
}

func TestApplyTextEdits_RejectsNilBuffer(t *testing.T) {
	err := ApplyTextEdits(nil, nil)
	if !errors.Is(err, ErrNilBuffer) {
		t.Errorf("err = %v, want ErrNilBuffer", err)
	}
}
