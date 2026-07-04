package terminal

import "fmt"

// ColorKind discriminates the color spaces a Cell color can live in.
//
// Do not renumber: renderers may switch on persisted values.
type ColorKind int

// ColorKind values.
const (
	// ColorDefault is the zero value: the terminal's default foreground
	// or background color, whatever the renderer's theme says that is.
	ColorDefault ColorKind = iota

	// Color16 selects one of the 16 classic ANSI palette entries via
	// Color.Index (0-7 normal, 8-15 bright).
	Color16

	// Color256 selects one of the xterm 256-color palette entries via
	// Color.Index.
	Color256

	// ColorRGB is a 24-bit truecolor value carried in Color.R, G, B.
	ColorRGB
)

// String returns a stable name for the ColorKind.
func (k ColorKind) String() string {
	switch k {
	case ColorDefault:
		return "default"
	case Color16:
		return "16"
	case Color256:
		return "256"
	case ColorRGB:
		return "rgb"
	default:
		return fmt.Sprintf("ColorKind(%d)", int(k))
	}
}

// Color is one terminal color. Kind says which of the remaining fields
// are meaningful: Index for Color16 and Color256, R/G/B for ColorRGB,
// none for ColorDefault. The zero Color is the default color.
type Color struct {
	// Kind selects the color space.
	Kind ColorKind

	// Index is the palette index for Color16 (0-15) and Color256 (0-255).
	Index uint8

	// R, G and B are the truecolor channels for ColorRGB.
	R, G, B uint8
}

// Cell is one character cell of the screen grid. The zero Cell is a
// blank cell: renderers must treat R == 0 as a space in the default
// colors.
type Cell struct {
	// R is the rune displayed in the cell; 0 means blank.
	R rune

	// FG and BG are the foreground and background colors.
	FG, BG Color

	// Bold, Underline and Inverse are the SGR attributes Kleiber
	// tracks. Inverse is stored, not applied: renderers swap FG/BG.
	Bold, Underline, Inverse bool
}
