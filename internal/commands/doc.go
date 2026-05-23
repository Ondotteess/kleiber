// Package commands provides Kleiber's Command Dispatcher: a typed
// registry that maps Command names to executable behavior. The UI,
// keyboard layer, command palette, AI bridge, and tests all dispatch
// commands through this single entry point. See
// docs/architecture/components.md (section "Command Dispatcher") for the
// design intent and docs/architecture/overview.md for how the dispatcher
// sits in the data flow.
//
// All operations are safe for concurrent use. The dispatcher owns no
// goroutines and starts none — commands execute on the caller's goroutine
// inside Dispatch.
package commands
