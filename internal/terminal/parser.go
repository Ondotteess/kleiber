package terminal

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"
)

// Parser buffer limits. Sequences that exceed them are consumed and
// discarded rather than truncated mid-way.
const (
	maxCSIParamBytes = 128
	maxCSIParams     = 32
	maxCSIParamValue = 65535
	maxOSCBytes      = 4096
)

// parseState tracks where the parser is inside an escape sequence.
//
// Do not renumber: stateGround must stay the zero value so a fresh
// Parser starts in ground state.
type parseState int

const (
	// stateGround is the default state: plain text and C0 controls.
	stateGround parseState = iota

	// stateEscape means an ESC byte was seen and the next byte picks
	// the sequence family.
	stateEscape

	// stateEscapeIntermediate consumes multi-byte ESC sequences such
	// as charset designations (ESC ( B) that Kleiber swallows.
	stateEscapeIntermediate

	// stateCSI collects parameter bytes of a CSI sequence.
	stateCSI

	// stateOSC collects the payload of an operating system command.
	stateOSC

	// stateOSCEscape means an ESC arrived inside an OSC payload; a
	// following backslash (ST) terminates the OSC.
	stateOSCEscape
)

// String returns a stable name for the parseState.
func (st parseState) String() string {
	switch st {
	case stateGround:
		return "ground"
	case stateEscape:
		return "escape"
	case stateEscapeIntermediate:
		return "escape-intermediate"
	case stateCSI:
		return "csi"
	case stateOSC:
		return "osc"
	case stateOSCEscape:
		return "osc-escape"
	default:
		return fmt.Sprintf("parseState(%d)", int(st))
	}
}

// Parser is a streaming VT/ANSI escape-sequence parser that applies a
// byte stream to a Screen. It never fails: malformed or unsupported
// sequences are consumed and dropped, and partial UTF-8 runes and
// escape sequences are buffered across Write calls.
//
// Parser is designed for a single producer (a Session's PTY reader);
// Write is serialized internally as a safety net, and the Screen's own
// lock keeps concurrent Snapshot calls safe.
type Parser struct {
	mu     sync.Mutex
	screen *Screen
	reply  io.Writer

	state parseState

	// partial buffers an incomplete UTF-8 sequence between Write
	// calls; partialWant is the total byte count the lead byte
	// announced.
	partial     [utf8.UTFMax]byte
	partialLen  int
	partialWant int

	// csiParams collects raw CSI parameter bytes (digits, ';', ':').
	csiParams  []byte
	csiPrivate bool
	csiIgnore  bool

	// osc collects the OSC payload.
	osc         []byte
	oscOverflow bool
}

// NewParser returns a Parser that mutates screen. reply, when non-nil,
// receives terminal responses such as the DSR cursor-position report;
// a Session passes the PTY input so responses flow back to the
// application. Reply write errors are dropped — a response the
// application no longer listens for is not the parser's problem. A nil
// screen is substituted with a default-sized one so a miswired Parser
// degrades to a hidden terminal instead of panicking.
func NewParser(screen *Screen, reply io.Writer) *Parser {
	if screen == nil {
		screen = NewScreen(defaultCols, defaultRows)
	}
	return &Parser{screen: screen, reply: reply}
}

// Write feeds a chunk of PTY output through the state machine. It
// always returns len(data) and a nil error: malformed input is
// consumed and dropped, never reported, so Parser satisfies io.Writer
// for use with io.Copy-style pumps.
func (p *Parser) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, b := range data {
		p.feed(b)
	}
	return len(data), nil
}

func (p *Parser) feed(b byte) {
	switch p.state {
	case stateEscape:
		p.feedEscape(b)
	case stateEscapeIntermediate:
		p.feedEscapeIntermediate(b)
	case stateCSI:
		p.feedCSI(b)
	case stateOSC:
		p.feedOSC(b)
	case stateOSCEscape:
		p.feedOSCEscape(b)
	default:
		p.feedGround(b)
	}
}

func (p *Parser) feedGround(b byte) {
	if p.partialLen > 0 {
		if b >= 0x80 && b < 0xC0 {
			p.partial[p.partialLen] = b
			p.partialLen++
			if p.partialLen == p.partialWant {
				r, _ := utf8.DecodeRune(p.partial[:p.partialLen])
				p.partialLen = 0
				p.screen.putRune(r)
			}
			return
		}
		// The multi-byte sequence was cut short. Surface a replacement
		// character and reprocess b as a fresh byte.
		p.partialLen = 0
		p.screen.putRune(utf8.RuneError)
	}
	switch {
	case b == 0x1B:
		p.state = stateEscape
	case b < 0x20:
		p.execC0(b)
	case b == 0x7F:
		// DEL: ignored.
	case b < 0x80:
		p.screen.putRune(rune(b))
	case b >= 0xC2 && b <= 0xDF:
		p.startPartial(b, 2)
	case b >= 0xE0 && b <= 0xEF:
		p.startPartial(b, 3)
	case b >= 0xF0 && b <= 0xF4:
		p.startPartial(b, 4)
	default:
		// Stray continuation byte or invalid lead (0x80-0xC1,
		// 0xF5-0xFF, including C1 controls, which Kleiber does not
		// interpret).
		p.screen.putRune(utf8.RuneError)
	}
}

func (p *Parser) startPartial(b byte, want int) {
	p.partial[0] = b
	p.partialLen = 1
	p.partialWant = want
}

// execC0 executes a C0 control byte. Per VT tradition C0 controls act
// even in the middle of a CSI sequence.
func (p *Parser) execC0(b byte) {
	switch b {
	case 0x08: // BS
		p.screen.backspace()
	case 0x09: // HT
		p.screen.tab()
	case 0x0A, 0x0B, 0x0C: // LF, VT, FF all act as line feeds.
		p.screen.lineFeed()
	case 0x0D: // CR
		p.screen.carriageReturn()
	default:
		// BEL, SO, SI, NUL and the rest: ignored.
	}
}

func (p *Parser) feedEscape(b byte) {
	switch {
	case b == '[':
		p.state = stateCSI
		p.resetCSI()
	case b == ']':
		p.state = stateOSC
		p.osc = p.osc[:0]
		p.oscOverflow = false
	case b == '7': // DECSC
		p.screen.saveCursor()
		p.state = stateGround
	case b == '8': // DECRC
		p.screen.restoreCursor()
		p.state = stateGround
	case b == 'c': // RIS
		p.screen.reset()
		p.state = stateGround
	case b == 'D': // IND
		p.screen.lineFeed()
		p.state = stateGround
	case b == 'E': // NEL
		p.screen.carriageReturn()
		p.screen.lineFeed()
		p.state = stateGround
	case b == 'M': // RI
		p.screen.reverseLineFeed()
		p.state = stateGround
	case b == 0x1B:
		// ESC ESC: restart the sequence.
	case b >= 0x20 && b <= 0x2F:
		p.state = stateEscapeIntermediate
	case b < 0x20:
		p.execC0(b)
	default:
		// Unsupported single-byte sequence: swallowed.
		p.state = stateGround
	}
}

func (p *Parser) feedEscapeIntermediate(b byte) {
	switch {
	case b == 0x1B:
		p.state = stateEscape
	case b >= 0x20 && b <= 0x2F:
		// More intermediates: keep consuming.
	case b >= 0x30 && b <= 0x7E:
		// Final byte of an unsupported sequence (e.g. ESC ( B
		// charset designation): swallowed.
		p.state = stateGround
	case b < 0x20:
		p.execC0(b)
	default:
		// DEL: ignored.
	}
}

func (p *Parser) resetCSI() {
	p.csiParams = p.csiParams[:0]
	p.csiPrivate = false
	p.csiIgnore = false
}

func (p *Parser) feedCSI(b byte) {
	switch {
	case b == 0x1B:
		p.state = stateEscape
	case b >= 0x40 && b <= 0x7E:
		p.dispatchCSI(b)
		p.state = stateGround
	case b >= 0x3C: // '<' '=' '>' '?'
		if b == '?' {
			p.csiPrivate = true
		} else {
			// Sequences with other markers (e.g. CSI > c) are not
			// Kleiber's: consume and drop.
			p.csiIgnore = true
		}
	case b >= 0x30: // digits, ';', ':'
		if len(p.csiParams) >= maxCSIParamBytes {
			p.csiIgnore = true
			return
		}
		p.csiParams = append(p.csiParams, b)
	case b >= 0x20:
		// Intermediate bytes announce sequences Kleiber does not
		// support: consume and drop.
		p.csiIgnore = true
	case b == 0x7F:
		// DEL: ignored.
	default:
		p.execC0(b)
	}
}

func (p *Parser) dispatchCSI(final byte) {
	if p.csiIgnore {
		return
	}
	params := parseCSIParams(p.csiParams)
	if p.csiPrivate {
		switch final {
		case 'h':
			p.setPrivateModes(params, true)
		case 'l':
			p.setPrivateModes(params, false)
		}
		return
	}
	switch final {
	case 'A': // CUU
		p.screen.moveCursor(0, -paramDefault(params, 0, 1))
	case 'B': // CUD
		p.screen.moveCursor(0, paramDefault(params, 0, 1))
	case 'C': // CUF
		p.screen.moveCursor(paramDefault(params, 0, 1), 0)
	case 'D': // CUB
		p.screen.moveCursor(-paramDefault(params, 0, 1), 0)
	case 'E': // CNL
		p.screen.moveCursor(0, paramDefault(params, 0, 1))
		p.screen.carriageReturn()
	case 'F': // CPL
		p.screen.moveCursor(0, -paramDefault(params, 0, 1))
		p.screen.carriageReturn()
	case 'G': // CHA
		p.screen.setCursorCol(paramDefault(params, 0, 1) - 1)
	case 'H', 'f': // CUP, HVP
		row := paramDefault(params, 0, 1)
		col := paramDefault(params, 1, 1)
		p.screen.setCursor(col-1, row-1)
	case 'J': // ED
		mode := paramAt(params, 0)
		if mode == 3 {
			p.screen.eraseDisplay(2)
			p.screen.clearScrollback()
		} else {
			p.screen.eraseDisplay(mode)
		}
	case 'K': // EL
		p.screen.eraseLine(paramAt(params, 0))
	case 'L': // IL
		p.screen.insertLines(paramDefault(params, 0, 1))
	case 'M': // DL
		p.screen.deleteLines(paramDefault(params, 0, 1))
	case 'P': // DCH
		p.screen.deleteChars(paramDefault(params, 0, 1))
	case '@': // ICH
		p.screen.insertChars(paramDefault(params, 0, 1))
	case 'S': // SU
		p.screen.scrollUp(paramDefault(params, 0, 1))
	case 'T': // SD; multi-parameter T initiates mouse tracking, dropped.
		if len(params) <= 1 {
			p.screen.scrollDown(paramDefault(params, 0, 1))
		}
	case 'X': // ECH
		p.screen.eraseChars(paramDefault(params, 0, 1))
	case 'd': // VPA
		p.screen.setCursorRow(paramDefault(params, 0, 1) - 1)
	case 'r': // DECSTBM
		top := paramDefault(params, 0, 1)
		bottom := paramAt(params, 1)
		if bottom <= 0 {
			bottom = 1 << 30 // "last row"; Screen clamps.
		}
		p.screen.setScrollRegion(top-1, bottom-1)
	case 's': // SCOSC
		p.screen.saveCursor()
	case 'u': // SCORC
		p.screen.restoreCursor()
	case 'm': // SGR
		p.screen.applySGR(params)
	case 'n': // DSR
		if paramAt(params, 0) == 6 {
			p.replyCursorPosition()
		}
	default:
		// Unsupported final byte: swallowed.
	}
}

func (p *Parser) setPrivateModes(params []int, on bool) {
	for _, m := range params {
		switch m {
		case 25: // DECTCEM
			p.screen.setCursorVisible(on)
		case 47, 1047, 1049: // alternate screen variants
			p.screen.setAltScreen(on)
		case 2004: // bracketed paste
			p.screen.setBracketedPaste(on)
		default:
			// Unsupported mode (mouse tracking, focus events, ...):
			// swallowed.
		}
	}
}

// replyCursorPosition emits the DSR 6 response, a 1-based cursor
// position report, to the reply writer.
func (p *Parser) replyCursorPosition() {
	if p.reply == nil {
		return
	}
	x, y := p.screen.cursorPosition()
	_, _ = fmt.Fprintf(p.reply, "\x1b[%d;%dR", y+1, x+1)
}

func (p *Parser) feedOSC(b byte) {
	switch {
	case b == 0x07: // BEL terminator
		p.dispatchOSC()
		p.state = stateGround
	case b == 0x1B:
		p.state = stateOSCEscape
	default:
		if len(p.osc) < maxOSCBytes {
			p.osc = append(p.osc, b)
		} else {
			p.oscOverflow = true
		}
	}
}

func (p *Parser) feedOSCEscape(b byte) {
	if b == '\\' { // ST terminator
		p.dispatchOSC()
		p.state = stateGround
		return
	}
	// The ESC did not introduce ST: abandon the OSC and treat the ESC
	// as the start of a new sequence.
	p.state = stateEscape
	p.feedEscape(b)
}

func (p *Parser) dispatchOSC() {
	if p.oscOverflow {
		return
	}
	code, rest, found := strings.Cut(string(p.osc), ";")
	if !found {
		return
	}
	switch code {
	case "0", "2": // icon name + window title / window title
		p.screen.setTitle(rest)
	default:
		// Other OSC commands (colors, clipboard, ...): swallowed.
	}
}

// parseCSIParams turns raw parameter bytes into integers. Empty
// positions parse as zero so each dispatch site applies its own
// default; a nil result means the sequence had no parameter bytes at
// all. Colon separators are treated like semicolons, which covers the
// common 38:5:n SGR form. Values clamp at maxCSIParamValue.
func parseCSIParams(buf []byte) []int {
	if len(buf) == 0 {
		return nil
	}
	params := make([]int, 0, 8)
	cur := 0
	for _, c := range buf {
		switch {
		case c >= '0' && c <= '9':
			cur = cur*10 + int(c-'0')
			if cur > maxCSIParamValue {
				cur = maxCSIParamValue
			}
		case c == ';' || c == ':':
			if len(params) < maxCSIParams {
				params = append(params, cur)
			}
			cur = 0
		}
	}
	return append(params, cur)
}

// paramAt returns params[i], or zero when absent.
func paramAt(params []int, i int) int {
	if i < len(params) {
		return params[i]
	}
	return 0
}

// paramDefault returns params[i], substituting def when the parameter
// is absent or zero — the standard "missing means 1" CSI rule.
func paramDefault(params []int, i, def int) int {
	if i < len(params) && params[i] > 0 {
		return params[i]
	}
	return def
}
