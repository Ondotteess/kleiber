// Package ui renders Kleiber's desktop interface on top of the gioui
// stack (see ADR-001 in docs/architecture/decisions.md). It captures
// keyboard and pointer input, dispatches commands into internal/commands,
// and observes Core state without ever owning it.
//
// Implementation lands in Milestone 1, Phase 4 of the development plan.
package ui
