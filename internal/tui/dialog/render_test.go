package dialog

import (
	"image/color"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// testOv is the overlay of the dark theme, which the dialog tests draw with.
var testOv = mustOverlay()

func mustOverlay() style.Overlay {
	th, err := theme.Load("dark")
	if err != nil {
		panic(err)
	}
	return style.NewOverlay(th)
}

// allDialogs returns one of each dialog in its first state, with a clock that
// has already passed the grace period.
func allDialogs() []Overlay {
	c := newFakeClock()
	return []Overlay{
		NewUnsavedChanges(c.Now, "a.txt", nil, testOv),
		NewConfirmTrash(c.Now, "a.txt", nil, testOv),
		NewSaveAs(c.Now, "a.txt", nil, PathChecks{Exists: func(string) bool { return true }}, testOv),
		NewOpenFolder(c.Now, []string{"/a"}, "/a", nil, nil, testOv),
		NewErrorDialog(c.Now, "boom", nil, testOv),
	}
}

// buttonsOf returns the buttons of a dialog in its current state.
func buttonsOf(o Overlay) []button {
	switch d := o.(type) {
	case *UnsavedChanges:
		return d.buttons
	case *ConfirmTrash:
		return d.buttons
	case *SaveAs:
		return d.buttons
	case *OpenFolder:
		return d.buttons
	case *ErrorDialog:
		return d.buttons
	}
	return nil
}

func TestButtonUnderlineIndexMatchesHotkey(t *testing.T) {
	t.Parallel()
	for _, o := range allDialogs() {
		for _, b := range buttonsOf(o) {
			r := []rune(b.label)
			if b.under < 0 || b.under >= len(r) {
				t.Fatalf("%q underline index %d is out of range", b.label, b.under)
			}
			if unicode.ToLower(r[b.under]) != b.key {
				t.Fatalf("%q underlines %q, hotkey is %q", b.label, r[b.under], b.key)
			}
		}
	}
}

func TestOnlyTheHotkeyLetterIsUnderlined(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewUnsavedChanges(c.Now, "a.txt", nil, testOv)
	g := draw(d, 80, 24)
	x, y := mustFind(t, g, "Discard")
	for i, r := range "Discard" {
		cell := g.At(x+i, y)
		if cell.Text != string(r) {
			t.Fatalf("cell %d is %q, want %q", i, cell.Text, string(r))
		}
		if got, want := cell.Style.Underline, i == 0; got != want {
			t.Fatalf("underline of %q = %v, want %v", string(r), got, want)
		}
	}
	// The padding around the label is never underlined.
	if g.At(x-1, y).Style.Underline || g.At(x+7, y).Style.Underline {
		t.Fatal("the button padding is underlined")
	}
}

func TestBoxUsesRoundedPrimaryBorder(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewUnsavedChanges(c.Now, "a.txt", nil, testOv)
	g := draw(d, 80, 24)
	x, y := mustFind(t, g, "╭")
	if fg, ok := g.At(x, y).Style.FG.(color.RGBA); !ok || fg != primary {
		t.Fatalf("top-left corner color = %v, want primary", g.At(x, y).Style.FG)
	}
	right, _ := mustFind(t, g, "╮")
	if right-x < 10 {
		t.Fatalf("box is only %d cells wide", right-x+1)
	}
	mustFind(t, g, "╰")
	mustFind(t, g, "╯")
	// The box is centered to within one cell.
	if off := x + right - 79; off < -1 || off > 1 {
		t.Fatalf("box is not centered: corners at %d and %d in 80 columns", x, right)
	}
}

func TestTitleFillIsTheAccent(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewUnsavedChanges(c.Now, "a.txt", nil, testOv)
	g := draw(d, 80, 24)
	x, y := mustFind(t, g, "╱")
	end := x
	for end < 80 && g.At(end, y).Text == "╱" {
		if fg := g.At(end, y).Style.FG; fg != color.Color(primary) {
			t.Fatalf("title fill cell %d is %v, want the accent %v", end, fg, primary)
		}
		end++
	}
	if end-x < 10 {
		t.Fatalf("title fill is only %d cells", end-x)
	}
}

func TestFocusedButtonIsPrimaryAndTabMovesIt(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewUnsavedChanges(c.Now, "a.txt", nil, testOv)
	// The button is "  Save  ". The body also says "Save changes", so the
	// padding anchors the search on the button.
	px, y := mustFind(t, draw(d, 80, 24), "  Save  ")
	x := px + 2
	if bg := draw(d, 80, 24).At(x, y).Style.BG; bg != color.Color(primary) {
		t.Fatalf("focused Save has background %v, want primary", bg)
	}
	d.Update(key("tab"))
	g := draw(d, 80, 24)
	if bg := g.At(x, y).Style.BG; bg != color.Color(bgLessVis) {
		t.Fatalf("blurred Save has background %v, want the Char blur color", bg)
	}
	dx, dy := mustFind(t, g, "Discard")
	if bg := g.At(dx, dy).Style.BG; bg != color.Color(primary) {
		t.Fatalf("focused Discard has background %v, want primary", bg)
	}
}

// The overlay colors the dialog tests pin, from the dark theme. primary is the
// accent, which the border, the title fill and the focused button use. bgLessVis
// is the unfocused button and fgMostSubtle the hovered one.
var (
	primary      = color.RGBA{0x2F, 0x81, 0xF7, 0xFF}
	bgLessVis    = color.RGBA{0x43, 0x48, 0x4E, 0xFF}
	fgMostSubtle = color.RGBA{0x64, 0x69, 0x6F, 0xFF}
)

func TestHoveredButtonIsOyster(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewUnsavedChanges(c.Now, "a.txt", nil, testOv)
	g := draw(d, 80, 24)
	x, y := mustFind(t, g, "Cancel")
	d.Update(motion(x+1, y))
	g = draw(d, 80, 24)
	if bg := g.At(x, y).Style.BG; bg != color.Color(fgMostSubtle) {
		t.Fatalf("hovered Cancel has background %v, want Oyster", bg)
	}
	d.Update(motion(0, 0))
	g = draw(d, 80, 24)
	if bg := g.At(x, y).Style.BG; bg == color.Color(fgMostSubtle) {
		t.Fatal("Cancel stays hovered after the pointer left")
	}
}

func TestDrawOnlyChangesTheBox(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewErrorDialog(c.Now, "boom", nil, testOv)
	g := grid.New(80, 24)
	for y := range 24 {
		for x := range 80 {
			g.Set(x, y, "x", grid.Style{})
		}
	}
	d.Draw(g)
	for _, p := range [][2]int{{0, 0}, {79, 0}, {0, 23}, {79, 23}} {
		if g.At(p[0], p[1]).Text != "x" {
			t.Fatalf("cell %v outside the box was painted", p)
		}
	}
}

func TestDialogsDoNotPanicOnSmallGrids(t *testing.T) {
	t.Parallel()
	sizes := [][2]int{{0, 0}, {1, 1}, {5, 3}, {20, 6}}
	for _, o := range allDialogs() {
		for _, s := range sizes {
			g := draw(o, s[0], s[1])
			if c, ok := o.(Caret); ok {
				c.Cursor(s[0], s[1])
			}
			_ = screen(g)
		}
	}
}

func TestDrawAfterEachKeyDoesNotPanic(t *testing.T) {
	t.Parallel()
	keys := []string{"tab", "shift+tab", "left", "right", "up", "down", "enter", "backspace", "x", "esc"}
	for _, o := range allDialogs() {
		c := tea.KeyPressMsg{}
		_ = c
		for _, k := range keys {
			next, _ := o.Update(key(k))
			if next == nil {
				break
			}
			o = next
			draw(o, 60, 20)
		}
	}
}

func TestFocusedFieldShowsCaretInsideBox(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	long := ""
	for range 120 {
		long += "p"
	}
	d := NewSaveAs(c.Now, long, nil, PathChecks{}, testOv)
	g := draw(d, 50, 16)
	x, y, ok := d.Cursor(50, 16)
	if !ok {
		t.Fatal("no caret for a long field at the end")
	}
	if x < 0 || x >= 50 || y < 0 || y >= 16 {
		t.Fatalf("caret (%d,%d) is outside the frame", x, y)
	}
	if g.At(x, y).Style.BG != color.Color(bgLessVis) {
		t.Fatal("caret is not on the field row")
	}
}

func TestChoiceString(t *testing.T) {
	t.Parallel()
	want := map[Choice]string{
		ChoiceCancel: "cancel", ChoiceSave: "save", ChoiceDiscard: "discard",
		ChoiceReplace: "replace", ChoiceMoveToTrash: "move-to-trash",
		ChoiceOpen: "open", ChoiceOK: "ok", Choice(99): "unknown",
	}
	for c, s := range want {
		if c.String() != s {
			t.Fatalf("Choice(%d).String() = %q, want %q", int(c), c.String(), s)
		}
	}
}
