package tui

import (
	"slices"
	"testing"

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
	// The choice starts the recorded agent argv in a new dock tab.
	_, choiceCmd := m.Update(agentPickMsg{name: "claude"})
	dockSettle(t, m, choiceCmd)
	if len(starter.launches) != 1 || !slices.Equal(starter.launches[0].Argv, []string{"claude", "--session-id", "s1"}) {
		t.Fatalf("launches = %+v", starter.launches)
	}
	if m.dock.Count() != 1 || m.dock.Name(0) != "claude" || !m.dock.Running() {
		t.Fatalf("dock tabs = %d, name %q, running %v", m.dock.Count(), m.dock.Name(0), m.dock.Running())
	}
}
