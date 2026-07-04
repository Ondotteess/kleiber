package terminal

import (
	"bytes"
	"testing"
)

// newTestParser builds a Screen of the given size with a Parser wired
// to it and no reply writer.
func newTestParser(t *testing.T, cols, rows int) (*Parser, *Screen) {
	t.Helper()
	s := NewScreen(cols, rows)
	return NewParser(s, nil), s
}

// feed writes the whole string in one Write call and fails the test on
// a violated Write contract.
func feed(t *testing.T, p *Parser, data string) {
	t.Helper()
	n, err := p.Write([]byte(data))
	if err != nil {
		t.Fatalf("Write(%q) error: %v", data, err)
	}
	if n != len(data) {
		t.Fatalf("Write(%q) = %d, want %d", data, n, len(data))
	}
}

func wantCursor(t *testing.T, s *Screen, x, y int) {
	t.Helper()
	snap := s.Snapshot()
	if snap.CursorX != x || snap.CursorY != y {
		t.Errorf("cursor = (%d,%d), want (%d,%d)", snap.CursorX, snap.CursorY, x, y)
	}
}

func TestParser_Write_PrintsText(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		cursorX int
		cursorY int
	}{
		{name: "plain ascii", input: "hello", want: []string{"hello"}, cursorX: 5},
		{name: "carriage return overwrites", input: "ab\rc", want: []string{"cb"}, cursorX: 1},
		{name: "bare LF keeps column", input: "a\nb", want: []string{"a", " b"}, cursorX: 2, cursorY: 1},
		{name: "CRLF starts next line", input: "a\r\nb", want: []string{"a", "b"}, cursorX: 1, cursorY: 1},
		{name: "tab to 8-column stop", input: "\tX", want: []string{"        X"}, cursorX: 9},
		{name: "backspace then overwrite", input: "ab\bX", want: []string{"aX"}, cursorX: 2},
		{name: "backspace stops at margin", input: "\b\bZ", want: []string{"Z"}, cursorX: 1},
		{name: "multi-byte utf8", input: "héllo", want: []string{"héllo"}, cursorX: 5},
		{name: "cjk runes one cell each", input: "日本", want: []string{"日本"}, cursorX: 2},
		{name: "bel ignored", input: "a\x07b", want: []string{"ab"}, cursorX: 2},
		{name: "so si ignored", input: "a\x0eb\x0fc", want: []string{"abc"}, cursorX: 3},
		{name: "vt and ff act as lf", input: "a\x0b\rb\x0c\rc", want: []string{"a", "b", "c"}, cursorX: 1, cursorY: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, s := newTestParser(t, 20, 5)
			feed(t, p, tt.input)
			wantRows(t, s, tt.want)
			wantCursor(t, s, tt.cursorX, tt.cursorY)
		})
	}
}

func TestParser_Write_AutowrapDeferred(t *testing.T) {
	t.Run("cursor parks on last column", func(t *testing.T) {
		p, s := newTestParser(t, 5, 3)
		feed(t, p, "abcde")
		wantRows(t, s, []string{"abcde"})
		wantCursor(t, s, 4, 0)
	})
	t.Run("next rune wraps", func(t *testing.T) {
		p, s := newTestParser(t, 5, 3)
		feed(t, p, "abcdef")
		wantRows(t, s, []string{"abcde", "f"})
		wantCursor(t, s, 1, 1)
	})
	t.Run("long text wraps continuously", func(t *testing.T) {
		p, s := newTestParser(t, 5, 3)
		feed(t, p, "abcdefghij")
		wantRows(t, s, []string{"abcde", "fghij"})
		wantCursor(t, s, 4, 1)
	})
	t.Run("carriage return cancels pending wrap", func(t *testing.T) {
		p, s := newTestParser(t, 5, 3)
		feed(t, p, "abcde\rX")
		wantRows(t, s, []string{"Xbcde"})
		wantCursor(t, s, 1, 0)
	})
	t.Run("cursor motion cancels pending wrap", func(t *testing.T) {
		p, s := newTestParser(t, 5, 3)
		feed(t, p, "abcde\x1b[Hf")
		wantRows(t, s, []string{"fbcde"})
		wantCursor(t, s, 1, 0)
	})
}

func TestParser_Write_LineFeedScrollsIntoScrollback(t *testing.T) {
	p, s := newTestParser(t, 8, 2)
	feed(t, p, "one\r\ntwo\r\nthree")
	wantRows(t, s, []string{"two", "three"})
	snap := s.Snapshot()
	if snap.ScrollbackLen != 1 {
		t.Fatalf("ScrollbackLen = %d, want 1", snap.ScrollbackLen)
	}
	if got := rowText(s.ScrollbackLines(0, 1)[0]); got != "one" {
		t.Errorf("scrollback line = %q, want %q", got, "one")
	}
}

func TestParser_Write_AltScreenHasNoScrollback(t *testing.T) {
	p, s := newTestParser(t, 8, 2)
	feed(t, p, "\x1b[?1049h")
	feed(t, p, "a\r\nb\r\nc\r\nd")
	if got := s.Snapshot().ScrollbackLen; got != 0 {
		t.Errorf("ScrollbackLen on alt screen = %d, want 0", got)
	}
}

func TestParser_Write_CursorMovement(t *testing.T) {
	tests := []struct {
		name  string
		input string
		x, y  int
	}{
		{name: "CUP 1-based", input: "\x1b[3;4H", x: 3, y: 2},
		{name: "CUP clamps", input: "\x1b[99;99H", x: 9, y: 4},
		{name: "CUP no params homes", input: "\x1b[5;5H\x1b[H", x: 0, y: 0},
		{name: "HVP", input: "\x1b[2;3f", x: 2, y: 1},
		{name: "CUU", input: "\x1b[4;4H\x1b[2A", x: 3, y: 1},
		{name: "CUU clamps at top", input: "\x1b[2;2H\x1b[9A", x: 1, y: 0},
		{name: "CUD default one", input: "\x1b[B", x: 0, y: 1},
		{name: "CUD zero means one", input: "\x1b[0B", x: 0, y: 1},
		{name: "CUF", input: "\x1b[3C", x: 3, y: 0},
		{name: "CUB", input: "\x1b[1;5H\x1b[2D", x: 2, y: 0},
		{name: "CNL to column zero", input: "\x1b[1;5H\x1b[2E", x: 0, y: 2},
		{name: "CPL", input: "\x1b[4;5H\x1b[F", x: 0, y: 2},
		{name: "CHA", input: "\x1b[7G", x: 6, y: 0},
		{name: "CHA clamps", input: "\x1b[77G", x: 9, y: 0},
		{name: "VPA", input: "\x1b[3;3H\x1b[2d", x: 2, y: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, s := newTestParser(t, 10, 5)
			feed(t, p, tt.input)
			wantCursor(t, s, tt.x, tt.y)
		})
	}
}

// setupGrid3 fills a 4x3 screen with abcd/efgh/ijkl and parks the
// cursor at (1,1).
func setupGrid3(t *testing.T) (*Parser, *Screen) {
	t.Helper()
	p, s := newTestParser(t, 4, 3)
	feed(t, p, "abcd\r\nefgh\r\nijkl\x1b[2;2H")
	return p, s
}

func TestParser_Write_EraseDisplay(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "mode 0 cursor to end", input: "\x1b[J", want: []string{"abcd", "e", ""}},
		{name: "mode 0 explicit", input: "\x1b[0J", want: []string{"abcd", "e", ""}},
		{name: "mode 1 start to cursor", input: "\x1b[1J", want: []string{"", "  gh", "ijkl"}},
		{name: "mode 2 whole screen", input: "\x1b[2J", want: []string{"", "", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, s := setupGrid3(t)
			feed(t, p, tt.input)
			wantRows(t, s, tt.want)
			wantCursor(t, s, 1, 1)
		})
	}
}

func TestParser_Write_EraseDisplayClearsScrollback(t *testing.T) {
	p, s := newTestParser(t, 4, 2)
	feed(t, p, "a\r\nb\r\nc\r\nd")
	if got := s.Snapshot().ScrollbackLen; got != 2 {
		t.Fatalf("ScrollbackLen before ED 3 = %d, want 2", got)
	}
	feed(t, p, "\x1b[3J")
	snap := s.Snapshot()
	if snap.ScrollbackLen != 0 {
		t.Errorf("ScrollbackLen after ED 3 = %d, want 0", snap.ScrollbackLen)
	}
	wantRows(t, s, []string{"", ""})
}

func TestParser_Write_EraseLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "mode 0 cursor to end", input: "\x1b[K", want: []string{"abcd", "e", "ijkl"}},
		{name: "mode 1 start to cursor", input: "\x1b[1K", want: []string{"abcd", "  gh", "ijkl"}},
		{name: "mode 2 whole line", input: "\x1b[2K", want: []string{"abcd", "", "ijkl"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, s := setupGrid3(t)
			feed(t, p, tt.input)
			wantRows(t, s, tt.want)
		})
	}
}

func TestParser_Write_EraseInsertDeleteChars(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "ECH blanks in place", input: "\x1b[2X", want: "ab  ef"},
		{name: "DCH shifts left", input: "\x1b[2P", want: "abef"},
		{name: "ICH shifts right", input: "\x1b[2@", want: "ab  cd"},
		{name: "ICH clamps at margin", input: "\x1b[1;6H\x1b[9@", want: "abcde"},
		{name: "DCH clamps at margin", input: "\x1b[1;5H\x1b[9P", want: "abcd"},
		{name: "ECH clamps at margin", input: "\x1b[1;5H\x1b[9X", want: "abcd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, s := newTestParser(t, 6, 2)
			feed(t, p, "abcdef\x1b[1;3H")
			feed(t, p, tt.input)
			wantRows(t, s, []string{tt.want})
		})
	}
}

// setupGrid5 fills a 4x5 screen with one letter per row.
func setupGrid5(t *testing.T) (*Parser, *Screen) {
	t.Helper()
	p, s := newTestParser(t, 4, 5)
	feed(t, p, "aaaa\r\nbbbb\r\ncccc\r\ndddd\r\neeee")
	return p, s
}

func TestParser_Write_ScrollRegion(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		want           []string
		wantScrollback int
	}{
		{
			name:  "LF at region bottom scrolls region only",
			input: "\x1b[2;4r\x1b[4;1H\n",
			want:  []string{"aaaa", "cccc", "dddd", "", "eeee"},
		},
		{
			name:  "SU within region",
			input: "\x1b[2;4r\x1b[S",
			want:  []string{"aaaa", "cccc", "dddd", "", "eeee"},
		},
		{
			name:  "SD within region",
			input: "\x1b[2;4r\x1b[T",
			want:  []string{"aaaa", "", "bbbb", "cccc", "eeee"},
		},
		{
			name:  "IL inside region pushes lines down",
			input: "\x1b[2;4r\x1b[3;1H\x1b[L",
			want:  []string{"aaaa", "bbbb", "", "cccc", "eeee"},
		},
		{
			name:  "DL inside region pulls lines up",
			input: "\x1b[2;4r\x1b[3;1H\x1b[M",
			want:  []string{"aaaa", "bbbb", "dddd", "", "eeee"},
		},
		{
			name:  "IL outside region ignored",
			input: "\x1b[2;4r\x1b[1;1H\x1b[L",
			want:  []string{"aaaa", "bbbb", "cccc", "dddd", "eeee"},
		},
		{
			name:  "DL outside region ignored",
			input: "\x1b[2;4r\x1b[5;1H\x1b[M",
			want:  []string{"aaaa", "bbbb", "cccc", "dddd", "eeee"},
		},
		{
			name:           "invalid region resets to full screen",
			input:          "\x1b[4;2r\x1b[5;1H\n",
			want:           []string{"bbbb", "cccc", "dddd", "eeee", ""},
			wantScrollback: 1,
		},
		{
			name:           "region reset restores scrollback feeding",
			input:          "\x1b[2;4r\x1b[r\x1b[5;1H\n",
			want:           []string{"bbbb", "cccc", "dddd", "eeee", ""},
			wantScrollback: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, s := setupGrid5(t)
			feed(t, p, tt.input)
			wantRows(t, s, tt.want)
			if got := s.Snapshot().ScrollbackLen; got != tt.wantScrollback {
				t.Errorf("ScrollbackLen = %d, want %d", got, tt.wantScrollback)
			}
		})
	}
}

func TestParser_Write_ScrollRegionHomesCursor(t *testing.T) {
	p, s := newTestParser(t, 10, 5)
	feed(t, p, "\x1b[3;3H\x1b[2;4r")
	wantCursor(t, s, 0, 0)
}

func TestParser_Write_ScrollUpFullScreenSkipsScrollback(t *testing.T) {
	p, s := setupGrid5(t)
	feed(t, p, "\x1b[S")
	wantRows(t, s, []string{"bbbb", "cccc", "dddd", "eeee", ""})
	if got := s.Snapshot().ScrollbackLen; got != 0 {
		t.Errorf("ScrollbackLen after CSI S = %d, want 0", got)
	}
}

func TestParser_Write_SGR(t *testing.T) {
	tests := []struct {
		name string
		seq  string
		want Cell
	}{
		{name: "bold", seq: "\x1b[1m", want: Cell{Bold: true}},
		{name: "underline", seq: "\x1b[4m", want: Cell{Underline: true}},
		{name: "inverse", seq: "\x1b[7m", want: Cell{Inverse: true}},
		{name: "combined attributes", seq: "\x1b[1;4;7m", want: Cell{Bold: true, Underline: true, Inverse: true}},
		{name: "attributes off", seq: "\x1b[1;4;7m\x1b[22;24;27m", want: Cell{}},
		{name: "fg 16", seq: "\x1b[31m", want: Cell{FG: Color{Kind: Color16, Index: 1}}},
		{name: "bg 16", seq: "\x1b[42m", want: Cell{BG: Color{Kind: Color16, Index: 2}}},
		{name: "fg bright", seq: "\x1b[94m", want: Cell{FG: Color{Kind: Color16, Index: 12}}},
		{name: "bg bright", seq: "\x1b[103m", want: Cell{BG: Color{Kind: Color16, Index: 11}}},
		{name: "fg default", seq: "\x1b[31;42m\x1b[39m", want: Cell{BG: Color{Kind: Color16, Index: 2}}},
		{name: "bg default", seq: "\x1b[31;42m\x1b[49m", want: Cell{FG: Color{Kind: Color16, Index: 1}}},
		{name: "fg 256", seq: "\x1b[38;5;196m", want: Cell{FG: Color{Kind: Color256, Index: 196}}},
		{name: "bg 256", seq: "\x1b[48;5;21m", want: Cell{BG: Color{Kind: Color256, Index: 21}}},
		{name: "fg 256 colon form", seq: "\x1b[38:5:99m", want: Cell{FG: Color{Kind: Color256, Index: 99}}},
		{name: "fg 256 index clamped", seq: "\x1b[38;5;300m", want: Cell{FG: Color{Kind: Color256, Index: 255}}},
		{name: "fg rgb", seq: "\x1b[38;2;10;20;30m", want: Cell{FG: Color{Kind: ColorRGB, R: 10, G: 20, B: 30}}},
		{name: "bg rgb", seq: "\x1b[48;2;255;0;127m", want: Cell{BG: Color{Kind: ColorRGB, R: 255, B: 127}}},
		{name: "bare reset", seq: "\x1b[1;31m\x1b[m", want: Cell{}},
		{name: "explicit reset", seq: "\x1b[1;31m\x1b[0m", want: Cell{}},
		{name: "truncated 256 ignored", seq: "\x1b[38;5m", want: Cell{}},
		{name: "truncated rgb ignored", seq: "\x1b[38;2;1;2m", want: Cell{}},
		{name: "unknown codes ignored", seq: "\x1b[31m\x1b[51;99m", want: Cell{FG: Color{Kind: Color16, Index: 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, s := newTestParser(t, 10, 3)
			feed(t, p, tt.seq+"X")
			got := s.Snapshot().Cells[0][0]
			want := tt.want
			want.R = 'X'
			if got != want {
				t.Errorf("cell = %+v, want %+v", got, want)
			}
		})
	}
}

func TestParser_Write_AltScreenRoundTrip(t *testing.T) {
	for _, mode := range []string{"47", "1047", "1049"} {
		t.Run("mode "+mode, func(t *testing.T) {
			p, s := newTestParser(t, 10, 3)
			feed(t, p, "main")
			feed(t, p, "\x1b[?"+mode+"h")
			wantRows(t, s, []string{"", "", ""})
			wantCursor(t, s, 0, 0)
			feed(t, p, "alt")
			wantRows(t, s, []string{"alt"})
			feed(t, p, "\x1b[?"+mode+"l")
			wantRows(t, s, []string{"main"})
			wantCursor(t, s, 4, 0)
		})
	}
}

func TestParser_Write_AltScreenRestoresPen(t *testing.T) {
	p, s := newTestParser(t, 10, 3)
	feed(t, p, "\x1b[31m\x1b[?1049h\x1b[32mx\x1b[?1049lX")
	got := s.Snapshot().Cells[0][0]
	want := Cell{R: 'X', FG: Color{Kind: Color16, Index: 1}}
	if got != want {
		t.Errorf("cell = %+v, want %+v", got, want)
	}
}

func TestParser_Write_AltScreenReentryIsNoop(t *testing.T) {
	p, s := newTestParser(t, 10, 3)
	feed(t, p, "main\x1b[?1049halt\x1b[?1049h")
	wantRows(t, s, []string{"alt"})
	feed(t, p, "\x1b[?1049l")
	wantRows(t, s, []string{"main"})
}

func TestParser_Write_CursorSaveRestore(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
		x, y  int
	}{
		{name: "DECSC DECRC", input: "ab\x1b7cd\x1b8X", want: []string{"abXd"}, x: 3},
		{name: "CSI s u", input: "ab\x1b[scd\x1b[uX", want: []string{"abXd"}, x: 3},
		{name: "restore without save homes", input: "cd\x1b8X", want: []string{"Xd"}, x: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, s := newTestParser(t, 10, 3)
			feed(t, p, tt.input)
			wantRows(t, s, tt.want)
			wantCursor(t, s, tt.x, tt.y)
		})
	}
}

func TestParser_Write_CursorSaveRestoresPen(t *testing.T) {
	p, s := newTestParser(t, 10, 3)
	feed(t, p, "\x1b[31m\x1b7\x1b[0m\x1b8X")
	got := s.Snapshot().Cells[0][0]
	want := Cell{R: 'X', FG: Color{Kind: Color16, Index: 1}}
	if got != want {
		t.Errorf("cell = %+v, want %+v", got, want)
	}
}

func TestParser_Write_CursorVisibility(t *testing.T) {
	p, s := newTestParser(t, 10, 3)
	feed(t, p, "\x1b[?25l")
	if s.Snapshot().CursorVisible {
		t.Error("CursorVisible after ?25l = true, want false")
	}
	feed(t, p, "\x1b[?25h")
	if !s.Snapshot().CursorVisible {
		t.Error("CursorVisible after ?25h = false, want true")
	}
}

func TestParser_Write_BracketedPaste(t *testing.T) {
	p, s := newTestParser(t, 10, 3)
	feed(t, p, "\x1b[?2004h")
	if !s.BracketedPaste() {
		t.Error("BracketedPaste after ?2004h = false, want true")
	}
	feed(t, p, "\x1b[?2004l")
	if s.BracketedPaste() {
		t.Error("BracketedPaste after ?2004l = true, want false")
	}
}

func TestParser_Write_OSCTitle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "OSC 0 BEL terminated", input: "\x1b]0;hello\x07", want: "hello"},
		{name: "OSC 2 ST terminated", input: "\x1b]2;world\x1b\\", want: "world"},
		{name: "empty title", input: "\x1b]0;old\x07\x1b]0;\x07", want: ""},
		{name: "semicolons kept in title", input: "\x1b]0;a;b\x07", want: "a;b"},
		{name: "unknown OSC swallowed", input: "\x1b]0;keep\x07\x1b]52;c;Zm9v\x07", want: "keep"},
		{name: "OSC without payload swallowed", input: "\x1b]0;keep\x07\x1b]2\x07", want: "keep"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, s := newTestParser(t, 10, 3)
			feed(t, p, tt.input)
			if got := s.Snapshot().Title; got != tt.want {
				t.Errorf("Title = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParser_Write_OSCSplitAcrossWrites(t *testing.T) {
	p, s := newTestParser(t, 10, 3)
	feed(t, p, "\x1b]0;he")
	feed(t, p, "llo\x07after")
	if got := s.Snapshot().Title; got != "hello" {
		t.Errorf("Title = %q, want %q", got, "hello")
	}
	wantRows(t, s, []string{"after"})
}

func TestParser_Write_DSRReply(t *testing.T) {
	t.Run("reports 1-based cursor position", func(t *testing.T) {
		s := NewScreen(10, 5)
		var reply bytes.Buffer
		p := NewParser(s, &reply)
		feed(t, p, "\x1b[3;5H\x1b[6n")
		if got := reply.String(); got != "\x1b[3;5R" {
			t.Errorf("DSR reply = %q, want %q", got, "\x1b[3;5R")
		}
	})
	t.Run("non-6 status request ignored", func(t *testing.T) {
		s := NewScreen(10, 5)
		var reply bytes.Buffer
		p := NewParser(s, &reply)
		feed(t, p, "\x1b[5n")
		if got := reply.String(); got != "" {
			t.Errorf("DSR reply = %q, want empty", got)
		}
	})
	t.Run("nil reply writer does not panic", func(t *testing.T) {
		p, _ := newTestParser(t, 10, 5)
		feed(t, p, "\x1b[6n")
	})
}

func TestParser_Write_UTF8SplitAcrossWrites(t *testing.T) {
	t.Run("byte at a time", func(t *testing.T) {
		p, s := newTestParser(t, 20, 3)
		text := "héllo 世界 \U0001f600"
		for _, b := range []byte(text) {
			if _, err := p.Write([]byte{b}); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
		wantRows(t, s, []string{text})
	})
	t.Run("aborted sequence yields replacement char", func(t *testing.T) {
		p, s := newTestParser(t, 20, 3)
		feed(t, p, "\xc3")
		feed(t, p, "A")
		wantRows(t, s, []string{"�A"})
	})
	t.Run("escape aborts pending sequence", func(t *testing.T) {
		p, s := newTestParser(t, 20, 3)
		feed(t, p, "\xe4\xb8")
		feed(t, p, "\x1b[31mA")
		snap := s.Snapshot()
		if snap.Cells[0][0].R != '�' {
			t.Errorf("cell 0 = %q, want replacement char", snap.Cells[0][0].R)
		}
		want := Cell{R: 'A', FG: Color{Kind: Color16, Index: 1}}
		if snap.Cells[0][1] != want {
			t.Errorf("cell 1 = %+v, want %+v", snap.Cells[0][1], want)
		}
	})
	t.Run("stray continuation bytes become replacement chars", func(t *testing.T) {
		p, s := newTestParser(t, 20, 3)
		feed(t, p, "\x80\x80A")
		wantRows(t, s, []string{"��A"})
	})
}

func TestParser_Write_MalformedInputNoPanic(t *testing.T) {
	long := bytes.Repeat([]byte("x"), maxOSCBytes+100)
	tests := []struct {
		name  string
		input string
	}{
		{name: "lone ESC at end", input: "abc\x1b"},
		{name: "lone CSI at end", input: "abc\x1b["},
		{name: "huge parameter", input: "\x1b[999999999999999999999999A"},
		{name: "many empty params", input: "\x1b[;;;;;;H"},
		{name: "too many params", input: "\x1b[1;2;3;4;5;6;7;8;9;10;11;12;13;14;15;16;17;18;19;20;21;22;23;24;25;26;27;28;29;30;31;32;33;34;35m"},
		{name: "private marker without params", input: "\x1b[?h"},
		{name: "unknown private mode", input: "\x1b[?31337h"},
		{name: "secondary DA marker", input: "\x1b[>c"},
		{name: "csi with intermediate", input: "\x1b[1 q"},
		{name: "charset designation", input: "\x1b(B"},
		{name: "unknown escape final", input: "\x1bZ"},
		{name: "C0 inside CSI", input: "\x1b[2\n;3H"},
		{name: "oversized OSC", input: "\x1b]0;" + string(long) + "\x07"},
		{name: "invalid utf8 soup", input: "\xff\xfe\x80\xc0\xc1\xf5\x80\x80"},
		{name: "overlong encoding", input: "\xe0\x80\x80"},
		{name: "nul bytes", input: "a\x00b"},
		{name: "esc inside csi", input: "\x1b[31\x1b[32mX"},
		{name: "csi param overflow", input: "\x1b[" + string(bytes.Repeat([]byte("1;"), 200)) + "H"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, s := newTestParser(t, 10, 4)
			n, err := p.Write([]byte(tt.input))
			if err != nil {
				t.Fatalf("Write error: %v", err)
			}
			if n != len(tt.input) {
				t.Fatalf("Write = %d, want %d", n, len(tt.input))
			}
			_ = s.Snapshot() // must not panic either
		})
	}
}

func TestParser_Write_GarbageStreamsNoPanic(t *testing.T) {
	p, s := newTestParser(t, 20, 6)
	blob := make([]byte, 8192)
	state := uint32(2463534242)
	for i := range blob {
		// xorshift32 keeps the stream deterministic without math/rand.
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		blob[i] = byte(state)
	}
	for start := 0; start < len(blob); start += 257 {
		end := start + 257
		if end > len(blob) {
			end = len(blob)
		}
		if _, err := p.Write(blob[start:end]); err != nil {
			t.Fatalf("Write: %v", err)
		}
		_ = s.Snapshot()
	}
}

func TestParser_Write_MalformedThenRecovers(t *testing.T) {
	p, s := newTestParser(t, 10, 3)
	// The unknown escape final swallows one byte, then output resumes.
	feed(t, p, "\x1b")
	feed(t, p, "QB")
	wantRows(t, s, []string{"B"})
}

func TestParser_Write_C0ExecutesInsideCSI(t *testing.T) {
	p, s := newTestParser(t, 10, 5)
	// The LF fires mid-sequence, then CUP still dispatches with its
	// params intact.
	feed(t, p, "\x1b[2\n;3H")
	wantCursor(t, s, 2, 1)
}

func TestParser_Write_TabStops(t *testing.T) {
	p, s := newTestParser(t, 20, 3)
	feed(t, p, "\tA\tB")
	snap := s.Snapshot()
	if snap.Cells[0][8].R != 'A' {
		t.Errorf("cell 8 = %q, want 'A'", snap.Cells[0][8].R)
	}
	if snap.Cells[0][16].R != 'B' {
		t.Errorf("cell 16 = %q, want 'B'", snap.Cells[0][16].R)
	}
	feed(t, p, "\x1b[1;18H\t")
	wantCursor(t, s, 19, 0)
}

func TestParser_Write_FullReset(t *testing.T) {
	p, s := newTestParser(t, 8, 3)
	feed(t, p, "1\r\n2\r\n3\r\n4")                                 // creates scrollback
	feed(t, p, "\x1b]0;title\x07")                                 // sets title
	feed(t, p, "\x1b[31m\x1b[?25l\x1b[?2004h\x1b[?1049h\x1b[2;3r") // modes, alt, region
	feed(t, p, "\x1bc")
	snap := s.Snapshot()
	wantRows(t, s, []string{"", "", ""})
	if snap.CursorX != 0 || snap.CursorY != 0 {
		t.Errorf("cursor = (%d,%d), want (0,0)", snap.CursorX, snap.CursorY)
	}
	if !snap.CursorVisible {
		t.Error("CursorVisible = false, want true")
	}
	if s.BracketedPaste() {
		t.Error("BracketedPaste = true, want false")
	}
	if snap.ScrollbackLen == 0 {
		t.Error("ScrollbackLen = 0, want scrollback preserved")
	}
	if snap.Title != "title" {
		t.Errorf("Title = %q, want %q", snap.Title, "title")
	}
	// Writes land on the (cleared) main screen with a default pen.
	feed(t, p, "Z")
	got := s.Snapshot().Cells[0][0]
	if got != (Cell{R: 'Z'}) {
		t.Errorf("cell = %+v, want plain 'Z'", got)
	}
}

func TestParser_Write_ReverseIndexScrollsDown(t *testing.T) {
	p, s := newTestParser(t, 4, 3)
	feed(t, p, "aa\r\nbb\r\ncc\x1b[1;1H\x1bM")
	wantRows(t, s, []string{"", "aa", "bb"})
	wantCursor(t, s, 0, 0)
}
