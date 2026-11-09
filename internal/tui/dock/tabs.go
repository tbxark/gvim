package dock

import (
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// Terminal tabs: the dock holds up to MaxTerminalTabs shells, one existing Panel
// per tab. The container owns the tab list, the active tab and the names, and
// forwards the panel API to the active tab; every Panel keeps its own screen,
// scrollback, transcript and exited state, so switching tabs loses nothing and a
// background shell keeps running (and keeps arming its output/exit waits through
// Sync). The dock title row becomes the tab strip.
//
// Workspace scoping of tabs (a tab set per workspace) is deferred to parity #19:
// the dock shows every tab of the current window, and a workspace switch keeps
// the dock's tabs as they are.
const MaxTerminalTabs = 16

// MaxTerminalTabTitleCells is the cell budget of one tab label in the strip
// before it is truncated with an ellipsis. It keeps the painting and the strip
// hit map measuring the same terminal cells.
const MaxTerminalTabTitleCells = 24

// Tabs is the dock's terminal-tab container. The zero value is an empty dock; the
// root creates tabs through Show/NewTab.
type Tabs struct {
	tabs    []*Panel
	active  int
	host    Host
	ports   Ports
	effects Effects
	names   []string // per tab: "" means the default name
	// drag is the in-flight strip drag: the Fresh tab-drag gesture reorders a tab
	// by pressing its label, moving past the threshold and releasing over the strip.
	drag tabDrag
	// dive is the restored tab the user just switched to or focused: on the next
	// Sync its live revival starts (a tea.Cmd, so Update never blocks on the spawn).
	// A second dive before the sync replaces it with the newest target.
	dive *Panel
	// nextGen is the next shell-generation namespace. Messages carry only a
	// generation, and every Panel counts its generations from 0, so each tab gets a
	// disjoint million-wide range; otherwise an output/exit of one tab could match
	// another tab's shell (two tabs would both start at generation 1).
	nextGen int
}

// SetHost sets the host services every tab uses (and future tabs inherit).
func (t *Tabs) SetHost(h Host) {
	t.host = h
	for _, p := range t.tabs {
		p.SetHost(h)
	}
}

// SetPorts sets the starter/clipboard ports every tab uses.
func (t *Tabs) SetPorts(p Ports) {
	t.ports = p
	for _, tab := range t.tabs {
		tab.SetPorts(p)
	}
}

// SetEffects sets the terminal effects (close_on_exit and the view options).
func (t *Tabs) SetEffects(e Effects) {
	t.effects = e
	for _, p := range t.tabs {
		p.SetEffects(e)
	}
}

// genBase returns the next tab's disjoint shell-generation namespace.
func (t *Tabs) genBase() int {
	t.nextGen++
	return t.nextGen << 20
}

// Active returns the active tab, or nil when the dock has no tabs.
func (t *Tabs) Active() *Panel {
	if t.active < 0 || t.active >= len(t.tabs) {
		return nil
	}
	return t.tabs[t.active]
}

// ActiveIndex returns the active tab's index, or 0 when there is none.
func (t *Tabs) ActiveIndex() int { return t.active }

// Count returns the number of tabs.
func (t *Tabs) Count() int { return len(t.tabs) }

// Name returns tab i's name, or "" for the default.
func (t *Tabs) Name(i int) string {
	if i < 0 || i >= len(t.names) {
		return ""
	}
	return t.names[i]
}

// RenameActive sets the active tab's name; an empty name restores the default.
func (t *Tabs) RenameActive(name string) {
	if i := t.active; i >= 0 && i < len(t.tabs) {
		t.names[i] = strings.TrimSpace(name)
	}
}

// TabNames returns the display names of every tab (default names for unnamed
// tabs, with an exited marker).
func (t *Tabs) TabNames() []string {
	out := make([]string, len(t.tabs))
	for i := range t.tabs {
		out[i] = t.tabLabel(i)
	}
	return out
}

// tabBaseLabels is the undecorated label of every tab: the explicit user name
// when set, else a Fresh-style title from the tab's launch record, else
// "Terminal N". Fresh names a terminal tab after the program it runs
// (command[0]), falling back to the configured agent name; gvim's launch record
// is the same information and is persisted per tab, so a restored agent tab has
// its title back.
func (t *Tabs) tabBaseLabels() []string {
	out := make([]string, len(t.tabs))
	for i, p := range t.tabs {
		name := ""
		if i < len(t.names) {
			name = t.names[i]
		}
		if name == "" {
			name = launchTitle(p.Launch())
		}
		if name == "" {
			// A session/record title (OSC 0/2) names a plain shell's tab, live or
			// restored; an explicit rename and the launch record still take precedence.
			name = cleanTabTitle(p.TitleText())
		}
		if name == "" {
			name = "Terminal " + strconv.Itoa(i+1)
		}
		out[i] = name
	}
	return out
}

// launchTitle derives the title of a launched program: the base name of argv[0],
// else the configured agent name (Fresh's command[0] fallback), "" for the plain
// shell.
func launchTitle(l terminal.Launch) string {
	if len(l.Argv) > 0 {
		if name := cleanTabTitle(commandBase(l.Argv[0])); name != "" {
			return name
		}
	}
	return cleanTabTitle(l.Agent)
}

// commandBase is the program name of an argv[0], dropping a path with either
// separator (argv comes from config and may name a Windows or Unix path).
func commandBase(arg string) string {
	if i := strings.LastIndexAny(arg, "/\\"); i >= 0 {
		return arg[i+1:]
	}
	return arg
}

// cleanTabTitle strips control characters a title could carry into the strip and
// trims it. The grid drops controls when painting, but the hit map must measure
// the same text, so titles are cleaned in one place.
func cleanTabTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// tabLabels returns every tab's logical label: base labels with Fresh-style
// duplicate disambiguation ("claude", "claude (2)") and the exited marker a dead
// tab shows in-session and after restore.
func (t *Tabs) tabLabels() []string {
	labels := t.tabBaseLabels()
	seen := make(map[string]int, len(labels))
	counts := make(map[string]int, len(labels))
	for _, label := range labels {
		counts[label]++
	}
	for i, label := range labels {
		if counts[label] > 1 {
			seen[label]++
			if seen[label] > 1 {
				labels[i] = label + " (" + strconv.Itoa(seen[label]) + ")"
			}
		}
		// A restored tab that was live at the capture is dormant, not exited: it
		// shows its title without the marker until a dive revives it (or the user
		// leaves it dead). Every exited tab keeps the marker, including every tab of
		// an old record without the live flag.
		if t.tabs[i].Exited() && (!t.tabs[i].restored || !t.tabs[i].wasLive) {
			labels[i] += " (exited)"
		}
	}
	return labels
}

// tabLabel is the label of tab i.
func (t *Tabs) tabLabel(i int) string {
	if i < 0 || i >= len(t.tabs) {
		return ""
	}
	return t.tabLabels()[i]
}

// stripLabel is a tab's label cleaned and truncated to one tab's cell budget with
// an ellipsis, in terminal cells, so the strip paints and the hit map measures
// the same text (a wide glyph is never split).
func (t *Tabs) stripLabel(i int) string {
	return ansi.Truncate(cleanTabTitle(t.tabLabel(i)), MaxTerminalTabTitleCells, "…")
}

// currentName is the active tab's label.
func (t *Tabs) currentName() string {
	if len(t.tabs) == 0 {
		return "Terminal"
	}
	return t.tabLabel(t.active)
}

// tabStartedMsg is the started message of one tab, tagged with the tab's Panel so
// the container routes it to that tab only: startedMsg itself carries no
// generation, and two tabs starting in a row would otherwise each consume the
// other's message. The Panel pointer (not an index) keeps the routing correct
// when tabs are reordered while a start is in flight.
type tabStartedMsg struct {
	tab   *Panel
	inner tea.Msg
}

// tabDragThreshold is Fresh's tab-drag activation distance: movement of more than
// three cells from the press turns a click into a drag (types/drag.rs).
const tabDragThreshold = 3

// tabDrag is one mouse drag of a strip label. on is a press on a label, active is
// the drag past the threshold, and x is the latest pointer column.
type tabDrag struct {
	from   int
	startX int
	x      int
	on     bool
	active bool
}

// NewTab adds a tab at the end, makes it active, and starts its shell. A dock at
// the tab cap refuses with a notice. The previous tab keeps running.
func (t *Tabs) NewTab() tea.Cmd {
	if len(t.tabs) >= MaxTerminalTabs {
		if t.host.Notice != nil {
			t.host.Notice("Terminal: the tab limit (" + strconv.Itoa(MaxTerminalTabs) + ") is reached")
		}
		return nil
	}
	if prev := t.Active(); prev != nil {
		prev.SetFocus(false)
	}
	var p Panel
	p.SetHost(t.host)
	p.SetPorts(t.ports)
	p.SetEffects(t.effects)
	p.gen = t.genBase()
	t.tabs = append(t.tabs, &p)
	t.names = append(t.names, "")
	t.active = len(t.tabs) - 1
	// Hide the tabs the new one replaces from the layout: only the active tab is
	// drawn, and a background tab keeps its shell and focus flag.
	for i, tab := range t.tabs {
		if i != t.active {
			tab.SetFocus(false)
		}
	}
	cmd := p.Show()
	if cmd == nil {
		return nil
	}
	tab := t.tabs[len(t.tabs)-1]
	return func() tea.Msg { return tabStartedMsg{tab: tab, inner: cmd()} }
}

// SetActive makes tab i active. It reports false for an out-of-range index.
func (t *Tabs) SetActive(i int) bool {
	if i < 0 || i >= len(t.tabs) {
		return false
	}
	if i == t.active {
		// A deliberate press on the tab that is already active is still a dive.
		t.queueDive(t.tabs[i])
		return true
	}
	wasFocused := t.Active() != nil && t.Active().Focus()
	t.Active().SetFocus(false)
	t.active = i
	if wasFocused {
		t.Active().SetFocus(true)
	}
	t.queueDive(t.tabs[i])
	return true
}

// queueDive arms the next Sync to revive tab p when the user dives into it (a
// switch, a focus or a dock show). It only stores the target: the start itself is
// a tea.Cmd run from Sync, so the Update loop never blocks on the spawn.
func (t *Tabs) queueDive(p *Panel) {
	if p != nil {
		t.dive = p
	}
}

// Next makes the next tab active, wrapping at the end.
func (t *Tabs) Next() { t.wrap(1) }

// Previous makes the previous tab active, wrapping at the start.
func (t *Tabs) Previous() { t.wrap(-1) }

// wrap moves the active tab by delta with wrapping.
func (t *Tabs) wrap(delta int) {
	if len(t.tabs) < 2 {
		return
	}
	i := ((t.active+delta)%len(t.tabs) + len(t.tabs)) % len(t.tabs)
	t.SetActive(i)
}

// MoveActive moves the active tab by delta (-1 left, +1 right) within the dock,
// keeping its identity, its shell and its focus: only the order of the tab list
// changes. It reports whether the tab moved. The palette's Move Tab To Left/Right
// commands and the Ctrl+Shift+PageUp/PageDown keys use it.
func (t *Tabs) MoveActive(delta int) bool {
	if delta == 0 || t.active < 0 || t.active >= len(t.tabs) {
		return false
	}
	to := t.active + delta
	if to < 0 || to >= len(t.tabs) {
		return false
	}
	t.moveTab(t.active, to)
	return true
}

// moveTab moves tab from index from to index to, carrying its name and its running
// session and keeping the active tab active (it follows its own move). No session
// is started, closed, resized or repainted.
func (t *Tabs) moveTab(from, to int) {
	n := len(t.tabs)
	if from < 0 || from >= n || to < 0 || to >= n || from == to {
		return
	}
	active := t.active
	tab := t.tabs[from]
	name := t.names[from]
	t.tabs = append(t.tabs[:from], t.tabs[from+1:]...)
	t.names = append(t.names[:from], t.names[from+1:]...)
	t.tabs = append(t.tabs, nil)
	copy(t.tabs[to+1:], t.tabs[to:])
	t.tabs[to] = tab
	t.names = append(t.names, "")
	copy(t.names[to+1:], t.names[to:])
	t.names[to] = name
	switch {
	case active == from:
		t.active = to
	case from < active && to >= active:
		t.active = active - 1
	case from > active && to <= active:
		t.active = active + 1
	}
}

// dropTab reorders tab from where the strip would insert it at insert (the index
// of the tab the pointer is before, len for the end), applying Fresh's
// remove-then-adjust rule. A drop on the tab's own place does nothing.
func (t *Tabs) dropTab(from, insert int) {
	to := min(max(insert, 0), len(t.tabs))
	if from < to {
		to--
	}
	to = min(max(to, 0), len(t.tabs)-1)
	t.moveTab(from, to)
}

// CloseTab closes the active tab's shell and removes the tab. The last tab
// closing leaves the dock closed. Closing a running shell is immediate: gvim has
// no process-confirmation prompt today (docs/terminal.md).
func (t *Tabs) CloseTab() {
	if len(t.tabs) == 0 {
		return
	}
	t.removeAt(t.active)
}

// removeAt closes and removes tab i.
func (t *Tabs) removeAt(i int) {
	if i < 0 || i >= len(t.tabs) {
		return
	}
	tab := t.tabs[i]
	if t.dive == tab {
		t.dive = nil
	}
	tab.Close()
	t.tabs = append(t.tabs[:i:i], t.tabs[i+1:]...)
	t.names = append(t.names[:i:i], t.names[i+1:]...)
	if len(t.tabs) == 0 {
		t.active = 0
		return
	}
	if t.active > i {
		t.active--
	} else if t.active >= len(t.tabs) {
		t.active = len(t.tabs) - 1
	}
}

// Show shows the dock: the first tab is created (starting a shell) when the dock
// has none, the active tab keeps or gets the focus.
func (t *Tabs) Show() tea.Cmd {
	if len(t.tabs) == 0 {
		return t.NewTab()
	}
	t.queueDive(t.Active())
	return t.Active().Show()
}

// Hide hides the dock; the shells keep running.
func (t *Tabs) Hide() {
	for _, p := range t.tabs {
		p.Hide()
	}
}

// Close closes every tab's shell. The dock becomes empty.
func (t *Tabs) Close() {
	for _, p := range t.tabs {
		p.Close()
	}
	t.tabs = nil
	t.names = nil
	t.active = 0
	t.dive = nil
}

// Restart restarts the active tab's shell, keeping its transcript. The start is
// tagged with the tab index, like NewTab's, so its result reaches that tab only.
func (t *Tabs) Restart() tea.Cmd {
	if len(t.tabs) == 0 {
		return t.NewTab()
	}
	tab := t.Active()
	cmd := tab.Restart()
	if cmd == nil {
		return nil
	}
	return func() tea.Msg { return tabStartedMsg{tab: tab, inner: cmd()} }
}

// Agents returns the configured agent presets the Run Agent picker offers.
func (t *Tabs) Agents() []Agent { return t.effects.Agents }

// StartAgent starts a configured agent in a new tab and makes it active. It
// reports whether name is configured. The agent session is ephemeral and records
// its launch and resume argv, so the tab's Restart can rejoin it.
func (t *Tabs) StartAgent(name string) (tea.Cmd, bool) {
	found := false
	for _, a := range t.effects.Agents {
		if a.Name == name {
			found = true
			break
		}
	}
	if !found {
		return nil, false
	}
	if len(t.tabs) >= MaxTerminalTabs {
		if t.host.Notice != nil {
			t.host.Notice("Terminal: the tab limit (" + strconv.Itoa(MaxTerminalTabs) + ") is reached")
		}
		return nil, false
	}
	if prev := t.Active(); prev != nil {
		prev.SetFocus(false)
	}
	var p Panel
	p.SetHost(t.host)
	p.SetPorts(t.ports)
	p.SetEffects(t.effects)
	p.gen = t.genBase()
	t.tabs = append(t.tabs, &p)
	t.names = append(t.names, name)
	t.active = len(t.tabs) - 1
	cmd, ok := p.StartAgent(name)
	if cmd == nil {
		return nil, ok
	}
	tab := t.tabs[len(t.tabs)-1]
	return func() tea.Msg { return tabStartedMsg{tab: tab, inner: cmd()} }, ok
}

// Open reports whether the dock is shown.
func (t *Tabs) Open() bool { p := t.Active(); return p != nil && p.Open() }

// Focus reports whether the active tab has the keyboard focus.
func (t *Tabs) Focus() bool { p := t.Active(); return p != nil && p.Focus() }

// SetFocus sets the keyboard focus: giving it to the dock clears it on the other
// tabs, taking it away clears it on the active one.
func (t *Tabs) SetFocus(v bool) {
	for i, p := range t.tabs {
		if i == t.active || !v {
			p.SetFocus(v)
		}
	}
	if v {
		t.queueDive(t.Active())
	}
}

// Running reports whether any tab's shell runs (the dock acts alive while one
// does).
func (t *Tabs) Running() bool {
	for _, p := range t.tabs {
		if p.Running() {
			return true
		}
	}
	return false
}

// Starting reports whether the active tab is waiting for its shell to start.
func (t *Tabs) Starting() bool { p := t.Active(); return p != nil && p.Starting() }

// Exited reports whether the active tab's shell has exited.
func (t *Tabs) Exited() bool { p := t.Active(); return p != nil && p.Exited() }

// RestartInfo is what the status row's {terminal_restart} element shows for an
// exited dock tab: the program a restart will run (the basename of the resume or
// launch argv's program; "" for a plain shell), how the process ended, and
// whether the restart rejoins an agent conversation.
type RestartInfo struct {
	Program      string
	ExitCode     int
	ExitKnown    bool
	ResumesAgent bool
}

// ExitedRestartInfo returns the restart description of the active tab while its
// shell has exited. A plain shell has no Program, matching Fresh's "Restart
// terminal"; the resume argv wins over the launch argv for the program name (it
// is what a restart runs), but the Resume wording needs terminal.resume_agents
// on and a non-empty resume argv, exactly like the status bar's rule.
func (t *Tabs) ExitedRestartInfo() (RestartInfo, bool) {
	p := t.Active()
	if p == nil || !p.Exited() {
		return RestartInfo{}, false
	}
	info := RestartInfo{ExitCode: p.exitCode, ExitKnown: p.exitKnown}
	launch := p.Launch()
	info.ResumesAgent = t.effects.ResumeAgents && len(launch.ResumeArgv) > 0
	argv := launch.ResumeArgv
	if len(argv) == 0 {
		argv = launch.Argv
	}
	info.Program = restartProgramName(argv)
	return info, true
}

// restartProgramName is the short program name a restart runs: the basename of
// argv[0] with Unix or Windows separators, or "" for an empty argv.
func restartProgramName(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	program := argv[0]
	if i := strings.LastIndexAny(program, "/\\"); i >= 0 {
		program = program[i+1:]
	}
	return program
}

// Restored reports whether the active tab is a restored transcript.
func (t *Tabs) Restored() bool { p := t.Active(); return p != nil && p.Restored() }

// Notice returns the active tab's start/exit notice.
func (t *Tabs) Notice() string {
	p := t.Active()
	if p == nil {
		return ""
	}
	return p.Notice()
}

// Capture reports whether keyboard capture is on in the active tab.
func (t *Tabs) Capture() bool { p := t.Active(); return p != nil && p.Capture() }

// ToggleCapture flips keyboard capture in the active tab.
func (t *Tabs) ToggleCapture() {
	if p := t.Active(); p != nil {
		p.ToggleCapture()
	}
}

// Keyboard reports whether key presses go to the active tab.
func (t *Tabs) Keyboard(l frame.Layout) bool { p := t.Active(); return p != nil && p.Keyboard(l) }

// Clipboard returns the clipboard reader of the shared ports. It is a port, not
// per-tab state: the root reads it for the editor clipboard actions even when the
// dock has no tab at all (exactly like a zero Panel returned its ports' reader),
// so it must not depend on an active tab.
func (t *Tabs) Clipboard() ClipboardReader { return t.ports.Clipboard }

// Scrolling reports whether the active tab shows its scrollback pager.
func (t *Tabs) Scrolling() bool { p := t.Active(); return p != nil && p.Scrolling() }

// ScrollTop and ScrollSelected expose the active tab's pager position.
func (t *Tabs) ScrollTop() int {
	p := t.Active()
	if p == nil {
		return 0
	}
	return p.ScrollTop()
}
func (t *Tabs) ScrollSelected() int {
	p := t.Active()
	if p == nil {
		return 0
	}
	return p.ScrollSelected()
}

// SearchQuery and SearchMatches expose the active tab's search state.
func (t *Tabs) SearchQuery() string {
	p := t.Active()
	if p == nil {
		return ""
	}
	return p.SearchQuery()
}
func (t *Tabs) SearchMatches() int {
	p := t.Active()
	if p == nil {
		return 0
	}
	return p.SearchMatches()
}

// Selection reports whether the active tab has a text selection.
func (t *Tabs) Selection() bool { p := t.Active(); return p != nil && p.Selection() }

// SelectionDragging reports whether a selection drag is in flight.
func (t *Tabs) SelectionDragging() bool { p := t.Active(); return p != nil && p.SelectionDragging() }

// ClearHover clears the active tab's Ctrl+hover link.
func (t *Tabs) ClearHover() {
	if p := t.Active(); p != nil {
		p.ClearHover()
	}
}

// Range returns the active tab's selection range in absolute rows.
func (t *Tabs) SelectionRange(abs int) (int, int, bool) {
	p := t.Active()
	if p == nil {
		return 0, 0, false
	}
	return p.SelectionRange(abs)
}

// ToggleScrollback enters/leaves the active tab's scrollback pager.
func (t *Tabs) ToggleScrollback() tea.Cmd {
	p := t.Active()
	if p == nil {
		return nil
	}
	return p.ToggleScrollback()
}

// ScrollKey forwards a scrollback/pager key to the active tab.
func (t *Tabs) ScrollKey(k tea.Key) (tea.Cmd, bool) {
	p := t.Active()
	if p == nil {
		return nil, false
	}
	return p.ScrollKey(k)
}

// PasteClipboard pastes the system clipboard into the active tab's shell. With
// no tab at all it says so, exactly like a zero Panel did (the no-tab dock is a
// closed dock, and the paste action is reachable from the menu).
func (t *Tabs) PasteClipboard() tea.Cmd {
	p := t.Active()
	if p == nil {
		if t.host.Notice != nil {
			t.host.Notice(NoTerminalNotice())
		}
		return nil
	}
	return p.PasteClipboard()
}

// SendKey sends a key to the active tab's shell.
func (t *Tabs) SendKey(k tea.Key) {
	if p := t.Active(); p != nil {
		p.SendKey(k)
	}
}

// SendPaste sends a paste to the active tab's shell.
func (t *Tabs) SendPaste(text string) {
	if p := t.Active(); p != nil {
		p.SendPaste(text)
	}
}

// SendToShell sends text to the active tab's shell.
func (t *Tabs) SendToShell(text string) bool {
	p := t.Active()
	if p == nil {
		return false
	}
	return p.SendToShell(text)
}

// SendBytes sends bytes to the active tab's shell.
func (t *Tabs) SendBytes(b []byte) {
	if p := t.Active(); p != nil {
		p.SendBytes(b)
	}
}

// Caret returns the active tab's caret cell.
func (t *Tabs) Caret(l frame.Layout) (int, int, bool) {
	p := t.Active()
	if p == nil {
		return 0, 0, false
	}
	return p.Caret(l)
}

// Press forwards a press in the active tab's body.
func (t *Tabs) Press(l frame.Layout, y int) bool {
	p := t.Active()
	if p == nil {
		return false
	}
	return p.Press(l, y)
}

// Update routes a dock message to every tab (messages carry the tab's shell
// generation, so at most one consumes it), batches the commands, and then applies
// the per-tab close_on_exit rule and the exited markers.
func (t *Tabs) Update(msg tea.Msg) (tea.Cmd, bool) {
	var cmds []tea.Cmd
	any := false
	for _, p := range t.tabs {
		update := msg
		if ts, ok := msg.(tabStartedMsg); ok {
			if ts.tab != p {
				continue
			}
			update = ts.inner
		}
		if cmd, ok := p.Update(update); ok {
			any = true
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	if t.effects.CloseOnExit {
		t.closeExited()
	}
	return tea.Batch(cmds...), any
}

// closeExited removes tabs whose shell exited under close_on_exit. A restored
// transcript is not a live shell and stays.
func (t *Tabs) closeExited() {
	for i := len(t.tabs) - 1; i >= 0; i-- {
		if t.tabs[i].Exited() && !t.tabs[i].Restored() {
			t.removeAt(i)
		}
	}
}

// Sync starts the commands every tab needs now: output/exit waits, resizes and
// queued input. Background tabs need this too, so their exited markers and
// scrollback stay current.
func (t *Tabs) Sync() tea.Cmd {
	var cmds []tea.Cmd
	if p := t.dive; p != nil {
		t.dive = nil
		if cmd := p.dive(); cmd != nil {
			tab := p
			cmds = append(cmds, func() tea.Msg { return tabStartedMsg{tab: tab, inner: cmd()} })
		}
	}
	for _, p := range t.tabs {
		if cmd := p.Sync(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// TabKey handles the dock's tab keys: Ctrl+PageDown/PageUp switch tabs and
// Ctrl+Shift+PageDown/PageUp move the active tab right/left (Fresh's Move Tab To
// Right/Left commands). The keys work while the dock has the keyboard focus, even
// in keyboard capture, and are documented in docs/terminal.md; they are not keymap
// data. Shift on a page key only arrives in terminals that report modified keys
// (xterm-style CSI 5;6~ / CSI-u); elsewhere Ctrl+Shift+PgUp arrives as Ctrl+PgUp
// and switches, which is why the palette rows and the mouse drag stay the
// guaranteed reorder paths.
func (t *Tabs) TabKey(k tea.Key) bool {
	switch {
	case k.Code == tea.KeyPgDown && k.Mod == tea.ModCtrl|tea.ModShift:
		t.MoveActive(1)
		return true
	case k.Code == tea.KeyPgUp && k.Mod == tea.ModCtrl|tea.ModShift:
		t.MoveActive(-1)
		return true
	case k.Code == tea.KeyPgDown && k.Mod == tea.ModCtrl:
		t.Next()
		return true
	case k.Code == tea.KeyPgUp && k.Mod == tea.ModCtrl:
		t.Previous()
		return true
	}
	return false
}

// Mouse routes mouse input. A left press on the strip selects a tab and starts a
// potential drag; moving past Fresh's three-cell threshold with the button down
// makes it a drag; releasing over the strip reorders the tab to the pointer's
// insertion point (a release anywhere else cancels without switching tabs, and
// the drag never turns into a body selection). All other mouse input goes to the
// active tab's body.
func (t *Tabs) Mouse(msg tea.Msg, l frame.Layout, count int) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case tea.MouseClickMsg:
		if l.DockOn && m.Button == tea.MouseLeft && m.Y == l.DockY {
			if i, ok := t.tabAtX(l.W, m.X); ok {
				t.SetActive(i)
				t.drag = tabDrag{from: i, startX: m.X, x: m.X, on: true}
				// A press on the strip is still a press on the dock: it takes the
				// keyboard when a shell runs (the host's focus gate refuses otherwise).
				if t.host.Focus != nil {
					t.host.Focus()
				}
				return nil, true
			}
			t.drag = tabDrag{}
			return nil, true
		}
	case tea.MouseMotionMsg:
		if t.drag.on {
			t.drag.x = m.X
			if !t.drag.active && dragDistance(m.X-t.drag.startX) > tabDragThreshold {
				t.drag.active = true
			}
			// The drag owns the pointer until release, even when it leaves the strip:
			// a body motion must not start a selection under the dragged label.
			return nil, true
		}
	case tea.MouseReleaseMsg:
		if t.drag.on {
			drag := t.drag
			t.drag = tabDrag{}
			if drag.active && l.DockOn && m.Y == l.DockY {
				t.dropTab(drag.from, t.tabInsertAtX(l.W, m.X))
			}
			return nil, true
		}
	}
	p := t.Active()
	if p == nil {
		return nil, false
	}
	return p.Mouse(msg, l, count)
}

// dragDistance is the absolute distance of a drag move.
func dragDistance(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// tabInsertAtX maps a title-row column to Fresh's tab-drop insertion index: the
// left half of a label inserts before that tab, its right half after, and a
// column past the last label inserts at the end.
func (t *Tabs) tabInsertAtX(w, x int) int {
	col := 1
	for i := range t.tabs {
		n := ansi.StringWidth(" " + t.stripLabel(i) + " ")
		mid := col + n/2
		if x < mid {
			return i
		}
		col += n
		if col >= w {
			return i + 1
		}
	}
	return len(t.tabs)
}

// tabInsertCol is the strip column the drop marker for tabInsertAtX draws at.
func (t *Tabs) tabInsertCol(w, x int) int {
	idx := t.tabInsertAtX(w, x)
	col := 1
	for i := 0; i < idx && i < len(t.tabs); i++ {
		col += ansi.StringWidth(" " + t.stripLabel(i) + " ")
	}
	return min(max(col, 0), max(w-1, 0))
}

// tabAtX maps a title-row column to a tab index. The strip mirrors drawStrip.
func (t *Tabs) tabAtX(w, x int) (int, bool) {
	if x <= 0 || len(t.tabs) == 0 {
		return 0, false
	}
	col := 1
	for i := range t.tabs {
		label := " " + t.stripLabel(i) + " "
		n := ansi.StringWidth(label)
		if col <= x && x < col+n {
			return i, true
		}
		col += n
		if col >= w {
			break
		}
	}
	return 0, false
}

// Draw paints the active tab and then the tab strip over the dock's title row.
func (t *Tabs) Draw(g *grid.Grid, l frame.Layout, st style.Styles) {
	p := t.Active()
	if p == nil || !l.DockOn {
		return
	}
	p.Draw(g, l, st)
	t.drawStrip(g, l, st)
}

// drawStrip paints " Terminal 1  Terminal 2 " across the dock title row, the
// active tab in the active-tab style.
func (t *Tabs) drawStrip(g *grid.Grid, l frame.Layout, st style.Styles) {
	if l.DockH <= 0 || l.W <= 0 {
		return
	}
	bar := style.Cell(st.ExplorerTitle)
	active := style.Cell(st.TabActive)
	inactive := style.Cell(st.Tab)
	g.Fill(0, l.W, l.DockY, bar)
	x := 1
	for i := range t.tabs {
		label := " " + t.stripLabel(i) + " "
		cell := inactive
		if i == t.active {
			cell = active
		}
		x += g.Put(x, l.DockY, l.W-1, label, cell)
		if x >= l.W-1 {
			return
		}
	}
	if t.drag.active {
		// Where the dragged tab would land: Fresh shows a drop indicator during the
		// drag, so the gesture is visible before the button is released.
		if col := t.tabInsertCol(l.W, t.drag.x); col < l.W {
			g.Put(col, l.DockY, min(col+1, l.W), "┃", active)
		}
	}
}

// RunningTab is one running tab as the close-workspace confirmation lists it: the
// tab's undecorated base name (explicit rename, else the launch title) and the
// configured agent name of its launch record, or "" for a plain shell.
type RunningTab struct {
	Name  string
	Agent string
}

// RunningTabs returns the running tabs in tab order, for a close confirmation that
// names exactly what closing the workspace would kill. It is a read-only snapshot.
func (t *Tabs) RunningTabs() []RunningTab {
	labels := t.tabBaseLabels()
	out := make([]RunningTab, 0, len(t.tabs))
	for i, p := range t.tabs {
		if !p.Running() {
			continue
		}
		name := labels[i]
		out = append(out, RunningTab{Name: name, Agent: p.Launch().Agent})
	}
	return out
}

// ActiveName returns the active tab's display name.
func (t *Tabs) ActiveName() string { return t.currentName() }

// ExitedFlags returns the exited state of every tab (tests and the strip).
func (t *Tabs) ExitedFlags() []bool {
	out := make([]bool, len(t.tabs))
	for i := range t.tabs {
		out[i] = t.tabs[i].Exited()
	}
	return out
}

// SavedState returns the terminal state the session file keeps: the active tab's
// record mirrored into the top-level fields (so a reader that does not know tabs
// restores that tab), plus every tab as a SavedTab and the active index. The
// per-tab rows/launch records are the same bounded panel records a single-tab
// dock wrote.
func (t *Tabs) SavedState() terminal.SavedState {
	if len(t.tabs) == 0 {
		return terminal.SavedState{Version: terminal.SavedStateVersion}
	}
	st := t.Active().SavedState()
	st.Tabs = make([]terminal.SavedTab, len(t.tabs))
	for i, p := range t.tabs {
		st.Tabs[i] = t.savedTabOf(i, p)
	}
	st.ActiveTab = t.active
	return st
}

// savedTabOf builds tab i's saved record from its panel.
func (t *Tabs) savedTabOf(i int, p *Panel) terminal.SavedTab {
	rec := p.SavedState()
	return terminal.SavedTab{
		Name: t.names[i], Open: rec.Open, Cwd: rec.Cwd, Width: rec.Width, Title: rec.Title,
		Rows: rec.Rows, LaunchArgv: rec.LaunchArgv, ResumeArgv: rec.ResumeArgv,
		Agent: rec.Agent, Ephemeral: rec.Ephemeral, LaunchDir: rec.LaunchDir,
		Source: rec.Source, Live: rec.Live, Screen: rec.Screen,
		ExitCode: rec.ExitCode, ExitKnown: rec.ExitKnown,
	}
}

// TabRef names one dock tab for the extract picker: Index is the tab's slot
// (stable until the selection is executed), Name is its saved name ("" for a
// default-named tab, used to verify identity before closing) and Label is the
// display label.
type TabRef struct {
	Index int
	Name  string
	Label string
}

// TabRefs returns every dock tab as a picker reference, in tab order.
func (t *Tabs) TabRefs() []TabRef {
	out := make([]TabRef, len(t.tabs))
	for i := range t.tabs {
		out[i] = TabRef{Index: i, Name: t.names[i], Label: t.tabLabel(i)}
	}
	return out
}

// SavedTabAt returns tab i's saved record, for the mixed extract action that
// moves a file+terminal selection into another workspace.
func (t *Tabs) SavedTabAt(i int) (terminal.SavedTab, bool) {
	if i < 0 || i >= len(t.tabs) {
		return terminal.SavedTab{}, false
	}
	return t.savedTabOf(i, t.tabs[i]), true
}

// CloseTabAtForExtract force-closes tab i when its saved name is name, without
// asking: the extract action already wrote the tab's record into the target, so
// closing stops this shell and drops nothing. It reports false when the tab
// changed (another tab closed/moved), leaving everything untouched.
func (t *Tabs) CloseTabAtForExtract(i int, name string) bool {
	if i < 0 || i >= len(t.tabs) || t.names[i] != name {
		return false
	}
	t.removeAt(i)
	return true
}

// ActiveSavedTab returns the active tab's saved record (the same per-tab record
// SavedState writes), for the extract-terminal-tab action that moves a tab to
// another workspace. A dock with no tabs reports false.
func (t *Tabs) ActiveSavedTab() (terminal.SavedTab, bool) {
	st := t.SavedState()
	if len(st.Tabs) == 0 || t.active < 0 || t.active >= len(st.Tabs) {
		return terminal.SavedTab{}, false
	}
	return st.Tabs[t.active], true
}

// AppendRestoredTab appends a saved tab as a read-only/dormant panel with its
// transcript, launch record and styled screen snapshot, and returns its index.
// It does not run anything: a tab that was live at the capture keeps its live
// flag, so the target revives it on the first dive (the dock's normal dive rule),
// never at append time. The existing tabs and the active index stay as they were.
func (t *Tabs) AppendRestoredTab(tab terminal.SavedTab) int {
	var p Panel
	p.SetHost(t.host)
	p.SetPorts(t.ports)
	p.SetEffects(t.effects)
	p.gen = t.genBase()
	p.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: tab.Open, Cwd: tab.Cwd, Width: tab.Width,
		Title: tab.Title, Rows: tab.Rows, LaunchArgv: tab.LaunchArgv, ResumeArgv: tab.ResumeArgv,
		Agent: tab.Agent, Ephemeral: tab.Ephemeral, LaunchDir: tab.LaunchDir,
		Source: tab.Source, Live: tab.Live, Screen: tab.Screen,
	})
	t.tabs = append(t.tabs, &p)
	t.names = append(t.names, tab.Name)
	if len(t.tabs) == 1 {
		t.active = 0
	}
	return len(t.tabs) - 1
}

// Restore puts a saved terminal state back. A record with tabs restores every tab
// as a read-only transcript with its saved name and launch record; a record
// without tabs restores the single terminal exactly as before. The active tab is
// the saved one, clamped to the restored set. Nothing runs automatically.
func (t *Tabs) Restore(st terminal.SavedState) {
	t.Close()
	t.tabs = nil
	t.names = nil
	t.active = 0
	t.dive = nil
	saved := st.Tabs
	if len(saved) == 0 {
		saved = []terminal.SavedTab{{
			Open: st.Open, Cwd: st.Cwd, Width: st.Width, Title: st.Title, Rows: st.Rows,
			LaunchArgv: st.LaunchArgv, ResumeArgv: st.ResumeArgv,
			Agent: st.Agent, Ephemeral: st.Ephemeral, LaunchDir: st.LaunchDir,
			Source: st.Source, Live: st.Live, Screen: st.Screen,
			ExitCode: st.ExitCode, ExitKnown: st.ExitKnown,
		}}
	}
	for _, tab := range saved {
		var p Panel
		p.SetHost(t.host)
		p.SetPorts(t.ports)
		p.SetEffects(t.effects)
		p.gen = t.genBase()
		p.Restore(terminal.SavedState{
			Version: st.Version, Open: tab.Open, Cwd: tab.Cwd, Width: tab.Width,
			Title: tab.Title, Rows: tab.Rows, LaunchArgv: tab.LaunchArgv, ResumeArgv: tab.ResumeArgv,
			Agent: tab.Agent, Ephemeral: tab.Ephemeral, LaunchDir: tab.LaunchDir,
			Source: tab.Source, Live: tab.Live, Screen: tab.Screen,
			ExitCode: tab.ExitCode, ExitKnown: tab.ExitKnown,
		})
		t.tabs = append(t.tabs, &p)
		t.names = append(t.names, tab.Name)
	}
	if len(t.tabs) > 0 {
		t.active = min(max(st.ActiveTab, 0), len(t.tabs)-1)
	}
}
