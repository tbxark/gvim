package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/agentdiscover"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/picker"
)

// Agent discovery: the "Discover Agent Sessions..." palette action scans the
// machine (through the app's trust-gated discoverer) for tmux sessions that are
// already running outside gvim, lists them, and opens the chosen session's attach
// argv in a new dock tab on a user action. The scan runs in a tea.Cmd, never in
// Update, and its report comes back as a message, so the picker opens at once in
// a scanning state and stays responsive.
//
// Level 0 in Fresh's terms: tmux draws the attached panes, so gvim's splits,
// scrollback search and links stop at that client. Import-as-workspace and dock
// rows come with the workspace registry; this file only reattaches.

// AgentDiscoverer runs one discovery scan off the update loop. The app implements
// it over internal/agentdiscover, deciding the tmux commands' trust gate.
type AgentDiscoverer interface {
	Discover(context.Context) agentdiscover.Report
}

// WorkspaceLaunch is the terminal program Import starts in a workspace's dock once
// the workspace is materialized and shown: the discovered session's attach argv as
// both its launch and its resume, the launch directory, and the durable external
// identity (Source, for example tmux:default/work) so repeated imports can find the
// existing attach tab instead of duplicating it.
type WorkspaceLaunch struct {
	Argv       []string
	Name       string // the new terminal tab's label
	Agent      string // empty for a discovered non-agent session
	ResumeArgv []string
	Ephemeral  bool
	Dir        string // the launch directory (the imported session's path)
	Source     string // stable external identity
}

// WorkspaceLauncher is the optional workspace-port method the discovery picker
// uses: it creates (or reuses) the workspace and starts launch in its dock once
// shown. The app implements it in addition to WorkspacePort, so existing ports and
// fakes keep working.
type WorkspaceLauncher interface {
	CreateWithLaunch(root, label string, launch WorkspaceLaunch) (WorkspaceCmd, error)
}

// SetAgentDiscoverer gives the root its discovery scanner. Nil disables the
// Discover Agent Sessions row's scan and shows a notice instead.
func (m *Model) SetAgentDiscoverer(d AgentDiscoverer) { m.discover = d }

const (
	agentDiscoverTitle    = "Discover Agent Sessions"
	agentDiscoverScanning = "Scanning for tmux sessions…"
	agentDiscoverNone     = "No agent sessions found."
	agentDiscoverFooter   = "Enter opens · i imports as a workspace · Ctrl+R scans again · Esc closes"
	agentDiscoverMissing  = "Agent discovery is not available in this session"
	// agentDiscoverNoDaemon is Import's notice outside a daemon-hosted session.
	agentDiscoverNoDaemon = "Import needs a daemon-hosted session: the workspace layer is not available here"
	// agentDiscoverNoQueuedLaunch is Import's fallback notice when the daemon's
	// workspace layer predates the queued launch: the workspace exists but the
	// session's terminal was not started there.
	agentDiscoverNoQueuedLaunch = "Import: this daemon build cannot start the session's terminal in the new workspace; the workspace was created - open its terminal and run: "
	agentDiscoverNoTmux         = "tmux is not running on this machine (no socket found in $TMUX_TMPDIR or /tmp)"
)

// agentDiscoverOverlay is the discovery picker. It owns the report, so nothing is
// stored on the root model and a closed picker leaves nothing behind.
type agentDiscoverOverlay struct {
	m      *Model
	p      *picker.Picker
	seq    int // scan generation: a report for an older scan is dropped
	report agentdiscover.Report
	rows   []agentdiscover.Session // the sessions, indexed by picker item ID
}

// agentDiscoverDoneMsg carries one scan's report back to the update loop.
type agentDiscoverDoneMsg struct {
	seq    int
	report agentdiscover.Report
}

// agentDiscoverRankMsg carries a ranking result back to the picker that asked for
// it, so ranking happens in a command like every other picker's.
type agentDiscoverRankMsg struct {
	ov  *agentDiscoverOverlay
	msg tea.Msg
}

func (o *agentDiscoverOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if done, ok := msg.(agentDiscoverDoneMsg); ok {
		if done.seq != o.seq {
			return o, nil // an older scan answered after a refresh
		}
		return o, o.cmdFor(o.fill(done.report))
	}
	if rank, ok := msg.(agentDiscoverRankMsg); ok && rank.ov == o {
		return o, o.cmdFor(o.p.Update(rank.msg))
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		k := tea.Key(key)
		// i imports the selected session as a workspace (create-only: the registry
		// v1 has no place for the attach argv, so the terminal is opened from the
		// workspace afterwards; Enter still opens the attach tab directly).
		if k.Code == 'i' && k.Mod == 0 {
			return o.importSelected()
		}
		// Ctrl+R scans again without leaving the picker.
		if k.Code == 'r' && k.Mod&tea.ModCtrl != 0 {
			o.p.SetStatus(agentDiscoverScanning)
			return o, o.scan()
		}
	}
	return o, o.cmdFor(o.p.Update(msg))
}

func (o *agentDiscoverOverlay) Draw(g *grid.Grid) { o.p.Draw(g) }
func (o *agentDiscoverOverlay) Active() bool      { return o.p.Active() }

// cmdFor turns what the picker asks of its owner into commands: the ranking, and
// the chosen session opening in a new dock tab.
func (o *agentDiscoverOverlay) cmdFor(r picker.Result) tea.Cmd {
	var cmds []tea.Cmd
	if r.Cmd != nil {
		cmds = append(cmds, r.Cmd)
	}
	if r.Rank != nil {
		rank := r.Rank
		cmds = append(cmds, func() tea.Msg { return agentDiscoverRankMsg{ov: o, msg: rank()} })
	}
	for _, event := range r.Events {
		chosen, ok := event.(picker.Chosen)
		if !ok || chosen.ID < 0 || chosen.ID >= len(o.rows) {
			continue
		}
		session := o.rows[chosen.ID]
		cmd, ok := o.m.dock.StartDiscovered(session.AttachArgv, discoverTabName(session))
		if !ok {
			o.m.notice = "Terminal: could not open the discovered session"
			continue
		}
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// importSelected creates (or reuses) a workspace for the selected session's
// directory and closes the picker. The workspace registry enforces one workspace
// per canonical root, so importing the same directory twice reuses it; a session
// without a recorded directory cannot be imported. Import needs the daemon's
// workspace layer; a direct launch states that instead.
func (o *agentDiscoverOverlay) importSelected() (Overlay, tea.Cmd) {
	id, ok := o.p.SelectedID()
	if !ok || id < 0 || id >= len(o.rows) {
		return o, nil
	}
	session := o.rows[id]
	if o.m.workspace == nil {
		o.m.notice = agentDiscoverNoDaemon
		return o, nil
	}
	root := strings.TrimSpace(session.Path)
	if root == "" {
		o.m.notice = "Import: the session records no directory"
		return o, nil
	}
	label := discoverImportLabel(session)
	if launcher, ok := o.m.workspace.(WorkspaceLauncher); ok {
		cmd, err := launcher.CreateWithLaunch(root, label, discoverWorkspaceLaunch(session))
		if err != nil {
			o.m.notice = "Import: " + err.Error()
			return o, nil
		}
		return nil, cmd
	}
	// The daemon's workspace layer predates the queued launch: create the workspace
	// and state the reduction with the command that rejoins the session.
	cmd, err := o.m.workspace.Create(root, label)
	if err != nil {
		o.m.notice = "Import: " + err.Error()
		return o, nil
	}
	o.m.notice = agentDiscoverNoQueuedLaunch + strings.Join(session.AttachArgv, " ")
	return nil, cmd
}

// discoverWorkspaceLaunch is the terminal Import starts in the new workspace: the
// session's qualified attach argv as launch and resume, in its directory, with the
// durable source identity.
func discoverWorkspaceLaunch(session agentdiscover.Session) WorkspaceLaunch {
	return WorkspaceLaunch{
		Argv:       append([]string(nil), session.AttachArgv...),
		Name:       discoverTabName(session),
		Agent:      strings.Join(session.Agents, ","),
		ResumeArgv: append([]string(nil), session.AttachArgv...),
		Ephemeral:  true,
		Dir:        session.Path,
		Source:     "tmux:" + session.ID,
	}
}

// discoverImportLabel is the workspace label of an imported session.
func discoverImportLabel(session agentdiscover.Session) string {
	return "tmux:" + session.Name
}

// scan starts one scan in a command and returns the report's message.
func (o *agentDiscoverOverlay) scan() tea.Cmd {
	o.seq++
	seq := o.seq
	discover := o.m.discover
	return func() tea.Msg {
		return agentDiscoverDoneMsg{seq: seq, report: discover.Discover(context.Background())}
	}
}

// fill installs one scan's report: the rows, their labels and a status line that
// states what could not be queried.
func (o *agentDiscoverOverlay) fill(report agentdiscover.Report) picker.Result {
	o.report = report
	o.rows = report.Sessions
	items := make([]picker.Item, len(report.Sessions))
	for i, session := range report.Sessions {
		items[i] = picker.Item{ID: i, Text: discoverRowText(session), Detail: discoverRowDetail(session)}
	}
	res := o.p.SetItems(items)
	o.p.SetStatus(discoverStatus(report))
	return res
}

// discoverRowText is a session's row title: its name, with the attachment and
// probable-agent markers.
func discoverRowText(session agentdiscover.Session) string {
	text := session.Name
	if session.Attached {
		text += " · attached"
	}
	if len(session.Agents) > 0 {
		text += " · " + strings.Join(session.Agents, ",")
	}
	return text
}

// discoverRowDetail is a session's second row: the server, pane count and path.
func discoverRowDetail(session agentdiscover.Session) string {
	parts := []string{"tmux", session.Server}
	if session.Panes > 1 {
		parts = append(parts, "panes")
	} else if session.Panes == 1 {
		parts = append(parts, "1 pane")
	}
	if session.Path != "" {
		parts = append(parts, session.Path)
	}
	if session.SocketOnly {
		parts = append(parts, "server not queried")
	}
	return strings.Join(parts, " · ")
}

// discoverStatus is the picker's status line: the scan's problems, or why the
// list is empty.
func discoverStatus(report agentdiscover.Report) string {
	if len(report.Problems) > 0 {
		return "Discovery problems: " + strings.Join(report.Problems, "; ")
	}
	if !report.Installed {
		return agentDiscoverNoTmux
	}
	if len(report.Sessions) == 0 {
		return agentDiscoverNone
	}
	if report.Truncated {
		return "Some sessions were left out: the scan hit its bound"
	}
	if !report.AllowCommands {
		return "tmux servers were not queried; their session lists are unknown"
	}
	return ""
}

// discoverTabName is the dock tab label of a discovered session: the tmux session
// name, prefixed so it reads as somebody else's session.
func discoverTabName(session agentdiscover.Session) string {
	name := "tmux:" + session.Name
	if len(session.Agents) > 0 {
		name += " · " + strings.Join(session.Agents, ",")
	}
	return name
}

// openAgentDiscover opens the discovery picker and starts its first scan. The
// picker shows at once in the scanning state; the report arrives as a message.
func (m *Model) openAgentDiscover() tea.Cmd {
	if m.discover == nil {
		m.notice = agentDiscoverMissing
		return nil
	}
	p := picker.New(m.styles, picker.Options{
		Title:       agentDiscoverTitle,
		Footer:      agentDiscoverFooter,
		Placeholder: "filter sessions",
		Fuzzy:       true,
	})
	p.SetStatus(agentDiscoverScanning)
	ov := &agentDiscoverOverlay{m: m, p: p}
	m.pushOverlay(ov)
	return ov.scan()
}
