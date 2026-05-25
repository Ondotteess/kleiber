package lsp

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentURIFromPath_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "space dir", "hash#file.go")

	uri, err := DocumentURIFromPath(path)
	if err != nil {
		t.Fatalf("DocumentURIFromPath: %v", err)
	}
	if !strings.HasPrefix(string(uri), "file://") {
		t.Fatalf("uri = %q, want file:// prefix", uri)
	}
	if strings.Contains(string(uri), " ") || strings.Contains(string(uri), "#file") {
		t.Fatalf("uri = %q, want escaped space and #", uri)
	}

	got, err := PathFromDocumentURI(uri)
	if err != nil {
		t.Fatalf("PathFromDocumentURI: %v", err)
	}
	want, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Errorf("round trip path = %q, want %q", got, want)
	}
}

func TestDocumentURIFromPath_EmptyPath(t *testing.T) {
	_, err := DocumentURIFromPath("")
	if !errors.Is(err, ErrEmptyPath) {
		t.Errorf("err = %v, want ErrEmptyPath", err)
	}
}

func TestPathFromDocumentURI_RejectsNonFile(t *testing.T) {
	_, err := PathFromDocumentURI("https://example.test/main.go")
	if !errors.Is(err, ErrUnsupportedDocumentURI) {
		t.Errorf("err = %v, want ErrUnsupportedDocumentURI", err)
	}
}

func TestPathFromDocumentURI_DecodesEscapes(t *testing.T) {
	got, err := PathFromDocumentURI("file:///tmp/space%20dir/hash%23file.go")
	if err != nil {
		t.Fatalf("PathFromDocumentURI: %v", err)
	}
	want := filepath.FromSlash("/tmp/space dir/hash#file.go")
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}
