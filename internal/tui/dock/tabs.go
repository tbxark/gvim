package dock

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

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

// Tabs is the dock's terminal-tab container. The zero value is an empty dock; the
// root creates tabs through Show/NewTab.
type Tabs struct {
	tabs    []*Panel
	active  int
	host    Host
	ports   Ports
	effects Effects
	names   []string // per tab: "" means the default name
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

// tabLabel is the strip/title label of tab i.
func (t *Tabs) tabLabel(i int) string {
	name := ""
	if i < len(t.names) {
		name = t.names[i]
	}
	if name == "" {
		name = "Terminal " + strconv.Itoa(i+1)
	}
	if t.tabs[i].Exited() {
		name += " (exited)"
	}
	return name
}

// currentName is the active tab's label.
func (t *Tabs) currentName() string {
	if len(t.tabs) == 0 {
		return "Terminal"
	}
	return t.tabLabel(t.active)
}

// tabStartedMsg is the started message of one tab, tagged with the tab's index so
// the container routes it to that tab only: startedMsg itself carries no
// generation, and two tabs starting in a row would otherwise each consume the
// other's message.
type tabStartedMsg struct {
	index int
	inner tea.Msg
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
	index := t.active
	return func() tea.Msg { return tabStartedMsg{index: index, inner: cmd()} }
}

// SetActive makes tab i active. It reports false for an out-of-range index.
func (t *Tabs) SetActive(i int) bool {
	if i < 0 || i >= len(t.tabs) || i == t.active {
		return i >= 0 && i < len(t.tabs)
	}
	wasFocused := t.Active() != nil && t.Active().Focus()
	t.Active().SetFocus(false)
	t.active = i
	if wasFocused {
		t.Active().SetFocus(true)
	}
	return true
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
}

// Restart restarts the active tab's shell, keeping its transcript. The start is
// tagged with the tab index, like NewTab's, so its result reaches that tab only.
func (t *Tabs) Restart() tea.Cmd {
	if len(t.tabs) == 0 {
		return t.NewTab()
	}
	index := t.active
	cmd := t.Active().Restart()
	if cmd == nil {
		return nil
	}
	return func() tea.Msg { return tabStartedMsg{index: index, inner: cmd()} }
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
	t.tabs = append(t.tabs, &p)
	t.names = append(t.names, name)
	t.active = len(t.tabs) - 1
	cmd, ok := p.StartAgent(name)
	if cmd == nil {
		return nil, ok
	}
	index := t.active
	return func() tea.Msg { return tabStartedMsg{index: index, inner: cmd()} }, ok
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
	for i, p := range t.tabs {
		update := msg
		if ts, ok := msg.(tabStartedMsg); ok {
			if ts.index != i {
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
	for _, p := range t.tabs {
		if cmd := p.Sync(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// TabKey handles the dock's quick-switch keys. It reports whether k was one of
// them. The keys work while the dock has the keyboard focus, even in keyboard
// capture, and are documented in docs/terminal.md; they are not keymap data.
func (t *Tabs) TabKey(k tea.Key) bool {
	switch {
	case k.Code == tea.KeyPgDown && k.Mod == tea.ModCtrl:
		t.Next()
		return true
	case k.Code == tea.KeyPgUp && k.Mod == tea.ModCtrl:
		t.Previous()
		return true
	}
	return false
}

// Mouse routes mouse input: a click on the dock's title row selects a tab, and
// everything else goes to the active tab's body.
func (t *Tabs) Mouse(msg tea.Msg, l frame.Layout, count int) (tea.Cmd, bool) {
	if click, ok := msg.(tea.MouseClickMsg); ok && l.DockOn && click.Button == tea.MouseLeft && click.Y == l.DockY {
		if i, ok := t.tabAtX(l.W, click.X); ok {
			t.SetActive(i)
			// A press on the strip is still a press on the dock: it takes the
			// keyboard when a shell runs (the host's focus gate refuses otherwise).
			if t.host.Focus != nil {
				t.host.Focus()
			}
			return nil, true
		}
		return nil, true
	}
	p := t.Active()
	if p == nil {
		return nil, false
	}
	return p.Mouse(msg, l, count)
}

// tabAtX maps a title-row column to a tab index. The strip mirrors drawStrip.
func (t *Tabs) tabAtX(w, x int) (int, bool) {
	if x <= 0 || len(t.tabs) == 0 {
		return 0, false
	}
	col := 1
	for i := range t.tabs {
		label := " " + t.tabLabel(i) + " "
		n := len([]rune(label))
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
		label := " " + t.tabLabel(i) + " "
		cell := inactive
		if i == t.active {
			cell = active
		}
		x += g.Put(x, l.DockY, l.W-1, label, cell)
		if x >= l.W-1 {
			return
		}
	}
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
		rec := p.SavedState()
		st.Tabs[i] = terminal.SavedTab{
			Name: t.names[i], Open: rec.Open, Cwd: rec.Cwd, Width: rec.Width,
			Rows: rec.Rows, LaunchArgv: rec.LaunchArgv, ResumeArgv: rec.ResumeArgv,
			Agent: rec.Agent, Ephemeral: rec.Ephemeral,
		}
	}
	st.ActiveTab = t.active
	return st
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
	saved := st.Tabs
	if len(saved) == 0 {
		saved = []terminal.SavedTab{{
			Open: st.Open, Cwd: st.Cwd, Width: st.Width, Rows: st.Rows,
			LaunchArgv: st.LaunchArgv, ResumeArgv: st.ResumeArgv,
			Agent: st.Agent, Ephemeral: st.Ephemeral,
		}}
	}
	for _, tab := range saved {
		var p Panel
		p.SetHost(t.host)
		p.SetPorts(t.ports)
		p.SetEffects(t.effects)
		p.Restore(terminal.SavedState{
			Version: st.Version, Open: tab.Open, Cwd: tab.Cwd, Width: tab.Width,
			Rows: tab.Rows, LaunchArgv: tab.LaunchArgv, ResumeArgv: tab.ResumeArgv,
			Agent: tab.Agent, Ephemeral: tab.Ephemeral,
		})
		t.tabs = append(t.tabs, &p)
		t.names = append(t.names, tab.Name)
	}
	if len(t.tabs) > 0 {
		t.active = min(max(st.ActiveTab, 0), len(t.tabs)-1)
	}
}
