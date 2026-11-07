package ui

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/fold"
	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/theme"
)

// winEditor is the plugin editor port over a multiFake: tab i is the buffer with id ids[i].
type winEditor struct {
	pluginapi.Unsupported
	f   *multiFake
	ids []pluginapi.BufferID
}

func (w *winEditor) ListBuffers() []pluginapi.BufferInfo {
	out := make([]pluginapi.BufferInfo, len(w.f.docs))
	for i := range out {
		out[i] = pluginapi.BufferInfo{ID: w.ids[i], Name: "t" + strconv.Itoa(i), Active: i == w.f.active}
	}
	return out
}

func (w *winEditor) Buffer(id pluginapi.BufferID) (pluginapi.BufferInfo, bool) {
	for _, b := range w.ListBuffers() {
		if b.ID == id {
			return b, true
		}
	}
	return pluginapi.BufferInfo{}, false
}

func (w *winEditor) ActiveBuffer() pluginapi.BufferID { return w.ids[w.f.active] }

func (w *winEditor) Activate(id pluginapi.BufferID) error {
	i := slices.Index(w.ids, id)
	if i < 0 {
		return pluginapi.Errf(pluginapi.CodeClosed, "no buffer %d", id)
	}
	w.f.ActivateTab(i)
	return nil
}

// Close removes the tab of buffer id.
func (w *winEditor) Close(id pluginapi.BufferID, _ bool) error {
	i := slices.Index(w.ids, id)
	if i < 0 {
		return pluginapi.Errf(pluginapi.CodeClosed, "no buffer %d", id)
	}
	if err := w.f.CloseTab(i, true); err != nil {
		return err
	}
	w.ids = slices.Delete(w.ids, i, i+1)
	return nil
}

// MoveTab moves the tab of buffer id by delta places, keeping the active document.
func (w *winEditor) MoveTab(id pluginapi.BufferID, delta int) error {
	from := slices.Index(w.ids, id)
	if from < 0 {
		return pluginapi.Errf(pluginapi.CodeClosed, "no buffer %d", id)
	}
	to := min(max(from+delta, 0), len(w.ids)-1)
	active := w.f.docs[w.f.active]
	doc := w.f.docs[from]
	w.f.docs = slices.Insert(slices.Delete(w.f.docs, from, from+1), to, doc)
	w.ids = slices.Insert(slices.Delete(w.ids, from, from+1), to, id)
	w.f.active = slices.Index(w.f.docs, active)
	return nil
}

// newWinModel returns an 80 by 24 model over ed, whose tabs are f's, with the plugin host
// and the editor port of winEditor.
func newWinModel(t *testing.T, ed EditorState, f *multiFake) *Model {
	t.Helper()
	ids := make([]pluginapi.BufferID, len(f.docs))
	for i := range ids {
		ids[i] = pluginapi.BufferID(i + 1)
	}
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock,
		Root: RootPorts{Plugins: newFakeRuntime(t), PluginEditor: &winEditor{f: f, ids: ids}}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

func newWinTabs(t *testing.T, tabs, lines int) (*Model, *multiFake) {
	t.Helper()
	f := newMultiFake(tabs, lines)
	return newWinModel(t, f, f), f
}

func winIDsOf(m *Model) []pluginapi.WinID {
	var out []pluginapi.WinID
	for _, w := range m.plug.host.Windows() {
		out = append(out, w.ID)
	}
	return out
}

func winInfo(t *testing.T, m *Model, id pluginapi.WinID) pluginapi.WindowInfo {
	t.Helper()
	for _, w := range m.plug.host.Windows() {
		if w.ID == id {
			return w
		}
	}
	t.Fatalf("no window %d among %v", id, winIDsOf(m))
	return pluginapi.WindowInfo{}
}

func TestWindowIDsSurviveSplitsAndCloses(t *testing.T) {
	m, _ := newWinTabs(t, 2, 30)
	h := m.plug.host
	steps := []struct {
		name    string
		do      func()
		want    []pluginapi.WinID // reading order
		current pluginapi.WinID
	}{
		{"one window", func() {}, []pluginapi.WinID{1}, 1},
		{"split_vertical keeps the old id on the old pane", func() { runAction(t, m, "split_vertical") }, []pluginapi.WinID{1, 2}, 2},
		{"split the new pane below", func() { runAction(t, m, "split_horizontal") }, []pluginapi.WinID{1, 2, 3}, 3},
		{"moving the focus changes no id", func() { runAction(t, m, "next_pane") }, []pluginapi.WinID{1, 2, 3}, 1},
		{"close a window that is not focused", func() {
			if err := h.CloseWindow(2); err != nil {
				t.Fatal(err)
			}
		}, []pluginapi.WinID{1, 3}, 1},
		{"close_split closes the focused pane", func() { runAction(t, m, "close_split") }, []pluginapi.WinID{3}, 3},
		{"ids are not reused", func() { runAction(t, m, "split_vertical") }, []pluginapi.WinID{3, 4}, 4},
	}
	for _, s := range steps {
		s.do()
		if got := winIDsOf(m); !slices.Equal(got, s.want) {
			t.Fatalf("%s: windows = %v, want %v", s.name, got, s.want)
		}
		if got := h.CurrentWindow(); got != s.current {
			t.Fatalf("%s: current window = %d, want %d", s.name, got, s.current)
		}
		for _, w := range h.Windows() {
			if w.Active != (w.ID == s.current) {
				t.Fatalf("%s: window %d has Active = %v", s.name, w.ID, w.Active)
			}
		}
	}
}

func TestWindowsFollowTheirBufferWhenATabCloses(t *testing.T) {
	// Windows name tabs by index inside the root; a tab that closes must not change which
	// buffer a window shows.
	m, _ := newWinTabs(t, 3, 5)
	h := m.plug.host
	id, err := h.SplitWindow(1, pluginapi.SplitOptions{Dir: "right", Buffer: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Close(2, false); err != nil { // tab 1 closes; buffer 3 moves from tab 2 to tab 1
		t.Fatal(err)
	}
	if got := winInfo(t, m, id).Buffer; got != 3 {
		t.Fatalf("the window that showed buffer 3 shows %d after another tab closed", got)
	}
	if got := winInfo(t, m, 1).Buffer; got != 1 {
		t.Fatalf("window 1 shows buffer %d, want 1", got)
	}
	if got := winIDsOf(m); !slices.Equal(got, []pluginapi.WinID{1, id}) {
		t.Fatalf("windows = %v", got)
	}
}

func TestWindowsFollowTheirBufferWhenATabMoves(t *testing.T) {
	m, f := newWinTabs(t, 3, 5)
	h := m.plug.host
	id, err := h.SplitWindow(1, pluginapi.SplitOptions{Dir: "right", Buffer: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.MoveTab(3, -2); err != nil { // buffer 3 goes from tab 2 to tab 0
		t.Fatal(err)
	}
	if got := winInfo(t, m, id).Buffer; got != 3 {
		t.Fatalf("the window that showed buffer 3 shows %d after the tab moved", got)
	}
	if got := winInfo(t, m, 1).Buffer; got != 1 || f.active != 1 {
		t.Fatalf("window 1 shows buffer %d with active tab %d, want 1 and tab 1", got, f.active)
	}
}

func TestSplitWindowPlacement(t *testing.T) {
	tests := []struct {
		name      string
		opts      pluginapi.SplitOptions
		dir       splitDir
		newFirst  bool
		wantRatio int
	}{
		{"right", pluginapi.SplitOptions{Dir: "right"}, sideBySide, false, 50},
		{"below with a ratio", pluginapi.SplitOptions{Dir: "below", Ratio: 0.3}, stacked, false, 70},
		{"right before", pluginapi.SplitOptions{Dir: "right", Before: true, Ratio: 0.25}, sideBySide, true, 25},
		{"the ratio is kept inside the pane", pluginapi.SplitOptions{Dir: "right", Ratio: 0.999}, sideBySide, false, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newWinTabs(t, 1, 5)
			band := m.layout().panes[0]
			id, err := m.plug.host.SplitWindow(1, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			root := m.splits.root
			created := root.b
			if tc.newFirst {
				created = root.a
			}
			if root.dir != tc.dir || root.ratio != tc.wantRatio || m.splitByID(uint64(id)) != created {
				t.Fatalf("root dir %v ratio %d new leaf id %d (want %d); want dir %v ratio %d", root.dir, root.ratio, created.id, id, tc.dir, tc.wantRatio)
			}
			// The share of the new window is what the geometry gives it.
			panes := m.layout().panes
			extent := func(p splitPane) int {
				if tc.dir == stacked {
					return p.h
				}
				return p.w
			}
			total := band.w
			if tc.dir == stacked {
				total = band.h
			}
			first, second := splitSpan(total, tc.wantRatio)
			if got := []int{extent(panes[0]), extent(panes[1])}; got[0] != first || got[1] != second {
				t.Fatalf("pane extents = %v, want [%d %d]", got, first, second)
			}
		})
	}
}

func TestSplitWindowFocusAndBuffer(t *testing.T) {
	tests := []struct {
		name       string
		opts       pluginapi.SplitOptions
		wantNewBuf pluginapi.BufferID
		focusNew   bool
		wantActive int // the active tab afterwards
	}{
		{"the new window shows the same buffer, the focus stays", pluginapi.SplitOptions{Dir: "right"}, 1, false, 0},
		{"another buffer in the new window, the focus stays", pluginapi.SplitOptions{Dir: "right", Buffer: 2}, 2, false, 0},
		{"another buffer and the focus", pluginapi.SplitOptions{Dir: "below", Buffer: 2, Focus: true}, 2, true, 1},
		{"the focus without a buffer", pluginapi.SplitOptions{Dir: "below", Focus: true}, 1, true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, f := newWinTabs(t, 2, 5)
			id, err := m.plug.host.SplitWindow(1, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := winInfo(t, m, id).Buffer; got != tc.wantNewBuf {
				t.Errorf("the new window shows buffer %d, want %d", got, tc.wantNewBuf)
			}
			wantCurrent := pluginapi.WinID(1)
			if tc.focusNew {
				wantCurrent = id
			}
			if got := m.plug.host.CurrentWindow(); got != wantCurrent {
				t.Errorf("current window = %d, want %d", got, wantCurrent)
			}
			if f.active != tc.wantActive {
				t.Errorf("active tab = %d, want %d", f.active, tc.wantActive)
			}
			if got := winInfo(t, m, 1).Buffer; got != 1 {
				t.Errorf("the split window shows buffer %d, want 1", got)
			}
		})
	}
}

func TestSplitWindowOfAnotherWindowKeepsTheFocus(t *testing.T) {
	m, f := newWinTabs(t, 2, 5)
	h := m.plug.host
	second, err := h.SplitWindow(1, pluginapi.SplitOptions{Dir: "right", Buffer: 2, Focus: true})
	if err != nil {
		t.Fatal(err)
	}
	// Window 2 has the focus and shows buffer 2; split window 1, which does not.
	third, err := h.SplitWindow(1, pluginapi.SplitOptions{Dir: "below"})
	if err != nil {
		t.Fatal(err)
	}
	if got := winIDsOf(m); !slices.Equal(got, []pluginapi.WinID{1, third, second}) {
		t.Fatalf("windows = %v, want [1 %d %d]", got, third, second)
	}
	if h.CurrentWindow() != second || f.active != 1 {
		t.Fatalf("current %d, active tab %d: the focus must stay on window %d with tab 1", h.CurrentWindow(), f.active, second)
	}
	if winInfo(t, m, third).Buffer != 1 {
		t.Fatalf("the new window shows buffer %d, want 1", winInfo(t, m, third).Buffer)
	}
}

func TestSplitWindowRefusals(t *testing.T) {
	tests := []struct {
		name string
		win  pluginapi.WinID
		opts pluginapi.SplitOptions
		want pluginapi.Code
	}{
		{"unknown window", 99, pluginapi.SplitOptions{Dir: "right"}, pluginapi.CodeClosed},
		{"unknown direction", 1, pluginapi.SplitOptions{Dir: "left"}, pluginapi.CodeInvalid},
		{"ratio of one", 1, pluginapi.SplitOptions{Dir: "right", Ratio: 1}, pluginapi.CodeInvalid},
		{"negative ratio", 1, pluginapi.SplitOptions{Dir: "right", Ratio: -0.5}, pluginapi.CodeInvalid},
		{"unknown buffer", 1, pluginapi.SplitOptions{Dir: "right", Buffer: 9}, pluginapi.CodeClosed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newWinTabs(t, 2, 5)
			_, err := m.plug.host.SplitWindow(tc.win, tc.opts)
			if pluginapi.CodeOf(err) != tc.want {
				t.Fatalf("SplitWindow = %v, want %s", err, tc.want)
			}
			if got := winIDsOf(m); !slices.Equal(got, []pluginapi.WinID{1}) {
				t.Fatalf("a refused split left windows %v", got)
			}
		})
	}
	t.Run("no room", func(t *testing.T) {
		m, _ := newWinTabs(t, 1, 5)
		h := m.plug.host
		cur := pluginapi.WinID(1)
		var err error
		for range 40 {
			var next pluginapi.WinID
			if next, err = h.SplitWindow(cur, pluginapi.SplitOptions{Dir: "below"}); err != nil {
				break
			}
			cur = next
		}
		if pluginapi.CodeOf(err) != pluginapi.CodeLimit {
			t.Fatalf("splitting until the pane is full ended with %v, want limit", err)
		}
		if h.CurrentWindow() != 1 {
			t.Fatalf("the focus moved to %d", h.CurrentWindow())
		}
	})
	t.Run("a refused split of another window restores the focus", func(t *testing.T) {
		m, _ := newWinTabs(t, 1, 5)
		h := m.plug.host
		// Window 1 gets a tiny pane: split below until it is 2 rows, then ask once more.
		cur := pluginapi.WinID(1)
		for {
			next, err := h.SplitWindow(cur, pluginapi.SplitOptions{Dir: "below"})
			if err != nil {
				break
			}
			cur = next
		}
		if _, err := h.SplitWindow(cur, pluginapi.SplitOptions{Dir: "below"}); err == nil {
			t.Fatal("the pane that was refused accepted a split")
		}
		if h.CurrentWindow() != 1 {
			t.Fatalf("the focus is on window %d after a refused split, want 1", h.CurrentWindow())
		}
	})
}

func TestSetWindowBuffer(t *testing.T) {
	m, f := newWinTabs(t, 3, 5)
	h := m.plug.host
	second, err := h.SplitWindow(1, pluginapi.SplitOptions{Dir: "right"}) // focus stays on 1
	if err != nil {
		t.Fatal(err)
	}
	t.Run("a window that is not focused keeps the focus", func(t *testing.T) {
		if err := h.SetWindowBuffer(second, 3); err != nil {
			t.Fatal(err)
		}
		if got := winInfo(t, m, second).Buffer; got != 3 {
			t.Fatalf("window %d shows buffer %d, want 3", second, got)
		}
		if f.active != 0 || h.CurrentWindow() != 1 {
			t.Fatalf("active tab %d, current %d: nothing should move", f.active, h.CurrentWindow())
		}
	})
	t.Run("the focused window changes the active tab", func(t *testing.T) {
		if err := h.SetWindowBuffer(1, 2); err != nil {
			t.Fatal(err)
		}
		if got := winInfo(t, m, 1).Buffer; got != 2 || f.active != 1 {
			t.Fatalf("window 1 shows %d, active tab %d, want 2 and 1", got, f.active)
		}
		if got := winInfo(t, m, second).Buffer; got != 3 {
			t.Fatalf("the other window shows %d, want 3", got)
		}
	})
	t.Run("refusals", func(t *testing.T) {
		if err := h.SetWindowBuffer(99, 1); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
			t.Fatalf("unknown window: %v", err)
		}
		if err := h.SetWindowBuffer(1, 99); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
			t.Fatalf("unknown buffer: %v", err)
		}
	})
	t.Run("a compare view is refused", func(t *testing.T) {
		m, _ := newWinTabs(t, 2, 5)
		if !m.openCompare(0, 1) {
			t.Fatal("no compare view")
		}
		if err := m.plug.host.SetWindowBuffer(m.plug.host.CurrentWindow(), 2); pluginapi.CodeOf(err) != pluginapi.CodeConflict {
			t.Fatalf("SetWindowBuffer in a compare view = %v, want conflict", err)
		}
	})
}

func TestFocusWindow(t *testing.T) {
	m, f := newWinTabs(t, 2, 5)
	h := m.plug.host
	second, _ := h.SplitWindow(1, pluginapi.SplitOptions{Dir: "right", Buffer: 2, Focus: true})
	m.explorerFocus = true
	if err := h.FocusWindow(1); err != nil {
		t.Fatal(err)
	}
	if h.CurrentWindow() != 1 || f.active != 0 || m.explorerFocus {
		t.Fatalf("current %d, active tab %d, explorer focus %v: the window and its tab should be focused", h.CurrentWindow(), f.active, m.explorerFocus)
	}
	if err := h.FocusWindow(second); err != nil || f.active != 1 {
		t.Fatalf("FocusWindow(%d) = %v, active tab %d, want nil and 1", second, err, f.active)
	}
	if err := h.FocusWindow(99); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("FocusWindow(99) = %v, want closed", err)
	}
}

func TestCloseWindow(t *testing.T) {
	m, f := newWinTabs(t, 2, 5)
	h := m.plug.host
	if err := h.CloseWindow(1); pluginapi.CodeOf(err) != pluginapi.CodeConflict {
		t.Fatalf("closing the last window = %v, want conflict", err)
	}
	second, _ := h.SplitWindow(1, pluginapi.SplitOptions{Dir: "right", Buffer: 2, Focus: true})
	third, _ := h.SplitWindow(second, pluginapi.SplitOptions{Dir: "below", Buffer: 1})
	// Focus is on window 2 (tab 1); close window 3, which is not focused.
	if err := h.CloseWindow(third); err != nil {
		t.Fatal(err)
	}
	if h.CurrentWindow() != second || f.active != 1 || len(f.docs) != 2 {
		t.Fatalf("current %d, active tab %d, tabs %d: closing another window must keep the focus and the buffers", h.CurrentWindow(), f.active, len(f.docs))
	}
	// Close the focused window: the neighbour takes the focus, with its own tab.
	if err := h.CloseWindow(second); err != nil {
		t.Fatal(err)
	}
	if h.CurrentWindow() != 1 || f.active != 0 || len(f.docs) != 2 {
		t.Fatalf("current %d, active tab %d, tabs %d after closing the focused window", h.CurrentWindow(), f.active, len(f.docs))
	}
	if err := h.CloseWindow(second); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("closing a closed window = %v, want closed", err)
	}
}

func TestEqualizeAndWindowsAlong(t *testing.T) {
	m, _ := newWinTabs(t, 1, 5)
	h := m.plug.host
	second, _ := h.SplitWindow(1, pluginapi.SplitOptions{Dir: "right"})
	if _, err := h.SplitWindow(second, pluginapi.SplitOptions{Dir: "right", Ratio: 0.2}); err != nil {
		t.Fatal(err)
	}
	m.splits.maximized = true
	if err := h.Equalize(); err != nil {
		t.Fatal(err)
	}
	panes := m.layout().panes
	if m.splits.maximized || len(panes) != 3 {
		t.Fatalf("maximized = %v, %d panes", m.splits.maximized, len(panes))
	}
	for _, p := range panes {
		if d := p.w - panes[0].w; d < -1 || d > 1 {
			t.Fatalf("pane widths are not equal: %v", paneRects(m.layout()))
		}
	}

	leaf := func() *splitNode { return &splitNode{} }
	row := &splitNode{dir: sideBySide, a: leaf(), b: &splitNode{dir: sideBySide, a: leaf(), b: leaf()}}
	column := &splitNode{dir: stacked, a: leaf(), b: leaf()}
	tests := []struct {
		name string
		n    *splitNode
		dir  splitDir
		want int
	}{
		{"a leaf", leaf(), sideBySide, 1},
		{"a row along the row", row, sideBySide, 3},
		{"a column along the row", column, sideBySide, 1},
		{"a column along the column", column, stacked, 2},
		{"a row inside a column along the column", &splitNode{dir: stacked, a: row, b: leaf()}, stacked, 2},
		{"a row inside a column along the row", &splitNode{dir: stacked, a: row, b: leaf()}, sideBySide, 3},
	}
	for _, tc := range tests {
		if got := windowsAlong(tc.n, tc.dir); got != tc.want {
			t.Errorf("%s: windowsAlong = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestScrollWindow(t *testing.T) {
	m, f := newWinTabs(t, 1, 100)
	h := m.plug.host
	second, _ := h.SplitWindow(1, pluginapi.SplitOptions{Dir: "right"}) // window 1 is not focused
	rows := winInfo(t, m, 1).Height
	tests := []struct {
		name    string
		win     pluginapi.WinID
		line    int
		center  bool
		wantTop int
	}{
		{"an unfocused window to a line", second, 40, false, 40},
		{"an unfocused window centered", second, 40, true, 40 - (rows-1)/2},
		{"past the end", second, 500, false, 99},
		{"before the start", second, -3, false, 0},
		{"centered near the start", second, 2, true, 0},
		{"the focused window to a line", 1, 30, false, 30},
		{"the focused window centered", 1, 30, true, 30 - (rows-1)/2},
	}
	for _, tc := range tests {
		if err := h.ScrollWindow(tc.win, tc.line, tc.center); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := winInfo(t, m, tc.win).TopLine
		if got != tc.wantTop {
			t.Errorf("%s: top line = %d, want %d", tc.name, got, tc.wantTop)
		}
	}
	if f.doc().top != 30-(rows-1)/2 {
		t.Errorf("the focused window scrolled the editor to %d", f.doc().top)
	}
	if err := h.ScrollWindow(99, 1, false); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Errorf("ScrollWindow(99) = %v, want closed", err)
	}
}

func TestWindowGeometry(t *testing.T) {
	m, f := newWinTabs(t, 1, 100)
	h := m.plug.host
	l := m.layout()
	p := l.panes[0]
	w := winInfo(t, m, 1)
	if w.X != p.textX || w.Y != p.y || w.Width != p.textW-1 || w.Height != p.h || w.TopLine != 0 || w.LeftCol != 0 {
		t.Fatalf("window = %+v, pane = %+v", w, p)
	}
	if w.Width != 80-p.gutter-1 {
		t.Fatalf("text width %d, want the pane less its gutter %d and the scrollbar column", w.Width, p.gutter)
	}
	f.docs[0].top = 7
	if got := winInfo(t, m, 1).TopLine; got != 7 {
		t.Fatalf("TopLine = %d after the editor scrolled to 7", got)
	}

	// Two windows side by side do not overlap, and each reports its own top. The split
	// scrolls the caret into view, as the split actions do, so both start at the caret.
	second, _ := h.SplitWindow(1, pluginapi.SplitOptions{Dir: "right"})
	_ = h.ScrollWindow(1, 20, false)
	a, b := winInfo(t, m, 1), winInfo(t, m, second)
	if a.X+a.Width >= b.X || a.Y != b.Y || a.TopLine != 20 || b.TopLine != 0 || !a.Active || b.Active {
		t.Fatalf("windows = %+v and %+v", a, b)
	}
	panes := m.layout().panes
	if a.Width != panes[0].textW-1 || b.X != panes[1].textX {
		t.Fatalf("windows %+v %+v disagree with the panes %+v %+v", a, b, panes[0], panes[1])
	}
}

func TestWindowLeftColFollowsTheHorizontalScroll(t *testing.T) {
	m, f := newWinTabs(t, 1, 3)
	m.view.lineWrap = false
	f.docs[0].lines[0] = strings.Repeat("0123456789", 20)
	f.docs[0].cursor = buffer.Pos{Col: 150}
	l := m.layout()
	p := l.panes[0]
	w := winInfo(t, m, 1)
	left, _ := m.paneGeometry(p, m.ed.Active()).HScroll(p.w, p.h)
	if left == 0 || w.LeftCol != left {
		t.Fatalf("LeftCol = %d, the pane scrolled to %d (want a positive column)", w.LeftCol, left)
	}
	// The horizontal bar takes the last row, so the window has one row less than the pane.
	if w.Height != p.h-1 {
		t.Fatalf("Height = %d, want %d: a window height counts text rows", w.Height, p.h-1)
	}
}

// foldDoc is a fake document with indentation folds, as *editor.Document has.
type foldDoc struct {
	*fakeDoc
	ranges []fold.Range
	folded map[int]bool
}

func (d foldDoc) Folds(int) []fold.Range { return d.ranges }
func (d foldDoc) Folded(line int) bool   { return d.folded[line] }

// foldEditor is a multiFake whose documents have folds.
type foldEditor struct {
	*multiFake
	ranges []fold.Range
	folded map[int]bool
}

func (e foldEditor) wrap(d *fakeDoc) Document {
	return foldDoc{fakeDoc: d, ranges: e.ranges, folded: e.folded}
}
func (e foldEditor) Active() Document { return e.wrap(e.docs[e.active]) }
func (e foldEditor) DocAt(i int) Document {
	if i < 0 || i >= len(e.docs) {
		return nil
	}
	return e.wrap(e.docs[i])
}

func TestVisibleRowsWithWrapAndFold(t *testing.T) {
	long := strings.Repeat("abcde ", 12) // 72 columns: three rows in a pane of this width
	f := newDocsFake([]string{"func a() {", "\tx := 1", "\ty := 2", "}", long, "end"})
	ed := foldEditor{multiFake: f, ranges: []fold.Range{{Start: 0, End: 2}}, folded: map[int]bool{0: true}}
	m := newWinModel(t, ed, f)
	m.view.lineWrap = true
	h := m.plug.host
	second, err := h.SplitWindow(1, pluginapi.SplitOptions{Dir: "right", Focus: true}) // a pane of 39 columns
	if err != nil {
		t.Fatal(err)
	}
	want := []pluginapi.VisibleRow{
		{Line: 0, ColFrom: 0, ColTo: 10},                 // the header of the collapsed fold; lines 1 and 2 are hidden
		{Line: 3, ColFrom: 0, ColTo: 1},                  //
		{Line: 4, ColFrom: 0, ColTo: 30},                 // a wrapped line takes three rows
		{Line: 4, ColFrom: 30, ColTo: 60, Wrapped: true}, //
		{Line: 4, ColFrom: 60, ColTo: 72, Wrapped: true}, //
		{Line: 5, ColFrom: 0, ColTo: 3},                  //
	}
	for _, id := range []pluginapi.WinID{1, second} {
		got, err := h.VisibleRows(id)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, want) {
			t.Errorf("window %d: VisibleRows = %+v, want %+v", id, got, want)
		}
	}
	// Expanding the fold lists its lines again.
	ed.folded[0] = false
	got, _ := h.VisibleRows(second)
	if len(got) != 8 || got[1].Line != 1 || got[2].Line != 2 {
		t.Errorf("with the fold open: %+v", got)
	}
	if _, err := h.VisibleRows(99); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Errorf("VisibleRows(99) = %v, want closed", err)
	}
}

func TestVisibleRowsMatchPainterRows(t *testing.T) {
	long := strings.Repeat("word ", 30)
	tests := []struct {
		name  string
		lines []string
		wrap  bool
		top   int
	}{
		{"wrap", []string{"first", long, "last"}, true, 0},
		{"wrap, scrolled", []string{"first", long, "last", long}, true, 1},
		{"no wrap", []string{"first", long, "last"}, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newDocsFake(tc.lines)
			f.docs[0].top = tc.top
			m := newWinModel(t, f, f)
			m.view.lineWrap = tc.wrap
			rows, err := m.plug.host.VisibleRows(1)
			if err != nil || len(rows) == 0 {
				t.Fatalf("VisibleRows = %v, %v", rows, err)
			}
			l := m.layout()
			p := l.panes[0]
			doc := m.paneDoc(p)
			pane := m.paneFor(p, doc)
			g := pane.Render(p.w, p.h)
			for y, r := range rows {
				var painted strings.Builder
				for x := p.gutter; x < p.w-1; x++ {
					painted.WriteString(g.At(x, y).Text)
				}
				text := strings.TrimRight(tc.lines[r.Line][r.ColFrom:r.ColTo], " ")
				if got := strings.TrimRight(painted.String(), " "); got != text {
					t.Errorf("row %d (line %d [%d,%d)) is painted as %q", y, r.Line, r.ColFrom, r.ColTo, got)
				}
				// The hit map agrees: the first cell of the row is the start of the span.
				if hit := pane.PosAt(p.w, p.h, p.gutter, y); hit.Line != r.Line || hit.Col != r.ColFrom {
					t.Errorf("row %d: the hit map says %+v, the list says line %d col %d", y, hit, r.Line, r.ColFrom)
				}
			}
			// The first body row past the list is a "~" row or the horizontal bar.
			if n := len(rows); n < p.h {
				if cell := g.At(0, n); cell.Text != "~" && !tc.wrap && n < p.h-1 {
					t.Errorf("row %d is %q, want a ~ row after the %d listed", n, cell.Text, n)
				}
			}
		})
	}
}

func TestWindowsWithoutAnEditorPortAreUnsupported(t *testing.T) {
	m, _ := newPluginModel(t, newFakeRuntime(t))
	ws := m.plug.host.Windows()
	if len(ws) != 1 || ws[0].ID != 1 || ws[0].Buffer != 0 || !ws[0].Active {
		t.Fatalf("Windows = %+v, want the one window with no buffer", ws)
	}
	if err := m.plug.host.SetWindowBuffer(1, 1); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("SetWindowBuffer without a port = %v, want closed", err)
	}
}
