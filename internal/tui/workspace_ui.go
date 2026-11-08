package tui

// The daemon workspace UI (#19a): the palette actions that list/switch/create/close/rename
// workspaces, the switcher and picker overlays, and the workspace segment of the status
// row. The app supplies a WorkspacePort; without it (a direct launch) none of the actions
// exist and the status row says nothing about workspaces.

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

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

// WorkspaceRunningTerminal is one running terminal tab of a workspace: the tab's name and,
// for an agent tab, the configured agent it runs (empty for a plain shell). The close
// workspace flow lists them in its confirmation and in the close report, so killing a
// workspace's processes is never a surprise.
type WorkspaceRunningTerminal struct {
	Name  string
	Agent string
}

// workspaceExtractPort is the optional extract action of the workspace layer: move the
// active tab into a new workspace (its own file's folder). A port without it (an older
// app, a test fake) simply has no extract row.
type workspaceExtractPort interface {
	ExtractTab() (WorkspaceCmd, error)
}

// workspaceRunningPort is the optional running-terminal surface of the workspace layer:
// which terminals closing id would stop. A port without it closes without asking, as
// before (a direct launch has no workspace layer at all).
type workspaceRunningPort interface {
	RunningTerminals(id string) []WorkspaceRunningTerminal
}

// RunningTerminals lists this model's running dock terminals, in tab order, for the
// workspace close confirmation and report. The app calls it on the UI goroutine.
func (m *Model) RunningTerminals() []WorkspaceRunningTerminal {
	running := m.dock.RunningTabs()
	out := make([]WorkspaceRunningTerminal, 0, len(running))
	for _, r := range running {
		out = append(out, WorkspaceRunningTerminal{Name: r.Name, Agent: r.Agent})
	}
	return out
}

// CloseTabForWorkspaceExtract force-closes the active tab when its path is path, without
// asking and without writing: the extract action has already carried the tab's unsaved
// text into the target workspace's session, so closing the source tab saves nothing and
// drops nothing. It reports false when the active tab is not that path any more (the UI
// moved during the extraction), so the caller keeps the source state untouched.
func (m *Model) CloseTabForWorkspaceExtract(path string) bool {
	t := m.tabPort()
	if t == nil {
		return false
	}
	i := m.activeTab()
	tabs := m.ed.Tabs()
	if i < 0 || i >= len(tabs) || tabs[i].Path != path {
		return false
	}
	if err := t.CloseTab(i, true); err != nil {
		m.quietNotice(err.Error())
		return false
	}
	m.syncViewport()
	return true
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
		m.openWorkspacePicker("Close Workspace", m.workspaceCloseEntry)
		return nil, true
	case "workspace_extract_tab":
		return m.extractWorkspaceTab(), true
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

// workspaceCloseEntry runs the close flow of one workspace: when the workspace still has
// running terminals, it asks first and names exactly what the close will stop; otherwise
// it closes at once. The close report itself comes from the workspace layer.
func (m *Model) workspaceCloseEntry(e WorkspaceEntry) tea.Cmd {
	if running := workspaceRunningTerminals(m.workspace, e.ID); len(running) > 0 {
		// Fresh's session close flushes recovery and drops the window; gvim's workspace
		// close also stops the dock shells, so it asks first and names exactly what it
		// will kill.
		m.pushOverlay(prompt.NewConfirmWith(m.styles,
			"Close workspace \""+e.Label+"\"? This stops "+workspaceRunningSummary(running),
			workspaceCloseRunningTag{id: e.ID},
			prompt.ConfirmOptions{Yes: "Close", No: "Cancel", DefaultYes: true}))
		return nil
	}
	cmd, err := m.workspace.Close(e.ID)
	if err != nil {
		m.quietNotice(err.Error())
	}
	return cmd
}

// workspaceCloseRunningTag is the tag of the close confirmation that lists the running
// terminals the close will stop. Yes closes the workspace; anything else leaves it open.
type workspaceCloseRunningTag struct{ id string }

// workspaceRunningTerminals asks the workspace layer which terminals closing id stops. A
// layer without the optional running surface reports none: the close keeps its old
// behavior for such a port.
func workspaceRunningTerminals(p WorkspacePort, id string) []WorkspaceRunningTerminal {
	if rp, ok := p.(workspaceRunningPort); ok {
		return rp.RunningTerminals(id)
	}
	return nil
}

// workspaceRunningSummary is the "N running terminals: ..." clause of the close question
// and the close report. It names every one and its agent, so the user knows exactly what
// the close kills.
func workspaceRunningSummary(running []WorkspaceRunningTerminal) string {
	parts := make([]string, len(running))
	for i, r := range running {
		name := r.Name
		if name == "" {
			name = "terminal"
		}
		if r.Agent != "" {
			parts[i] = name + " (agent " + r.Agent + ")"
		} else {
			parts[i] = name
		}
	}
	noun := "terminal"
	if len(running) != 1 {
		noun = "terminals"
	}
	return fmt.Sprintf("%d running %s: %s", len(running), noun, strings.Join(parts, ", "))
}

// extractWorkspaceTab asks the workspace layer to move the active tab into its own
// workspace. The layer captures the tab on this goroutine and returns a command for the
// persistence and materialization; a tab it cannot move (no file path, a large-file
// overlay, a folder that is already the workspace root) comes back as an error with the
// reason and the tab stays open.
func (m *Model) extractWorkspaceTab() tea.Cmd {
	if m.workspace == nil {
		m.quietNotice("Extracting a tab needs the daemon workspace layer")
		return nil
	}
	p, ok := m.workspace.(workspaceExtractPort)
	if !ok {
		m.quietNotice("Extracting a tab is not supported by this workspace layer")
		return nil
	}
	cmd, err := p.ExtractTab()
	if err != nil {
		// The layer's errors are lower-case (Go error style); the status row is a
		// sentence, so it capitalizes the first letter for display.
		msg := err.Error()
		if r, size := utf8.DecodeRuneInString(msg); size > 0 {
			msg = string(unicode.ToUpper(r)) + msg[size:]
		}
		m.quietNotice(msg)
		return nil
	}
	return cmd
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
	case workspaceCloseRunningTag:
		if d.Outcome == prompt.Yes && m.workspace != nil {
			cmd, err := m.workspace.Close(tag.id)
			if err != nil {
				m.quietNotice(err.Error())
			}
			return cmd, true
		}
		return nil, true
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
	if _, ok := m.workspace.(workspaceExtractPort); ok {
		// Fresh's action label for the same command; the tab menu item is the other way in.
		rows = append(rows, commands.Entry{Name: "Extract tab to new workspace", Description: "Move the active tab into a workspace rooted at its file's folder", Action: "workspace_extract_tab", Source: commands.SourceBuiltin})
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
