package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	palmodel "github.com/tbxark/gvim/internal/palette"
	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/browser"
)

// cmpFake is a multiFake whose tabs have paths and that opens the files of a map, as the
// app adapter opens files from disk.
type cmpFake struct {
	*multiFake
	paths []string
	files map[string][]string
	dirty map[string]bool // the files with unsaved changes
}

func newCmpFake(files map[string][]string, open ...string) *cmpFake {
	f := &cmpFake{multiFake: &multiFake{}, files: files, dirty: map[string]bool{}}
	for _, p := range open {
		_ = f.OpenFile(p)
	}
	return f
}

func (f *cmpFake) Tabs() []TabInfo {
	tabs := f.multiFake.Tabs()
	for i := range tabs {
		tabs[i].Path = f.paths[i]
		tabs[i].Modified = f.dirty[f.paths[i]]
	}
	return tabs
}

func (f *cmpFake) OpenFile(path string) error {
	for i, p := range f.paths {
		if p == path {
			f.active = i
			return nil
		}
	}
	lines, ok := f.files[path]
	if !ok {
		return errors.New("no such file: " + path)
	}
	f.docs = append(f.docs, &fakeDoc{lines: slices.Clone(lines)})
	f.paths = append(f.paths, path)
	f.active = len(f.docs) - 1
	return nil
}

func (f *cmpFake) OpenPreview(path string) error { return f.OpenFile(path) }
func (f *cmpFake) GotoLine(int, int) error       { return nil }

func (f *cmpFake) CloseTab(i int, force bool) error {
	if i >= 0 && i < len(f.docs) && f.dirty[f.paths[i]] && !force {
		return errors.New("unsaved changes")
	}
	if err := f.multiFake.CloseTab(i, force); err != nil {
		return err
	}
	f.paths = slices.Delete(f.paths, i, i+1)
	return nil
}

// cmpPE is the plugin editor port over a cmpFake: it lists the tabs, records the carets
// and applies an edit to the lines of a document.
type cmpPE struct {
	pluginapi.Unsupported
	f       *cmpFake
	edits   int
	carets  map[pluginapi.BufferID]pluginapi.Pos
	failure error
}

func (p *cmpPE) ListBuffers() []pluginapi.BufferInfo {
	var out []pluginapi.BufferInfo
	for i := range p.f.docs {
		out = append(out, pluginapi.BufferInfo{ID: pluginapi.BufferID(i + 1), Path: p.f.paths[i]})
	}
	return out
}

func (p *cmpPE) SetCursor(id pluginapi.BufferID, pos pluginapi.Pos, _, _, _ bool) error {
	if p.carets == nil {
		p.carets = map[pluginapi.BufferID]pluginapi.Pos{}
	}
	p.carets[id] = pos
	return nil
}

func (p *cmpPE) Edit(id pluginapi.BufferID, edits []pluginapi.Edit, _ pluginapi.EditOptions) error {
	if p.failure != nil {
		return p.failure
	}
	d := p.f.docs[int(id)-1]
	text := strings.Join(d.lines, "\n")
	off := func(pos pluginapi.Pos) int {
		n := 0
		for i := range pos.Line {
			n += len(d.lines[i]) + 1
		}
		return n + pos.Col
	}
	e := edits[0]
	text = text[:off(e.Range.From)] + e.Text + text[off(e.Range.To):]
	d.lines = strings.Split(text, "\n")
	p.edits++
	return nil
}

func newCmpModel(t *testing.T, f *cmpFake) (*Model, *cmpPE) {
	t.Helper()
	pe := &cmpPE{f: f}
	m := New(Deps{Editor: f, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{PluginEditor: pe}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m, pe
}

var cmpFiles = map[string][]string{
	"/w/a.txt": {"one", "two", "three", "four"},
	"/w/b.txt": {"one", "2", "three", "four", "five"},
	"/w/c.txt": {"x"},
}

func TestCompareFilesPicksTwoFilesAndOpensTheView(t *testing.T) {
	m, _ := newCmpModel(t, newCmpFake(cmpFiles))
	runAction(t, m, "compare_files")
	if m.splits.pick == nil {
		t.Fatal("Compare Files did not wait for a file")
	}
	m.Update(browser.Open{Path: "/w/a.txt"})
	if m.splits.pick == nil || m.splits.pick.left != "/w/a.txt" || m.splits.comparing {
		t.Fatalf("after the first file: pick %+v, comparing %v; want it to wait for the right file", m.splits.pick, m.splits.comparing)
	}
	m.Update(browser.Open{Path: "/w/b.txt"})
	if m.splits.pick != nil || !m.splits.comparing {
		t.Fatalf("after the second file: pick %+v, comparing %v; want a compare view", m.splits.pick, m.splits.comparing)
	}
	ps := panesOf(t, m, 2)
	if ps[0].tab != 0 || ps[1].tab != 1 || m.layout().focus != 0 {
		t.Errorf("panes %+v focus %d, want tabs 0 and 1 and the left pane focused", ps, m.layout().focus)
	}
	if !strings.HasPrefix(m.notice, "Difference 1/") {
		t.Errorf("notice = %q, want it to say which difference the view is on", m.notice)
	}
}

func TestCompareWithFileTakesTheActiveFile(t *testing.T) {
	f := newCmpFake(cmpFiles, "/w/a.txt")
	m, _ := newCmpModel(t, f)
	runAction(t, m, "compare_with_file")
	if m.splits.pick == nil || m.splits.pick.left != "/w/a.txt" {
		t.Fatalf("pick = %+v, want it to wait for the right file with a.txt on the left", m.splits.pick)
	}
	m.Update(browser.Open{Path: "/w/b.txt"})
	if !m.splits.comparing || len(f.docs) != 2 {
		t.Fatalf("comparing %v with %d tabs, want a compare view of two tabs", m.splits.comparing, len(f.docs))
	}
	if !slices.Equal(f.paths, []string{"/w/a.txt", "/w/b.txt"}) {
		t.Errorf("tabs = %v, want a.txt then b.txt", f.paths)
	}
}

func TestCompareWithFileNeedsAFile(t *testing.T) {
	f := newCmpFake(cmpFiles)
	f.docs = append(f.docs, &fakeDoc{lines: []string{"untitled"}})
	f.paths = append(f.paths, "")
	m, _ := newCmpModel(t, f)
	runAction(t, m, "compare_with_file")
	if m.splits.pick != nil || m.notice != "Compare: the active buffer is not a file" {
		t.Errorf("pick %+v notice %q, want the notice and no waiting", m.splits.pick, m.notice)
	}
}

func TestCompareRefusesTheSameFileTwice(t *testing.T) {
	m, _ := newCmpModel(t, newCmpFake(cmpFiles, "/w/a.txt"))
	m.compareFiles("/w/a.txt", "/w/../w/a.txt")
	if m.splits.comparing || m.notice != "Compare: that is the same file twice" {
		t.Errorf("comparing %v notice %q, want a refusal", m.splits.comparing, m.notice)
	}
}

func TestCompareOfAMissingFileSaysWhy(t *testing.T) {
	m, _ := newCmpModel(t, newCmpFake(cmpFiles))
	m.compareFiles("/w/a.txt", "/w/none.txt")
	if m.splits.comparing || !strings.Contains(m.notice, "no such file") {
		t.Errorf("comparing %v notice %q, want the open error", m.splits.comparing, m.notice)
	}
}

func TestCompareCloseClosesTheFilesItOpened(t *testing.T) {
	tests := []struct {
		name     string
		open     []string // the tabs open before the comparison
		modify   string   // a file edited while the comparison is open
		wantTabs []string
	}{
		{"both opened by the view", nil, "", nil},
		{"a file that was open stays", []string{"/w/a.txt"}, "", []string{"/w/a.txt"}},
		{"a file with unsaved changes stays", nil, "/w/b.txt", []string{"/w/b.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCmpFake(cmpFiles, tt.open...)
			m, _ := newCmpModel(t, f)
			m.compareFiles("/w/a.txt", "/w/b.txt")
			if !m.splits.comparing {
				t.Fatalf("no compare view: %q", m.notice)
			}
			if tt.modify != "" {
				f.dirty[tt.modify] = true
			}
			runAction(t, m, "compare_close")
			if m.splits.comparing || !m.splits.root.leaf() {
				t.Error("the compare view is still open")
			}
			if !slices.Equal(f.paths, tt.wantTabs) {
				t.Errorf("tabs after the close = %v, want %v", f.paths, tt.wantTabs)
			}
		})
	}
}

func TestCompareCloseWithNoViewSaysSo(t *testing.T) {
	m, _ := newCmpModel(t, newCmpFake(cmpFiles, "/w/a.txt"))
	runAction(t, m, "compare_close")
	if m.notice != noCompareNotice {
		t.Errorf("notice = %q, want %q", m.notice, noCompareNotice)
	}
}

func TestCompareMergeMakesTheSidesMatch(t *testing.T) {
	left := []string{"a", "b", "c", "d"}
	right := []string{"a", "X", "Y", "d", "e"}
	tests := []struct {
		name      string
		toOther   bool
		line      int // the caret line on the left
		wantLeft  []string
		wantRight []string
	}{
		{"copy a change to the other side", true, 1,
			[]string{"a", "b", "c", "d"}, []string{"a", "b", "c", "d", "e"}},
		{"take a change from the other side", false, 2,
			[]string{"a", "X", "Y", "d"}, right},
		{"a caret off any change changes nothing", true, 0,
			[]string{"a", "b", "c", "d"}, []string{"a", "X", "Y", "d", "e"}},
		{"a caret at the seam of a gap takes the change there", true, 3,
			[]string{"a", "b", "c", "d"}, []string{"a", "X", "Y", "d"}}, // "e" is only on the right, after line 3
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCmpFake(map[string][]string{"/w/l": left, "/w/r": right}, "/w/l", "/w/r")
			m, pe := newCmpModel(t, f)
			if !m.openCompare(0, 1) {
				t.Fatal("openCompare = false")
			}
			m.ed.SetCursor(buffer.Pos{Line: tt.line}, false)
			action := "compare_take_change"
			if tt.toOther {
				action = "compare_copy_change"
			}
			runAction(t, m, action)
			if !slices.Equal(f.docs[0].lines, tt.wantLeft) || !slices.Equal(f.docs[1].lines, tt.wantRight) {
				t.Errorf("left %v right %v, want left %v right %v (notice %q)",
					f.docs[0].lines, f.docs[1].lines, tt.wantLeft, tt.wantRight, m.notice)
			}
			if pe.edits > 1 {
				t.Errorf("%d edits, want at most one: one undo step", pe.edits)
			}
		})
	}
}

func TestCompareMergeWithTheCaretOffAChange(t *testing.T) {
	f := newCmpFake(map[string][]string{"/w/l": {"a", "b"}, "/w/r": {"a", "c"}}, "/w/l", "/w/r")
	m, pe := newCmpModel(t, f)
	m.openCompare(0, 1)
	runAction(t, m, "compare_copy_change") // the caret is on line 0, which is shared
	if m.notice != "Compare: the cursor is not on a difference" || pe.edits != 0 {
		t.Errorf("notice %q, %d edits; want the notice and no edit", m.notice, pe.edits)
	}
}

func TestCompareMergeReportsAFailedEdit(t *testing.T) {
	f := newCmpFake(map[string][]string{"/w/l": {"a", "b"}, "/w/r": {"a", "c"}}, "/w/l", "/w/r")
	m, pe := newCmpModel(t, f)
	m.openCompare(0, 1)
	m.ed.SetCursor(buffer.Pos{Line: 1}, false)
	pe.failure = errors.New("read-only")
	runAction(t, m, "compare_copy_change")
	if !strings.Contains(m.notice, "was not changed") || !strings.Contains(m.notice, "read-only") {
		t.Errorf("notice = %q, want it to say the file was not changed and why", m.notice)
	}
}

func TestPaletteListsCompareFilesWithoutAComparison(t *testing.T) {
	m, _ := newCmpModel(t, newCmpFake(cmpFiles, "/w/a.txt"))
	has := func(action string) bool {
		return slices.ContainsFunc(m.allCommands(), func(e palmodel.Entry) bool { return e.Action == action })
	}
	for _, action := range []string{"compare_files", "compare_with_file"} {
		if !has(action) {
			t.Errorf("%s is not in the palette", action)
		}
	}
	for _, action := range []string{"compare_next_difference", "compare_copy_change", "compare_take_change", "compare_close"} {
		if has(action) {
			t.Errorf("%s is listed with no comparison open", action)
		}
	}
}

func TestCompareHasCompareRowsOnlyDuringAComparison(t *testing.T) {
	f := newCmpFake(cmpFiles, "/w/a.txt", "/w/b.txt")
	m, _ := newCmpModel(t, f)
	m.openCompare(0, 1)
	for _, action := range []string{"compare_next_difference", "compare_prev_difference", "compare_switch_side", "compare_copy_change", "compare_take_change", "compare_close"} {
		if !slices.ContainsFunc(m.allCommands(), func(e palmodel.Entry) bool { return e.Action == action }) {
			t.Errorf("%s is not listed during a comparison", action)
		}
	}
}

// TestClosingASideTabEndsTheCompareView checks that closing the tab of either file of a comparison
// ends the view, as Fresh closes the whole group, instead of leaving a split of the other file
// shown twice with no marks.
func TestClosingASideTabEndsTheCompareView(t *testing.T) {
	tests := []struct {
		name  string
		close func(m *Model, f *cmpFake)
	}{
		{"close_tab on the active side", func(m *Model, _ *cmpFake) { runAction(t, m, "close_tab") }},
		{"the tab of the other side", func(m *Model, _ *cmpFake) {
			if err := m.closeTab(m.tabPort(), 1, false); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCmpFake(cmpFiles)
			m, _ := newCmpModel(t, f)
			m.compareFiles("/w/a.txt", "/w/b.txt")
			if !m.splits.comparing {
				t.Fatalf("no compare view: %q", m.notice)
			}
			tt.close(m, f)
			if m.splits.comparing || !m.splits.root.leaf() {
				t.Fatalf("after closing a side: comparing %v, one pane %v; want the view gone and a single pane", m.splits.comparing, m.splits.root.leaf())
			}
			if len(f.docs) != 1 {
				t.Errorf("tabs = %v, want the other file still open", f.paths)
			}
			if m.splits.owned != nil {
				t.Errorf("owned = %v, want it forgotten with the view", m.splits.owned)
			}
		})
	}
}

// TestCompareTabsAreMarkedAsAPair checks that the strip shows the two tabs of a comparison with
// a pair mark, with the unsaved mark of each side on its own tab, and the mark goes with the view.
func TestCompareTabsAreMarkedAsAPair(t *testing.T) {
	f := newCmpFake(cmpFiles, "/w/c.txt")
	m, _ := newCmpModel(t, f)
	m.compareFiles("/w/a.txt", "/w/b.txt")
	if !m.splits.comparing {
		t.Fatalf("no compare view: %q", m.notice)
	}
	f.dirty["/w/b.txt"] = true
	var labels []string
	for _, tab := range m.stripTabs() {
		labels = append(labels, stripLabel(tab))
	}
	if len(labels) != 3 || !strings.HasPrefix(labels[1], comparePairMark+" ") || !strings.HasPrefix(labels[2], modifiedMark+" "+comparePairMark+" ") {
		t.Errorf("labels = %q, want the two compared tabs marked as a pair, the unsaved one also with its dot", labels)
	}
	if strings.Contains(labels[0], comparePairMark) {
		t.Errorf("the tab outside the comparison is marked: %q", labels[0])
	}
	runAction(t, m, "compare_close")
	for _, tab := range m.stripTabs() {
		if strings.Contains(stripLabel(tab), comparePairMark) {
			t.Errorf("label %q is still marked after the view closed", stripLabel(tab))
		}
	}
}

// TestCompareStepMovesTheCaretOfTheOtherSide checks that Next Difference moves the caret of the
// unfocused file as well (Fresh's compare plugin does the same). cmpFiles differs at line 1 and
// then by a line that only b.txt has: on a.txt the caret goes to line 3, the line before the gap
// above its filler row, and on b.txt to line 4, the added line.
func TestCompareStepMovesTheCaretOfTheOtherSide(t *testing.T) {
	f := newCmpFake(cmpFiles, "/w/a.txt", "/w/b.txt")
	m, pe := newCmpModel(t, f)
	m.OpenCompare(0, 1)
	runAction(t, m, "compare_next_difference")
	if !strings.HasPrefix(m.notice, "Difference 2/") {
		t.Fatalf("notice = %q, want the second difference", m.notice)
	}
	if got := m.ed.Active().Cursor().Line; got != 3 {
		t.Errorf("focused caret line = %d, want 3, the line before the gap", got)
	}
	right := pe.ListBuffers()[1].ID
	if got, ok := pe.carets[right]; !ok || got.Line != 4 {
		t.Errorf("other side caret = %+v (set %t), want line 4, the added line", got, ok)
	}
}
