package dock

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
)

// StartDiscovered starts argv in a new ephemeral tab named name and makes it
// active. It is the discovery picker's open action: argv is an external rejoin
// command (tmux -S <socket> attach -t <session>), not a configured agent, so the
// tab carries no agent name and the record keeps the same argv as its resume: a
// user restart reattaches to the same session. The tab is ephemeral because the
// process is somebody else's session, not a shell the dock owns. It is additive:
// the tab lifecycle mirrors NewTab and StartAgent, and no existing tab code
// changes.
func (t *Tabs) StartDiscovered(argv []string, name string) (tea.Cmd, bool) {
	return t.startLaunchRecordAt(terminal.Launch{Argv: argv, ResumeArgv: argv, Ephemeral: true}, name, t.LaunchDir())
}

// StartAgentChoice starts a configured agent with the user's launch choice.
// resume runs the agent's resume argv now (a user-initiated rejoin, so it is
// allowed even with terminal.resume_agents off, which only gates automatic
// restarts); otherwise the new-session argv runs. The tab is named after the
// agent and the record keeps both argv lists, so a later Restart follows the
// configured gate.
func (t *Tabs) StartAgentChoice(name string, resume bool) (tea.Cmd, bool) {
	return t.StartAgentChoiceAt(name, resume, t.LaunchDir())
}

// StartAgentChoiceAt is StartAgentChoice with an explicit start directory: the Run
// Agent dialog's directory answer. An empty dir falls back to the workspace root.
func (t *Tabs) StartAgentChoiceAt(name string, resume bool, dir string) (tea.Cmd, bool) {
	if strings.TrimSpace(dir) == "" {
		dir = t.LaunchDir()
	}
	for _, agent := range t.effects.Agents {
		if agent.Name != name {
			continue
		}
		argv := agent.Argv
		if resume && len(agent.ResumeArgv) > 0 {
			argv = agent.ResumeArgv
		}
		return t.startLaunchRecordAt(terminal.Launch{
			Argv: argv, Agent: agent.Name, ResumeArgv: agent.ResumeArgv, Ephemeral: true,
		}, agent.Name, dir)
	}
	return nil, false
}

// StartProgram starts launch as a new tab named name in dir, with its whole record:
// the launch argv, the agent name, the resume argv, the ephemeral flag, the source
// identity and the launch directory. The workspace Import path uses it (unlike
// StartDiscovered it takes the full launch and an explicit directory); an empty dir
// falls back to the workspace root.
func (t *Tabs) StartProgram(launch terminal.Launch, name, dir string) (tea.Cmd, bool) {
	if strings.TrimSpace(dir) == "" {
		dir = t.LaunchDir()
	}
	return t.startLaunchRecordAt(launch, name, dir)
}

// FindSource returns the index of the tab whose launch record has source, so an
// Import can focus the existing attach tab instead of opening a duplicate.
func (t *Tabs) FindSource(source string) (int, bool) {
	if source == "" {
		return 0, false
	}
	for i, p := range t.tabs {
		if p.launch.Source == source {
			return i, true
		}
	}
	return 0, false
}

// LaunchDir is the default directory Run Agent starts a terminal in the dock: the
// current workspace root. The dialog can override it per launch.
func (t *Tabs) LaunchDir() string {
	if t.host.RootDir != nil {
		return t.host.RootDir()
	}
	return ""
}

// ResumeAgents reports whether terminal.resume_agents is on, for the Run Agent
// dialog's hint about what a later Restart does.
func (t *Tabs) ResumeAgents() bool { return t.effects.ResumeAgents }

// startLaunchRecordAt is the shared body of the discovered-session and
// agent-choice starts: a new tab in dir, a launch record and a start command
// tagged with the tab index.
func (t *Tabs) startLaunchRecordAt(launch terminal.Launch, name, dir string) (tea.Cmd, bool) {
	launch = launch.Clean()
	if launch.Empty() {
		return nil, false
	}
	if len(t.tabs) >= MaxTerminalTabs {
		if t.host.Notice != nil {
			t.host.Notice(dockText("gvim.dock.tab_limit", "Terminal: the tab limit is reached"))
		}
		return nil, false
	}
	if prev := t.Active(); prev != nil {
		prev.SetFocus(false)
	}
	p := t.newPanel()
	if p.launcher == nil {
		if t.host.Notice != nil {
			t.host.Notice(NoTerminalNotice())
		}
		return nil, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "session"
	}
	t.tabs = append(t.tabs, p)
	t.names = append(t.names, name)
	t.ids = append(t.ids, t.newID())
	t.active = len(t.tabs) - 1
	p.open = true
	p.focus = false
	cmd := p.launchProgram(dir, launch.Argv, launch, launch.Ephemeral)
	if cmd == nil {
		return nil, false
	}
	tab := t.tabs[t.active]
	return func() tea.Msg { return tabStartedMsg{tab: tab, inner: cmd()} }, true
}
