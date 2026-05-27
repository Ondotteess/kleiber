// Package ui defines Kleiber's desktop interface boundary on top of the gioui
// stack (see ADR-001 in docs/architecture/decisions.md). It will capture
// keyboard and pointer input, dispatch commands into internal/commands, and
// observe Core state without ever owning it.
//
// The current package contains pure state/view-model adapters, a presenter that
// tracks when state should be refreshed after core editor events, a typed
// controller that maps future UI intents onto app-owned commands, a shell
// boundary future windows can drive, and a minimal read-only Gio renderer behind
// the gio build tag. Editor rendering, production input handling, and widgets
// beyond the experimental state display are still pending.
package ui
