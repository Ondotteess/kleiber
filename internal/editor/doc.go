// Package editor owns Kleiber's in-memory text-editing state: buffers,
// views, cursors, and undo history.
//
// The intended public API is sketched in docs/architecture/components.md
// (section "Editor Engine"). Implementation lands in Milestone 1, Phase 2 of
// the development plan; until then this file exists only to reserve the
// package path and keep `go build ./...` clean.
package editor
