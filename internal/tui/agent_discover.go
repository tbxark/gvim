package tui

import (
	"context"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/agentdiscover"
	"github.com/tbxark/gvim/internal/terminal"
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

// The agent-discovery picker chrome and notices are resolved on each use so a live locale
// change is seen without a restart.
func agentDiscoverNoDaemon() string {
	return uiText("gvim.agent_discover.no_daemon", "Import needs a daemon-hosted session: the workspace layer is not available here")
}
func agentDiscoverNoQueuedLaunch() string {
	return uiText("gvim.agent_discover.no_queued_launch", "Import: this daemon build cannot start the session's terminal in the new workspace; the workspace was created - open its terminal and run: ")
}
func agentDiscoverNoTmux() string {
	return uiText("gvim.agent_discover.no_tmux", "tmux is not running and no Claude Code/Codex transcripts were found")
}
func agentDiscoverResumeFailed() string {
	return uiText("gvim.agent_discover.resume_failed", "Terminal: could not resume the discovered agent session")
}
func agentDiscoverNoSessionID() string {
	return uiText("gvim.agent_discover.no_session_id", "Import: the session records no directory")
}
func agentDiscoverTitle() string {
	return uiText("gvim.agent_discover.title", "Discover Agent Sessions")
}
func agentDiscoverScanning() string {
	return uiText("gvim.agent_discover.scanning", "Scanning for local agent sessions…")
}
func agentDiscoverNone() string {
	return uiText("gvim.agent_discover.none", "No agent sessions found.")
}
func agentDiscoverFooter() string {
	return uiText("gvim.agent_discover.footer", "Enter opens · Alt+I imports as a workspace · Ctrl+R scans again · Esc closes")
}
func agentDiscoverMissing() string {
	return uiText("gvim.agent_discover.missing", "Agent discovery is not available in this session")
}

// agentDiscoverOverlay is the discovery picker. It owns the report, so nothing is
// stored on the root model and a closed picker leaves nothing behind.
type agentDiscoverOverlay struct {
	m      *Model
	p      *picker.Picker
	seq    int // scan generation: a report for an older scan is dropped
	report agentdiscover.Report
	rows   []discoverRow // the rows, indexed by picker item ID
}

// discoverRow is one row of the merged picker: an outside tmux session, or a
// local Claude Code/Codex transcript. Exactly one field is set.
type discoverRow struct {
	tmux     *agentdiscover.Session
	external *agentdiscover.ExternalSession
}

// agentDiscoverDoneMsg carries one scan's report back to the update loop.
type agentDiscoverDoneMsg struct {
	ov     *agentDiscoverOverlay // the picker instance that started this scan
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
		if done.ov != o {
			return o, nil // a scan from a closed/replaced picker instance
		}
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
		// Alt+I imports the selected session as a workspace (create-only: the registry
		// v1 has no place for the attach argv, so the terminal is opened from the
		// workspace afterwards; Enter still opens the attach tab directly). A bare i
		// is filter text.
		if k.Code == 'i' && k.Mod == tea.ModAlt {
			return o.importSelected()
		}
		// Ctrl+R scans again without leaving the picker.
		if k.Code == 'r' && k.Mod&tea.ModCtrl != 0 {
			o.p.SetStatus(agentDiscoverScanning())
			return o, o.scan()
		}
	}
	return o, o.cmdFor(o.p.Update(msg))
}

func (o *agentDiscoverOverlay) Draw(g *grid.Grid) { o.p.Draw(g) }
func (o *agentDiscoverOverlay) Active() bool      { return o.p.Active() }

// ExclusiveOverlay marks the picker as a transient interaction. See
// overlay.Exclusive.
func (*agentDiscoverOverlay) ExclusiveOverlay() {}

// cmdFor turns what the picker asks of its owner into commands: the ranking, and
// the chosen row opening in a new dock tab. A tmux row attaches; a Claude
// Code/Codex transcript row starts its agent's resume command.
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
		row := o.rows[chosen.ID]
		if row.tmux != nil {
			session := *row.tmux
			cmd, ok := o.m.dock.StartDiscovered(session.AttachArgv, discoverTabName(session))
			if !ok {
				o.m.notice = uiText("gvim.notice.agent.terminal_open_failed", "Terminal: could not open the discovered session")
				continue
			}
			cmds = append(cmds, cmd)
			continue
		}
		if row.external != nil {
			cmds = append(cmds, o.startExternal(*row.external))
		}
	}
	return tea.Batch(cmds...)
}

// startExternal starts the discovered transcript's resume command in a new
// ephemeral terminal tab, in the session's directory. A repeated choice of the
// same session focuses the tab that already resumed it instead of starting a
// second copy. The argv is the fixed registry template built by the scanner; the
// transcript itself never supplies a program, only the validated session id.
func (o *agentDiscoverOverlay) startExternal(session agentdiscover.ExternalSession) tea.Cmd {
	source := externalSource(session)
	if source != "" {
		if idx, ok := o.m.dock.FindSource(source); ok {
			o.m.dock.SetActive(idx)
			o.m.focusDock()
			return o.m.showDock()
		}
	}
	launch := terminal.Launch{
		Argv:       append([]string(nil), session.ResumeArgv...),
		Agent:      session.Agent,
		ResumeArgv: append([]string(nil), session.ResumeArgv...),
		Ephemeral:  true,
		Source:     source,
	}
	cmd, ok := o.m.dock.StartProgram(launch, externalTabName(session), session.CWD)
	if !ok {
		o.m.notice = agentDiscoverResumeFailed()
		return nil
	}
	return cmd
}

// importSelected creates (or reuses) a workspace for the selected row's
// directory and closes the picker. A tmux row imports its attach argv; a
// transcript row imports its resume command. The registry enforces one workspace
// per canonical root; Import needs the daemon's workspace layer.
func (o *agentDiscoverOverlay) importSelected() (Overlay, tea.Cmd) {
	id, ok := o.p.SelectedID()
	if !ok || id < 0 || id >= len(o.rows) {
		return o, nil
	}
	row := o.rows[id]
	if o.m.workspace == nil {
		o.m.notice = agentDiscoverNoDaemon()
		return o, nil
	}
	var root, label string
	var launch WorkspaceLaunch
	var fallback string
	switch {
	case row.tmux != nil:
		session := *row.tmux
		root = strings.TrimSpace(session.Path)
		label = discoverImportLabel(session)
		launch = discoverWorkspaceLaunch(session)
		fallback = strings.Join(session.AttachArgv, " ")
	case row.external != nil:
		session := *row.external
		root = strings.TrimSpace(session.CWD)
		label = discoverExternalImportLabel(session)
		launch = discoverExternalWorkspaceLaunch(session)
		fallback = strings.Join(session.ResumeArgv, " ")
	default:
		return o, nil
	}
	if root == "" {
		o.m.notice = agentDiscoverNoSessionID()
		return o, nil
	}
	if launcher, ok := o.m.workspace.(WorkspaceLauncher); ok {
		cmd, err := launcher.CreateWithLaunch(root, label, launch)
		if err != nil {
			o.m.notice = uiTextVars("gvim.notice.agent.import_error", "Import: %{error}", map[string]any{"error": err.Error()})
			return o, nil
		}
		return nil, cmd
	}
	// The daemon's workspace layer predates the queued launch: create the workspace
	// and state the reduction with the command that rejoins the session.
	cmd, err := o.m.workspace.Create(root, label)
	if err != nil {
		o.m.notice = uiTextVars("gvim.notice.agent.import_error", "Import: %{error}", map[string]any{"error": err.Error()})
		return o, nil
	}
	o.m.notice = agentDiscoverNoQueuedLaunch() + fallback
	return nil, cmd
}

// discoverWorkspaceLaunch is the terminal Import starts in the new workspace for
// a tmux row: the qualified attach argv as launch and resume, in its directory.
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

// discoverExternalWorkspaceLaunch is the terminal Import starts for a transcript
// row: the agent's fixed resume argv, run in the recorded session directory.
func discoverExternalWorkspaceLaunch(session agentdiscover.ExternalSession) WorkspaceLaunch {
	return WorkspaceLaunch{
		Argv:       append([]string(nil), session.ResumeArgv...),
		Name:       externalTabName(session),
		Agent:      session.Agent,
		ResumeArgv: append([]string(nil), session.ResumeArgv...),
		Ephemeral:  true,
		Dir:        session.CWD,
		Source:     externalSource(session),
	}
}

// discoverImportLabel is the workspace label of an imported tmux session.
func discoverImportLabel(session agentdiscover.Session) string {
	return "tmux:" + session.Name
}

// discoverExternalImportLabel is the workspace label of an imported transcript
// session: the tool and the session's title, bounded.
func discoverExternalImportLabel(session agentdiscover.ExternalSession) string {
	return clipDiscoveryText(session.Agent+":"+session.Title, 80)
}

// scan starts one scan in a command and returns the report's message.
func (o *agentDiscoverOverlay) scan() tea.Cmd {
	o.seq++
	seq := o.seq
	discover := o.m.discover
	return func() tea.Msg {
		return agentDiscoverDoneMsg{ov: o, seq: seq, report: discover.Discover(context.Background())}
	}
}

// fill installs one scan's report: the merged rows, their labels and a status
// line that states what could not be read or queried.
func (o *agentDiscoverOverlay) fill(report agentdiscover.Report) picker.Result {
	o.report = report
	rows := make([]discoverRow, 0, len(report.Sessions)+len(report.External.Sessions))
	for i := range report.Sessions {
		rows = append(rows, discoverRow{tmux: &report.Sessions[i]})
	}
	for i := range report.External.Sessions {
		rows = append(rows, discoverRow{external: &report.External.Sessions[i]})
	}
	o.rows = rows
	items := make([]picker.Item, len(rows))
	for i, row := range rows {
		items[i] = picker.Item{ID: i, Text: discoverRowText(row), Detail: discoverRowDetail(row)}
	}
	res := o.p.SetItems(items)
	o.p.SetStatus(discoverStatus(report))
	return res
}

// discoverRowText is a row's title: a tmux session's name with its markers, or a
// transcript's tool and directory title.
func discoverRowText(row discoverRow) string {
	if row.tmux != nil {
		text := row.tmux.Name
		if row.tmux.Attached {
			text += " · attached"
		}
		if len(row.tmux.Agents) > 0 {
			text += " · " + strings.Join(row.tmux.Agents, ",")
		}
		return text
	}
	if row.external == nil {
		return ""
	}
	return externalToolName(row.external.Tool) + " · " + row.external.Title
}

// discoverRowDetail is a row's second line: for tmux the server and panes, for a
// transcript the directory, the resume command and its exactness.
func discoverRowDetail(row discoverRow) string {
	if row.tmux != nil {
		parts := []string{"tmux", row.tmux.Server}
		if row.tmux.Panes > 1 {
			parts = append(parts, "panes")
		} else if row.tmux.Panes == 1 {
			parts = append(parts, "1 pane")
		}
		if row.tmux.Path != "" {
			parts = append(parts, row.tmux.Path)
		}
		if row.tmux.SocketOnly {
			parts = append(parts, "server not queried")
		}
		return strings.Join(parts, " · ")
	}
	if row.external == nil {
		return ""
	}
	session := row.external
	parts := []string{"resume: " + strings.Join(session.ResumeArgv, " ")}
	if session.CWD != "" {
		parts = append(parts, session.CWD)
	}
	if !session.Exact {
		parts = append(parts, "resumes the newest session in that directory")
	}
	return strings.Join(parts, " · ")
}

// discoverStatus is the picker's status line: the scan's problems, why the list
// is empty, or that a bound cut some rows.
func discoverStatus(report agentdiscover.Report) string {
	problems := append(append([]string(nil), report.Problems...), report.External.Problems...)
	if len(problems) > 0 {
		return "Discovery problems: " + strings.Join(problems, "; ")
	}
	if len(report.Sessions) == 0 && len(report.External.Sessions) == 0 {
		if !report.Installed && !report.External.Installed {
			return agentDiscoverNoTmux()
		}
		return agentDiscoverNone()
	}
	if report.Truncated || report.External.Truncated {
		return "Some sessions were left out: the scan hit its bound"
	}
	if !report.AllowCommands && len(report.Sessions) > 0 {
		return "tmux servers were not queried; their session lists are unknown"
	}
	return ""
}

// discoverTabName is the dock tab label of a discovered tmux session.
func discoverTabName(session agentdiscover.Session) string {
	name := "tmux:" + session.Name
	if len(session.Agents) > 0 {
		name += " · " + strings.Join(session.Agents, ",")
	}
	return clipDiscoveryText(name, 80)
}

// externalToolName is the display name of an external store.
func externalToolName(tool string) string {
	if tool == agentdiscover.ExternalCodex {
		return "Codex CLI"
	}
	return "Claude Code"
}

// externalSource is a transcript session's durable identity in the dock, used to
// focus an existing resumed tab instead of opening a duplicate.
func externalSource(session agentdiscover.ExternalSession) string {
	if session.ID == "" {
		return ""
	}
	return session.Tool + ":" + session.ID
}

// externalTabName is the dock tab label of a resumed transcript session.
func externalTabName(session agentdiscover.ExternalSession) string {
	name := externalToolName(session.Tool) + ":" + session.Title
	return clipDiscoveryText(name, 80)
}

// clipDiscoveryText bounds a display label to limit bytes, cut on a rune boundary.
func clipDiscoveryText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	n := limit
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	return text[:n]
}

// openAgentDiscover opens the discovery picker and starts its first scan. The
// picker shows at once in the scanning state; the report arrives as a message.
func (m *Model) openAgentDiscover() tea.Cmd {
	if m.discover == nil {
		m.notice = agentDiscoverMissing()
		return nil
	}
	p := picker.New(m.styles, picker.Options{
		Title:       agentDiscoverTitle(),
		Footer:      agentDiscoverFooter(),
		Placeholder: "filter sessions",
		Fuzzy:       true,
	})
	p.SetStatus(agentDiscoverScanning())
	ov := &agentDiscoverOverlay{m: m, p: p}
	m.pushOverlay(ov)
	return ov.scan()
}
