package dock

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// tabsStarter starts a fresh fake terminal for every tab. It also launches
// programs, so an agent tab can restart and rejoin its record.
type tabsStarter struct {
	terms    []*fakeTerm
	dirs     []string
	launches []terminal.Launch
}

func (s *tabsStarter) Start(dir string, cols, rows int) (Terminal, error) {
	s.dirs = append(s.dirs, dir)
	t := newFakeTerm()
	t.history = []string{"output of " + dir}
	s.terms = append(s.terms, t)
	return t, nil
}

// StartLaunch starts a program (Run Agent/restart of a launched terminal).
func (s *tabsStarter) StartLaunch(launch terminal.Launch, dir string, cols, rows int) (Terminal, error) {
	s.launches = append(s.launches, launch)
	return s.Start(dir, cols, rows)
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

// setLaunch records a launch record on tab i, the way Run Agent/StartLaunch does.
func setLaunch(tabs *Tabs, i int, l terminal.Launch) { tabs.tabs[i].launch = l }

// TestTabsTitleFromLaunchRecord is the Fresh parity behavior: a tab is titled
// after the program it runs (command[0]), else the configured agent name, and an
// explicit user rename still wins.
func TestTabsTitleFromLaunchRecord(t *testing.T) {
	t.Parallel()
	var notices []string
	tabs, _ := newTestTabs(t, &notices)
	startTab(t, tabs) // plain shell: the numbered default
	if got := tabs.TabNames()[0]; got != "Terminal 1" {
		t.Fatalf("plain shell title = %q, want Terminal 1", got)
	}
	startTab(t, tabs)
	setLaunch(tabs, 1, terminal.Launch{Argv: []string{"/usr/local/bin/claude", "--resume", "s1"}, Agent: "claude"})
	if got := tabs.TabNames()[1]; got != "claude" {
		t.Fatalf("launched tab title = %q, want claude", got)
	}
	// A Windows-style argv[0] is reduced to its base name too.
	startTab(t, tabs)
	setLaunch(tabs, 2, terminal.Launch{Argv: []string{"C:\\tools\\node.exe", "x"}})
	if got := tabs.TabNames()[2]; got != "node.exe" {
		t.Fatalf("windows argv title = %q, want node.exe", got)
	}
	// Agent-only records (no argv) fall back to the configured agent name.
	startTab(t, tabs)
	setLaunch(tabs, 3, terminal.Launch{Agent: "aider"})
	if got := tabs.TabNames()[3]; got != "aider" {
		t.Fatalf("agent-only title = %q, want aider", got)
	}
	// An explicit user rename wins over the launch record.
	tabs.SetActive(1)
	tabs.RenameActive("build")
	if got := tabs.TabNames()[1]; got != "build" {
		t.Fatalf("renamed title = %q, want build", got)
	}
	tabs.RenameActive("")
	if got := tabs.TabNames()[1]; got != "claude" {
		t.Fatalf("cleared title = %q, want the launch title back", got)
	}
}

// TestTabsTitleDuplicatesDisambiguated checks Fresh's name (k) disambiguation for
// launcher titles and for explicit duplicates, and that the exited marker comes
// after the title.
func TestTabsTitleDuplicatesDisambiguated(t *testing.T) {
	t.Parallel()
	var notices []string
	tabs, _ := newTestTabs(t, &notices)
	for range 3 {
		startTab(t, tabs)
	}
	claude := terminal.Launch{Argv: []string{"claude"}}
	setLaunch(tabs, 0, claude)
	setLaunch(tabs, 1, claude)
	setLaunch(tabs, 2, claude)
	if got := tabs.TabNames(); strings.Join(got, "|") != "claude|claude (2)|claude (3)" {
		t.Fatalf("duplicate titles = %q", got)
	}
	// An exited duplicate keeps the disambiguated title and adds the marker.
	if _, ok := tabs.Update(exitMsg{gen: tabs.tabs[1].gen}); !ok {
		t.Fatal("the exit message was not taken")
	}
	if got := tabs.TabNames()[1]; got != "claude (2) (exited)" {
		t.Fatalf("exited duplicate = %q", got)
	}
	// Explicit duplicate renames disambiguate too: rename the exited tab and the
	// third tab to the same name.
	tabs.SetActive(1)
	tabs.RenameActive("build")
	tabs.SetActive(2)
	tabs.RenameActive("build")
	if got := tabs.TabNames(); got[0] != "claude" || got[1] != "build (exited)" || got[2] != "build (2)" {
		t.Fatalf("renamed duplicates = %q", got)
	}
}

// TestTabsRestoredAgentTitle checks a restored dead agent tab keeps the title its
// saved launch record names, with the exited marker Fresh shows.
func TestTabsRestoredAgentTitle(t *testing.T) {
	t.Parallel()
	var notices []string
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Tabs: []terminal.SavedTab{
			{Name: "build", Open: true, Rows: []string{"old build"}},
			{Open: true, Rows: []string{"old agent"}, Agent: "claude", LaunchArgv: []string{"claude", "--session-id", "s1"}},
		},
		ActiveTab: 1,
	}
	tabs, _ := newTestTabs(t, &notices)
	tabs.Restore(st)
	if got := tabs.TabNames(); got[0] != "build (exited)" || got[1] != "claude (exited)" {
		t.Fatalf("restored titles = %q", got)
	}
	if got := tabs.ActiveName(); got != "claude (exited)" {
		t.Fatalf("active restored title = %q", got)
	}
}

// TestTabsRestartDropsExitedMarker checks the marker follows the state, not the
// title: restarting an exited launched tab shows the program name again.
func TestTabsRestartDropsExitedMarker(t *testing.T) {
	t.Parallel()
	var notices []string
	tabs, starter := newTestTabs(t, &notices)
	startTab(t, tabs)
	setLaunch(tabs, 0, terminal.Launch{Argv: []string{"claude"}})
	if _, ok := tabs.Update(exitMsg{gen: tabs.tabs[0].gen}); !ok {
		t.Fatal("the exit message was not taken")
	}
	if got := tabs.TabNames()[0]; got != "claude (exited)" {
		t.Fatalf("exited title = %q", got)
	}
	cmd := tabs.Restart()
	if cmd == nil {
		t.Fatal("Restart did not start a command")
	}
	if _, ok := tabs.Update(cmd()); !ok {
		t.Fatal("the restart message was not taken")
	}
	if !tabs.Running() || len(starter.terms) != 2 {
		t.Fatalf("running = %v starts = %d after restart", tabs.Running(), len(starter.terms))
	}
	if got := tabs.TabNames()[0]; got != "claude" {
		t.Fatalf("restarted title = %q, want the marker gone", got)
	}
}

// TestTabsTitleStripIsCellAware checks the strip sanitizes control characters,
// truncates in terminal cells (wide glyphs never split), and that the hit map
// measures the same cells.
func TestTabsTitleStripIsCellAware(t *testing.T) {
	t.Parallel()
	var notices []string
	tabs, _ := newTestTabs(t, &notices)
	startTab(t, tabs)
	startTab(t, tabs)
	// A title with a control character and a wide glyph; the display must drop the
	// control and count the wide glyph as two cells.
	setLaunch(tabs, 0, terminal.Launch{Argv: []string{"bad\x1bname\u6784\u5efa"}})
	label := tabs.stripLabel(0)
	if strings.ContainsRune(label, 0x1b) {
		t.Fatalf("control character survived: %q", label)
	}
	if ansi.StringWidth(label) > MaxTerminalTabTitleCells {
		t.Fatalf("label width %d over the cell budget", ansi.StringWidth(label))
	}
	// A very long title is truncated with an ellipsis and stays clickable.
	setLaunch(tabs, 1, terminal.Launch{Argv: []string{strings.Repeat("x", 120)}})
	if got := tabs.stripLabel(1); ansi.StringWidth(got) > MaxTerminalTabTitleCells || !strings.HasSuffix(got, "\u2026") {
		t.Fatalf("long title = %q (width %d)", got, ansi.StringWidth(got))
	}
	l := frame.Layout{DockOn: true, DockY: 2, DockH: 4, W: 60}
	g := grid.New(60, 8)
	tabs.Draw(g, l, style.Styles{})
	// Click the second tab using the cell width the strip painted: first label
	// starts at column 1, so the second starts after its cells plus the two
	// surrounding spaces.
	first := 1 + ansi.StringWidth(" "+tabs.stripLabel(0)+" ")
	if _, ok := tabs.Mouse(tea.MouseClickMsg{X: first, Y: 2, Button: tea.MouseLeft}, l, 1); !ok {
		t.Fatal("the second tab's strip cells were not hit")
	}
	if got := tabs.ActiveIndex(); got != 1 {
		t.Fatalf("active after the cell-aware click = %d, want 1", got)
	}
}
