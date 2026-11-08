package editor

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// docAt returns an editor holding text with the primary caret at (line, col).
func docAt(text string, line, col int) *Editor {
	e := withDoc(text)
	marksCaret(e, line, col)
	return e
}

// withExtras gives the active document extra carets at the given positions.
func withExtras(e *Editor, ps ...buffer.Pos) {
	for _, p := range ps {
		e.Active().Extra = append(e.Active().Extra, Caret{Cursor: p})
	}
}

func TestPlainMoveCollapsesSelectionToEdge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name  string
		Text  string
		Start buffer.Pos
		steps []Action
		Want  buffer.Pos
		Sel   bool
	}{
		{"left goes to the selection start", "abcdef", caretPos(0, 1), []Action{"select_right", "select_right", "select_right", "move_left"}, caretPos(0, 1), false},
		{"right goes to the selection end", "abcdef", caretPos(0, 1), []Action{"select_right", "select_right", "select_right", "move_right"}, caretPos(0, 4), false},
		{"left on a backward selection goes to its start", "abcdef", caretPos(0, 4), []Action{"select_left", "select_left", "move_left"}, caretPos(0, 2), false},
		{"right on a backward selection goes to its end", "abcdef", caretPos(0, 4), []Action{"select_left", "select_left", "move_right"}, caretPos(0, 4), false},
		{"up starts from the selection start", "abcdef\nabcdef\nabcdef", caretPos(1, 1), []Action{"select_right", "select_right", "select_right", "move_up"}, caretPos(0, 1), false},
		{"down starts from the selection end", "abcdef\nabcdef\nabcdef", caretPos(1, 1), []Action{"select_right", "select_right", "select_right", "move_down"}, caretPos(2, 4), false},
		{"mark mode keeps extending", "abcdef", caretPos(0, 1), []Action{"set_mark", "move_right", "move_right", "move_left"}, caretPos(0, 2), true},
		{"word motions do not collapse", "abc def ghi", caretPos(0, 0), []Action{"select_right", "move_word_right"}, caretPos(0, 4), false},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docAt(tt.Text, tt.Start.Line, tt.Start.Col)
			run(t, e, tt.steps...)
			d := e.Active()
			if d.Cursor != tt.Want || d.HasSelection() != tt.Sel {
				t.Fatalf("caret %+v selection %v, want %+v selection %v", d.Cursor, d.HasSelection(), tt.Want, tt.Sel)
			}
		})
	}
}

func TestVerticalMoveAtDocumentEdges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Start    buffer.Pos
		steps    []Action
		Want     buffer.Pos
		wantAnch *buffer.Pos
	}{
		{"up on the first line goes to the start", caretPos(0, 3), []Action{"move_up"}, caretPos(0, 0), nil},
		{"shift up on the first line selects to the start", caretPos(0, 3), []Action{"select_up"}, caretPos(0, 0), &buffer.Pos{Col: 3}},
		{"down on the last line goes to the end", caretPos(1, 1), []Action{"move_down"}, caretPos(1, 4), nil},
		{"shift down on the last line selects to the end", caretPos(1, 1), []Action{"select_down"}, caretPos(1, 4), &buffer.Pos{Line: 1, Col: 1}},
		{"a plain edge move clears the selection", caretPos(1, 1), []Action{"select_right", "move_down"}, caretPos(1, 4), nil},
		{"up at the very start stays put", caretPos(0, 0), []Action{"move_up"}, caretPos(0, 0), nil},
		{"down in the middle is unchanged", caretPos(0, 3), []Action{"move_down"}, caretPos(1, 3), nil},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docAt("abcd\nefgh", tt.Start.Line, tt.Start.Col)
			run(t, e, tt.steps...)
			d := e.Active()
			if d.Cursor != tt.Want {
				t.Fatalf("caret = %+v, want %+v", d.Cursor, tt.Want)
			}
			if tt.wantAnch == nil {
				if d.HasSelection() {
					t.Fatalf("selection = %+v, want none", d.Sel)
				}
				return
			}
			if d.Sel == nil || d.Sel.Anchor != *tt.wantAnch {
				t.Fatalf("selection = %+v, want anchor %+v", d.Sel, *tt.wantAnch)
			}
		})
	}
}

func TestVerticalMoveKeepsDisplayColumn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name  string
		Text  string
		Start buffer.Pos
		steps []Action
		Want  buffer.Pos
	}{
		{"wide characters count two cells", "你好xy\nabcdef", caretPos(0, 2), []Action{"move_down"}, caretPos(1, 4)},
		{"back up lands after the wide characters", "你好xy\nabcdef", caretPos(1, 4), []Action{"move_up"}, caretPos(0, 2)},
		{"never splits a wide character", "你好xy\nabcdef", caretPos(1, 3), []Action{"move_up"}, caretPos(0, 2)},
		{"a tab counts to the next tab stop", "\tab\nabcdefgh", caretPos(0, 1), []Action{"move_down"}, caretPos(1, 4)},
		{"a tab line below keeps the cells", "abcdefgh\n\tab", caretPos(0, 4), []Action{"move_down"}, caretPos(1, 1)},
		{"combining marks take no cell", "e\u0301x\nabc", caretPos(0, 2), []Action{"move_down"}, caretPos(1, 1)},
		{"the wanted column survives a short line", "abcdef\n你\nabcdef", caretPos(0, 4), []Action{"move_down", "move_down"}, caretPos(2, 4)},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docAt(tt.Text, tt.Start.Line, tt.Start.Col)
			run(t, e, tt.steps...)
			if got := e.Active().Cursor; got != tt.Want {
				t.Fatalf("caret = %+v, want %+v", got, tt.Want)
			}
		})
	}
}

func TestSelectWordNextToAWord(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name string
		Text string
		Col  int
		Want string
	}{
		{"inside a word", "foo bar", 1, "foo"},
		{"just after a word", "foo bar", 3, "foo"},
		{"end of line", "foo bar", 7, "bar"},
		{"between blanks selects nothing", "foo  bar", 4, ""},
		{"empty line", "", 0, ""},
		{"after a word before punctuation", "(foo)", 4, "foo"},
		{"on punctuation after punctuation", "a((b", 2, ""},
		{"a combining mark stays in the word", "cafe\u0301 bar", 1, "cafe\u0301"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docAt(tt.Text, 0, tt.Col)
			run(t, e, "select_word")
			if got := marksSelected(e); got != tt.Want {
				t.Fatalf("selected %q, want %q", got, tt.Want)
			}
		})
	}
}

func TestMoveWordEndStopsAtPunctuation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name  string
		Text  string
		Start buffer.Pos
		Want  buffer.Pos
	}{
		{"word before punctuation", "foo(bar)", caretPos(0, 0), caretPos(0, 3)},
		{"punctuation run stops", "foo((bar)", caretPos(0, 3), caretPos(0, 5)},
		{"lone punctuation", "a.b", caretPos(0, 1), caretPos(0, 2)},
		{"from mid word", "foo(bar)", caretPos(0, 5), caretPos(0, 7)},
		{"blanks then a word", "a  bc d", caretPos(0, 1), caretPos(0, 5)},
		{"blanks then punctuation", "a  (b", caretPos(0, 1), caretPos(0, 4)},
		{"across a line break", "foo\n  bar", caretPos(0, 3), caretPos(1, 5)},
		{"end of document stays", "foo", caretPos(0, 3), caretPos(0, 3)},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docAt(tt.Text, tt.Start.Line, tt.Start.Col)
			run(t, e, "move_word_end")
			if got := e.Active().Cursor; got != tt.Want {
				t.Fatalf("caret = %+v, want %+v", got, tt.Want)
			}
		})
	}
}

func TestEscapeEndsMarkModeAndSelection(t *testing.T) {
	t.Parallel()
	e := docAt("abcdef", 0, 1)
	run(t, e, "set_mark", "move_right", "move_right")
	withExtras(e, caretPos(0, 5))
	run(t, e, "remove_secondary_cursors")
	d := e.Active()
	if d.Sel != nil || len(d.Cursors()) != 0 || e.marking(d) {
		t.Fatalf("sel %+v, extras %v, marking %v: want all cleared", d.Sel, d.Cursors(), e.marking(d))
	}
	if d.Cursor != caretPos(0, 3) {
		t.Fatalf("caret = %+v, want it kept at col 3", d.Cursor)
	}
}

func TestGotoMatchingBracketEnclosingAndAngle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Text     string
		lang     string
		Col      int
		wantCol  int
		wantFail bool
	}{
		{"inside a pair goes to the closer", "(ab)", "", 2, 3, false},
		{"nested kinds pick the innermost", "f([a b])", "", 5, 6, false},
		{"after a nested closer takes the enclosing pair", "((a) b)", "", 5, 6, false},
		{"outside every pair fails", "ab (c)", "", 1, 0, true},
		{"angle brackets are not brackets by default", "<a>", "", 0, 0, true},
		{"angle brackets pair in html", "<a>", "HTML", 0, 2, false},
		{"a comparison is skipped outside html", "if (a < b) {", "", 6, 9, false},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docAt(tt.Text, 0, tt.Col)
			e.SetLanguage(tt.lang)
			r := e.Do("goto_matching_bracket", nil)
			if (r.Err != nil) != tt.wantFail {
				t.Fatalf("err = %v, want failure %v", r.Err, tt.wantFail)
			}
			if !tt.wantFail && e.Active().Cursor.Col != tt.wantCol {
				t.Fatalf("caret col = %d, want %d", e.Active().Cursor.Col, tt.wantCol)
			}
		})
	}
}

func TestGraphemeClusterMovementAndDeletion(t *testing.T) {
	t.Parallel()
	const (
		eAcute = "e\u0301"
		family = "\U0001F468\u200d\U0001F469\u200d\U0001F467"
		thai   = "ก\u0e31"
		flags  = "\U0001F1E9\U0001F1EA\U0001F1EB\U0001F1F7"
	)
	tests := []struct {
		Name     string
		Text     string
		Start    int
		action   Action
		wantCol  int
		wantText string
	}{
		{"right steps over a combining mark", eAcute + "x", 0, "move_right", 2, eAcute + "x"},
		{"left steps over a combining mark", eAcute + "x", 2, "move_left", 0, eAcute + "x"},
		{"backspace removes the whole cluster", eAcute + "x", 2, "delete_backward", 0, "x"},
		{"delete removes the whole cluster", eAcute + "x", 0, "delete_forward", 0, "x"},
		{"right steps over a zwj family", family + "x", 0, "move_right", 5, family + "x"},
		{"backspace removes a zwj family", family + "x", 5, "delete_backward", 0, "x"},
		{"thai cluster is one step", thai + "x", 0, "move_right", 2, thai + "x"},
		{"thai cluster deletes as one", thai + "x", 2, "delete_backward", 0, "x"},
		{"flags pair by two", flags, 0, "move_right", 2, flags},
		{"plain text is unchanged", "abc", 1, "move_right", 2, "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docAt(tt.Text, 0, tt.Start)
			run(t, e, tt.action)
			if got := e.Active().Cursor.Col; got != tt.wantCol {
				t.Fatalf("caret col = %d, want %d", got, tt.wantCol)
			}
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestGraphemeDeletionAtEveryCaret(t *testing.T) {
	t.Parallel()
	e := docAt("e\u0301a\ne\u0301b", 0, 2)
	withExtras(e, caretPos(1, 2))
	run(t, e, "delete_backward")
	if got := text(e); got != "a\nb" {
		t.Fatalf("text = %q, want both clusters removed", got)
	}
}

func TestMotionsRunAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name  string
		Text  string
		Start buffer.Pos
		Extra buffer.Pos
		steps []Action
		Want  carets
	}{
		{
			Name: "right moves both", Text: "abcd\nefgh", Start: caretPos(0, 1), Extra: caretPos(1, 1),
			steps: []Action{"move_right"},
			Want:  carets{Primary: Caret{Cursor: caretPos(0, 2)}, extras: []Caret{{Cursor: caretPos(1, 2)}}},
		},
		{
			Name: "select_right extends both", Text: "abcd\nefgh", Start: caretPos(0, 1), Extra: caretPos(1, 1),
			steps: []Action{"select_right"},
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 2), Sel: caretSel(caretPos(0, 1), caretPos(0, 2))},
				extras:  []Caret{{Cursor: caretPos(1, 2), Sel: caretSel(caretPos(1, 1), caretPos(1, 2))}},
			},
		},
		{
			Name: "each caret keeps its own wanted column", Text: "abcd\nx\nabcdef\ny", Start: caretPos(0, 3), Extra: caretPos(2, 5),
			steps: []Action{"move_down", "move_up"},
			Want:  carets{Primary: Caret{Cursor: caretPos(0, 3)}, extras: []Caret{{Cursor: caretPos(2, 5)}}},
		},
		{
			Name: "carets that meet merge", Text: "abcd", Start: caretPos(0, 1), Extra: caretPos(0, 2),
			steps: []Action{"move_line_start"},
			Want:  carets{Primary: Caret{Cursor: caretPos(0, 0)}},
		},
		{
			Name: "end of line moves both", Text: "ab\ncdef", Start: caretPos(0, 0), Extra: caretPos(1, 0),
			steps: []Action{"move_line_end"},
			Want:  carets{Primary: Caret{Cursor: caretPos(0, 2)}, extras: []Caret{{Cursor: caretPos(1, 4)}}},
		},
		{
			Name: "select_word selects at both", Text: "ab cd\nef gh", Start: caretPos(0, 1), Extra: caretPos(1, 4),
			steps: []Action{"select_word"},
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 2), Sel: caretSel(caretPos(0, 0), caretPos(0, 2))},
				extras:  []Caret{{Cursor: caretPos(1, 5), Sel: caretSel(caretPos(1, 3), caretPos(1, 5))}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docAt(tt.Text, tt.Start.Line, tt.Start.Col)
			withExtras(e, tt.Extra)
			run(t, e, tt.steps...)
			if got := caretState(e); !sameCarets(got, tt.Want) {
				t.Fatalf("carets = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestPageMoveShiftsEveryCaret(t *testing.T) {
	t.Parallel()
	text := ""
	for range 30 {
		text += "line\n"
	}
	e := docAt(text, 2, 1)
	withExtras(e, caretPos(5, 2))
	before := e.Active().Cursor.Line
	run(t, e, "move_page_down")
	d := e.Active()
	delta := d.Cursor.Line - before
	if delta <= 0 {
		t.Fatalf("primary caret did not move down: %+v", d.Cursor)
	}
	cs := d.Cursors()
	if len(cs) != 1 || cs[0].Cursor != caretPos(5+delta, 2) {
		t.Fatalf("extra carets = %+v, want one at line %d col 2", cs, 5+delta)
	}
}

func TestSetCursorSnapsToClusterStart(t *testing.T) {
	t.Parallel()
	e := docAt("ae\u0301b", 0, 0)
	for _, tt := range []struct{ Col, Want int }{{0, 0}, {1, 1}, {2, 1}, {3, 3}, {4, 4}} {
		e.SetCursor(caretPos(0, tt.Col), false)
		if got := e.Active().Cursor.Col; got != tt.Want {
			t.Errorf("SetCursor(col %d) -> col %d, want %d", tt.Col, got, tt.Want)
		}
	}
}

func TestSetCursorExtendSelectsAcrossAPartialCluster(t *testing.T) {
	t.Parallel()
	// "cafe" with a combining acute: the cluster e+U+0301 spans columns 3 to 5.
	const text = "cafe\u0301 x"
	tests := []struct {
		Name         string
		From, To     buffer.Pos
		wantAnchor   buffer.Pos
		wantHead     buffer.Pos
		wantSelected string
	}{
		{"end inside the cluster extends to its end", caretPos(0, 3), caretPos(0, 4), caretPos(0, 3), caretPos(0, 5), "e\u0301"},
		{"end on a boundary is kept", caretPos(0, 3), caretPos(0, 5), caretPos(0, 3), caretPos(0, 5), "e\u0301"},
		{"backward end inside the cluster snaps to its start", caretPos(0, 6), caretPos(0, 4), caretPos(0, 6), caretPos(0, 3), "e\u0301 "},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docAt(text, 0, 0)
			e.SetCursor(tt.From, false)
			e.SetCursor(tt.To, true)
			d := e.Active()
			if !d.HasSelection() {
				t.Fatalf("no selection, cursor %+v sel %+v", d.Cursor, d.Sel)
			}
			if d.Sel.Anchor != tt.wantAnchor || d.Cursor != tt.wantHead {
				t.Fatalf("anchor %+v head %+v, want %+v and %+v", d.Sel.Anchor, d.Cursor, tt.wantAnchor, tt.wantHead)
			}
			if got := marksSelected(e); got != tt.wantSelected {
				t.Fatalf("selected %q, want %q", got, tt.wantSelected)
			}
		})
	}
}
