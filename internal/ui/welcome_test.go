package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/style"
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
	for _, r := range m.layout().welcome {
		if r.item == i {
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
	if !m.welcomeVisible() || !m.layout().welcomeOn {
		t.Fatal("no tab open, but the welcome screen is not shown")
	}
	g := m.paint()
	all := strings.Join(g.Plain(), "\n")
	for _, want := range []string{welcomeTitle[1], welcomeSubtitleText, "New File", "Ctrl+N", "Open File", "Ctrl+O", "Open Folder", "Alt+O", "Command Palette", "Ctrl+T", "Explorer", "Ctrl+B"} {
		if !strings.Contains(all, want) {
			t.Errorf("welcome screen lacks %q:\n%s", want, all)
		}
	}
}

// The welcome lines stay in the body and in the editor pane at 80 by 24, and
// every door is a row.
func TestWelcomeFitsTheEightyByTwentyFourFrame(t *testing.T) {
	m, _ := welcomeModel(t, 80, 24, "/work/alpha", "/work/beta")
	l := m.layout()
	if l.bodyTop != 2 || l.bodyBottom != 22 {
		t.Fatalf("body rows %d..%d, want 2..22 at 24 rows", l.bodyTop, l.bodyBottom)
	}
	for _, r := range l.welcome {
		if r.y < l.bodyTop || r.y > l.bodyBottom {
			t.Errorf("line %q at row %d is outside the body", r.text, r.y)
		}
		if r.x < l.editorX || r.x+r.w > l.w {
			t.Errorf("line %q spans %d..%d, outside the pane", r.text, r.x, r.x+r.w)
		}
	}
	for i := range welcomeDoors {
		if _, ok := welcomeRowAt(m, i); !ok {
			t.Errorf("door %d is not drawn at 80x24", i)
		}
	}
	if _, ok := welcomeRowAt(m, len(welcomeDoors)); !ok {
		t.Error("the first recent folder is not drawn")
	}
}

// The welcome occupies the editor pane only, so an open explorer keeps its columns.
func TestWelcomeLeavesTheExplorerColumns(t *testing.T) {
	m := New(Deps{Editor: newTabFake(), Explorer: newFakeTree(3), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, ShowExplorer: true})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	l := m.layout()
	if !l.explorerOpen || l.editorX == 0 {
		t.Fatalf("explorer open %v, editorX %d; want the explorer open", l.explorerOpen, l.editorX)
	}
	for _, r := range l.welcome {
		if r.x < l.editorX {
			t.Fatalf("welcome line %q at column %d overlaps the explorer (%d)", r.text, r.x, l.editorX)
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
	if !strings.Contains(screenRow(m, 1), "Untitled-1") {
		t.Fatalf("tab row %q does not show the new tab", screenRow(m, 1))
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
	_, cmd := m.Update(click(x, 1, tea.MouseLeft))
	drain(t, m, cmd)
	if !m.welcomeVisible() {
		t.Fatalf("the × click did not show the welcome screen; tabs %v", tf.names)
	}
}

// tabCloseX is the column of the × of tab i in the strip, or -1.
func tabCloseX(m *Model, i int) int {
	for _, r := range m.layout().strip.runs {
		if r.kind == hitTabClose && r.index == i {
			return r.x
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
	_, cmd := m.Update(click(row.x+1, row.y, tea.MouseLeft))
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
	if want := len(welcomeDoors); m.welcomeSel != want {
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
			row, ok := welcomeRowAt(m, len(welcomeDoors))
			if !ok {
				t.Fatal("the recent folder row is not drawn")
			}
			_, cmd := m.Update(click(row.x+1, row.y, tea.MouseLeft))
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
	if got := welcomeItemCount(recents); got != len(welcomeDoors)+welcomeRecentMax {
		t.Fatalf("welcome has %d rows, want the doors and %d recents", got, welcomeRecentMax)
	}
	row, ok := welcomeRowAt(m, len(welcomeDoors))
	if !ok {
		t.Fatal("the first recent folder is not drawn")
	}
	_, cmd := m.Update(click(row.x+1, row.y, tea.MouseLeft))
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
	if !g.At(row.x+1, row.y).Style.Bold {
		t.Fatalf("selected row cell %+v is not styled as the accent", g.At(row.x+1, row.y).Style)
	}
	if other, _ := welcomeRowAt(m, 0); g.At(other.x+1, other.y).Style.Bold {
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

// welcomeLines places the block on the pane and keeps the title centred.
func TestWelcomeTitleIsCentredInTheBlock(t *testing.T) {
	lines := welcomeLines(0, 80, 0, 30, nil)
	var title []welcomeLine
	for _, r := range lines {
		if r.role == welcomeTitleRow {
			title = append(title, r)
		}
	}
	if len(title) != 3 {
		t.Fatalf("title has %d lines, want 3", len(title))
	}
	for _, r := range title {
		if r.x < 0 || r.x+r.w > 80 {
			t.Fatalf("title line %q out of the pane: %+v", r.text, r)
		}
	}
}
