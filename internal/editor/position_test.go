package editor

import "testing"

func TestPosition_String(t *testing.T) {
	cases := []struct {
		pos  Position
		want string
	}{
		{Position{0, 0}, "1:1"},
		{Position{4, 9}, "5:10"},
		{Position{99, 0}, "100:1"},
	}
	for _, tc := range cases {
		if got := tc.pos.String(); got != tc.want {
			t.Errorf("Position{%d,%d}.String() = %q, want %q",
				tc.pos.Line, tc.pos.Column, got, tc.want)
		}
	}
}

func TestPosition_LessThan(t *testing.T) {
	cases := []struct {
		a, b Position
		want bool
	}{
		{Position{0, 0}, Position{0, 1}, true},
		{Position{0, 0}, Position{1, 0}, true},
		{Position{1, 5}, Position{2, 0}, true},
		{Position{0, 5}, Position{0, 5}, false},
		{Position{1, 0}, Position{0, 99}, false},
		{Position{2, 3}, Position{2, 2}, false},
	}
	for _, tc := range cases {
		if got := tc.a.LessThan(tc.b); got != tc.want {
			t.Errorf("%v.LessThan(%v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestPosition_Equal(t *testing.T) {
	if !(Position{1, 2}).Equal(Position{1, 2}) {
		t.Error("identical positions reported unequal")
	}
	if (Position{1, 2}).Equal(Position{1, 3}) {
		t.Error("differing positions reported equal")
	}
}

func TestRange_Empty(t *testing.T) {
	r := Range{Start: Position{1, 2}, End: Position{1, 2}}
	if !r.Empty() {
		t.Error("Range with Start == End not Empty()")
	}
	r2 := Range{Start: Position{1, 2}, End: Position{1, 3}}
	if r2.Empty() {
		t.Error("Range with Start != End reports Empty()")
	}
}

func TestRange_Normalized_AlreadyOrdered(t *testing.T) {
	r := Range{Start: Position{0, 0}, End: Position{1, 3}}
	if got := r.Normalized(); got != r {
		t.Errorf("Normalized() = %v, want %v", got, r)
	}
}

func TestRange_Normalized_Swaps(t *testing.T) {
	r := Range{Start: Position{1, 3}, End: Position{0, 0}}
	want := Range{Start: Position{0, 0}, End: Position{1, 3}}
	if got := r.Normalized(); got != want {
		t.Errorf("Normalized() = %v, want %v", got, want)
	}
}

func TestRange_Normalized_PreservesEmpty(t *testing.T) {
	r := Range{Start: Position{2, 2}, End: Position{2, 2}}
	if got := r.Normalized(); got != r {
		t.Errorf("Normalized() of empty range = %v, want %v", got, r)
	}
}
