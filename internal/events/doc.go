// Package events provides a typed pub/sub bus that Kleiber's Core uses to
// notify the UI and other subsystems of state changes without sharing
// mutable state across goroutines.
//
// A Topic[T] is a typed channel of events of type T. Subscribers receive a
// buffered receive-only channel and a cancel function. Publishers block
// until each current subscriber accepts the event, or until the caller's
// context is canceled — backpressure is the explicit design choice, since
// silently dropping editor events leads to UI inconsistencies that are
// hard to debug.
//
// All operations are safe for concurrent use. No package-level state.
package events
