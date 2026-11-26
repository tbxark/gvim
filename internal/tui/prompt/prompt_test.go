package prompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

func testStyles() style.Styles { return style.New(theme.Theme{}) }

// key returns a key press for a keystroke name ("enter", "esc", "left", ...) or
// for one character typed as text.
func key(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	r := []rune(name)[0]
	return tea.KeyPressMsg{Code: r, Text: name}
}

// typeText sends each character of s as a key press, and returns the command of
// the last one.
func typeText(l *Line, s string) tea.Cmd {
	var cmd tea.Cmd
	for _, r := range s {
		_, cmd = l.Update(key(string(r)))
	}
	return cmd
}

// emitted runs cmd and returns the message it produced as a Done, or false.
func emitted(cmd tea.Cmd) (Done, bool) {
	if cmd == nil {
		return Done{}, false
	}
	d, ok := cmd().(Done)
	return d, ok
}

func TestLineSubmitsInputWithTag(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "Go to line:", Input: "1", Tag: "goto"})
	typeText(l, "2:5")
	_, cmd := l.Update(key("enter"))

	d, ok := emitted(cmd)
	if !ok {
		t.Fatal("Enter did not emit Done")
	}
	if d.Tag != "goto" || d.Outcome != Submitted || d.Text != "12:5" {
		t.Fatalf("Done = %+v, want tag goto, Submitted, text 12:5", d)
	}
	if l.Active() {
		t.Fatal("prompt still active after submit")
	}
}

func TestLineStartsWithInputAndCaretAtEnd(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "Save as:", Input: "/tmp/a.txt"})
	if got := l.Value(); got != "/tmp/a.txt" {
		t.Fatalf("Value = %q", got)
	}
	lay := l.layout(40, 5)
	x, y, ok := l.Cursor(40, 5)
	if !ok || y != lay.fieldY || x != lay.box.X+2+len("/tmp/a.txt") {
		t.Fatalf("Cursor = (%d, %d, %v)", x, y, ok)
	}
}

func TestLineValidationKeepsPromptOpen(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "Go to line:", Validate: ValidateLineCol})
	typeText(l, "x")
	_, cmd := l.Update(key("enter"))
	if _, ok := emitted(cmd); ok {
		t.Fatal("rejected input emitted Done")
	}
	if !l.Active() {
		t.Fatal("rejected input closed the prompt")
	}
	// The rejection sits on its own row under the field.
	lay := l.layout(40, 8)
	if lay.errY <= lay.fieldY {
		t.Fatalf("the rejection row %d is not under the field row %d", lay.errY, lay.fieldY)
	}
	rows := draw(l, 40, 8)
	if !strings.Contains(rows[lay.errY], "Not a line number: x") {
		t.Fatalf("rejection row %q does not show the rejection", rows[lay.errY])
	}

	// Editing clears the rejection, and a valid line then submits.
	l.Update(key("backspace"))
	typeText(l, "7")
	if strings.Contains(strings.Join(draw(l, 40, 8), "\n"), "Not a line") {
		t.Fatal("rejection still shown after the input changed")
	}
	_, cmd = l.Update(key("enter"))
	if d, ok := emitted(cmd); !ok || d.Text != "7" {
		t.Fatalf("Done = %+v, %v; want text 7", d, ok)
	}
}

func TestLineEscCloses(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "Rename to:"})
	typeText(l, "abc")
	_, cmd := l.Update(key("esc"))
	if l.Active() {
		t.Fatal("Esc left the prompt open")
	}
	if _, ok := emitted(cmd); ok {
		t.Fatal("Esc emitted Done")
	}
}

func TestLineEditsAtTheCaret(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "New file:", Input: "ac"})
	l.Update(key("left"))
	typeText(l, "b")
	if got := l.Value(); got != "abc" {
		t.Fatalf("Value = %q, want %q", got, "abc")
	}
}

func TestLineDrawShowsLabelAndInput(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "New folder:", Input: "src"})
	lay := l.layout(30, 6)
	rows := draw(l, 30, 6)
	if !strings.Contains(rows[lay.labelY], "New folder:") {
		t.Fatalf("label row %q has no label", rows[lay.labelY])
	}
	if !strings.Contains(rows[lay.fieldY], "src") {
		t.Fatalf("field row %q has no input", rows[lay.fieldY])
	}
}

func TestValidateLineCol(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"1", "42", "3:9", " 5 : 2 "} {
		if err := ValidateLineCol(s); err != nil {
			t.Errorf("ValidateLineCol(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range []string{"", "0", "-1", "a", "1:b", "1:2:3"} {
		if err := ValidateLineCol(s); err == nil {
			t.Errorf("ValidateLineCol(%q) = nil, want an error", s)
		}
	}
}

func TestConfirmDefaultsToNoAndEnterTakesIt(t *testing.T) {
	t.Parallel()
	c := NewConfirmWith(testStyles(), "Delete a.txt? (y/n)", 7, ConfirmOptions{})
	_, cmd := c.Update(key("enter"))
	d, ok := emitted(cmd)
	if !ok || d.Outcome != No || d.Tag != 7 {
		t.Fatalf("Done = %+v, %v; want No with tag 7", d, ok)
	}
	if c.Active() {
		t.Fatal("confirm still active after an answer")
	}
}

func TestConfirmArrowsMoveFocusToYes(t *testing.T) {
	t.Parallel()
	c := NewConfirmWith(testStyles(), "Overwrite? (y/n)", "save", ConfirmOptions{})
	c.Update(key("left"))
	_, cmd := c.Update(key("enter"))
	d, ok := emitted(cmd)
	if !ok || d.Outcome != Yes {
		t.Fatalf("Done = %+v, %v; want Yes", d, ok)
	}
}

func TestConfirmLetterKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		text string
		want Outcome
	}{
		{"y", Yes},
		{"Y", Yes},
		{"n", No},
		{"N", No},
	}
	for _, tt := range tests {
		c := NewConfirmWith(testStyles(), "Q?", nil, ConfirmOptions{})
		_, cmd := c.Update(key(tt.text))
		d, ok := emitted(cmd)
		if !ok || d.Outcome != tt.want {
			t.Errorf("key %q: Done = %+v, %v; want %v", tt.text, d, ok, tt.want)
		}
	}
}

func TestConfirmIgnoresOtherKeys(t *testing.T) {
	t.Parallel()
	c := NewConfirmWith(testStyles(), "Q?", nil, ConfirmOptions{})
	_, cmd := c.Update(key("k"))
	if _, ok := emitted(cmd); ok || !c.Active() {
		t.Fatal("an unrelated key answered or closed the confirm")
	}
}

func TestConfirmEscCloses(t *testing.T) {
	t.Parallel()
	c := NewConfirmWith(testStyles(), "Q?", nil, ConfirmOptions{})
	_, cmd := c.Update(key("esc"))
	if c.Active() {
		t.Fatal("Esc left the confirm open")
	}
	if _, ok := emitted(cmd); ok {
		t.Fatal("Esc emitted Done")
	}
}

func TestConfirmDrawShowsBothAnswers(t *testing.T) {
	t.Parallel()
	c := NewConfirmWith(testStyles(), "Delete a.txt? (y/n)", nil, ConfirmOptions{})
	rows := draw(c, 50, 8)
	text := strings.Join(rows, "\n")
	for _, want := range []string{"Delete a.txt? (y/n)", "  Yes  ", "  No  "} {
		if !strings.Contains(text, want) {
			t.Fatalf("card = %q, missing %q", text, want)
		}
	}
	if _, _, ok := c.Cursor(50, 8); ok {
		t.Fatal("confirm reported a text caret")
	}
}

func TestEmptyFrameDrawsNothing(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "x:"})
	g := grid.New(10, 0)
	l.Draw(g)
	if _, _, ok := l.Cursor(10, 0); ok {
		t.Fatal("caret in a frame with no rows")
	}
	NewConfirmWith(testStyles(), "Q?", nil, ConfirmOptions{}).Draw(g)
}

// drawer is a prompt that can paint itself into a grid.
type drawer interface {
	Draw(g *grid.Grid)
}

// draw paints d into a w by h grid and returns its plain rows.
func draw(d drawer, w, h int) []string {
	g := grid.New(w, h)
	d.Draw(g)
	return g.Plain()
}

func TestLineSelectAllFirstTypedKeyReplacesPrefill(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "Rename to:", Input: "old.txt", SelectAll: true})
	typeText(l, "new.txt")
	if got := l.Value(); got != "new.txt" {
		t.Fatalf("Value = %q, want new.txt", got)
	}
}

func TestLineSelectAllKeepsPrefillAfterCaretMove(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "Rename to:", Input: "ab", SelectAll: true})
	l.Update(key("left"))
	typeText(l, "x")
	if got := l.Value(); got != "axb" {
		t.Fatalf("Value = %q, want axb", got)
	}
}

func TestLineWithoutSelectAllAppendsToPrefill(t *testing.T) {
	t.Parallel()
	l := NewLine(testStyles(), LineOptions{Label: "Go to line:", Input: "1"})
	typeText(l, "2")
	if got := l.Value(); got != "12" {
		t.Fatalf("Value = %q, want 12", got)
	}
}
