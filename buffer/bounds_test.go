package buffer

import "testing"

func TestClassOfRunes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		r    rune
		ok   bool
		want Class
	}{
		{'a', true, ClassWord},
		{'7', true, ClassWord},
		{'_', true, ClassWord},
		{'é', true, ClassWord},
		{'.', true, ClassPunct},
		{' ', true, ClassSpace},
		{'x', false, ClassSpace},
	}
	for _, tt := range tests {
		if got := classOf(tt.r, tt.ok); got != tt.want {
			t.Fatalf("classOf(%q, %v) = %v, want %v", tt.r, tt.ok, got, tt.want)
		}
	}
}

func TestClassAtEndOfLineIsSpace(t *testing.T) {
	t.Parallel()
	b := New("ab\ncd")
	if got := b.ClassAt(Pos{Line: 0, Col: 2}); got != ClassSpace {
		t.Fatalf("ClassAt end of line = %v, want ClassSpace", got)
	}
	if got := b.ClassAt(Pos{Line: 1, Col: 0}); got != ClassWord {
		t.Fatalf("ClassAt(1,0) = %v, want ClassWord", got)
	}
}

func TestWordBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		text      string
		at        Pos
		wantStart Pos
		wantEnd   Pos
	}{
		{"word start", "quick brown", Pos{0, 6}, Pos{0, 6}, Pos{0, 11}},
		{"word middle", "quick brown", Pos{0, 8}, Pos{0, 6}, Pos{0, 11}},
		{"word first char", "quick brown", Pos{0, 0}, Pos{0, 0}, Pos{0, 5}},
		{"word last char", "quick brown", Pos{0, 4}, Pos{0, 0}, Pos{0, 5}},
		{"space run", "a   b", Pos{0, 2}, Pos{0, 1}, Pos{0, 4}},
		{"punct run", "x==y", Pos{0, 2}, Pos{0, 1}, Pos{0, 3}},
		{"past line end uses char before", "one two", Pos{0, 7}, Pos{0, 4}, Pos{0, 7}},
		{"clamped past line end", "one", Pos{0, 99}, Pos{0, 0}, Pos{0, 3}},
		{"empty line", "a\n\nb", Pos{1, 0}, Pos{1, 0}, Pos{1, 0}},
		{"out of range line", "one\ntwo", Pos{5, 1}, Pos{1, 0}, Pos{1, 3}},
		{"unicode word", "héllo wörld", Pos{0, 1}, Pos{0, 0}, Pos{0, 5}},
		{"stops at line break", "ab\ncd", Pos{0, 1}, Pos{0, 0}, Pos{0, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := New(tt.text)
			start, end := WordBounds(b, tt.at)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Fatalf("WordBounds(%+v) = %+v, %+v; want %+v, %+v",
					tt.at, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestLineBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		text      string
		line      int
		wantStart Pos
		wantEnd   Pos
	}{
		{"first line includes newline", "one\ntwo\nthree", 0, Pos{0, 0}, Pos{1, 0}},
		{"middle line", "one\ntwo\nthree", 1, Pos{1, 0}, Pos{2, 0}},
		{"last line ends at buffer end", "one\ntwo\nthree", 2, Pos{2, 0}, Pos{2, 5}},
		{"single line", "solo", 0, Pos{0, 0}, Pos{0, 4}},
		{"empty buffer", "", 0, Pos{0, 0}, Pos{0, 0}},
		{"trailing newline last line is empty", "a\n", 1, Pos{1, 0}, Pos{1, 0}},
		{"negative line clamps to first", "a\nb", -3, Pos{0, 0}, Pos{1, 0}},
		{"large line clamps to last", "a\nb", 40, Pos{1, 0}, Pos{1, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := New(tt.text)
			start, end := LineBounds(b, tt.line)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Fatalf("LineBounds(%d) = %+v, %+v; want %+v, %+v",
					tt.line, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}
