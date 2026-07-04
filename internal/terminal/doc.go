// Package terminal implements Kleiber's terminal emulator core: an
// in-memory screen model, a streaming VT/ANSI escape-sequence parser,
// and a PTY-backed session that runs a real shell. The package renders
// nothing — UI layers consume Screen snapshots and draw them.
//
// The layering is strict:
//
//   - Screen is a pure data structure: a grid of Cells, a cursor, pen
//     state, a scrollback ring, and main/alternate buffers. It knows
//     nothing about PTYs, processes, or escape sequences.
//   - Parser translates a byte stream (VT/ANSI escape sequences and
//     UTF-8 text) into Screen mutations. It knows nothing about where
//     the bytes come from; feeding it from a file works as well as
//     feeding it from a PTY.
//   - Session composes the two: it starts a shell on an OS pseudo
//     terminal (ConPTY on Windows, /dev/ptmx via creack/pty elsewhere),
//     pumps PTY output through a Parser into a Screen, and forwards
//     keyboard input back to the PTY.
//
// Screen is safe for concurrent use; renderers call Screen.Snapshot at
// any time while a Session pumps output into it. Parser is intended
// for a single writer (Session's reader goroutine), though Write is
// internally serialized as a safety net.
//
// Known MVP limitations, documented on the relevant methods: every
// rune occupies one column (no wide-glyph handling), Resize does not
// reflow, and erase operations preserve only the pen's background
// color.
package terminal
