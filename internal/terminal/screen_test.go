package terminal

import (
	"strconv"
	"strings"
	"testing"
)

// rowText renders one row of cells as a string, mapping blank cells to
// spaces and trimming trailing whitespace.
func rowText(row []Cell) string {
	runes := make([]rune, len(row))
	for i, c := range row {
		if c.R == 0 {
			runes[i] = ' '
		} else {
			runes[i] = c.R
		}
	}
	return strings.TrimRight(string(runes), " ")
}

// screenRows renders every visible row of a snapshot via rowText.
func screenRows(snap ScreenSnapshot) []string {
	rows := make([]string, len(snap.Cells))
	for i, row := range snap.Cells {
		rows[i] = rowText(row)
	}
	return rows
}

// wantRows compares the visible rows against want; missing entries in
// want mean "blank row".
func wantRows(t *testing.T, s *Screen, want []string) {
	t.Helper()
	snap := s.Snapshot()
	got := screenRows(snap)
	for i, row := range got {
		w := ""
		if i < len(want) {
			w = want[i]
		}
		if row != w {
			t.Errorf("row %d = %q, want %q (all rows: %q)", i, row, w, got)
		}
	}
}

// writeLine types text onto the screen followed by CR and LF.
func writeLine(t *testing.T, s *Screen, text string) {
	t.Helper()
	for _, r := range text {
		s.putRune(r)
	}
	s.carriageReturn()
	s.lineFeed()
}

func TestScreen_NewScreen_Defaults(t *testing.T) {
	s := NewScreen(10, 5)
	snap := s.Snapshot()
	if snap.Cols != 10 || snap.Rows != 5 {
		t.Errorf("size = %dx%d, want 10x5", snap.Cols, snap.Rows)
	}
	if snap.CursorX != 0 || snap.CursorY != 0 {
		t.Errorf("cursor = (%d,%d), want (0,0)", snap.CursorX, snap.CursorY)
	}
	if !snap.CursorVisible {
		t.Error("CursorVisible = false, want true")
	}
	if snap.ScrollbackLen != 0 {
		t.Errorf("ScrollbackLen = %d, want 0", snap.ScrollbackLen)
	}
	if len(snap.Cells) != 5 || len(snap.Cells[0]) != 10 {
		t.Errorf("grid = %dx%d rows, want 5 rows of 10", len(snap.Cells), len(snap.Cells[0]))
	}
	if s.BracketedPaste() {
		t.Error("BracketedPaste = true, want false")
	}
}

func TestScreen_NewScreen_ClampsMinimumSize(t *testing.T) {
	s := NewScreen(0, -3)
	snap := s.Snapshot()
	if snap.Cols != 1 || snap.Rows != 1 {
		t.Errorf("size = %dx%d, want 1x1", snap.Cols, snap.Rows)
	}
}

func TestScreen_Resize_Table(t *testing.T) {
	tests := []struct {
		name       string
		cols, rows int
		wantRow0   string
		wantCursor [2]int
	}{
		{name: "grow both", cols: 8, rows: 6, wantRow0: "abc", wantCursor: [2]int{3, 0}},
		{name: "shrink cols truncates right", cols: 2, rows: 4, wantRow0: "ab", wantCursor: [2]int{1, 0}},
		{name: "shrink rows truncates bottom", cols: 6, rows: 2, wantRow0: "abc", wantCursor: [2]int{3, 0}},
		{name: "clamps to 1x1", cols: 0, rows: 0, wantRow0: "a", wantCursor: [2]int{0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScreen(6, 4)
			for _, r := range "abc" {
				s.putRune(r)
			}
			s.Resize(tt.cols, tt.rows)
			snap := s.Snapshot()
			wantCols, wantRows := tt.cols, tt.rows
			if wantCols < 1 {
				wantCols = 1
			}
			if wantRows < 1 {
				wantRows = 1
			}
			if snap.Cols != wantCols || snap.Rows != wantRows {
				t.Errorf("size = %dx%d, want %dx%d", snap.Cols, snap.Rows, wantCols, wantRows)
			}
			if got := rowText(snap.Cells[0]); got != tt.wantRow0 {
				t.Errorf("row 0 = %q, want %q", got, tt.wantRow0)
			}
			if snap.CursorX != tt.wantCursor[0] || snap.CursorY != tt.wantCursor[1] {
				t.Errorf("cursor = (%d,%d), want (%d,%d)",
					snap.CursorX, snap.CursorY, tt.wantCursor[0], tt.wantCursor[1])
			}
		})
	}
}

func TestScreen_Resize_ResetsScrollRegion(t *testing.T) {
	s := NewScreen(6, 4)
	s.setScrollRegion(1, 2)
	s.Resize(6, 3)
	// With the region reset to the full screen, a line feed on the last
	// row must scroll and feed the scrollback.
	s.setCursor(0, 2)
	s.putRune('x')
	s.lineFeed()
	if got := s.Snapshot().ScrollbackLen; got != 1 {
		t.Errorf("ScrollbackLen after resize+scroll = %d, want 1", got)
	}
}

func TestScreen_Resize_AltBufferFollows(t *testing.T) {
	s := NewScreen(6, 4)
	s.setAltScreen(true)
	s.Resize(3, 2)
	s.setCursor(2, 1)
	s.putRune('z')
	snap := s.Snapshot()
	if snap.Cols != 3 || snap.Rows != 2 {
		t.Errorf("size = %dx%d, want 3x2", snap.Cols, snap.Rows)
	}
	if snap.Cells[1][2].R != 'z' {
		t.Errorf("bottom-right cell = %q, want 'z'", snap.Cells[1][2].R)
	}
}

func TestScreen_Snapshot_DeepCopies(t *testing.T) {
	s := NewScreen(4, 2)
	s.putRune('a')
	snap := s.Snapshot()
	snap.Cells[0][0].R = 'Z'
	if got := s.Snapshot().Cells[0][0].R; got != 'a' {
		t.Errorf("cell after mutating snapshot = %q, want 'a'", got)
	}
}

func TestScreen_ScrollbackLines_RangesAndCopies(t *testing.T) {
	s := NewScreen(8, 2)
	for i := 0; i < 10; i++ {
		writeLine(t, s, "line"+strconv.Itoa(i))
	}
	// Two rows stay visible; lines 0..8 have been evicted.
	if got := s.Snapshot().ScrollbackLen; got != 9 {
		t.Fatalf("ScrollbackLen = %d, want 9", got)
	}

	tests := []struct {
		name         string
		start, count int
		want         []string
	}{
		{name: "oldest first", start: 0, count: 3, want: []string{"line0", "line1", "line2"}},
		{name: "count clamped", start: 7, count: 100, want: []string{"line7", "line8"}},
		{name: "negative start clamped", start: -2, count: 4, want: []string{"line0", "line1"}},
		{name: "start past end", start: 9, count: 1, want: nil},
		{name: "zero count", start: 0, count: 0, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := s.ScrollbackLines(tt.start, tt.count)
			if len(lines) != len(tt.want) {
				t.Fatalf("got %d lines, want %d", len(lines), len(tt.want))
			}
			for i, line := range lines {
				if got := rowText(line); got != tt.want[i] {
					t.Errorf("line %d = %q, want %q", i, got, tt.want[i])
				}
			}
		})
	}

	t.Run("returned lines are copies", func(t *testing.T) {
		lines := s.ScrollbackLines(0, 1)
		lines[0][0].R = 'Z'
		again := s.ScrollbackLines(0, 1)
		if got := rowText(again[0]); got != "line0" {
			t.Errorf("scrollback after mutating result = %q, want %q", got, "line0")
		}
	})
}

func TestScreen_Scrollback_RingEvictsOldest(t *testing.T) {
	s := NewScreen(8, 2)
	total := scrollbackCap + 51
	for i := 0; i < total; i++ {
		writeLine(t, s, strconv.Itoa(i))
	}
	snap := s.Snapshot()
	if snap.ScrollbackLen != scrollbackCap {
		t.Fatalf("ScrollbackLen = %d, want %d", snap.ScrollbackLen, scrollbackCap)
	}
	// total-1 lines were evicted; the ring keeps the newest
	// scrollbackCap of them.
	wantOldest := strconv.Itoa(total - 1 - scrollbackCap)
	wantNewest := strconv.Itoa(total - 2)
	if got := rowText(s.ScrollbackLines(0, 1)[0]); got != wantOldest {
		t.Errorf("oldest retained line = %q, want %q", got, wantOldest)
	}
	if got := rowText(s.ScrollbackLines(scrollbackCap-1, 1)[0]); got != wantNewest {
		t.Errorf("newest retained line = %q, want %q", got, wantNewest)
	}
}

func TestScreen_Snapshot_ConcurrentWithParserWrites(t *testing.T) {
	screen := NewScreen(20, 6)
	parser := NewParser(screen, nil)
	payload := []byte("hello \x1b[31mworld\x1b[0m\r\n\ttab\x1b[?1049h alt \x1b[?1049l\x1b[2J\x1b[H")
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			_, _ = parser.Write(payload)
		}
	}()
	for reading := true; reading; {
		select {
		case <-done:
			reading = false
		default:
		}
		snap := screen.Snapshot()
		if len(snap.Cells) != snap.Rows {
			t.Fatalf("snapshot has %d rows, want %d", len(snap.Cells), snap.Rows)
		}
		for _, row := range snap.Cells {
			if len(row) != snap.Cols {
				t.Fatalf("snapshot row has %d cells, want %d", len(row), snap.Cols)
			}
		}
		_ = screen.ScrollbackLines(0, snap.ScrollbackLen)
		_ = screen.BracketedPaste()
	}
}
