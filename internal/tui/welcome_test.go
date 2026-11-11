package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// welcomeModel is a model with no tab open in a w by h frame.
func welcomeModel(t *testing.T, w, h int, recents ...string) (*Model, *tabFake) {
	t.Helper()
	tf := newTabFake()
	m := New(Deps{Editor: tf, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Recents: recents})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m, tf
}

// welcomeRowAt returns the welcome line of item i, or false when it is not drawn.
func welcomeRowAt(m *Model, i int) (welcomeLine, bool) {
	for _, r := range m.layout().Welcome {
		if r.Item == i {
			return r, true
		}
	}
	return welcomeLine{}, false
}

// screenRow returns row y of the painted frame as text.
func screenRow(m *Model, y int) string {
	return m.paint().Plain()[y]
}

func TestWelcomeShowsWhenNoTabIsOpen(t *testing.T) {
	m, _ := welcomeModel(t, 80, 24)
	if !m.welcomeVisible() || !m.layout().WelcomeOn {
		t.Fatal("no tab open, but the welcome screen is not shown")
	}
	g := m.paint()
	all := strings.Join(g.Plain(), "\n")
	for _, want := range []string{frame.WelcomeTitle[1], frame.WelcomeSubtitleText, "New File", "Open File", "Open Folder", "Command Palette", "Explorer"} {
		if !strings.Contains(all, want) {
			t.Errorf("welcome screen lacks %q:\n%s", want, all)
		}
	}
	// A keymap that cannot list its bindings gives no keys: the doors do not carry the
	// keys of one preset.
	for _, bad := range []string{"Ctrl+N", "Ctrl+O", "Alt+O", "Ctrl+T", "Ctrl+B"} {
		if strings.Contains(all, bad) {
			t.Errorf("welcome screen shows the fixed key %q:\n%s", bad, all)
		}
	}
}

// The welcome lines stay in the body and in the editor pane at 80 by 24, and
// every door is a row.
func TestWelcomeFitsTheEightyByTwentyFourFrame(t *testing.T) {
	m, _ := welcomeModel(t, 80, 24, "/work/alpha", "/work/beta")
	l := m.layout()
	if l.BodyTop != 3 || l.BodyBottom != 21 {
		t.Fatalf("body rows %d..%d, want 3..21 at 24 rows", l.BodyTop, l.BodyBottom)
	}
	for _, r := range l.Welcome {
		if r.Y < l.BodyTop || r.Y > l.BodyBottom {
			t.Errorf("line %q at row %d is outside the body", r.Text, r.Y)
		}
		if r.X < l.EditorX || r.X+r.W > l.W {
			t.Errorf("line %q spans %d..%d, outside the pane", r.Text, r.X, r.X+r.W)
		}
	}
	for i := range frame.WelcomeDoors {
		if _, ok := welcomeRowAt(m, i); !ok {
			t.Errorf("door %d is not drawn at 80x24", i)
		}
	}
	if _, ok := welcomeRowAt(m, len(frame.WelcomeDoors)); !ok {
		t.Error("the first recent folder is not drawn")
	}
}

// The welcome occupies the editor pane only, so an open explorer keeps its columns.
func TestWelcomeLeavesTheExplorerColumns(t *testing.T) {
	m := New(Deps{Editor: newTabFake(), Explorer: newFakeTree(3), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, ShowExplorer: true})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	l := m.layout()
	if !l.ExplorerOpen || l.EditorX == 0 {
		t.Fatalf("explorer open %v, editorX %d; want the explorer open", l.ExplorerOpen, l.EditorX)
	}
	for _, r := range l.Welcome {
		if r.X < l.EditorX {
			t.Fatalf("welcome line %q at column %d overlaps the explorer (%d)", r.Text, r.X, l.EditorX)
		}
	}
}

// Ctrl+N replaces the welcome screen with an untitled tab.
func TestNewTabHidesTheWelcomeScreen(t *testing.T) {
	m, tf := welcomeModel(t, 80, 24)
	press(m, ctrl('n'))
	if m.welcomeVisible() {
		t.Fatal("the welcome screen is still shown after Ctrl+N")
	}
	if len(tf.names) != 1 || tf.names[0] != "Untitled-1" {
		t.Fatalf("tabs = %v, want one untitled tab", tf.names)
	}
	if strip := m.layout().StripY; !strings.Contains(screenRow(m, strip), "Untitled-1") {
		t.Fatalf("tab row %q does not show the new tab", screenRow(m, strip))
	}
}

// Closing the last tab, with Ctrl+W on a clean tab or with the × button, brings
// the welcome screen back.
func TestClosingTheLastTabShowsTheWelcomeScreenAgain(t *testing.T) {
	m, tf := welcomeModel(t, 80, 24)
	tf.names, tf.dirty = []string{"a.txt"}, []bool{false}
	tf.sync()
	if m.welcomeVisible() {
		t.Fatal("a tab is open, but the welcome screen is shown")
	}
	press(m, ctrl('w'))
	if !m.welcomeVisible() {
		t.Fatalf("Ctrl+W on the last clean tab did not show the welcome screen; tabs %v", tf.names)
	}

	m, tf = welcomeModel(t, 80, 24)
	tf.names, tf.dirty = []string{"b.txt"}, []bool{false}
	tf.sync()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	x := tabCloseX(m, 0)
	if x < 0 {
		t.Fatal("the × of the only tab is not drawn")
	}
	_, cmd := m.Update(click(x, m.layout().StripY, tea.MouseLeft))
	drain(t, m, cmd)
	if !m.welcomeVisible() {
		t.Fatalf("the × click did not show the welcome screen; tabs %v", tf.names)
	}
}

// tabCloseX is the column of the × of tab i in the strip, or -1.
func tabCloseX(m *Model, i int) int {
	for _, r := range m.layout().Strip.Runs {
		if r.Kind == hitTabClose && r.Index == i {
			return r.X
		}
	}
	return -1
}

// A click on the "New File" row opens an untitled tab.
func TestClickingNewFileRowOpensAnUntitledTab(t *testing.T) {
	m, tf := welcomeModel(t, 80, 24)
	row, ok := welcomeRowAt(m, 0)
	if !ok {
		t.Fatal("New File row is not drawn")
	}
	_, cmd := m.Update(click(row.X+1, row.Y, tea.MouseLeft))
	drain(t, m, cmd)
	if len(tf.names) != 1 || tf.names[0] != "Untitled-1" {
		t.Fatalf("tabs = %v after clicking New File", tf.names)
	}
	if m.welcomeVisible() {
		t.Fatal("the welcome screen is still shown after New File")
	}
}

// Up and Down move the highlight over the rows, and Enter runs the highlighted one.
func TestWelcomeKeysMoveTheHighlightAndRun(t *testing.T) {
	m, tf := welcomeModel(t, 80, 24)
	press(m, special(tea.KeyUp))
	if m.welcomeSel != 0 {
		t.Fatalf("Up at the first row moved the highlight to %d", m.welcomeSel)
	}
	press(m, special(tea.KeyDown))
	press(m, special(tea.KeyDown))
	if m.welcomeSel != 2 {
		t.Fatalf("two Down presses left the highlight at %d", m.welcomeSel)
	}
	press(m, special(tea.KeyUp))
	if m.welcomeSel != 1 {
		t.Fatalf("Up moved the highlight to %d, want 1", m.welcomeSel)
	}
	press(m, special(tea.KeyEnter))
	if len(m.overlays) != 1 {
		t.Fatalf("Enter on Open File opened %d overlays, want the file browser", len(m.overlays))
	}
	if len(tf.names) != 0 {
		t.Fatalf("Enter on Open File opened a tab: %v", tf.names)
	}
}

// Down stops at the last row, and Enter on the last row runs it.
func TestWelcomeHighlightStopsAtTheLastRow(t *testing.T) {
	m, _ := welcomeModel(t, 80, 24, "/work/alpha")
	for range 10 {
		press(m, special(tea.KeyDown))
	}
	if want := len(frame.WelcomeDoors); m.welcomeSel != want {
		t.Fatalf("highlight = %d, want the last row %d", m.welcomeSel, want)
	}
}

// welcomeProjectModel is a welcome screen over the project port p, which may be nil,
// with the recent folders recents. The explorer roots at /ws.
func welcomeProjectModel(t *testing.T, p *fakeProjects, recents ...string) *Model {
	t.Helper()
	ports := RootPorts{}
	if p != nil {
		ports.Projects = p
	}
	m := New(Deps{Editor: newTabFake(), Explorer: newFakeTree(3), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Recents: recents, Root: ports})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

// A click on a recent folder runs the switch-project flow for that folder, the one the
// open_folder action reaches through the browser: the port opens it and its tree
// becomes the root. Without a port the flow shows the Switch Project notice.
func TestRecentFolderRowSwitchesTheProject(t *testing.T) {
	t.Parallel()
	const folder = "/work/alpha"
	tests := []struct {
		name       string
		projects   *fakeProjects // nil leaves the Projects port out
		wantNotice string        // the notice when the port is out
		wantRoot   string        // the explorer root after the click
	}{
		{
			name:       "without a projects port the notice says so",
			wantNotice: "Switch Project is not supported in this port yet",
			wantRoot:   "/ws",
		},
		{
			name:     "with a projects port the folder becomes the root",
			projects: &fakeProjects{project: Project{Tree: treeAt(folder, 2)}},
			wantRoot: folder,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := welcomeProjectModel(t, tc.projects, folder)
			row, ok := welcomeRowAt(m, len(frame.WelcomeDoors))
			if !ok {
				t.Fatal("the recent folder row is not drawn")
			}
			_, cmd := m.Update(click(row.X+1, row.Y, tea.MouseLeft))
			drain(t, m, cmd)
			if got := m.rootDir(); got != tc.wantRoot {
				t.Fatalf("explorer root = %q, want %q", got, tc.wantRoot)
			}
			if tc.projects == nil {
				if m.notice != tc.wantNotice {
					t.Fatalf("notice = %q, want %q", m.notice, tc.wantNotice)
				}
				return
			}
			if !slices.Equal(tc.projects.opened, []string{folder}) {
				t.Fatalf("the port opened %v, want [%s]", tc.projects.opened, folder)
			}
		})
	}
}

// Recent folders are listed up to five. Without a projects port, a click on one shows
// the notice that Switch Project is not supported.
func TestRecentFoldersAreCappedAndNoticeWithoutProjects(t *testing.T) {
	recents := []string{"/w/1", "/w/2", "/w/3", "/w/4", "/w/5", "/w/6"}
	m, tf := welcomeModel(t, 80, 24, recents...)
	if got := frame.WelcomeItemCount(recents); got != len(frame.WelcomeDoors)+frame.WelcomeRecentMax {
		t.Fatalf("welcome has %d rows, want the doors and %d recents", got, frame.WelcomeRecentMax)
	}
	row, ok := welcomeRowAt(m, len(frame.WelcomeDoors))
	if !ok {
		t.Fatal("the first recent folder is not drawn")
	}
	_, cmd := m.Update(click(row.X+1, row.Y, tea.MouseLeft))
	drain(t, m, cmd)
	if len(tf.names) != 0 || !strings.Contains(m.notice, "not supported") {
		t.Fatalf("clicking a recent folder: tabs %v, notice %q", tf.names, m.notice)
	}
}

// The welcome screen is drawn with the accent on the selected row.
func TestWelcomeHighlightUsesTheAccent(t *testing.T) {
	m, _ := welcomeModel(t, 80, 24)
	m.styles = style.Styles{Accent: lipgloss.NewStyle().Bold(true)}
	press(m, special(tea.KeyDown))
	row, _ := welcomeRowAt(m, 1)
	g := m.paint()
	if !g.At(row.X+1, row.Y).Style.Bold {
		t.Fatalf("selected row cell %+v is not styled as the accent", g.At(row.X+1, row.Y).Style)
	}
	if other, _ := welcomeRowAt(m, 0); g.At(other.X+1, other.Y).Style.Bold {
		t.Fatal("an unselected row takes the selection style")
	}
}

// A very short frame drops the rows that do not fit, and does not panic.
func TestWelcomeInAShortFrameDoesNotPanic(t *testing.T) {
	for h := 3; h <= 12; h++ {
		m, _ := welcomeModel(t, 30, h)
		_ = m.paint()
	}
}

// A narrow pane drops the wordmark and any key it cannot show whole instead of
// clipping them; the doors and the subtitle stay.
func TestWelcomeCompactsOnANarrowPane(t *testing.T) {
	t.Parallel()
	keys := []string{"Ctrl+N", "Ctrl+O", "", "Ctrl+P", "Ctrl+B"}
	const x0, paneW = 0, 24
	lines := frame.WelcomeLines(x0, paneW, 0, 14, nil, keys)
	var title, subtitle, doors int
	for _, l := range lines {
		switch l.Role {
		case welcomeTitleRow:
			title++
		case welcomeSubtitle:
			subtitle++
		case welcomeDoorRow:
			doors++
		}
		if l.W <= 0 {
			continue
		}
		if l.X < x0 || l.X+l.W > x0+paneW {
			t.Errorf("line %q leaves the %d-cell pane: %+v", l.Text, paneW, l)
		}
		if l.Role == welcomeDoorRow && l.Key != "" {
			t.Errorf("door %q shows the cut key %q", l.Text, l.Key)
		}
	}
	if title != 0 {
		t.Errorf("a %d-cell pane drew %d wordmark lines", paneW, title)
	}
	if subtitle != 1 || doors != len(frame.WelcomeDoors) {
		t.Fatalf("subtitle %d doors %d, want 1 and %d", subtitle, doors, len(frame.WelcomeDoors))
	}
	// A wide pane keeps the wordmark and the whole shortcuts.
	wide := frame.WelcomeLines(0, 80, 0, 30, nil, keys)
	var sawTitle, sawKey bool
	for _, l := range wide {
		sawTitle = sawTitle || l.Role == welcomeTitleRow
		sawKey = sawKey || (l.Role == welcomeDoorRow && l.Key == "Ctrl+N")
	}
	if !sawTitle || !sawKey {
		t.Errorf("wide welcome: title %v, Ctrl+N key %v", sawTitle, sawKey)
	}
}

// At a very narrow pane the labels truncate and the keys go, but nothing spills
// past the pane.
func TestWelcomeNeverSpillsAtTinyWidths(t *testing.T) {
	t.Parallel()
	for paneW := 4; paneW <= 24; paneW++ {
		lines := frame.WelcomeLines(0, paneW, 0, 12, []string{"/ws/001"}, []string{"Ctrl+N", "Ctrl+O", "", "Ctrl+P", "Ctrl+B"})
		for _, l := range lines {
			if l.W <= 0 {
				continue
			}
			if l.X < 0 || l.X+l.W > paneW {
				t.Errorf("pane %d: line %q leaves it: %+v", paneW, l.Text, l)
			}
			if l.Key != "" && l.Role == welcomeDoorRow {
				t.Errorf("pane %d: a door key survived at %q", paneW, l.Key)
			}
		}
	}
}

// frame.WelcomeLines places the block on the pane and keeps the title centred.
func TestWelcomeTitleIsCentredInTheBlock(t *testing.T) {
	lines := frame.WelcomeLines(0, 80, 0, 30, nil, nil)
	var title []welcomeLine
	for _, r := range lines {
		if r.Role == welcomeTitleRow {
			title = append(title, r)
		}
	}
	if len(title) != 3 {
		t.Fatalf("title has %d lines, want 3", len(title))
	}
	for _, r := range title {
		if r.X < 0 || r.X+r.W > 80 {
			t.Fatalf("title line %q out of the pane: %+v", r.Text, r)
		}
	}
}
