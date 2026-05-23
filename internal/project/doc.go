// Package project models an opened Kleiber workspace: its modules,
// packages, files, and filesystem-watch lifecycle. The Project type wraps
// go.mod/go.work parsing (via golang.org/x/mod), package loading (via
// golang.org/x/tools/go/packages), and recursive directory watching (via
// github.com/fsnotify/fsnotify).
//
// All accessors return defensive snapshots: callers may freely keep and
// mutate the returned slices and structs without affecting other readers.
//
// See docs/architecture/components.md (section "Project Model") for the
// API intent and docs/architecture/overview.md for the data-flow story.
package project
