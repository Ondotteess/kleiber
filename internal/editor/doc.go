// Package editor implements Kleiber's in-memory editor engine: text
// buffers, edit operations, and undo/redo. The package is UI- and
// I/O-agnostic so it can be driven equally well from a graphical
// frontend, the LSP layer (textDocument/didChange), or unit tests.
//
// The central type is Buffer. A Buffer holds the text of one open
// file, supports Insert and Delete, and maintains an undo/redo
// history. Higher-level orchestration (EditorEngine: opening files
// from disk, saving, publishing change events) lands in a follow-up
// — this package is the bottom of the editor stack.
//
// Positions are zero-based, with Column measured in bytes — see
// Position for the rationale.
package editor
