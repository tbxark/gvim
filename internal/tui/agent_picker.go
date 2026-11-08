package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/picker"
)

// Terminal: Run Agent... starts one of the configured terminal.agents presets in
// the dock. The action opens a picker of the agent names; choosing one starts the
// agent's argv and records it as an ephemeral session terminal, so Restart can
// rejoin it. This is the minimal #20a surface: no workspace/dock-row integration
// yet, and no agent discovery beyond the config list (Fresh's agent data comes
// from plugins; gvim's comes from terminal.agents).

// noAgentsNotice is the status text of Run Agent without a configured agent.
const noAgentsNotice = "No terminal agents are configured: add terminal.agents to your config."

// agentPickerOverlay is the picker of Run Agent. Like the other pickers it acts on
// nothing: its choice and its ranking come back to the root as messages.
type agentPickerOverlay struct {
	p     *picker.Picker
	names []string // the row texts; a picker item's ID indexes them
}

func (o *agentPickerOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	cmd := o.cmdFor(o.p.Update(msg))
	if !o.p.Active() {
		return nil, cmd
	}
	return o, cmd
}

func (o *agentPickerOverlay) Draw(g *grid.Grid) { o.p.Draw(g) }
func (o *agentPickerOverlay) Active() bool      { return o.p.Active() }

// agentPickMsg carries the chosen agent name, and agentRankMsg a ranking result
// back to the picker that asked for it.
type (
	agentPickMsg struct{ name string }
	agentRankMsg struct {
		ov  *agentPickerOverlay
		msg tea.Msg
	}
)

// cmdFor turns what the picker asks of its owner into commands.
func (o *agentPickerOverlay) cmdFor(r picker.Result) tea.Cmd {
	var cmds []tea.Cmd
	if r.Cmd != nil {
		cmds = append(cmds, r.Cmd)
	}
	if r.Rank != nil {
		rank := r.Rank
		cmds = append(cmds, func() tea.Msg { return agentRankMsg{ov: o, msg: rank()} })
	}
	for _, e := range r.Events {
		if c, ok := e.(picker.Chosen); ok && c.ID >= 0 && c.ID < len(o.names) {
			name := o.names[c.ID]
			cmds = append(cmds, func() tea.Msg { return agentPickMsg{name: name} })
		}
	}
	return tea.Batch(cmds...)
}

// openAgentPicker opens the Run Agent picker over the configured agents, or shows
// the "no agents" notice. It returns the picker's init command.
func (m *Model) openAgentPicker() tea.Cmd {
	agents := m.dock.Agents()
	if len(agents) == 0 {
		m.notice = noAgentsNotice
		return nil
	}
	names := make([]string, len(agents))
	items := make([]picker.Item, len(agents))
	for i, a := range agents {
		names[i] = a.Name
		detail := strings.Join(a.Argv, " ")
		if len(detail) > 120 {
			detail = detail[:120]
		}
		items[i] = picker.Item{ID: i, Text: a.Name, Detail: detail}
	}
	p := picker.New(m.styles, picker.Options{
		Title:       "Run Agent",
		Footer:      "Terminal: Run Agent — Enter starts the agent · Esc closes",
		Placeholder: "agent",
		Items:       items,
		Fuzzy:       true,
	})
	ov := &agentPickerOverlay{p: p, names: names}
	m.pushOverlay(ov)
	return ov.cmdFor(p.Init())
}

// startConfiguredAgent starts the named configured agent in the dock, taking the
// keyboard once it is up. It is the root's half of a picker choice.
func (m *Model) startConfiguredAgent(name string) tea.Cmd {
	cmd, ok := m.dock.StartAgent(name)
	if !ok {
		return nil
	}
	return tea.Batch(cmd, m.dockSync())
}
