package menubar

import (
	"maps"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/ui/grid"
)

// underlined returns the cells of g whose underline is set, keyed by position.
func underlined(g *grid.Grid) map[[2]int]string {
	out := map[[2]int]string{}
	for y := range g.Height() {
		for x := range g.Width() {
			if c := g.At(x, y); c.Style.Underline {
				out[[2]int{x, y}] = c.Text
			}
		}
	}
	return out
}

// paint returns a grid of m's size with m painted on it.
func paint(m Model) *grid.Grid {
	g := grid.New(m.w, m.h)
	m.Paint(g)
	return g
}

// rowAccels maps each selectable row label of the dropdown title to its letter.
func rowAccels(t *testing.T, title string) map[string]rune {
	t.Helper()
	out := map[string]rune{}
	for _, r := range dropdownRows(title, func(string) bool { return true }) {
		if r.Separator {
			continue
		}
		out[r.Label] = r.Accel()
	}
	return out
}

func TestTitleUnderlineIsOnlyTheAcceleratorCell(t *testing.T) {
	got := underlined(paint(newBar(allOn(), 60, 10)))
	want := map[[2]int]string{
		{1, 0}: "F", {8, 0}: "E", {15, 0}: "V", {22, 0}: "S",
		{34, 0}: "G", {39, 0}: "L", {45, 0}: "H",
	}
	if !maps.Equal(got, want) {
		t.Fatalf("underlined cells = %v, want %v", got, want)
	}
}

func TestOpenDropdownUnderlinesTitleAndRowAccelerators(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	got := underlined(paint(m))
	// Every title keeps its letter underlined. The open File title and the rows
	// below it add theirs. A letter is not always the label's first column:
	// "Save As..." takes the A at column 1 of its label.
	want := map[[2]int]string{
		{1, 0}: "F", {8, 0}: "E", {15, 0}: "V", {22, 0}: "S",
		{34, 0}: "G", {39, 0}: "L", {45, 0}: "H",
		{2, 2}:  "N", // New File
		{2, 3}:  "O", // Open File...
		{2, 5}:  "S", // Save
		{3, 6}:  "a", // Save As...
		{4, 7}:  "v", // Save All
		{2, 8}:  "R", // Revert
		{3, 9}:  "e", // Reload with Encoding...
		{2, 11}: "C", // Close Buffer
		{3, 13}: "w", // Switch Project...
		{2, 15}: "D", // Detach
		{2, 16}: "Q", // Quit
	}
	if !maps.Equal(got, want) {
		t.Fatalf("underlined cells = %v, want %v", got, want)
	}
}

func TestDropdownLettersAreTheFirstFreeLetter(t *testing.T) {
	edit := rowAccels(t, "Edit")
	want := map[string]rune{
		"Undo": 'u', "Redo": 'r', "Cut": 'c', "Copy": 'o', "Copy with Formatting": 'p',
		"Paste": 'a', "Select All": 's', "Find...": 'f', "Find in Selection": 'i',
		"Find Next": 'n', "Find Previous": 'd', "Replace...": 'e', "Query Replace...": 'q',
		"Delete Line": 'l', "Format Buffer": 'm', "Settings...": 't', "Keybinding Editor...": 'k',
	}
	for label, letter := range want {
		if edit[label] != letter {
			t.Errorf("Edit > %s letter = %q, want %q", label, edit[label], letter)
		}
	}
}

func TestLettersAreUniqueWithinEachDropdown(t *testing.T) {
	for _, lm := range legacyMenus {
		seen := map[rune]string{}
		for _, r := range dropdownRows(lm.label, func(string) bool { return true }) {
			if r.Accel() == 0 {
				continue
			}
			if prev, ok := seen[r.Accel()]; ok {
				t.Errorf("%s: %q is both %q and %q", lm.label, r.Accel(), prev, r.Label)
			}
			seen[r.Accel()] = r.Label
		}
	}
}

func TestTitleLetters(t *testing.T) {
	var got []rune
	for _, r := range labelRuns(false) {
		got = append(got, r.acc.key)
	}
	if want := []rune("fevsglh"); !slices.Equal(got, want) {
		t.Fatalf("title letters = %q, want %q", string(got), string(want))
	}
	withExplorer := labelRuns(true)
	if withExplorer[6].title != "Explorer" || withExplorer[6].acc.key != 'x' {
		t.Fatalf("Explorer letter = %q, want x", withExplorer[6].acc.key)
	}
	if withExplorer[7].acc.key != 'h' {
		t.Fatalf("Help letter with explorer = %q, want h", withExplorer[7].acc.key)
	}
}

func TestAccelOnlyTitleLetterAndCaseInsensitive(t *testing.T) {
	m := newBar(allOn(), 60, 30)
	m, ok := m.Accelerator('E')
	if !ok || m.Title() != "Edit" {
		t.Fatalf("Accelerator('E') = %q, %v; want Edit", m.Title(), ok)
	}
	m, ok = m.Accelerator('h')
	if !ok || m.Title() != "Help" {
		t.Fatalf("Accelerator('h') = %q, %v; want Help", m.Title(), ok)
	}
	before := m
	m, ok = m.Accelerator('z')
	if ok || m.Title() != before.Title() {
		t.Fatalf("Accelerator('z') = %q, %v; want no change", m.Title(), ok)
	}
}

func TestAltLetterOpensTitleEvenWhenAnItemHasThatLetter(t *testing.T) {
	// Edit has a row "Find..." on F. Alt+F while Edit is open still opens File.
	m := newBar(allOn(), 60, 30).Open(1)
	m, ok := m.Accelerator('f')
	if !ok || m.Title() != "File" {
		t.Fatalf("Alt+F over Edit opened %q", m.Title())
	}
}

func TestAccelerateExplorerOnlyWithFocus(t *testing.T) {
	m := newBar(allOn(), 80, 20)
	if _, ok := m.Accelerator('x'); ok {
		t.Fatal("x opened a title while the explorer label is not in the bar")
	}
	m = m.WithExplorerFocus(true)
	m, ok := m.Accelerator('x')
	if !ok || m.Title() != "Explorer" {
		t.Fatalf("x opened %q, want Explorer", m.Title())
	}
}

func TestOpenByIndex(t *testing.T) {
	m := newBar(allOn(), 60, 30)
	if got := m.Open(2); got.Title() != "View" || !got.IsOpen() {
		t.Fatalf("Open(2) = %q", got.Title())
	}
	if got := m.Open(-1); got.IsOpen() {
		t.Fatal("Open(-1) opened a title")
	}
	if got := m.Open(99); got.IsOpen() {
		t.Fatal("Open(99) opened a title")
	}
}

func TestNextPrevWrapWhenOpenAndDoNothingWhenClosed(t *testing.T) {
	m := newBar(allOn(), 60, 30)
	if got := m.Next(); got.IsOpen() {
		t.Fatal("Next opened a title on a closed bar")
	}
	m = m.Activate()
	m = m.Next()
	if m.Title() != "Edit" {
		t.Fatalf("Next from File = %q", m.Title())
	}
	m = m.Prev()
	m = m.Prev()
	if m.Title() != "Help" {
		t.Fatalf("Prev from File = %q, want Help", m.Title())
	}
}

func TestSelectRunsHighlightedRow(t *testing.T) {
	p := allOn()
	m := newBar(p, 60, 30).Activate()
	m, cmd := m.Select()
	if m.IsOpen() || cmd != nil {
		t.Fatalf("Select closed=%v cmd=%v", !m.IsOpen(), cmd)
	}
	if !slices.Equal(p.ran, []string{"new"}) {
		t.Fatalf("ran %v, want [new]", p.ran)
	}
	if got, cmd := newBar(p, 60, 30).Select(); got.IsOpen() || cmd != nil {
		t.Fatal("Select on a closed bar changed state")
	}
}

func TestLetterActivatesItemInOpenDropdown(t *testing.T) {
	p := allOn()
	m := newBar(p, 60, 30).Activate()
	m, _ = send(m, keyMsg("o"))
	if m.IsOpen() {
		t.Fatal("letter left the dropdown open")
	}
	if !slices.Equal(p.ran, []string{"open"}) {
		t.Fatalf("ran %v, want [open]", p.ran)
	}
}

func TestLetterOnEditFindsAndRuns(t *testing.T) {
	p := allOn()
	m := newBar(p, 60, 30).Open(1)
	m, _ = send(m, keyMsg("F"))
	if m.IsOpen() || !slices.Equal(p.ran, []string{"search"}) {
		t.Fatalf("open=%v ran=%v, want search", m.IsOpen(), p.ran)
	}
}

func TestLetterOnDisabledRowClosesWithoutRun(t *testing.T) {
	p := &fakePorts{on: map[string]bool{}}
	m := newBar(p, 60, 30).Activate()
	m, cmd := send(m, keyMsg("s")) // Save needs can_save
	if m.IsOpen() || cmd != nil || len(p.ran) != 0 {
		t.Fatalf("open=%v cmd=%v ran=%v", m.IsOpen(), cmd, p.ran)
	}
}

func TestLetterWithoutRowIsNotConsumed(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	if m.Consumes(keyMsg("z")) {
		t.Fatal("z is not a File row letter but was consumed")
	}
	if !m.Consumes(keyMsg("n")) {
		t.Fatal("n is the New File letter but was not consumed")
	}
	ctrlO := tea.KeyPressMsg(tea.Key{Text: "o", Code: 'o', Mod: tea.ModCtrl})
	if m.Consumes(ctrlO) {
		t.Fatal("Ctrl+O is consumed as the Open File letter")
	}
	closed := newBar(allOn(), 60, 30)
	if closed.Consumes(keyMsg("n")) {
		t.Fatal("a letter is consumed on a closed bar")
	}
}

func TestLetterKeyUpperCaseMatches(t *testing.T) {
	cases := []struct {
		msg  tea.KeyPressMsg
		want rune
	}{
		{keyMsg("F"), 'f'},
		{keyMsg("f"), 'f'},
		{tea.KeyPressMsg(tea.Key{Text: "f", Code: 'f', Mod: tea.ModAlt}), 0},
		{tea.KeyPressMsg(tea.Key{Text: "1", Code: '1'}), 0},
		{tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}), 0},
	}
	for _, c := range cases {
		if got := letterOf(c.msg); got != c.want {
			t.Errorf("letterOf(%q) = %q, want %q", c.msg.String(), got, c.want)
		}
	}
}

func TestHoverHighlightsSelectableRow(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	m, _ = send(m, tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 3})) // Open File...
	if m.Selected() != 1 {
		t.Fatalf("hover on row 1 selected %d", m.Selected())
	}
	m, _ = send(m, tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 4})) // separator
	if m.Selected() != 1 {
		t.Fatalf("hover on a separator selected %d", m.Selected())
	}
	off := &fakePorts{on: map[string]bool{}}
	d := newBar(off, 60, 30).Activate()
	d, _ = send(d, tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 5})) // Save, disabled
	if d.Selected() == 3 {
		t.Fatal("hover highlighted a disabled row")
	}
}

func TestHoverOverLabelSwitchesTheOpenDropdown(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	hover := tea.MouseMotionMsg(tea.Mouse{X: labelRuns(false)[1].x, Y: 0})
	m, _ = send(m, hover)
	if m.Title() != "Edit" {
		t.Fatalf("hover over Edit opened %q", m.Title())
	}
	m, _ = send(m, hover)
	if m.Title() != "Edit" || m.Selected() < 0 {
		t.Fatalf("second hover on the same label changed the dropdown to %q", m.Title())
	}
}

func TestClickEditLabelOpensWithUnderlinedE(t *testing.T) {
	m := newBar(allOn(), 60, 30)
	m, _ = send(m, press(labelRuns(false)[1].x, 0))
	g := paint(m)
	if c := g.At(8, 0); c.Text != "E" || !c.Style.Underline {
		t.Fatalf("E cell = %+v", c)
	}
	if c := g.At(1, 0); c.Text != "F" || !c.Style.Underline {
		t.Fatalf("File letter is not underlined while Edit is open: %+v", c)
	}
}

func TestDropdownBorderIsRoundedAndPrimary(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	g := paint(m)
	box := m.geometry(m.w, m.h).box
	right := box.x + box.w - 1
	corners := map[[2]int]string{{box.x, 1}: "╭", {right, 1}: "╮", {box.x, box.bottom}: "╰", {right, box.bottom}: "╯"}
	for pos, glyph := range corners {
		c := g.At(pos[0], pos[1])
		if c.Text != glyph || c.Style.FG != colorPrimary {
			t.Errorf("corner %v = %+v, want %q in primary", pos, c, glyph)
		}
	}
	if c := g.At(box.x, 2); c.Text != "│" || c.Style.FG != colorPrimary {
		t.Errorf("left edge = %+v", c)
	}
}

func TestTitleOpenUsesPrimaryBackground(t *testing.T) {
	g := paint(newBar(allOn(), 60, 30).Activate())
	if c := g.At(1, 0).Style; c.BG != colorPrimary || c.FG != colorOnPrimary {
		t.Fatalf("open title style = %+v", c)
	}
	if c := g.At(8, 0).Style; c.BG == colorPrimary {
		t.Fatalf("closed title has the primary background: %+v", c)
	}
}

func TestUnderlineNeverOnSpaces(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	g := paint(m)
	for pos, text := range underlined(g) {
		if text == " " || text == "" {
			t.Fatalf("blank cell %v is underlined", pos)
		}
	}
}

func TestRow0UnderlineOnlyOnAcceleratorsAt100Wide(t *testing.T) {
	t.Parallel()
	closed := underlined(paint(newBar(allOn(), 100, 30)))
	wantClosed := map[[2]int]string{
		{1, 0}: "F", {8, 0}: "E", {15, 0}: "V", {22, 0}: "S",
		{34, 0}: "G", {39, 0}: "L", {45, 0}: "H",
	}
	if !maps.Equal(closed, wantClosed) {
		t.Fatalf("closed bar underlined cells = %v, want %v", closed, wantClosed)
	}
	open := underlined(paint(newBar(allOn(), 100, 30).Activate()))
	for pos := range open {
		if pos[1] == 0 && !slices.Contains([]int{1, 8, 15, 22, 34, 39, 45}, pos[0]) {
			t.Fatalf("row 0 cell %v is underlined with the File menu open", pos)
		}
	}
	if len(open) < len(wantClosed) {
		t.Fatalf("open bar lost accelerator underlines: %v", open)
	}
}
