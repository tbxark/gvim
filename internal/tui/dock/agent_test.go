package dock

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
)

// agentPanel returns a dock panel with the host and ports but no shell started.
func agentPanel(effects Effects, starter *fakeStarter) *Panel {
	var p Panel
	p.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	p.SetPorts(Ports{Starter: starter})
	p.SetEffects(effects)
	return &p
}

func TestRestartPrecedence(t *testing.T) {
	t.Parallel()
	claude := terminal.Launch{
		Argv:       []string{"claude", "--session-id", "s1"},
		Agent:      "claude",
		ResumeArgv: []string{"claude", "--resume", "s1"},
	}
	tests := []struct {
		name    string
		resume  bool
		record  terminal.Launch
		want    []string // the StartLaunch argv, nil for the plain shell
		started bool
	}{
		{"resume on uses the resume argv", true, claude, []string{"claude", "--resume", "s1"}, true},
		{"resume off uses the launch argv", false, claude, []string{"claude", "--session-id", "s1"}, true},
		{"launch only", true, terminal.Launch{Argv: []string{"aider"}}, []string{"aider"}, true},
		{"agent without a resume argv uses the launch argv", true, terminal.Launch{Argv: []string{"codex"}, Agent: "codex"}, []string{"codex"}, true},
		{"no record uses the shell", true, terminal.Launch{}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			starter := &fakeStarter{term: newFakeTerm()}
			p := agentPanel(Effects{ResumeAgents: tt.resume}, starter)
			p.launch = tt.record
			cmd := p.Restart()
			if cmd == nil {
				t.Fatal("Restart started no command")
			}
			p.Update(cmd())
			if tt.started {
				if len(starter.launches) != 1 || !slices.Equal(starter.launches[0].Argv, tt.want) {
					t.Fatalf("launches = %+v, want argv %v", starter.launches, tt.want)
				}
				if len(starter.dirs) != 0 {
					t.Fatalf("the shell started too: %v", starter.dirs)
				}
				// The record is kept, so a second restart can resume again.
				if p.launch.Agent != tt.record.Agent {
					t.Fatalf("record agent = %q after the restart", p.launch.Agent)
				}
				return
			}
			if len(starter.launches) != 0 || len(starter.dirs) != 1 {
				t.Fatalf("launches = %+v shell starts = %v, want the shell", starter.launches, starter.dirs)
			}
		})
	}
}

func TestRestoredTerminalNeverStartsAndRestartsWithItsRecord(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	p := agentPanel(Effects{ResumeAgents: true}, starter)
	p.Restore(terminal.SavedTab{Open: true, Cwd: "/ws", Rows: []string{"old output"}, LaunchArgv: []string{"claude", "--session-id", "s1"}, ResumeArgv: []string{"claude", "--resume", "s1"}, Agent: "claude", Ephemeral: true})
	if len(starter.dirs) != 0 || len(starter.launches) != 0 {
		t.Fatalf("restore started something: shell %v launches %+v", starter.dirs, starter.launches)
	}
	if !p.Exited() || !p.Restored() {
		t.Fatalf("restored: exited %v restored %v, want both", p.Exited(), p.Restored())
	}
	if title := p.title(); !strings.Contains(title, "restored") || !strings.Contains(title, "claude") {
		t.Fatalf("title = %q, want the restored agent transcript", title)
	}
	// A failed start keeps the record, so a retry can still resume.
	starter.err = errors.New("no pty")
	p.Update(p.Restart()())
	if len(starter.launches) != 1 || starter.launches[0].Argv[1] != "--resume" {
		t.Fatalf("launches = %+v, want the resume argv", starter.launches)
	}
	if p.launch.Agent != "claude" {
		t.Fatalf("the failed start lost the record: %+v", p.launch)
	}
	starter.err = nil
	p.Update(p.Restart()())
	if len(starter.launches) != 2 || !slices.Equal(starter.launches[1].Argv, []string{"claude", "--resume", "s1"}) {
		t.Fatalf("retry launches = %+v", starter.launches)
	}
}

func TestSavedStateCarriesTheLaunchRecord(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	p := agentPanel(Effects{}, starter)
	p.launch = terminal.Launch{
		Argv: []string{"aider"}, Agent: "aider",
		ResumeArgv: []string{"aider", "--restore"}, Ephemeral: true,
	}
	st := p.SavedState()
	if !slices.Equal(st.LaunchArgv, []string{"aider"}) ||
		!slices.Equal(st.ResumeArgv, []string{"aider", "--restore"}) ||
		st.Agent != "aider" || !st.Ephemeral {
		t.Fatalf("saved launch record = %+v", st)
	}
	var q Panel
	q.SetHost(p.host)
	q.SetPorts(Ports{Starter: starter})
	q.SetEffects(Effects{})
	q.Restore(st)
	if !slices.Equal(q.launch.ResumeArgv, []string{"aider", "--restore"}) || q.launch.Agent != "aider" || !q.launch.Ephemeral {
		t.Fatalf("restored record = %+v", q.launch)
	}
}

func TestTabsStartAgentStartsATabAndRestartsWithResume(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	var tabs Tabs
	tabs.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	tabs.SetPorts(Ports{Starter: starter})
	tabs.SetEffects(Effects{
		ResumeAgents: true,
		Agents: []Agent{{
			Name: "claude", Argv: []string{"claude", "--session-id", "s1"},
			ResumeArgv: []string{"claude", "--resume", "s1"},
		}},
	})
	if _, ok := tabs.StartAgent("nope"); ok {
		t.Fatal("StartAgent accepted an unknown agent")
	}
	cmd, ok := tabs.StartAgent("claude")
	if !ok || cmd == nil {
		t.Fatalf("StartAgent = %v, %v", cmd != nil, ok)
	}
	msg := cmd() // tabStartedMsg
	if _, ok := tabs.Update(msg); !ok {
		t.Fatal("the start message was not taken")
	}
	if tabs.Count() != 1 || tabs.Name(0) != "claude" || !tabs.Active().Running() {
		t.Fatalf("tabs after StartAgent: count %d name %q running %v", tabs.Count(), tabs.Name(0), tabs.Active().Running())
	}
	if len(starter.launches) != 1 || !slices.Equal(starter.launches[0].Argv, []string{"claude", "--session-id", "s1"}) {
		t.Fatalf("launches = %+v", starter.launches)
	}
	if !tabs.Active().launch.Ephemeral || tabs.Active().launch.Agent != "claude" {
		t.Fatalf("record = %+v", tabs.Active().launch)
	}
	// A user restart resumes.
	if cmd := tabs.Restart(); cmd != nil {
		tabs.Update(cmd())
	}
	if len(starter.launches) != 2 || !slices.Equal(starter.launches[1].Argv, []string{"claude", "--resume", "s1"}) {
		t.Fatalf("restart launches = %+v", starter.launches)
	}
}

func TestTabsStartAgentChoice(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	var tabs Tabs
	tabs.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	tabs.SetPorts(Ports{Starter: starter})
	tabs.SetEffects(Effects{
		ResumeAgents: true,
		Agents: []Agent{{
			Name: "claude", Argv: []string{"claude", "--session-id", "s1"},
			ResumeArgv: []string{"claude", "--resume", "s1"},
		}, {Name: "codex", Argv: []string{"codex"}}},
	})
	// A resume choice starts the resume argv now; the record keeps both lists.
	cmd, ok := tabs.StartAgentChoice("claude", true)
	if !ok || cmd == nil {
		t.Fatalf("StartAgentChoice = %v, %v", cmd != nil, ok)
	}
	if _, ok := tabs.Update(cmd()); !ok {
		t.Fatal("the start message was not taken")
	}
	if len(starter.launches) != 1 || !slices.Equal(starter.launches[0].Argv, []string{"claude", "--resume", "s1"}) {
		t.Fatalf("launches = %+v, want the resume argv", starter.launches)
	}
	rec := tabs.Active()
	if rec == nil || rec.launch.Agent != "claude" || !rec.launch.Ephemeral || len(rec.launch.ResumeArgv) != 3 {
		t.Fatalf("launch record = %+v", rec)
	}
	if got := tabs.LaunchDir(); got != "/ws" {
		t.Fatalf("LaunchDir = %q", got)
	}
	// An agent without a resume argv starts its launch argv even when resume is asked.
	cmd, ok = tabs.StartAgentChoice("codex", true)
	if !ok || cmd == nil {
		t.Fatalf("codex choice = %v, %v", cmd != nil, ok)
	}
	tabs.Update(cmd())
	if len(starter.launches) != 2 || !slices.Equal(starter.launches[1].Argv, []string{"codex"}) {
		t.Fatalf("launches = %+v, want the codex launch argv", starter.launches)
	}
	if _, ok := tabs.StartAgentChoice("nope", false); ok {
		t.Fatal("StartAgentChoice accepted an unknown agent")
	}
}

// newAgentTabs is a dock with a fake starter and one configured agent.
func newAgentTabs(starter *fakeStarter) *Tabs {
	var tabs Tabs
	tabs.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	tabs.SetPorts(Ports{Starter: starter})
	tabs.SetEffects(Effects{
		ResumeAgents: true,
		Agents: []Agent{{
			Name: "claude", Argv: []string{"claude", "--session-id", "s1"},
			ResumeArgv: []string{"claude", "--resume", "s1"},
		}},
	})
	return &tabs
}

// TestAgentRestartUsesTheReportedDirectory is Fresh's respawn rule: the agent's
// last reported cwd (OSC 7) wins for a restart/resume, including across the
// saved-state round trip. The chosen launch_dir is only the fallback for an older
// record that never reported a cwd (see the legacy migration test).
func TestAgentRestartUsesTheReportedDirectory(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	tabs := newAgentTabs(starter)
	cmd, ok := tabs.StartAgentChoiceAt("claude", false, "/chosen")
	if !ok || cmd == nil {
		t.Fatalf("start = %v, %v", cmd != nil, ok)
	}
	tabs.Update(cmd())
	if got := starter.launchDirs; !slices.Equal(got, []string{"/chosen"}) {
		t.Fatalf("launch dirs = %v, want the chosen directory", got)
	}
	// The agent's program reports a different directory; Fresh's respawn uses that
	// reported cwd for the restart/resume.
	starter.term.cwd = "/reported"
	st := tabs.SavedState()
	if activeSaved(st).Cwd != "/reported" {
		t.Fatalf("saved cwd = %q, want the terminal's reported directory", activeSaved(st).Cwd)
	}
	if activeSaved(st).LaunchDir != "/chosen" {
		t.Fatalf("saved launch dir = %q, want the chosen directory", activeSaved(st).LaunchDir)
	}
	if len(st.Tabs) != 1 || st.Tabs[0].LaunchDir != "/chosen" {
		t.Fatalf("saved tabs = %+v, want the per-tab launch dir", st.Tabs)
	}
	if cmd := tabs.Restart(); cmd == nil {
		t.Fatal("restart returned no command")
	} else {
		tabs.Update(cmd())
	}
	if got := starter.launchDirs; !slices.Equal(got, []string{"/chosen", "/reported"}) {
		t.Fatalf("launch dirs = %v, want the reported cwd for the restart", got)
	}
	if got := len(starter.launches); got != 2 || !slices.Equal(starter.launches[1].Argv, []string{"claude", "--resume", "s1"}) {
		t.Fatalf("restart launches = %+v, want the resume argv", starter.launches)
	}
	// The round trip through JSON keeps both directories, and the restored tab
	// (dead until restarted) uses the launch directory.
	st = tabs.SavedState()
	// Cwd is the fake term's report again; make the saved terminal consistent.
	starter.term.cwd = "/reported"
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"launch_dir":"/chosen"`) {
		t.Fatalf("saved json = %s, want launch_dir", data)
	}
	var loaded terminal.SavedState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	restored := newAgentTabs(starter)
	restored.Restore(loaded)
	if got := restored.Restored(); !got {
		t.Fatal("the restored tab is not marked restored")
	}
	if cmd := restored.Restart(); cmd == nil {
		t.Fatal("restored restart returned no command")
	} else {
		restored.Update(cmd())
	}
	if got := starter.launchDirs; len(got) != 3 || got[2] != "/reported" {
		t.Fatalf("launch dirs = %v, want the restored restart in the saved cwd", got)
	}
}

// TestRestoreClipsTheSavedLaunchDir bounds a hand-edited launch_dir on load, so a
// corrupt record cannot pass an unbounded path to a start.
func TestRestoreClipsTheSavedLaunchDir(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	tabs := newAgentTabs(starter)
	tabs.Restore(terminal.SavedState{Version: terminal.SavedStateVersion, Tabs: []terminal.SavedTab{{Open: true, Cwd: "/reported", LaunchArgv: []string{"claude"}, LaunchDir: "/" + strings.Repeat("a", maxSavedDirBytes+10)}}})
	if got := len(activeSaved(tabs.SavedState()).LaunchDir); got != maxSavedDirBytes {
		t.Fatalf("restored launch dir length = %d, want the bound %d", got, maxSavedDirBytes)
	}
}

// TestTabsStartProgramKeepsTheWholeRecord checks the workspace Import start: full
// record (resume argv, agent, source, launch directory) and source lookup for
// de-duplicating a repeated import.
func TestTabsStartProgramKeepsTheWholeRecord(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	tabs := newAgentTabs(starter)
	attach := []string{"tmux", "-S", "/tmp/tmux-501/default", "attach", "-t", "work"}
	launch := terminal.Launch{
		Argv: attach, Agent: "claude", ResumeArgv: attach, Ephemeral: true,
		Source: "tmux:default/work",
	}
	cmd, ok := tabs.StartProgram(launch, "tmux:work", "/chosen")
	if !ok || cmd == nil {
		t.Fatalf("StartProgram = %v, %v", cmd != nil, ok)
	}
	tabs.Update(cmd())
	if len(starter.launches) != 1 {
		t.Fatalf("launches = %+v", starter.launches)
	}
	got := starter.launches[0]
	if !slices.Equal(got.Argv, attach) || !slices.Equal(got.ResumeArgv, attach) || got.Agent != "claude" || !got.Ephemeral || got.Source != "tmux:default/work" {
		t.Fatalf("record = %+v", got)
	}
	if len(starter.launchDirs) != 1 || starter.launchDirs[0] != "/chosen" {
		t.Fatalf("launch dirs = %v", starter.launchDirs)
	}
	if idx, ok := tabs.FindSource("tmux:default/work"); !ok || idx != 0 {
		t.Fatalf("FindSource = %d, %v, want the started tab", idx, ok)
	}
	if _, ok := tabs.FindSource("tmux:default/other"); ok {
		t.Fatal("FindSource found an unknown source")
	}
	if _, ok := tabs.FindSource(""); ok {
		t.Fatal("FindSource accepted the empty source")
	}
}

// TestSourceSurvivesTheSavedStateRoundTrip keeps the attach identity across an
// editor restart, so a repeated import after restore does not open a duplicate.
func TestSourceSurvivesTheSavedStateRoundTrip(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	tabs := newAgentTabs(starter)
	attach := []string{"tmux", "attach", "-t", "work"}
	cmd, _ := tabs.StartProgram(terminal.Launch{
		Argv: attach, ResumeArgv: attach, Ephemeral: true, Source: "tmux:default/work",
	}, "tmux:work", "/chosen")
	tabs.Update(cmd())
	st := tabs.SavedState()
	if activeSaved(st).Source != "tmux:default/work" || len(st.Tabs) != 1 || st.Tabs[0].Source != "tmux:default/work" {
		t.Fatalf("saved source = %q tabs %+v", activeSaved(st).Source, st.Tabs)
	}
	restored := newAgentTabs(starter)
	restored.Restore(st)
	if idx, ok := restored.FindSource("tmux:default/work"); !ok || idx != 0 {
		t.Fatalf("restored FindSource = %d, %v", idx, ok)
	}
	if got := restored.Active().launch.Source; got != "tmux:default/work" {
		t.Fatalf("restored launch source = %q", got)
	}
	if cmd := restored.Restart(); cmd == nil {
		t.Fatal("restart returned no command")
	} else {
		restored.Update(cmd())
	}
	if got := starter.launchDirs; len(got) != 2 || got[1] != "/chosen" {
		t.Fatalf("restart dirs = %v, want the chosen directory", got)
	}
}

// The agent starts below are exercised by tests only: the picker starts agents
// through StartAgentChoiceAt.

// StartAgent starts a configured agent preset in the dock. It reports whether the
// name is a configured agent; a miss is a status notice. The session terminal is
// ephemeral and records the launch and resume argv, so Restart can rejoin it.
func (p *Panel) StartAgent(name string) (tea.Cmd, bool) {
	for _, agent := range p.effects.Agents {
		if agent.Name != name {
			continue
		}
		dir := ""
		if p.host.RootDir != nil {
			dir = p.host.RootDir()
		}
		record := terminal.Launch{Argv: agent.Argv, Agent: agent.Name, ResumeArgv: agent.ResumeArgv, Ephemeral: true}
		p.open = true
		p.focus = false
		return p.launchProgram(dir, record.Argv, record, true), true
	}
	p.notice = dockText("gvim.dock.no_agent", "No terminal agent is configured under that name.")
	return nil, false
}

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
			t.host.Notice(dockTextVars("gvim.dock.tab_limit_max", "Terminal: the tab limit (%{max}) is reached", map[string]any{"max": MaxTerminalTabs}))
		}
		return nil, false
	}
	if prev := t.Active(); prev != nil {
		prev.SetFocus(false)
	}
	p := t.newPanel()
	t.tabs = append(t.tabs, p)
	t.names = append(t.names, name)
	t.ids = append(t.ids, t.newID())
	t.active = len(t.tabs) - 1
	cmd, ok := p.StartAgent(name)
	if cmd == nil {
		return nil, ok
	}
	tab := t.tabs[len(t.tabs)-1]
	return func() tea.Msg { return tabStartedMsg{tab: tab, inner: cmd()} }, ok
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
