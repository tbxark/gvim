package tui

import (
	"errors"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/dock"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/picker"
	"github.com/tbxark/gvim/internal/tui/prompt"
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

// agentActionPickerOverlay is the second Run Agent picker: the chosen agent's
// launch choices (a new session, a resume when the entry has one, and a directory
// change), with the directory and the terminal.resume_agents hint. It acts on
// nothing: the choice comes back to the root as a message.
type agentActionPickerOverlay struct {
	m       *Model
	p       *picker.Picker
	name    string
	dir     string
	choices []agentActionChoice
}

// agentActionChoice is one row of the action picker. changeDir opens the
// directory prompt instead of starting.
type agentActionChoice struct {
	resume    bool
	changeDir bool
}

// agentActionMsg carries the Run Agent dialog's final choice; agentChangeDirMsg
// asks for the directory prompt; agentRunDirMsg is that prompt's checked answer.
type (
	agentActionMsg struct {
		name   string
		resume bool
		dir    string
	}
	agentChangeDirMsg struct {
		name string
		dir  string
	}
	agentRunDirMsg struct {
		name string
		dir  string
		err  error
	}
	agentRunDirTag struct{ name string }
)

func (o *agentActionPickerOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	var cmds []tea.Cmd
	res := o.p.Update(msg)
	if res.Cmd != nil {
		cmds = append(cmds, res.Cmd)
	}
	for _, event := range res.Events {
		chosen, ok := event.(picker.Chosen)
		if !ok || chosen.ID < 0 || chosen.ID >= len(o.choices) {
			continue
		}
		choice := o.choices[chosen.ID]
		name, dir := o.name, o.dir
		if choice.changeDir {
			cmds = append(cmds, func() tea.Msg { return agentChangeDirMsg{name: name, dir: dir} })
			continue
		}
		resume := choice.resume
		cmds = append(cmds, func() tea.Msg {
			return agentActionMsg{name: name, resume: resume, dir: dir}
		})
	}
	if !o.p.Active() {
		return nil, tea.Batch(cmds...)
	}
	return o, tea.Batch(cmds...)
}

func (o *agentActionPickerOverlay) Draw(g *grid.Grid) { o.p.Draw(g) }
func (o *agentActionPickerOverlay) Active() bool      { return o.p.Active() }

// openAgentActionPicker asks the Run Agent dialog's second question for the
// configured agent name, starting from the workspace root.
func (m *Model) openAgentActionPicker(name string) tea.Cmd {
	return m.openAgentActionPickerAt(name, m.dock.LaunchDir())
}

// openAgentActionPickerAt is openAgentActionPicker for an explicit start
// directory, so a directory change re-opens the dialog on the checked answer.
func (m *Model) openAgentActionPickerAt(name, dir string) tea.Cmd {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = m.dock.LaunchDir()
	}
	var agent dock.Agent
	found := false
	for _, candidate := range m.dock.Agents() {
		if candidate.Name == name {
			agent, found = candidate, true
			break
		}
	}
	if !found {
		m.notice = noAgentsNotice
		return nil
	}
	resumeHint := "Restart will replay the launch command (terminal.resume_agents: off)"
	if m.dock.ResumeAgents() {
		resumeHint = "Restart will resume too (terminal.resume_agents: on)"
	}
	items := []picker.Item{{
		ID: 0, Text: "New session",
		Detail: strings.Join(agent.Argv, " ") + " · in " + dir,
	}}
	choices := []agentActionChoice{{}}
	if len(agent.ResumeArgv) > 0 {
		items = append(items, picker.Item{
			ID: 1, Text: "Resume last session",
			Detail: strings.Join(agent.ResumeArgv, " ") + " · " + resumeHint + " · in " + dir,
		})
		choices = append(choices, agentActionChoice{resume: true})
	}
	items = append(items, picker.Item{
		ID: len(choices), Text: "Change directory…",
		Detail: dir,
	})
	choices = append(choices, agentActionChoice{changeDir: true})
	p := picker.New(m.styles, picker.Options{
		Title:  "Run Agent · " + name,
		Footer: "Enter chooses · Esc closes",
		Items:  items,
	})
	p.SetStatus("Starts in " + dir)
	ov := &agentActionPickerOverlay{m: m, p: p, name: name, dir: dir, choices: choices}
	m.pushOverlay(ov)
	return nil
}

// openAgentDirPrompt asks for the Run Agent start directory, prefilled with the
// current one. The answer is checked off the update loop and comes back as
// agentRunDirMsg.
func (m *Model) openAgentDirPrompt(name, dir string) tea.Cmd {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = m.dock.LaunchDir()
	}
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Label:     uiText("gvim.prompt.run_agent_directory", "Run Agent directory:"),
		Input:     dir,
		SelectAll: true,
		Tag:       agentRunDirTag{name: name},
	}))
	return nil
}

// agentDirAnswer checks the directory prompt's answer as a command (an os.Stat
// is IO, so it must not run on the update loop) and returns its report.
func (m *Model) agentDirAnswer(d prompt.Done) tea.Cmd {
	tag, ok := d.Tag.(agentRunDirTag)
	if !ok || d.Outcome != prompt.Submitted {
		return nil
	}
	dir := strings.TrimSpace(d.Text)
	if dir == "" {
		dir = m.dock.LaunchDir()
	}
	return func() tea.Msg {
		info, err := os.Stat(dir)
		if err != nil {
			return agentRunDirMsg{name: tag.name, dir: dir, err: err}
		}
		if !info.IsDir() {
			return agentRunDirMsg{name: tag.name, dir: dir, err: errors.New("not a directory")}
		}
		return agentRunDirMsg{name: tag.name, dir: dir}
	}
}

// agentDirChosen re-opens the action picker on a checked directory, or states why
// the directory cannot be used.
func (m *Model) agentDirChosen(msg agentRunDirMsg) tea.Cmd {
	if msg.err != nil {
		m.notice = uiTextVars("gvim.notice.agent.run_error", "Run Agent: %{error}", map[string]any{"error": msg.err.Error()})
		return nil
	}
	return m.openAgentActionPickerAt(msg.name, msg.dir)
}

// startAgentChoice starts the Run Agent dialog's final choice in a new dock tab:
// a new session or a user-initiated resume, in the chosen directory, recorded as
// an ephemeral agent tab.
func (m *Model) startAgentChoice(name string, resume bool, dir string) tea.Cmd {
	cmd, ok := m.dock.StartAgentChoiceAt(name, resume, dir)
	if !ok {
		return nil
	}
	return tea.Batch(cmd, m.dockSync())
}
