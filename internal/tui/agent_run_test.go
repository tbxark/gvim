package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/dock"
)

// agentPorts are dock ports with the given configured agents.
func agentPorts(starter *fakeStarter, agents ...dock.Agent) DockPorts {
	return DockPorts{
		Starter: starter,
		Effects: DockEffects{ResumeAgents: true, Agents: agents},
	}
}

func TestRunAgentWithoutAgentsShowsANotice(t *testing.T) {
	m := newDockModel(t, newFakeEditor("hello"), agentPorts(newFakeStarter()))
	if cmd := rootPaletteRun(m, "run_agent"); cmd != nil {
		t.Fatalf("run_agent started a command without agents: %v", cmd != nil)
	}
	if len(m.overlays) != 0 {
		t.Fatalf("run_agent opened %d overlays without agents", len(m.overlays))
	}
	if m.notice != noAgentsNotice {
		t.Fatalf("notice = %q, want %q", m.notice, noAgentsNotice)
	}
}

func TestRunAgentPickerStartsTheChosenAgent(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), agentPorts(starter, dock.Agent{
		Name:       "claude",
		Argv:       []string{"claude", "--session-id", "s1"},
		ResumeArgv: []string{"claude", "--resume", "s1"},
	}))
	rootPaletteRun(m, "run_agent")
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the Run Agent picker", len(m.overlays))
	}
	if _, ok := m.overlays[0].(*agentPickerOverlay); !ok {
		t.Fatalf("top overlay = %T, want the Run Agent picker", m.overlays[0])
	}
	// Choosing the agent (Enter on the only row) asks the second question: new
	// session or resume.
	chooseFirstAgent(t, m)
	if len(starter.launches) != 0 {
		t.Fatalf("the first question started a session: %+v", starter.launches)
	}
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the Run Agent action picker", len(m.overlays))
	}
	action, ok := m.overlays[0].(*agentActionPickerOverlay)
	if !ok || actionRows(action) != 3 {
		t.Fatalf("action overlay = %T rows %d, want new + resume + change-directory rows", m.overlays[0], actionRows(action))
	}
	// "New session" (the first row) starts the launch argv in a new dock tab.
	chooseRunAgentAction(t, m, 0)
	if len(starter.launches) != 1 || !slices.Equal(starter.launches[0].Argv, []string{"claude", "--session-id", "s1"}) {
		t.Fatalf("launches = %+v", starter.launches)
	}
	if m.dock.Count() != 1 || m.dock.Name(0) != "claude" || !m.dock.Running() {
		t.Fatalf("dock tabs = %d, name %q, running %v", m.dock.Count(), m.dock.Name(0), m.dock.Running())
	}
}

// chooseFirstAgent presses Enter on the Run Agent picker (the first row is the
// only configured agent) and applies its choice message.
func chooseFirstAgent(t *testing.T, m *Model) {
	t.Helper()
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on the agent picker started no choice")
	}
	m.Update(cmd())
}

// chooseRunAgentAction moves down by rows and presses Enter on the action picker,
// then settles the dock start.
func chooseRunAgentAction(t *testing.T, m *Model, rows int) {
	t.Helper()
	for range rows {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	_, pick := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if pick == nil {
		t.Fatal("Enter on the action picker started no choice")
	}
	_, start := m.Update(pick())
	dockSettle(t, m, start)
}

// actionRows is the row count of an action overlay (0 for another overlay type).
func actionRows(o *agentActionPickerOverlay) int {
	if o == nil {
		return 0
	}
	return o.p.Rows()
}

func TestRunAgentResumeChoiceStartsTheResumeArgv(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), agentPorts(starter, dock.Agent{
		Name:       "claude",
		Argv:       []string{"claude", "--session-id", "s1"},
		ResumeArgv: []string{"claude", "--resume", "s1"},
	}))
	rootPaletteRun(m, "run_agent")
	chooseFirstAgent(t, m)
	chooseRunAgentAction(t, m, 1)
	if len(starter.launches) != 1 || !slices.Equal(starter.launches[0].Argv, []string{"claude", "--resume", "s1"}) {
		t.Fatalf("resume launches = %+v, want the resume argv", starter.launches)
	}
	if rec := m.dock.Name(0); rec != "claude" {
		t.Fatalf("tab name = %q", rec)
	}
}

func TestRunAgentActionPickerWithoutResumeArgv(t *testing.T) {
	m := newDockModel(t, newFakeEditor("hello"), agentPorts(newFakeStarter(), dock.Agent{
		Name: "codex", Argv: []string{"codex"},
	}))
	rootPaletteRun(m, "run_agent")
	chooseFirstAgent(t, m)
	o, ok := m.overlays[0].(*agentActionPickerOverlay)
	if !ok {
		t.Fatalf("overlay = %T, want the action picker", m.overlays[0])
	}
	if got := o.p.Rows(); got != 2 {
		t.Fatalf("action rows = %d, want New session and Change directory", got)
	}
}

// TestRunAgentChangeDirectory checks the dialog's directory question end to end:
// the answer is stat-checked in a command, re-opens the action picker, and the
// chosen new session starts in that directory.
func TestRunAgentChangeDirectory(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), agentPorts(starter, dock.Agent{
		Name: "codex", Argv: []string{"codex"},
	}))
	dir := t.TempDir()
	rootPaletteRun(m, "run_agent")
	chooseFirstAgent(t, m)
	if o, ok := m.overlays[0].(*agentActionPickerOverlay); !ok || actionRows(o) != 2 {
		t.Fatalf("action overlay = %T, want New session + Change directory", m.overlays[0])
	}
	// Move to "Change directory…" and open the directory prompt.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, changeCmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if changeCmd == nil {
		t.Fatal("Change directory produced no message")
	}
	m.Update(changeCmd())
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the directory prompt", len(m.overlays))
	}
	// Type the directory (the prefill is selected, so typing replaces it), submit,
	// and apply the stat answer.
	m.Update(typed(dir))
	_, doneCmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if doneCmd == nil {
		t.Fatal("the directory prompt produced no answer command")
	}
	_, statCmd := m.Update(doneCmd())
	if statCmd == nil {
		t.Fatal("the checked directory produced no re-open command")
	}
	m.Update(statCmd())
	if o, ok := m.overlays[0].(*agentActionPickerOverlay); !ok || o.dir != dir {
		t.Fatalf("action overlay = %T dir %q, want the checked directory %q", m.overlays[0], o.dir, dir)
	}
	// Choosing "New session" starts it in the chosen directory.
	chooseRunAgentAction(t, m, 0)
	if len(starter.launchDirs) != 1 || starter.launchDirs[0] != dir {
		t.Fatalf("launch dirs = %v, want %q", starter.launchDirs, dir)
	}
}

// TestRunAgentChangeDirectoryRejectsAMissingPath checks that a missing directory is
// stated instead of starting a terminal in it.
func TestRunAgentChangeDirectoryRejectsAMissingPath(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), agentPorts(starter, dock.Agent{
		Name: "codex", Argv: []string{"codex"},
	}))
	rootPaletteRun(m, "run_agent")
	chooseFirstAgent(t, m)
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, changeCmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(changeCmd())
	m.Update(typed(t.TempDir() + "/missing/sub"))
	_, doneCmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, statCmd := m.Update(doneCmd())
	if statCmd == nil {
		t.Fatal("the missing directory produced no answer")
	}
	m.Update(statCmd())
	if len(m.overlays) != 0 {
		t.Fatalf("overlays = %d, want none after the refusal", len(m.overlays))
	}
	if !strings.Contains(m.notice, "Run Agent:") {
		t.Fatalf("notice = %q, want the directory error", m.notice)
	}
	if len(starter.launchDirs) != 0 || len(starter.launches) != 0 {
		t.Fatalf("a terminal started despite the refusal: %v %v", starter.launchDirs, starter.launchDirs)
	}
}
