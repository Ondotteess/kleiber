package lsp

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

var (
	// ErrEmptyPath is returned when converting an empty filesystem path
	// to a document URI.
	ErrEmptyPath = errors.New("lsp: empty path")

	// ErrUnsupportedDocumentURI is returned when a URI is not a local
	// file:// URI.
	ErrUnsupportedDocumentURI = errors.New("lsp: unsupported document URI")
)

// DocumentURIFromPath converts a local filesystem path to an LSP file URI.
// Relative paths are first resolved to absolute paths.
func DocumentURIFromPath(path string) (DocumentURI, error) {
	if path == "" {
		return "", ErrEmptyPath
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("lsp: resolving path %s: %w", path, err)
	}
	slash := filepath.ToSlash(abs)
	if len(slash) >= 2 && slash[1] == ':' {
		slash = "/" + slash
	}
	u := url.URL{Scheme: "file", Path: slash}
	return DocumentURI(u.String()), nil
}

// PathFromDocumentURI converts a local file URI to a filesystem path.
func PathFromDocumentURI(uri DocumentURI) (string, error) {
	u, err := url.Parse(string(uri))
	if err != nil {
		return "", fmt.Errorf("lsp: parsing document URI %q: %w", uri, err)
	}
	if u.Scheme != "file" || u.Host != "" {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedDocumentURI, uri)
	}
	path := u.Path
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = strings.TrimPrefix(path, "/")
	}
	return filepath.FromSlash(path), nil
}
