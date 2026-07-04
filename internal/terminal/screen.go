package terminal

import "sync"

// scrollbackCap is the maximum number of evicted lines the scrollback
// ring retains. Once full, the oldest line is dropped for each new one.
const scrollbackCap = 4000

// gridState is one screen buffer: a rows x cols cell grid plus the
// cursor that lives in it.
type gridState struct {
	cells   [][]Cell
	cursorX int
	cursorY int
}

// savedCursor remembers a cursor position and pen for DECSC/DECRC and
// the alternate-screen round trip.
type savedCursor struct {
	x, y  int
	pen   Cell
	valid bool
}

// ScreenSnapshot is a deep, immutable copy of the visible terminal
// state, safe to hand to a renderer while the Screen keeps mutating.
type ScreenSnapshot struct {
	// Cells is the visible grid, Rows slices of Cols cells each.
	Cells [][]Cell

	// CursorX and CursorY are the zero-based cursor position.
	CursorX, CursorY int

	// CursorVisible reports whether DECTCEM (CSI ?25) currently shows
	// the cursor.
	CursorVisible bool

	// Cols and Rows are the grid dimensions.
	Cols, Rows int

	// ScrollbackLen is the number of lines currently retained in the
	// scrollback ring; fetch them with Screen.ScrollbackLines.
	ScrollbackLen int

	// Title is the window title last set via OSC 0/2.
	Title string
}

// Screen is the terminal's display model: a main and an alternate cell
// grid, a cursor, pen state, a scroll region and a scrollback ring. It
// knows nothing about escape sequences or PTYs — Parser mutates it,
// renderers read it via Snapshot.
//
// All methods are safe for concurrent use.
type Screen struct {
	mu sync.RWMutex

	cols, rows int

	main      gridState
	alt       gridState
	altActive bool

	// pen is the current SGR state; its R field is unused.
	pen Cell

	// sb is the scrollback ring of lines evicted off the top of the
	// main screen. sbHead indexes the oldest line once the ring is
	// full; before that, sb is in insertion order and sbHead is 0.
	sb     [][]Cell
	sbHead int

	// scrollTop and scrollBottom bound the DECSTBM scroll region,
	// zero-based and inclusive.
	scrollTop, scrollBottom int

	cursorVisible  bool
	bracketedPaste bool
	title          string

	// saved backs ESC 7 / ESC 8 and CSI s / CSI u.
	saved savedCursor

	// altSaved backs the DECSET 1049 enter/leave cursor round trip.
	altSaved savedCursor

	// wrapPending implements deferred autowrap: writing in the last
	// column leaves the cursor on it and sets this flag; the next
	// printable rune wraps first. Explicit cursor motion clears it.
	wrapPending bool
}

// NewScreen returns a Screen of the given size showing the main buffer
// with a visible cursor at the origin. Dimensions are clamped to a
// minimum of 1x1.
func NewScreen(cols, rows int) *Screen {
	cols, rows = clampSize(cols, rows)
	return &Screen{
		cols:          cols,
		rows:          rows,
		main:          newGrid(cols, rows),
		alt:           newGrid(cols, rows),
		scrollBottom:  rows - 1,
		cursorVisible: true,
	}
}

func clampSize(cols, rows int) (int, int) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return cols, rows
}

func newGrid(cols, rows int) gridState {
	cells := make([][]Cell, rows)
	for i := range cells {
		cells[i] = make([]Cell, cols)
	}
	return gridState{cells: cells}
}

// Resize changes the grid dimensions, clamped to a minimum of 1x1.
// Content is preserved top-left aligned: shrinking truncates the
// bottom rows and right columns, growing pads with blank cells. There
// is no reflow — long lines stay truncated, and scrollback lines keep
// the width they had when evicted. The cursor is clamped into the new
// bounds and the scroll region resets to the full screen.
func (s *Screen) Resize(cols, rows int) {
	cols, rows = clampSize(cols, rows)
	s.mu.Lock()
	defer s.mu.Unlock()
	if cols == s.cols && rows == s.rows {
		return
	}
	resizeGrid(&s.main, cols, rows)
	resizeGrid(&s.alt, cols, rows)
	s.cols, s.rows = cols, rows
	s.scrollTop, s.scrollBottom = 0, rows-1
	s.wrapPending = false
	clampCursor(&s.main, cols, rows)
	clampCursor(&s.alt, cols, rows)
}

func resizeGrid(g *gridState, cols, rows int) {
	if len(g.cells) > rows {
		g.cells = g.cells[:rows]
	}
	for len(g.cells) < rows {
		g.cells = append(g.cells, make([]Cell, cols))
	}
	for i, row := range g.cells {
		switch {
		case len(row) > cols:
			g.cells[i] = row[:cols:cols]
		case len(row) < cols:
			padded := make([]Cell, cols)
			copy(padded, row)
			g.cells[i] = padded
		}
	}
}

func clampCursor(g *gridState, cols, rows int) {
	g.cursorX = clampInt(g.cursorX, 0, cols-1)
	g.cursorY = clampInt(g.cursorY, 0, rows-1)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Snapshot returns a deep copy of the visible state. The returned
// snapshot shares no memory with the Screen, so renderers may hold it
// for as long as they like.
func (s *Screen) Snapshot() ScreenSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g := s.grid()
	cells := make([][]Cell, len(g.cells))
	for i, row := range g.cells {
		cells[i] = append([]Cell(nil), row...)
	}
	return ScreenSnapshot{
		Cells:         cells,
		CursorX:       g.cursorX,
		CursorY:       g.cursorY,
		CursorVisible: s.cursorVisible,
		Cols:          s.cols,
		Rows:          s.rows,
		ScrollbackLen: len(s.sb),
		Title:         s.title,
	}
}

// ScrollbackLines returns deep copies of scrollback lines, oldest
// first: start 0 is the oldest retained line. Out-of-range requests
// are clamped; if nothing is in range the result is nil. Lines keep
// the column count they had when they were evicted, which may differ
// from the current width after a Resize.
func (s *Screen) ScrollbackLines(start, count int) [][]Cell {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.sb)
	if start < 0 {
		count += start
		start = 0
	}
	if n == 0 || count <= 0 || start >= n {
		return nil
	}
	if start+count > n {
		count = n - start
	}
	out := make([][]Cell, count)
	for i := range out {
		idx := (s.sbHead + start + i) % n
		out[i] = append([]Cell(nil), s.sb[idx]...)
	}
	return out
}

// BracketedPaste reports whether the application enabled bracketed
// paste mode (DECSET 2004). UI layers consult this before pasting so
// they can wrap the payload in ESC [200~ / ESC [201~ markers.
func (s *Screen) BracketedPaste() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bracketedPaste
}

// grid returns the active buffer. Callers must hold mu.
func (s *Screen) grid() *gridState {
	if s.altActive {
		return &s.alt
	}
	return &s.main
}

// blankCell is the fill value for erased and scrolled-in cells. Only
// the pen's background survives (a cheap take on background color
// erase); other attributes are dropped. Callers must hold mu.
func (s *Screen) blankCell() Cell {
	return Cell{BG: s.pen.BG}
}

// blankRow allocates a row of blank cells. Callers must hold mu.
func (s *Screen) blankRow() []Cell {
	row := make([]Cell, s.cols)
	blank := s.blankCell()
	if blank != (Cell{}) {
		for i := range row {
			row[i] = blank
		}
	}
	return row
}

// putRune writes r at the cursor with the current pen and advances.
// Autowrap is deferred: writing in the last column sets wrapPending
// instead of moving, and the wrap happens just before the next rune.
// Every rune is treated as one column wide — wide CJK glyphs and
// combining marks are a known MVP limitation.
func (s *Screen) putRune(r rune) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.grid()
	if s.wrapPending {
		s.wrapPending = false
		g.cursorX = 0
		s.lineFeedLocked(g)
	}
	c := s.pen
	c.R = r
	g.cells[g.cursorY][g.cursorX] = c
	if g.cursorX == s.cols-1 {
		s.wrapPending = true
	} else {
		g.cursorX++
	}
}

// lineFeed moves the cursor down one row, scrolling when it sits on
// the bottom margin of the scroll region.
func (s *Screen) lineFeed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	s.lineFeedLocked(s.grid())
}

func (s *Screen) lineFeedLocked(g *gridState) {
	switch {
	case g.cursorY == s.scrollBottom:
		s.scrollUpLocked(1, true)
	case g.cursorY < s.rows-1:
		g.cursorY++
	}
}

// reverseLineFeed moves the cursor up one row, scrolling down when it
// sits on the top margin of the scroll region (ESC M).
func (s *Screen) reverseLineFeed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	g := s.grid()
	switch {
	case g.cursorY == s.scrollTop:
		s.scrollDownLocked(1)
	case g.cursorY > 0:
		g.cursorY--
	}
}

// carriageReturn moves the cursor to column zero.
func (s *Screen) carriageReturn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	s.grid().cursorX = 0
}

// backspace moves the cursor one column left, stopping at the margin.
func (s *Screen) backspace() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	g := s.grid()
	if g.cursorX > 0 {
		g.cursorX--
	}
}

// tab advances the cursor to the next 8-column tab stop, clamped to
// the last column. Custom tab stops (HTS/TBC) are not supported.
func (s *Screen) tab() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	g := s.grid()
	next := (g.cursorX/8 + 1) * 8
	g.cursorX = clampInt(next, 0, s.cols-1)
}

// moveCursor moves the cursor by the given delta, clamped to the grid.
func (s *Screen) moveCursor(dx, dy int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	g := s.grid()
	g.cursorX = clampInt(g.cursorX+dx, 0, s.cols-1)
	g.cursorY = clampInt(g.cursorY+dy, 0, s.rows-1)
}

// setCursor places the cursor at the zero-based position, clamped to
// the grid. DECOM origin mode is not implemented; coordinates are
// always absolute.
func (s *Screen) setCursor(x, y int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	g := s.grid()
	g.cursorX = clampInt(x, 0, s.cols-1)
	g.cursorY = clampInt(y, 0, s.rows-1)
}

// setCursorCol sets the zero-based cursor column, clamped.
func (s *Screen) setCursorCol(x int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	s.grid().cursorX = clampInt(x, 0, s.cols-1)
}

// setCursorRow sets the zero-based cursor row, clamped.
func (s *Screen) setCursorRow(y int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	s.grid().cursorY = clampInt(y, 0, s.rows-1)
}

// cursorPosition returns the zero-based cursor position (for DSR).
func (s *Screen) cursorPosition() (x, y int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g := s.grid()
	return g.cursorX, g.cursorY
}

// eraseDisplay implements CSI J modes 0 (cursor to end), 1 (start to
// cursor, inclusive) and 2 (whole screen). Mode 3's scrollback part is
// clearScrollback.
func (s *Screen) eraseDisplay(mode int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	g := s.grid()
	blank := s.blankCell()
	switch mode {
	case 0:
		fillRow(g.cells[g.cursorY], g.cursorX, s.cols, blank)
		for y := g.cursorY + 1; y < s.rows; y++ {
			fillRow(g.cells[y], 0, s.cols, blank)
		}
	case 1:
		for y := 0; y < g.cursorY; y++ {
			fillRow(g.cells[y], 0, s.cols, blank)
		}
		fillRow(g.cells[g.cursorY], 0, g.cursorX+1, blank)
	case 2:
		for y := 0; y < s.rows; y++ {
			fillRow(g.cells[y], 0, s.cols, blank)
		}
	}
}

// clearScrollback drops every retained scrollback line (CSI 3 J).
func (s *Screen) clearScrollback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sb = nil
	s.sbHead = 0
}

// eraseLine implements CSI K modes 0 (cursor to end of line), 1 (start
// of line to cursor, inclusive) and 2 (whole line).
func (s *Screen) eraseLine(mode int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	g := s.grid()
	row := g.cells[g.cursorY]
	blank := s.blankCell()
	switch mode {
	case 0:
		fillRow(row, g.cursorX, s.cols, blank)
	case 1:
		fillRow(row, 0, g.cursorX+1, blank)
	case 2:
		fillRow(row, 0, s.cols, blank)
	}
}

// eraseChars blanks n cells starting at the cursor without shifting
// (CSI X).
func (s *Screen) eraseChars(n int) {
	if n < 1 {
		n = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	g := s.grid()
	end := clampInt(g.cursorX+n, 0, s.cols)
	fillRow(g.cells[g.cursorY], g.cursorX, end, s.blankCell())
}

// insertChars inserts n blank cells at the cursor, shifting the rest
// of the line right; cells pushed past the margin are lost (CSI @).
func (s *Screen) insertChars(n int) {
	if n < 1 {
		n = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	g := s.grid()
	row := g.cells[g.cursorY]
	if n > s.cols-g.cursorX {
		n = s.cols - g.cursorX
	}
	copy(row[g.cursorX+n:], row[g.cursorX:s.cols-n])
	fillRow(row, g.cursorX, g.cursorX+n, s.blankCell())
}

// deleteChars deletes n cells at the cursor, shifting the rest of the
// line left and blank-filling the tail (CSI P).
func (s *Screen) deleteChars(n int) {
	if n < 1 {
		n = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	g := s.grid()
	row := g.cells[g.cursorY]
	if n > s.cols-g.cursorX {
		n = s.cols - g.cursorX
	}
	copy(row[g.cursorX:], row[g.cursorX+n:])
	fillRow(row, s.cols-n, s.cols, s.blankCell())
}

// insertLines inserts n blank lines at the cursor row, shifting lines
// below it down within the scroll region (CSI L). Ignored when the
// cursor is outside the region.
func (s *Screen) insertLines(n int) {
	if n < 1 {
		n = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	g := s.grid()
	y := g.cursorY
	if y < s.scrollTop || y > s.scrollBottom {
		return
	}
	if n > s.scrollBottom-y+1 {
		n = s.scrollBottom - y + 1
	}
	for r := s.scrollBottom; r >= y+n; r-- {
		g.cells[r] = g.cells[r-n]
	}
	for r := y; r < y+n; r++ {
		g.cells[r] = s.blankRow()
	}
}

// deleteLines deletes n lines at the cursor row, shifting lines below
// it up within the scroll region and blank-filling the bottom (CSI M).
// Ignored when the cursor is outside the region.
func (s *Screen) deleteLines(n int) {
	if n < 1 {
		n = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	g := s.grid()
	y := g.cursorY
	if y < s.scrollTop || y > s.scrollBottom {
		return
	}
	if n > s.scrollBottom-y+1 {
		n = s.scrollBottom - y + 1
	}
	for r := y; r+n <= s.scrollBottom; r++ {
		g.cells[r] = g.cells[r+n]
	}
	for r := s.scrollBottom - n + 1; r <= s.scrollBottom; r++ {
		g.cells[r] = s.blankRow()
	}
}

// scrollUp scrolls the scroll region up n lines (CSI S). Lines leaving
// the region are discarded, not added to scrollback — only line feeds
// feed the scrollback ring.
func (s *Screen) scrollUp(n int) {
	if n < 1 {
		n = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	s.scrollUpLocked(n, false)
}

// scrollDown scrolls the scroll region down n lines (CSI T).
func (s *Screen) scrollDown(n int) {
	if n < 1 {
		n = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	s.scrollDownLocked(n)
}

// scrollUpLocked shifts the scroll region up. When record is true and
// the region spans the whole main screen, evicted lines are pushed
// into scrollback; the alternate screen never accumulates scrollback.
// Callers must hold mu.
func (s *Screen) scrollUpLocked(n int, record bool) {
	g := s.grid()
	top, bot := s.scrollTop, s.scrollBottom
	if n > bot-top+1 {
		n = bot - top + 1
	}
	record = record && !s.altActive && top == 0 && bot == s.rows-1
	for i := 0; i < n; i++ {
		if record {
			s.pushScrollbackLocked(g.cells[top])
		}
		copy(g.cells[top:bot], g.cells[top+1:bot+1])
		g.cells[bot] = s.blankRow()
	}
}

// scrollDownLocked shifts the scroll region down. Callers must hold mu.
func (s *Screen) scrollDownLocked(n int) {
	g := s.grid()
	top, bot := s.scrollTop, s.scrollBottom
	if n > bot-top+1 {
		n = bot - top + 1
	}
	for i := 0; i < n; i++ {
		for r := bot; r > top; r-- {
			g.cells[r] = g.cells[r-1]
		}
		g.cells[top] = s.blankRow()
	}
}

// pushScrollbackLocked appends a line to the scrollback ring, dropping
// the oldest line once the ring holds scrollbackCap lines. The line is
// adopted, not copied — callers must not keep mutating it. Callers
// must hold mu.
func (s *Screen) pushScrollbackLocked(line []Cell) {
	if len(s.sb) < scrollbackCap {
		s.sb = append(s.sb, line)
		return
	}
	s.sb[s.sbHead] = line
	s.sbHead = (s.sbHead + 1) % scrollbackCap
}

// setScrollRegion sets the DECSTBM margins, zero-based inclusive.
// Invalid regions (top >= bottom after clamping) reset to the full
// screen. The cursor homes to the origin, as xterm does.
func (s *Screen) setScrollRegion(top, bottom int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	top = clampInt(top, 0, s.rows-1)
	bottom = clampInt(bottom, 0, s.rows-1)
	if top >= bottom {
		top, bottom = 0, s.rows-1
	}
	s.scrollTop, s.scrollBottom = top, bottom
	g := s.grid()
	g.cursorX, g.cursorY = 0, 0
}

// saveCursor records the cursor position and pen (ESC 7, CSI s).
func (s *Screen) saveCursor() {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.grid()
	s.saved = savedCursor{x: g.cursorX, y: g.cursorY, pen: s.pen, valid: true}
}

// restoreCursor restores the state recorded by saveCursor (ESC 8,
// CSI u). Without a prior save it homes the cursor and resets the pen,
// matching xterm.
func (s *Screen) restoreCursor() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrapPending = false
	g := s.grid()
	if !s.saved.valid {
		g.cursorX, g.cursorY = 0, 0
		s.pen = Cell{}
		return
	}
	g.cursorX = clampInt(s.saved.x, 0, s.cols-1)
	g.cursorY = clampInt(s.saved.y, 0, s.rows-1)
	s.pen = s.saved.pen
}

// setCursorVisible tracks DECTCEM (CSI ?25 h/l).
func (s *Screen) setCursorVisible(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cursorVisible = v
}

// setBracketedPaste tracks DECSET 2004.
func (s *Screen) setBracketedPaste(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bracketedPaste = v
}

// setAltScreen switches between the main and alternate buffers. The
// modes 47, 1047 and 1049 are all treated like 1049: entering saves
// the cursor and pen and presents a cleared alternate screen; leaving
// restores them along with the untouched main screen content. The
// scroll region resets to full screen on every switch. Re-entering the
// active buffer is a no-op.
func (s *Screen) setAltScreen(enter bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enter == s.altActive {
		return
	}
	s.wrapPending = false
	if enter {
		s.altSaved = savedCursor{x: s.main.cursorX, y: s.main.cursorY, pen: s.pen, valid: true}
		s.alt = newGrid(s.cols, s.rows)
		s.altActive = true
	} else {
		s.altActive = false
		if s.altSaved.valid {
			s.main.cursorX = clampInt(s.altSaved.x, 0, s.cols-1)
			s.main.cursorY = clampInt(s.altSaved.y, 0, s.rows-1)
			s.pen = s.altSaved.pen
			s.altSaved = savedCursor{}
		}
	}
	s.scrollTop, s.scrollBottom = 0, s.rows-1
}

// setTitle records the window title (OSC 0/2).
func (s *Screen) setTitle(title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.title = title
}

// reset implements RIS (ESC c): both buffers are cleared, the cursor
// homes, pen and modes return to defaults, and the main screen becomes
// active. Scrollback and the window title survive, matching xterm.
func (s *Screen) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pen = Cell{}
	s.main = newGrid(s.cols, s.rows)
	s.alt = newGrid(s.cols, s.rows)
	s.altActive = false
	s.scrollTop, s.scrollBottom = 0, s.rows-1
	s.cursorVisible = true
	s.bracketedPaste = false
	s.saved = savedCursor{}
	s.altSaved = savedCursor{}
	s.wrapPending = false
}

// applySGR folds one CSI m parameter list into the pen. An empty list
// acts as a full reset, per ECMA-48. Extended color introducers
// (38/48) consume their sub-parameters; truncated ones end processing.
func (s *Screen) applySGR(params []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(params) == 0 {
		params = []int{0}
	}
	for i := 0; i < len(params); i++ {
		p := params[i]
		switch {
		case p == 0:
			s.pen = Cell{}
		case p == 1:
			s.pen.Bold = true
		case p == 4:
			s.pen.Underline = true
		case p == 7:
			s.pen.Inverse = true
		case p == 22:
			s.pen.Bold = false
		case p == 24:
			s.pen.Underline = false
		case p == 27:
			s.pen.Inverse = false
		case p >= 30 && p <= 37:
			s.pen.FG = Color{Kind: Color16, Index: uint8(p - 30)}
		case p == 39:
			s.pen.FG = Color{}
		case p >= 40 && p <= 47:
			s.pen.BG = Color{Kind: Color16, Index: uint8(p - 40)}
		case p == 49:
			s.pen.BG = Color{}
		case p >= 90 && p <= 97:
			s.pen.FG = Color{Kind: Color16, Index: uint8(p - 90 + 8)}
		case p >= 100 && p <= 107:
			s.pen.BG = Color{Kind: Color16, Index: uint8(p - 100 + 8)}
		case p == 38 || p == 48:
			color, consumed, ok := parseExtendedColor(params[i+1:])
			if !ok {
				return
			}
			if p == 38 {
				s.pen.FG = color
			} else {
				s.pen.BG = color
			}
			i += consumed
		}
	}
}

// parseExtendedColor decodes the tail of a 38/48 SGR introducer:
// "5;n" (256-color) or "2;r;g;b" (truecolor). It reports how many
// parameters it consumed and whether the tail was well-formed.
func parseExtendedColor(rest []int) (color Color, consumed int, ok bool) {
	if len(rest) == 0 {
		return Color{}, 0, false
	}
	switch rest[0] {
	case 5:
		if len(rest) < 2 {
			return Color{}, 0, false
		}
		return Color{Kind: Color256, Index: clampChannel(rest[1])}, 2, true
	case 2:
		if len(rest) < 4 {
			return Color{}, 0, false
		}
		return Color{
			Kind: ColorRGB,
			R:    clampChannel(rest[1]),
			G:    clampChannel(rest[2]),
			B:    clampChannel(rest[3]),
		}, 4, true
	default:
		return Color{}, 0, false
	}
}

func clampChannel(v int) uint8 {
	return uint8(clampInt(v, 0, 255))
}

func fillRow(row []Cell, from, to int, c Cell) {
	for i := from; i < to && i < len(row); i++ {
		row[i] = c
	}
}
