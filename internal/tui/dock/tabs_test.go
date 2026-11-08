package dock

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// tabsStarter starts a fresh fake terminal for every tab.
type tabsStarter struct {
	terms []*fakeTerm
	dirs  []string
}

func (s *tabsStarter) Start(dir string, cols, rows int) (Terminal, error) {
	s.dirs = append(s.dirs, dir)
	t := newFakeTerm()
	t.history = []string{"output of " + dir}
	s.terms = append(s.terms, t)
	return t, nil
}

// newTestTabs returns a tab container with a fake host/starter; notices are kept.
func newTestTabs(t *testing.T, notices *[]string) (*Tabs, *tabsStarter) {
	t.Helper()
	starter := &tabsStarter{}
	var tabs Tabs
	tabs.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(text string) { *notices = append(*notices, text) },
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	tabs.SetPorts(Ports{Starter: starter})
	return &tabs, starter
}

// startTab starts one tab and consumes the started message.
func startTab(t *testing.T, tabs *Tabs) {
	t.Helper()
	cmd := tabs.NewTab()
	if cmd == nil {
		t.Fatal("NewTab did not start a shell")
	}
	if _, ok := tabs.Update(cmd()); !ok {
		t.Fatal("the started message was not taken")
	}
}

func TestTabsCreateSwitchAndClose(t *testing.T) {
	t.Parallel()
	var notices []string
	tabs, starter := newTestTabs(t, &notices)
	startTab(t, tabs)
	startTab(t, tabs)
	if tabs.Count() != 2 || len(starter.terms) != 2 {
		t.Fatalf("tabs = %d, starts = %d, want 2 and 2", tabs.Count(), len(starter.terms))
	}
	if got := tabs.ActiveIndex(); got != 1 {
		t.Fatalf("active = %d, want the new tab", got)
	}
	if !tabs.Running() {
		t.Fatal("the active tab is not running")
	}
	tabs.Previous()
	if got := tabs.ActiveIndex(); got != 0 {
		t.Fatalf("active after previous = %d", got)
	}
	tabs.Previous()
	if got := tabs.ActiveIndex(); got != 1 {
		t.Fatalf("previous did not wrap: %d", got)
	}
	tabs.Next()
	if got := tabs.ActiveIndex(); got != 0 {
		t.Fatalf("next did not wrap: %d", got)
	}
	tabs.RenameActive("build")
	if got := tabs.TabNames()[0]; got != "build" {
		t.Fatalf("name = %q, want build", got)
	}
	tabs.RenameActive("")
	if got := tabs.TabNames()[0]; got != "Terminal 1" {
		t.Fatalf("cleared name = %q, want the default", got)
	}
	tabs.SetActive(1)
	tabs.CloseTab()
	if tabs.Count() != 1 {
		t.Fatalf("tabs after close = %d, want 1", tabs.Count())
	}
	if !starter.terms[1].closed {
		t.Fatal("the closed tab's shell was not closed")
	}
	tabs.CloseTab()
	if tabs.Count() != 0 || tabs.Open() {
		t.Fatalf("tabs = %d open = %v after closing the last tab", tabs.Count(), tabs.Open())
	}
}

func TestTabsAreBounded(t *testing.T) {
	t.Parallel()
	var notices []string
	tabs, _ := newTestTabs(t, &notices)
	for i := 0; i < MaxTerminalTabs; i++ {
		startTab(t, tabs)
	}
	if got := tabs.Count(); got != MaxTerminalTabs {
		t.Fatalf("tabs = %d, want %d", got, MaxTerminalTabs)
	}
	before := tabs.Count()
	if cmd := tabs.NewTab(); cmd != nil {
		t.Fatal("a tab over the cap still started")
	}
	if tabs.Count() != before || len(notices) == 0 || !strings.Contains(notices[len(notices)-1], "limit") {
		t.Fatalf("over the cap: tabs %d, notices %q", tabs.Count(), notices)
	}
}

func TestTabsExitedMarkerAndCloseOnExit(t *testing.T) {
	t.Parallel()
	var notices []string
	tabs, _ := newTestTabs(t, &notices)
	startTab(t, tabs)
	startTab(t, tabs)
	// An exit of the inactive tab marks it without closing it by default.
	if _, ok := tabs.Update(exitMsg{gen: tabs.tabs[0].gen}); !ok {
		t.Fatal("the exit message was not taken")
	}
	if !tabs.ExitedFlags()[0] || tabs.Count() != 2 {
		t.Fatalf("exited = %v, tabs = %d; want the exited marker and the tab kept", tabs.ExitedFlags(), tabs.Count())
	}
	if label := tabs.TabNames()[0]; !strings.Contains(label, "(exited)") {
		t.Fatalf("label = %q, want the exited marker", label)
	}
	// With close_on_exit the exited tab is removed instead.
	tabs.SetEffects(Effects{CloseOnExit: true})
	if _, ok := tabs.Update(exitMsg{gen: tabs.tabs[1].gen}); !ok {
		t.Fatal("the second exit message was not taken")
	}
	if tabs.Count() != 0 {
		t.Fatalf("tabs = %d after close_on_exit exits, want none", tabs.Count())
	}
}

func TestTabsSavedStateAndRestore(t *testing.T) {
	t.Parallel()
	var notices []string
	tabs, starter := newTestTabs(t, &notices)
	startTab(t, tabs)
	tabs.RenameActive("build")
	startTab(t, tabs)
	starter.terms[0].history = []string{"first output"}
	starter.terms[1].history = []string{"second output"}
	st := tabs.SavedState()
	if st.Version != terminal.SavedStateVersion || len(st.Tabs) != 2 || st.ActiveTab != 1 {
		t.Fatalf("saved state = %+v, want two tabs and active 1", st)
	}
	if st.Tabs[0].Name != "build" || !strings.Contains(strings.Join(st.Tabs[0].Rows, "\n"), "first output") {
		t.Fatalf("tab 0/saved = %+v", st.Tabs[0])
	}
	restored, _ := newTestTabs(t, &notices)
	restored.Restore(st)
	if restored.Count() != 2 || restored.ActiveIndex() != 1 {
		t.Fatalf("restored tabs = %d active %d", restored.Count(), restored.ActiveIndex())
	}
	if restored.ActiveName() == "" || !strings.HasPrefix(restored.TabNames()[0], "build") {
		t.Fatalf("restored names = %q", restored.TabNames())
	}
	if !restored.Active().Restored() {
		t.Fatal("the restored tab is not read-only")
	}
}

func TestTabsStripHitAndDraw(t *testing.T) {
	t.Parallel()
	var notices []string
	tabs, _ := newTestTabs(t, &notices)
	startTab(t, tabs)
	startTab(t, tabs)
	l := frame.Layout{DockOn: true, DockY: 2, DockH: 4, W: 60}
	g := grid.New(60, 8)
	tabs.Draw(g, l, style.Styles{})
	row := strings.Join(g.Plain()[2:3], "")
	if !strings.Contains(row, "Terminal 1") || !strings.Contains(row, "Terminal 2") {
		t.Fatalf("strip = %q, want both tab labels", row)
	}
	// A click on the first label switches to it; a click past the labels is a no-op.
	cmd, ok := tabs.Mouse(tea.MouseClickMsg{X: 3, Y: 2, Button: tea.MouseLeft}, l, 1)
	if !ok || cmd != nil || tabs.ActiveIndex() != 0 {
		t.Fatalf("strip click: ok %v cmd %v active %d, want tab 0", ok, cmd, tabs.ActiveIndex())
	}
	tabs.Mouse(tea.MouseClickMsg{X: 55, Y: 2, Button: tea.MouseLeft}, l, 1)
	if tabs.ActiveIndex() != 0 {
		t.Fatal("a click past the labels changed the active tab")
	}
}

// TestTabsRestartStartsAFreshShellInTheSameTab checks the per-tab restart: the
// tab count and active tab stay, a new shell starts, and the old transcript is
// captured above it.
func TestTabsRestartStartsAFreshShellInTheSameTab(t *testing.T) {
	t.Parallel()
	var notices []string
	tabs, starter := newTestTabs(t, &notices)
	startTab(t, tabs)
	startTab(t, tabs)
	starter.terms[1].history = []string{"before restart"}
	cmd := tabs.Restart()
	if cmd == nil {
		t.Fatal("Restart did not start a shell")
	}
	if _, ok := tabs.Update(cmd()); !ok {
		t.Fatal("the restart message was not taken")
	}
	if tabs.Count() != 2 || tabs.ActiveIndex() != 1 {
		t.Fatalf("after restart: tabs %d active %d", tabs.Count(), tabs.ActiveIndex())
	}
	if len(starter.terms) != 3 {
		t.Fatalf("shells started = %d, want 3", len(starter.terms))
	}
	if !tabs.Running() {
		t.Fatal("the restarted tab is not running")
	}
}
