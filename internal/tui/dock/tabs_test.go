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

// tabX returns the first and middle title-row columns of tab i's strip label.
func tabX(tabs *Tabs, i int) (start, mid int) {
	col := 1
	for j := 0; j < i; j++ {
		col += ansi.StringWidth(" " + tabs.stripLabel(j) + " ")
	}
	n := ansi.StringWidth(" " + tabs.stripLabel(i) + " ")
	return col, col + n/2
}

// TestTabsMoveActiveReordersKeepsSessionsAndPersistence checks the reorder API:
// only the order changes, running sessions keep their identity, the active tab
// follows its move, the edges are no-ops, and the saved order/active index follow.
func TestTabsMoveActiveReordersKeepsSessionsAndPersistence(t *testing.T) {
	t.Parallel()
	var notices []string
	tabs, starter := newTestTabs(t, &notices)
	startTab(t, tabs)
	startTab(t, tabs)
	startTab(t, tabs)
	for i, name := range []string{"alpha", "beta", "gamma"} {
		tabs.SetActive(i)
		tabs.RenameActive(name)
	}
	terms := make([]*fakeTerm, 3)
	for i := range tabs.tabs {
		terms[i] = starter.terms[i]
		if tabs.tabs[i].term != terms[i] {
			t.Fatalf("tab %d does not hold starter terminal %d", i, i)
		}
	}
	if len(starter.dirs) != 3 {
		t.Fatalf("starts = %d, want 3", len(starter.dirs))
	}

	// Move the active (alpha, index 0) one to the right.
	tabs.SetActive(0)
	if !tabs.MoveActive(1) {
		t.Fatal("MoveActive(1) did not move")
	}
	if got := strings.Join(tabs.TabNames(), ","); got != "beta,alpha,gamma" {
		t.Fatalf("order after move right = %q", got)
	}
	if tabs.ActiveIndex() != 1 || tabs.ActiveName() != "alpha" {
		t.Fatalf("active = %d %q, want the moved tab", tabs.ActiveIndex(), tabs.ActiveName())
	}
	// The sessions moved with their tabs and none was restarted or closed.
	if tabs.tabs[1].term != terms[0] || tabs.tabs[0].term != terms[1] {
		t.Fatal("the running sessions did not move with their tabs")
	}
	if len(starter.dirs) != 3 || starter.terms[0].closed {
		t.Fatalf("reorder touched the sessions: starts %d closed %v", len(starter.dirs), starter.terms[0].closed)
	}
	// Move back left; the edges are no-ops.
	if !tabs.MoveActive(-1) || strings.Join(tabs.TabNames(), ",") != "alpha,beta,gamma" {
		t.Fatalf("move left = %v order %q", tabs.MoveActive(-1), tabs.TabNames())
	}
	tabs.SetActive(0)
	if tabs.MoveActive(-1) {
		t.Fatal("moving the first tab left reported a move")
	}
	tabs.SetActive(2)
	if tabs.MoveActive(1) {
		t.Fatal("moving the last tab right reported a move")
	}
	// Reorder again and check the saved order and active index.
	tabs.SetActive(2) // gamma
	tabs.MoveActive(-1)
	st := tabs.SavedState()
	names := make([]string, len(st.Tabs))
	for i := range st.Tabs {
		names[i] = st.Tabs[i].Name
	}
	if strings.Join(names, ",") != "alpha,gamma,beta" || st.ActiveTab != 1 {
		t.Fatalf("saved order = %q active %d", names, st.ActiveTab)
	}
	restored, _ := newTestTabs(t, &notices)
	restored.Restore(st)
	// The saved tabs were live at the capture, so their restored labels carry no
	// exited marker (they are dormant, not exited); the saved order and active index
	// are what the reorder had to preserve.
	if strings.Join(restored.TabNames(), ",") != "alpha,gamma,beta" || restored.ActiveIndex() != 1 {
		t.Fatalf("restored order = %q active %d", restored.TabNames(), restored.ActiveIndex())
	}
}

// TestTabsTabKeyReorder checks the documented keys: Ctrl+Shift+PgUp/PgDn move the
// active tab, plain Ctrl+PgUp/PgDn still switch, and a move at an edge is consumed
// without changing the order.
func TestTabsTabKeyReorder(t *testing.T) {
	t.Parallel()
	var notices []string
	tabs, _ := newTestTabs(t, &notices)
	startTab(t, tabs)
	startTab(t, tabs)
	startTab(t, tabs)
	tabs.SetActive(0)
	right := tea.Key{Code: tea.KeyPgDown, Mod: tea.ModCtrl | tea.ModShift}
	if !tabs.TabKey(right) {
		t.Fatal("Ctrl+Shift+PgDown was not taken")
	}
	if got := tabs.ActiveIndex(); got != 1 {
		t.Fatalf("active after move right = %d, want 1", got)
	}
	left := tea.Key{Code: tea.KeyPgUp, Mod: tea.ModCtrl | tea.ModShift}
	if !tabs.TabKey(left) || tabs.ActiveIndex() != 0 {
		t.Fatalf("move left: taken %v active %d", tabs.TabKey(left), tabs.ActiveIndex())
	}
	// Plain Ctrl+PgDown switches; it must not reorder.
	before := strings.Join(tabs.TabNames(), ",")
	if !tabs.TabKey(tea.Key{Code: tea.KeyPgDown, Mod: tea.ModCtrl}) {
		t.Fatal("Ctrl+PgDown was not taken as a switch")
	}
	if tabs.ActiveIndex() != 1 || strings.Join(tabs.TabNames(), ",") != before {
		t.Fatalf("Ctrl+PgDown changed the order/active: %q %d", tabs.TabNames(), tabs.ActiveIndex())
	}
	// At the edge the chord is still consumed, but nothing moves.
	tabs.SetActive(2)
	if !tabs.TabKey(right) {
		t.Fatal("the edge move chord was not consumed")
	}
	if tabs.ActiveIndex() != 2 || strings.Join(tabs.TabNames(), ",") != before {
		t.Fatalf("edge move changed state: active %d order %q", tabs.ActiveIndex(), tabs.TabNames())
	}
}

// stripLayout is the dock layout the strip tests draw in: the title row is row 2.
func stripLayout() frame.Layout { return frame.Layout{DockOn: true, DockY: 2, DockH: 4, W: 60} }

// TestTabsDragReorder drives Fresh's tab-drag gesture: press a label, move past the
// three-cell threshold, release over the strip; the drop inserts at the pointer's
// side of the label. Sub-threshold releases are clicks, and a release outside the
// strip cancels without switching tabs.
func TestTabsDragReorder(t *testing.T) {
	t.Parallel()
	var notices []string
	tabs, _ := newTestTabs(t, &notices)
	startTab(t, tabs)
	startTab(t, tabs)
	startTab(t, tabs)
	for i, name := range []string{"alpha", "beta", "gamma"} {
		tabs.SetActive(i)
		tabs.RenameActive(name)
	}
	l := stripLayout()
	g := grid.New(60, 8)
	tabs.Draw(g, l, style.Styles{})
	tabs.SetActive(0)
	startX, _ := tabX(tabs, 0)
	_, gammaMid := tabX(tabs, 2)

	// Press alpha's label, drag past the threshold to the right half of gamma, and
	// release there: alpha inserts after gamma.
	tabs.Mouse(tea.MouseClickMsg{X: startX, Y: 2, Button: tea.MouseLeft}, l, 1)
	if !tabs.drag.on || tabs.drag.from != 0 {
		t.Fatalf("press did not start a drag: %+v", tabs.drag)
	}
	tabs.Mouse(tea.MouseMotionMsg{X: gammaMid + 2, Y: 2, Button: tea.MouseLeft}, l, 1)
	if !tabs.drag.active {
		t.Fatal("the drag did not pass the threshold")
	}
	// The drop marker paints while dragging.
	g = grid.New(60, 8)
	tabs.Draw(g, l, style.Styles{})
	if row := g.Plain()[2]; !strings.Contains(row, "┃") {
		t.Fatalf("strip = %q, want the drop marker", row)
	}
	tabs.Mouse(tea.MouseReleaseMsg{X: gammaMid + 2, Y: 2, Button: tea.MouseLeft}, l, 1)
	if tabs.drag.on {
		t.Fatal("the release left the drag active")
	}
	if got := strings.Join(tabs.TabNames(), ","); got != "beta,gamma,alpha" {
		t.Fatalf("order after the drop = %q", got)
	}
	if tabs.ActiveIndex() != 2 {
		t.Fatalf("active after the drop = %d, want the dragged tab", tabs.ActiveIndex())
	}

	// A sub-threshold press/release is a click: no reorder.
	tabs.SetActive(0)
	betaStart, _ := tabX(tabs, 0)
	tabs.Mouse(tea.MouseClickMsg{X: betaStart, Y: 2, Button: tea.MouseLeft}, l, 1)
	tabs.Mouse(tea.MouseMotionMsg{X: betaStart + 2, Y: 2, Button: tea.MouseLeft}, l, 1)
	tabs.Mouse(tea.MouseReleaseMsg{X: betaStart + 2, Y: 2, Button: tea.MouseLeft}, l, 1)
	if got := strings.Join(tabs.TabNames(), ","); got != "beta,gamma,alpha" {
		t.Fatalf("a click reordered: %q", got)
	}

	// A drag released outside the strip cancels and does not switch tabs (the press
	// had selected the dragged tab, gamma at index 2).
	tabs.SetActive(1)
	gammaStart, _ := tabX(tabs, 1)
	tabs.Mouse(tea.MouseClickMsg{X: gammaStart, Y: 2, Button: tea.MouseLeft}, l, 1)
	tabs.Mouse(tea.MouseMotionMsg{X: gammaStart + 20, Y: 5, Button: tea.MouseLeft}, l, 1)
	tabs.Mouse(tea.MouseReleaseMsg{X: gammaStart + 20, Y: 5, Button: tea.MouseLeft}, l, 1)
	if got := strings.Join(tabs.TabNames(), ","); got != "beta,gamma,alpha" {
		t.Fatalf("a cancelled drag reordered: %q", got)
	}
	if tabs.ActiveIndex() != 1 {
		t.Fatalf("a cancelled drag changed the active tab to %d", tabs.ActiveIndex())
	}
}
