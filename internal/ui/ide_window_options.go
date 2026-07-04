package ui

// IDEWindowOptions configures the experimental read-only IDE window. It is
// shared by the gio and non-gio builds so the cmd launcher can construct it
// unconditionally; the field values are honored only when the window is
// built with the gio tag.
type IDEWindowOptions struct {
	// Title is the window title. Empty falls back to a default.
	Title string
	// WidthDP is the initial window width in device-independent pixels.
	// Non-positive falls back to a default.
	WidthDP int
	// HeightDP is the initial window height in device-independent pixels.
	// Non-positive falls back to a default.
	HeightDP int
}
