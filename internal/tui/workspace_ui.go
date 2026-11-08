package tui

// The daemon workspace UI (#19a): the palette actions that list/switch/create/close/rename
// workspaces, the switcher and picker overlays, and the workspace segment of the status
// row. The app supplies a WorkspacePort; without it (a direct launch) none of the actions
// exist and the status row says nothing about workspaces.

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/prompt"
)

// WorkspaceEntry is one workspace as the switcher shows it.
type WorkspaceEntry struct {
	ID           string
	Label        string
	Root         string
	Materialized bool
	Active       bool
}

// WorkspaceCmd is a command the workspace layer asks the program to run: the switch
// materializes a dormant workspace off the UI goroutine and reports the outcome through
// the model's notice. It is tea.Cmd so the app does not import Bubble Tea.
type WorkspaceCmd = tea.Cmd

// WorkspaceWaitCmd wraps a workspace layer's wait function into a program command.
// A nil wait means the switch (or close) finished synchronously.
func WorkspaceWaitCmd(wait func() error) WorkspaceCmd {
	if wait == nil {
		return nil
	}
	return func() tea.Msg {
		_ = wait()
		return nil
	}
}

// StartWorkspaceLaunch starts l in the model's dock as an ephemeral tab named l.Name. It
// reports nil when the dock cannot start a terminal. A launch with a Source that the
// dock already shows (a repeated Import, possibly after a restore) focuses that tab
// instead of opening a duplicate; otherwise the whole record (resume argv, launch
// directory, source) is kept so a user Restart reattaches in the chosen directory.
func (m *Model) StartWorkspaceLaunch(l WorkspaceLaunch) WorkspaceCmd {
	if len(l.Argv) == 0 {
		return nil
	}
	if l.Source != "" {
		if idx, ok := m.dock.FindSource(l.Source); ok {
			m.dock.SetActive(idx)
			m.dock.Focus()
			return nil
		}
	}
	resume := append([]string(nil), l.ResumeArgv...)
	if len(resume) == 0 {
		resume = append([]string(nil), l.Argv...)
	}
	cmd, ok := m.dock.StartProgram(terminal.Launch{
		Argv:       append([]string(nil), l.Argv...),
		Agent:      l.Agent,
		ResumeArgv: resume,
		Ephemeral:  l.Ephemeral,
		Source:     l.Source,
	}, l.Name, l.Dir)
	if !ok {
		return nil
	}
	return cmd
}

// DockTabNames reports the dock's tab labels. The workspace layer uses it to assert an
// imported launch opened its tab; the app may also use it for future status items.
func (m *Model) DockTabNames() []string { return m.dock.TabNames() }

// WorkspaceCmdOf wraps a function as a program command: the workspace layer uses it for
// the work that must not block the UI (creating, closing, materializing).
func WorkspaceCmdOf(fn func()) WorkspaceCmd {
	return func() tea.Msg {
		fn()
		return nil
	}
}

// WorkspacePort is the daemon's workspace layer as the TUI calls it. The methods run on
// the UI goroutine (the overlay callbacks) and return a command for the work that must
// not block the UI (materializing a dormant workspace builds a whole runtime); a
// user-visible failure is reported as an error or through the command's notice.
type WorkspacePort interface {
	Entries() []WorkspaceEntry
	Select(id string) (WorkspaceCmd, error)
	Create(root, label string) (WorkspaceCmd, error)
	Close(id string) (WorkspaceCmd, error)
	Rename(id, label string) error
	Next() (WorkspaceCmd, error)
}

// SetWorkspacePort gives the root the daemon's workspace layer. The app calls it after
// New for a daemon-hosted session only.
func (m *Model) SetWorkspacePort(p WorkspacePort) { m.workspace = p }

// SetWorkspaceLabel sets the status-row label of the active workspace.
func (m *Model) SetWorkspaceLabel(label string) { m.workspaceLabel = label }

// workspaceSegment is the status-row item of the active workspace, if any.
func (m *Model) workspaceSegment() (statusItem, bool) {
	if m.workspace == nil || m.workspaceLabel == "" {
		return statusItem{}, false
	}
	return statusItem{ID: "workspace", Text: "WS " + m.workspaceLabel, Prio: 1<<20 - 1}, true
}

// workspaceAction handles the daemon workspace palette actions. It reports false when the
// action is not a workspace action (or there is no workspace layer), so the other action
// tables get their turn.
func (m *Model) workspaceAction(action string) (tea.Cmd, bool) {
	if m.workspace == nil {
		return nil, false
	}
	switch action {
	case "workspace_switch":
		m.openWorkspaceSwitcher()
		return nil, true
	case "workspace_next":
		cmd, err := m.workspace.Next()
		if err != nil {
			m.quietNotice(err.Error())
		}
		return cmd, true
	case "workspace_new":
		m.openNewWorkspacePrompt()
		return nil, true
	case "workspace_close":
		m.openWorkspacePicker("Close Workspace", func(e WorkspaceEntry) tea.Cmd {
			cmd, err := m.workspace.Close(e.ID)
			if err != nil {
				m.quietNotice(err.Error())
			}
			return cmd
		})
		return nil, true
	case "workspace_rename":
		m.openWorkspacePicker("Rename Workspace", func(e WorkspaceEntry) tea.Cmd {
			m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
				Label: "Workspace label:",
				Input: e.Label,
				Tag:   workspaceRenameTag{id: e.ID},
			}))
			return nil
		})
		return nil, true
	}
	return nil, false
}

// workspaceRowText is the switcher text of one workspace: the label, the root and the
// state (active/materialized/dormant).
func workspaceRowText(e WorkspaceEntry) (left, right string) {
	left = e.Label
	state := "dormant"
	if e.Materialized {
		state = "open"
	}
	if e.Active {
		state = "active"
	}
	return left, state + "  " + e.Root
}

// openWorkspaceSwitcher lists every workspace and switches to the chosen one.
func (m *Model) openWorkspaceSwitcher() {
	entries := m.workspace.Entries()
	rows := make([]list.Row, 0, len(entries))
	for _, e := range entries {
		left, right := workspaceRowText(e)
		rows = append(rows, list.Row{Left: left, Right: right, Value: e.ID})
	}
	v := list.New("Workspaces", rows, m.listChrome(), func(_ *list.View, r list.Row) tea.Cmd {
		cmd, err := m.workspace.Select(r.Value)
		if err != nil {
			m.quietNotice(err.Error())
		}
		return cmd
	})
	v.Fit, v.MaxW = true, 80
	m.pushOverlay(v)
}

// openWorkspacePicker lists the workspaces and runs choose for the chosen one.
func (m *Model) openWorkspacePicker(title string, choose func(WorkspaceEntry) tea.Cmd) {
	entries := m.workspace.Entries()
	rows := make([]list.Row, 0, len(entries))
	byID := make(map[string]WorkspaceEntry, len(entries))
	for _, e := range entries {
		left, right := workspaceRowText(e)
		rows = append(rows, list.Row{Left: left, Right: right, Value: e.ID})
		byID[e.ID] = e
	}
	v := list.New(title, rows, m.listChrome(), func(_ *list.View, r list.Row) tea.Cmd {
		return choose(byID[r.Value])
	})
	v.Fit, v.MaxW = true, 80
	m.pushOverlay(v)
}

// openNewWorkspacePrompt asks for the root, then the label.
func (m *Model) openNewWorkspacePrompt() {
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Label:    "Workspace root:",
		Tag:      workspaceNewRootTag{},
		Validate: validateWorkspaceRoot,
	}))
}

func validateWorkspaceRoot(text string) error {
	if strings.TrimSpace(text) == "" {
		return errWorkspaceRootEmpty
	}
	return nil
}

// openNewWorkspaceLabelPrompt asks for the label of the workspace rooted at root.
func (m *Model) openNewWorkspaceLabelPrompt(root string) {
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Label: "Workspace label (empty: the folder name):",
		Tag:   workspaceNewLabelTag{root: root},
	}))
}

// workspaceActionAnswer applies the answers of the workspace prompts. It returns nil
// when the tag is not a workspace tag.
func (m *Model) workspaceActionAnswer(d prompt.Done) (tea.Cmd, bool) {
	switch tag := d.Tag.(type) {
	case workspaceNewRootTag:
		if d.Outcome == prompt.Submitted {
			m.openNewWorkspaceLabelPrompt(strings.TrimSpace(d.Text))
		}
		return nil, true
	case workspaceNewLabelTag:
		if d.Outcome != prompt.Submitted {
			return nil, true
		}
		if m.workspace == nil {
			return nil, true
		}
		cmd, err := m.workspace.Create(tag.root, strings.TrimSpace(d.Text))
		if err != nil {
			m.quietNotice(err.Error())
		}
		return cmd, true
	case workspaceRenameTag:
		if d.Outcome != prompt.Submitted || strings.TrimSpace(d.Text) == "" {
			return nil, true
		}
		if m.workspace != nil {
			if err := m.workspace.Rename(tag.id, strings.TrimSpace(d.Text)); err != nil {
				m.quietNotice(err.Error())
			}
		}
		return nil, true
	}
	return nil, false
}

// AddWorkspaceCommands appends the daemon workspace palette rows to the command table.
// The app calls it after SetWorkspacePort for a daemon-hosted session only, so a direct
// launch keeps its palette. The rows open the switcher/prompts of workspace_ui.go.
func (m *Model) AddWorkspaceCommands() {
	if m.workspace == nil {
		return
	}
	rows := []commands.Entry{
		{Name: "Workspace: Switch", Description: "Switch to another workspace", Action: "workspace_switch", Source: commands.SourceBuiltin},
		{Name: "Workspace: New", Description: "Create a workspace for a folder", Action: "workspace_new", Source: commands.SourceBuiltin},
		{Name: "Workspace: Close", Description: "Close a workspace and keep its unsaved text", Action: "workspace_close", Source: commands.SourceBuiltin},
		{Name: "Workspace: Rename", Description: "Rename a workspace", Action: "workspace_rename", Source: commands.SourceBuiltin},
		{Name: "Workspace: Next", Description: "Switch to the next workspace", Action: "workspace_next", Source: commands.SourceBuiltin},
	}
	for _, row := range rows {
		if slices.ContainsFunc(m.commands, func(e commands.Entry) bool { return e.Action == row.Action }) {
			continue
		}
		m.commands = append(m.commands, row)
	}
}

// Notify shows text on the status row. The app's workspace layer reports the outcome of
// asynchronous work (a failed switch, a slow materialization) through it.
func (m *Model) Notify(text string) { m.quietNotice(text) }
