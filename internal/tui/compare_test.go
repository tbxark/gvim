package tui

import (
	"image/color"
	"maps"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/diff"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// compareLeft and compareRight differ in three hunks, with zero-based lines. b is
// removed: left line 1 is right line 1. X is added at right line 3, which comes
// after left line 3, so it starts at left line 4. f is added at the end of the
// right, after left line 4, which is the last line of the left, so its left start
// is clamped to line 4.
var (
	compareLeft  = []string{"a", "b", "c", "d", "e"}
	compareRight = []string{"a", "c", "d", "X", "e", "f"}
)

// newCompareModel returns a model with the two tabs in a compare view, the left pane
// focused.
func newCompareModel(t *testing.T, left, right []string) *Model {
	t.Helper()
	m := newSplitModel(t, newDocsFake(left, right), 80, 24)
	if !m.openCompare(0, 1) {
		t.Fatal("openCompare(0, 1) = false")
	}
	return m
}

// alignedTops returns the display row at the top of each pane of the compare view, counting
// the filler rows: the two are equal when the sides are level.
func alignedTops(t *testing.T, m *Model) (left, right int) {
	t.Helper()
	st, ok := m.compareState()
	if !ok {
		t.Fatal("compareState = not ok")
	}
	nodes := [2]*splitNode{m.splits.root.a, m.splits.root.b}
	var rows [2]int
	for side, n := range nodes {
		top, topRow := n.top, n.topRow
		if n == m.splits.focus {
			top, topRow = m.ed.Active().Top(), m.topRow(m.ed.Active().Top())
		}
		rows[side] = st.side[side].fill.BlockStart(top) + topRow
	}
	return rows[0], rows[1]
}

func TestCompareStepWrapsAroundOnTheFocusedSide(t *testing.T) {
	// A difference is where hunkJump puts the caret. The left jumps are 1 (b removed), 3 (the
	// line before X goes in) and 4 (the line before f goes in, the last one). The right
	// jumps are 0 (the line before the gap where b was), 3 and 5.
	tests := []struct {
		name       string
		focusRight bool
		from       int  // the cursor line on the focused side before the step
		forward    bool // next_difference when set, prev_difference otherwise
		want       int  // the cursor line on the focused side after the step
		wrapped    bool
	}{
		{"left, next from the top", false, 0, true, 1, false},
		{"left, next from the first difference", false, 1, true, 3, false},
		{"left, next from the second difference", false, 3, true, 4, false},
		{"left, next from the last difference wraps", false, 4, true, 1, true},
		{"left, prev from the top wraps", false, 0, false, 4, true},
		{"left, prev from the last difference", false, 4, false, 3, false},
		{"left, prev from the first difference wraps", false, 1, false, 4, true},
		{"right, next from the top", true, 0, true, 3, false},
		{"right, next from the second difference", true, 3, true, 5, false},
		{"right, next from the last difference wraps", true, 5, true, 0, true},
		{"right, prev from the top wraps", true, 0, false, 5, true},
		{"right, prev from the last difference", true, 5, false, 3, false},
		{"right, prev from the second difference", true, 3, false, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newCompareModel(t, compareLeft, compareRight)
			if tt.focusRight {
				runAction(t, m, "next_split")
			}
			m.ed.SetCursor(buffer.Pos{Line: tt.from}, false)
			action := "compare_prev_difference"
			if tt.forward {
				action = "compare_next_difference"
			}
			runAction(t, m, action)
			if got := m.ed.Active().Cursor().Line; got != tt.want {
				t.Errorf("focused cursor line = %d, want %d", got, tt.want)
			}
			if got := strings.HasSuffix(m.notice, "[wrapped]"); got != tt.wrapped {
				t.Errorf("notice = %q, wrapped = %v, want %v", m.notice, got, tt.wrapped)
			}
			if l, r := alignedTops(t, m); l != r {
				t.Errorf("panes start at rows %d and %d, want the same row", l, r)
			}
		})
	}
}

func TestCompareStepSaysWhichDifference(t *testing.T) {
	m := newCompareModel(t, compareLeft, compareRight)
	runAction(t, m, "compare_next_difference")
	if m.notice != "Difference 1/3" {
		t.Errorf("notice = %q, want %q", m.notice, "Difference 1/3")
	}
	runAction(t, m, "compare_next_difference")
	if m.notice != "Difference 2/3" {
		t.Errorf("notice = %q, want %q", m.notice, "Difference 2/3")
	}
}

func TestCompareHunksListsTheDifferences(t *testing.T) {
	// The hunk list is what a renderer paints, so it is read here and not from the
	// frame. Each hunk gives its start on the left and on the right.
	m := newSplitModel(t, newDocsFake(compareLeft, compareRight), 80, 24)
	if _, ok := m.compareHunks(); ok {
		t.Fatal("compareHunks with no compare view reports ok")
	}
	if !m.openCompare(0, 1) {
		t.Fatal("openCompare(0, 1) = false")
	}
	hunks, ok := m.compareHunks()
	if !ok {
		t.Fatal("compareHunks = not ok")
	}
	var starts [][2]int
	for _, h := range hunks {
		starts = append(starts, [2]int{h.OldStart, h.NewStart})
	}
	if want := [][2]int{{1, 1}, {4, 3}, {5, 5}}; !slices.Equal(starts, want) {
		t.Errorf("hunk starts (left, right) = %v, want %v", starts, want)
	}
	// Focus the right pane: the list is the same, because it does not depend on focus.
	runAction(t, m, "next_split")
	if again, _ := m.compareHunks(); !slices.Equal(again, hunks) {
		t.Errorf("hunks changed with the focus: %v, want %v", again, hunks)
	}
}

func TestCompareReportsNoDifferencesAndKeepsTheCursor(t *testing.T) {
	m := newCompareModel(t, []string{"a", "b"}, []string{"a", "b"})
	runAction(t, m, "compare_next_difference")
	if m.notice != "Compare: the files are identical" {
		t.Errorf("notice = %q, want %q", m.notice, "Compare: the files are identical")
	}
	if got := m.ed.Active().Cursor().Line; got != 0 {
		t.Errorf("cursor line = %d, want 0", got)
	}
}

func TestCompareNeedsACompareView(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 3), 80, 24)
	for _, action := range []string{"compare_next_difference", "compare_prev_difference", "compare_switch_side"} {
		runAction(t, m, action)
		if m.notice != noCompareNotice {
			t.Errorf("%s: notice = %q, want %q", action, m.notice, noCompareNotice)
		}
	}
}

func TestCompareWithoutTheOtherDocumentHasNoDifferences(t *testing.T) {
	// fakeEditor has no DocAt, so the right tab has no document to compare.
	f := newFakeEditor("a", "b")
	f.tabs = []TabInfo{{Name: "a.txt", Active: true}, {Name: "b.txt"}}
	m := New(Deps{Editor: f, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !m.openCompare(0, 1) {
		t.Fatal("openCompare(0, 1) = false")
	}
	runAction(t, m, "compare_next_difference")
	if m.notice == "" {
		t.Error("no notice for a compare whose other document is missing")
	}
	if got := f.doc.cursor.Line; got != 0 {
		t.Errorf("cursor line = %d, want 0", got)
	}
}

func TestCompareSwitchSideMovesTheFocusToTheOtherFile(t *testing.T) {
	m := newCompareModel(t, compareLeft, compareRight)
	runAction(t, m, "compare_switch_side")
	ps := panesOf(t, m, 2)
	if ps[0].tab != 0 || ps[1].tab != 1 {
		t.Errorf("tabs after the switch = %d and %d, want 0 and 1: the panes stay where they are", ps[0].tab, ps[1].tab)
	}
	if m.layout().focus != 1 || m.activeTab() != 1 {
		t.Errorf("focus on pane %d with tab %d active, want pane 1 and tab 1", m.layout().focus, m.activeTab())
	}
	runAction(t, m, "compare_switch_side")
	if m.layout().focus != 0 || m.activeTab() != 0 {
		t.Errorf("focus on pane %d with tab %d active after a second switch, want pane 0 and tab 0", m.layout().focus, m.activeTab())
	}
}

func TestOpenCompareShowsTwoTabsSideBySide(t *testing.T) {
	m := newSplitModel(t, newDocsFake([]string{"a"}, []string{"b"}), 80, 24)
	if !m.openCompare(0, 1) {
		t.Fatal("openCompare(0, 1) = false")
	}
	l := m.layout()
	if len(l.panes) != 2 || l.panes[0].tab != 0 || l.panes[1].tab != 1 || l.focus != 0 {
		t.Fatalf("panes %+v focus %d, want tabs 0 and 1 with the focus on pane 0", l.panes, l.focus)
	}
	if l.panes[0].y != l.panes[1].y || l.panes[1].x <= l.panes[0].x {
		t.Errorf("panes %+v are not side by side", l.panes)
	}
	if m.openCompare(0, 2) {
		t.Error("openCompare with a missing tab = true")
	}
	if n := len(m.layout().panes); n != 2 {
		t.Errorf("%d panes after a refused compare, want 2", n)
	}
}

func TestSplitLeavesTheCompareView(t *testing.T) {
	m := newCompareModel(t, compareLeft, compareRight)
	runAction(t, m, "split_horizontal")
	runAction(t, m, "compare_next_difference")
	if m.notice != noCompareNotice {
		t.Errorf("notice = %q after a split, want the compare view closed", m.notice)
	}
}

func TestPickHunkWrapsAtBothEnds(t *testing.T) {
	// The jumps on the left are 1, 3 and 4, and on the right 0, 3 and 5.
	hunks := []diff.Hunk{
		{Kind: diff.Delete, OldStart: 1, OldLines: 1, NewStart: 1},
		{Kind: diff.Insert, OldStart: 4, NewStart: 3, NewLines: 1},
		{Kind: diff.Insert, OldStart: 5, NewStart: 5, NewLines: 1},
	}
	tests := []struct {
		name        string
		side        int
		line, col   int
		last        int // the last line index of the focused document
		forward     bool
		want        int
		wantWrapped bool
	}{
		{"left, forward from the top", 0, 0, 0, 4, true, 0, false},
		{"left, forward to the second", 0, 1, 0, 4, true, 1, false},
		{"left, forward past the last wraps", 0, 4, 0, 4, true, 0, true},
		{"left, backward from the top wraps", 0, 0, 0, 4, false, 2, true},
		{"left, backward skips the hunk at the caret", 0, 4, 0, 4, false, 1, false},
		{"left, backward from inside a hunk line finds its start", 0, 1, 3, 4, false, 0, false},
		{"right, forward from the first jump", 1, 0, 0, 5, true, 1, false},
		{"right, forward from the second start", 1, 3, 0, 5, true, 2, false},
		{"right, backward from the top wraps", 1, 0, 0, 5, false, 2, true},
		{"right, backward from the second start", 1, 3, 0, 5, false, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, wrapped := pickHunk(hunks, tt.side, tt.line, tt.col, tt.last, tt.forward)
			if got != tt.want || wrapped != tt.wantWrapped {
				t.Errorf("pickHunk = %d wrapped %v, want %d wrapped %v", got, wrapped, tt.want, tt.wantWrapped)
			}
		})
	}
	if got, _ := pickHunk(nil, 0, 0, 0, 0, true); got != -1 {
		t.Errorf("pickHunk with no hunks = %d, want -1", got)
	}
}

// TestCompareLinesLooksByHunk checks which lines of each side a difference covers: a
// change pairs its lines up and leaves the extra ones lone, an insert is on the right
// only and a delete on the left only.
func TestCompareLinesLooksByHunk(t *testing.T) {
	tests := []struct {
		name  string
		hunks []diff.Hunk
		left  map[int]compareLook
		right map[int]compareLook
	}{
		{"delete", []diff.Hunk{{Kind: diff.Delete, OldStart: 2, OldLines: 2, NewStart: 2}},
			map[int]compareLook{2: lookRemoved, 3: lookRemoved}, map[int]compareLook{}},
		{"insert", []diff.Hunk{{Kind: diff.Insert, OldStart: 2, NewStart: 2, NewLines: 2}},
			map[int]compareLook{}, map[int]compareLook{2: lookAdded, 3: lookAdded}},
		{"change with more lines on the right", []diff.Hunk{{Kind: diff.Change, OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 3}},
			map[int]compareLook{1: lookModified}, map[int]compareLook{1: lookModified, 2: lookAdded, 3: lookAdded}},
		{"change with more lines on the left", []diff.Hunk{{Kind: diff.Change, OldStart: 0, OldLines: 2, NewStart: 0, NewLines: 1}},
			map[int]compareLook{0: lookModified, 1: lookRemoved}, map[int]compareLook{0: lookModified}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := compareLines(tc.hunks, 0); !maps.Equal(got, tc.left) {
				t.Errorf("left looks = %v, want %v", got, tc.left)
			}
			if got := compareLines(tc.hunks, 1); !maps.Equal(got, tc.right) {
				t.Errorf("right looks = %v, want %v", got, tc.right)
			}
		})
	}
}

// TestComparePanesTintTheLinesThatDiffer checks the painted frame of a compare view at a
// few cells: the removed line of the left, the added line of the right, the lines both
// sides share, and the cells past the end of a tinted line.
func TestComparePanesTintTheLinesThatDiffer(t *testing.T) {
	th := mustLoadTheme(t, "dark")
	m := newSplitModelTheme(t, newDocsFake(compareLeft, compareRight), "dark")
	if !m.openCompare(0, 1) {
		t.Fatal("openCompare(0, 1) = false")
	}
	remove := style.Color(th, "editor", "diff_remove_bg")
	add := style.Color(th, "editor", "diff_add_bg")
	if remove == nil || add == nil {
		t.Fatal("the dark theme has no diff colors")
	}
	l := m.layout()
	left, right := l.panes[0], l.panes[1]
	st, _ := m.compareState()
	bgAt := func(p splitPane, side, line int, tail bool) color.Color {
		x := p.textX
		if tail {
			x = p.textX + 10 // well past the one-character text
		}
		// The row of a line counts the filler rows above it.
		return m.paint().At(x, l.bodyTop+st.side[side].fill.TextRow(line)).Style.BG
	}
	tests := []struct {
		name string
		pane splitPane
		side int
		line int
		want color.Color
	}{
		{"removed line on the left", left, 0, 1, remove},
		{"added line on the right", right, 1, 3, add},
		{"the shared first line on the left", left, 0, 0, nil},
		{"the shared first line on the right", right, 1, 0, nil},
		{"left line that has no partner on the right", left, 0, 3, nil},
		{"right line that has no partner on the left", right, 1, 1, nil},
	}
	for _, tc := range tests {
		got := bgAt(tc.pane, tc.side, tc.line, false)
		if tc.want == nil {
			if got == remove || got == add {
				t.Errorf("%s has the diff background %v", tc.name, got)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("%s background = %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := bgAt(left, 0, 1, true); got != remove {
		t.Errorf("the end of the removed line has background %v, want the row tinted to the edge", got)
	}
}

func TestPaletteListsTheCompareCommandsOnlyDuringACompare(t *testing.T) {
	m := newSplitModel(t, newDocsFake(compareLeft, compareRight), 80, 24)
	has := func(action string) bool {
		return slices.ContainsFunc(m.allCommands(), func(e commands.Entry) bool { return e.Action == action })
	}
	if has("compare_next_difference") {
		t.Fatal("a compare command is listed with no compare view")
	}
	m.openCompare(0, 1)
	for _, action := range []string{"compare_next_difference", "compare_prev_difference", "compare_switch_side"} {
		if !has(action) {
			t.Errorf("%s is not listed during a compare", action)
		}
	}
	// The palette's entry runs the action.
	m.dispatch(Command{Action: "compare_next_difference"})
	if got := m.ed.Active().Cursor().Line; got != 1 {
		t.Errorf("the next difference moved the caret to line %d, want 1", got)
	}
}

// verDoc is a fake document that reports a version, as the editor's documents do.
type verDoc struct {
	*fakeDoc
	version uint64
}

func (d *verDoc) Version() uint64 { return d.version }

// verFake is a multiFake over verDocs.
type verFake struct {
	*multiFake
	docs []*verDoc
}

func (f *verFake) Active() Document { return f.docs[f.active] }
func (f *verFake) DocAt(i int) Document {
	if i < 0 || i >= len(f.docs) {
		return nil
	}
	return f.docs[i]
}

func TestCompareDiffIsKeptUntilTheTextChanges(t *testing.T) {
	base := newDocsFake(compareLeft, compareRight)
	f := &verFake{multiFake: base, docs: []*verDoc{{fakeDoc: base.docs[0]}, {fakeDoc: base.docs[1]}}}
	m := newSplitModel(t, base, 80, 24)
	m.ed = f
	if !m.openCompare(0, 1) {
		t.Fatal("openCompare(0, 1) = false")
	}
	first, _ := m.compareHunks()
	again, _ := m.compareHunks()
	if len(first) == 0 || &first[0] != &again[0] {
		t.Fatal("the diff was computed again for the same text")
	}
	f.docs[1].lines = slices.Clone(compareLeft) // the right side now equals the left
	f.docs[1].version++
	if hunks, _ := m.compareHunks(); len(hunks) != 0 {
		t.Errorf("%d hunks after the sides became equal, want 0", len(hunks))
	}
}
