package prompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// TestConfirmCardIsCentred checks the card sits in the middle of the frame, fully
// inside it, with the title on its top border.
func TestConfirmCardIsCentred(t *testing.T) {
	t.Parallel()
	c := NewConfirmWith(testStyles(), "Delete a.txt?", nil, ConfirmOptions{Title: "Delete"})
	g := grid.New(80, 24)
	c.Draw(g)
	// The box is centred on both axes: at least one cell of margin on every side.
	for _, r := range c.hits {
		if r.X <= 0 || r.Y <= 0 || r.X+r.W >= g.Width() || r.Y >= g.Height() {
			t.Fatalf("button hit %+v is not inside the frame", r)
		}
	}
	row := func(y int) string { return g.Plain()[y] }
	var top, bottom = -1, -1
	for y := range g.Height() {
		if strings.Contains(row(y), "Delete a.txt?") {
			if top == -1 {
				top = y
			}
			bottom = y
		}
	}
	if top == -1 {
		t.Fatal("the card's question is not drawn")
	}
	if top < g.Height()/2-6 || bottom > g.Height()/2+6 {
		t.Fatalf("the card rows %d..%d are not centred in 24 rows", top, bottom)
	}
	if !strings.Contains(row(top-1), "Delete") {
		t.Fatalf("the row above the question %q has no title", row(top-1))
	}
}

// TestChoiceWrapsButtonsOnANarrowFrame checks the buttons flow onto several centred
// rows when they do not fit the content width, and every button stays clickable.
func TestChoiceWrapsButtonsOnANarrowFrame(t *testing.T) {
	t.Parallel()
	c := NewChoice(testStyles(), "Conflict", "Paste conflict?", "tag", choiceOptions())
	// Width 46: the card's content is 42 cells, too narrow for one row of five buttons.
	c.Draw(grid.New(46, 12))
	if len(c.hits) != len(choiceOptions()) {
		t.Fatalf("hits = %d, want one per option", len(c.hits))
	}
	rows := map[int]bool{}
	for i, r := range c.hits {
		if r.W == 0 {
			t.Fatalf("button %d has no hit rectangle", i)
		}
		rows[r.Y] = true
	}
	if len(rows) < 2 {
		t.Fatalf("the buttons did not wrap: rows %v", rows)
	}
	// Every wrapped button answers a click.
	for i, r := range c.hits {
		c2 := NewChoice(testStyles(), "Conflict", "Paste conflict?", "tag", choiceOptions())
		c2.hits = c.hits
		_, cmd := c2.Update(tea.MouseClickMsg{X: r.X + r.W/2, Y: r.Y, Button: tea.MouseLeft})
		d, ok := emitted(cmd)
		if !ok || d.Choice != i {
			t.Fatalf("click on wrapped button %d: %+v %v", i, d, ok)
		}
	}
}

// TestLineCursorLiesInsideTheField checks Cursor returns the caret inside the field
// and follows the text as it is typed.
func TestLineCursorLiesInsideTheField(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "Name:"})
	lay := l.layout(80, 24)
	x, y, ok := l.Cursor(80, 24)
	if !ok || y != lay.fieldY || x != lay.box.X+2 {
		t.Fatalf("empty cursor = (%d,%d,%v), want the field's first cell (%d,%d)", x, y, ok, lay.box.X+2, lay.fieldY)
	}
	typeText(l, "abc")
	x, y, ok = l.Cursor(80, 24)
	if !ok || y != lay.fieldY || x != lay.box.X+2+3 {
		t.Fatalf("cursor after typing = (%d,%d,%v)", x, y, ok)
	}
	if x < lay.box.X+2 || x >= lay.box.X+2+lay.cw {
		t.Fatalf("cursor x %d is outside the field %d..%d", x, lay.box.X+2, lay.box.X+2+lay.cw)
	}
}

// TestLineSuggestionsStayInsideTheCard checks the list grows the card downward under
// the field, keeps the top row fixed, and stays inside the box.
func TestLineSuggestionsStayInsideTheCard(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "Name:"})
	before := l.layout(80, 24)
	l.SetSuggestions([]Suggestion{{Text: "one"}, {Text: "two"}, {Text: "three"}, {Text: "four"}, {Text: "five"}, {Text: "six"}}, 0)
	lay := l.layout(80, 24)
	if lay.box.Y != before.box.Y {
		t.Fatalf("the card moved: top %d, was %d", lay.box.Y, before.box.Y)
	}
	if lay.suggY <= lay.fieldY || lay.suggN != maxSuggestionRows {
		t.Fatalf("suggestions at y %d x %d rows, want %d rows under the field %d", lay.suggY, lay.suggN, maxSuggestionRows, lay.fieldY)
	}
	if lay.suggY+lay.suggN != lay.box.Y+lay.box.H-1 {
		t.Fatalf("suggestion rows %d..%d are not the card's last body rows (box %+v)", lay.suggY, lay.suggY+lay.suggN-1, lay.box)
	}
	rows := draw(l, 80, 24)
	for _, want := range []string{"one", "two", "three", "four"} {
		if !strings.Contains(rows[lay.suggY], want) && !strings.Contains(strings.Join(rows[lay.suggY:lay.suggY+lay.suggN], "\n"), want) {
			t.Fatalf("the card's suggestion rows miss %q", want)
		}
	}
	// A click on a suggestion row fills it, like the keys do.
	l.Draw(grid.New(80, 24))
	l.Update(tea.MouseClickMsg{X: l.suggestionX, Y: lay.suggY + 2, Button: tea.MouseLeft})
	if l.Value() != "three" {
		t.Fatalf("clicked suggestion filled %q, want three", l.Value())
	}
}

// TestTinyFramesDoNotPanic checks a card draws into frames too small for it.
func TestTinyFramesDoNotPanic(t *testing.T) {
	t.Parallel()
	for _, size := range [][2]int{{0, 0}, {10, 1}, {8, 3}, {5, 5}} {
		g := grid.New(size[0], size[1])
		NewLine(testStyles(), LineOptions{Label: "L:", Input: "x"}).Draw(g)
		NewConfirmWith(testStyles(), "Q?", nil, ConfirmOptions{}).Draw(g)
		NewChoice(testStyles(), "T", "Q?", nil, choiceOptions()).Draw(g)
	}
}

// TestFallbackTitles checks an opener that gives no title gets the generic one.
func TestFallbackTitles(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{})
	if l.title != "Input" {
		t.Fatalf("line title = %q, want Input", l.title)
	}
	c := NewConfirmWith(testStyles(), "Q?", nil, ConfirmOptions{})
	if c.title != "Confirm" {
		t.Fatalf("confirm title = %q, want Confirm", c.title)
	}
	ch := NewChoice(testStyles(), "", "Q?", nil, choiceOptions())
	if ch.title != "Choose" {
		t.Fatalf("choice title = %q, want Choose", ch.title)
	}
}
