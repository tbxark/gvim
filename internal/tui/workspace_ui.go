package tui

// The daemon workspace UI (#19a): the palette actions that list/switch/create/close/rename
// workspaces, the switcher and picker overlays, and the workspace segment of the status
// row. The app supplies a WorkspacePort; without it (a direct launch) none of the actions
// exist and the status row says nothing about workspaces.

import (
	"fmt"
	"slices"
	"strconv"
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

// workspaceExtractTargetPort is the optional "extract into an existing workspace" surface:
// the co-tenant workspaces over the same root that can receive the active file tab or the
// active terminal tab, and the two commands. A port without it has no target picker (the
// new-workspace extract above stays available).
type workspaceExtractTargetPort interface {
	ExtractTargets() []WorkspaceEntry
	ExtractTabTo(id string) (WorkspaceCmd, error)
	ExtractTerminalTabTo(id string) (WorkspaceCmd, error)
}

// workspaceExtractManyPort is the optional multi-tab surface: open the selected FILE tabs
// of the active workspace in one new co-tenant workspace (Fresh has no multi-tab extract;
// this is gvim's bounded extension, documented in docs/ui.md). A port without it has no
// multi-select row.
type workspaceExtractManyPort interface {
	ExtractTabs(paths []string) (WorkspaceCmd, error)
}

// ExtractTerminalRef names one dock terminal tab in a mixed extract selection. Index
// is the tab's slot at capture time and Name its saved name ("" for a default-named
// tab), so the command can verify the tab still exists before closing it.
type ExtractTerminalRef struct {
	Index int
	Name  string
}

// ExtractSelection is the tabs the mixed extract picker selected: file tabs by path
// and dock terminal tabs by reference. It is a gvim extension: Fresh's only extract
// action moves a single tab into a new co-tenant window.
type ExtractSelection struct {
	Files     []string
	Terminals []ExtractTerminalRef
}

// Len is the number of selected tabs.
func (s ExtractSelection) Len() int { return len(s.Files) + len(s.Terminals) }

// workspaceExtractMixedPort is the optional mixed-selection surface: move the selected
// file AND terminal tabs into a new co-tenant workspace (targetID "") or an existing
// one over the same root. replace allows replacing a target file's existing state; a
// same-name terminal tab is added alongside (never replaced), which the conflict
// prompt states. A conflict is reported through ExtractConflictMsg before any write.
type workspaceExtractMixedPort interface {
	ExtractMixed(selection ExtractSelection, targetID string, replace bool) (WorkspaceCmd, error)
}

// ExtractConflictMsg reports that an existing target workspace already holds some of
// the selected tabs. It is sent by the extraction command BEFORE any write; the root
// asks the confirmation and only then retries with replace=true. Conflicts names the
// colliding paths/tab names.
type ExtractConflictMsg struct {
	Selection   ExtractSelection
	TargetID    string
	TargetLabel string
	Conflicts   []string
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

// CanCloseTabForWorkspaceExtract reports whether the extract action could close the active
// tab right now: the tab surface exists and the active tab is path. The workspace layer
// checks this BEFORE it queues the switch, so a tab that changed during the extraction is
// refused while it is still open (nothing is closed, nothing moves).
func (m *Model) CanCloseTabForWorkspaceExtract(path string) bool {
	if m.tabPort() == nil {
		return false
	}
	i := m.activeTab()
	tabs := m.ed.Tabs()
	return i >= 0 && i < len(tabs) && tabs[i].Path == path
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

// CloseTabForWorkspaceExtractAt force-closes the tab whose path is path, without
// asking and without writing. It is the multi-tab extract's close: the tab need not
// be the active one, and a tab that changed/closed since the capture is skipped
// (false) while the others still close. The extraction already wrote the tab's text
// into the target workspace, so closing the source saves nothing.
func (m *Model) CloseTabForWorkspaceExtractAt(path string) bool {
	t := m.tabPort()
	if t == nil {
		return false
	}
	for i, tab := range m.ed.Tabs() {
		if tab.Path != path {
			continue
		}
		if err := t.CloseTab(i, true); err != nil {
			m.quietNotice(err.Error())
			return false
		}
		m.syncViewport()
		return true
	}
	return false
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

// WorkspaceCmdWhen wraps work that may report a message: a non-nil result is
// delivered to Update (the mixed extract's pre-write conflict report), a nil one is
// a silent side effect. The workspace layer uses it so it never imports Bubble Tea.
func WorkspaceCmdWhen(work func() any) WorkspaceCmd {
	return func() tea.Msg { return work() }
}

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
		m.openWorkspacePicker(uiText("gvim.workspace.picker_close", "Close Workspace"), m.workspaceCloseEntry)
		return nil, true
	case "workspace_extract_tab":
		return m.extractWorkspaceTab(), true
	case "workspace_extract_tab_to":
		return m.extractWorkspaceTabTo(), true
	case "workspace_extract_terminal_to":
		return m.extractWorkspaceTerminalTo(), true
	case "workspace_extract_tabs":
		if _, ok := m.workspace.(workspaceExtractMixedPort); ok {
			m.openMixedExtractPicker()
		} else {
			m.openExtractTabsPicker()
		}
		return nil, true
	case "workspace_rename":
		m.openWorkspacePicker(uiText("gvim.workspace.picker_rename", "Rename Workspace"), func(e WorkspaceEntry) tea.Cmd {
			m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
				Label: uiText("gvim.workspace.label_prompt", "Workspace label:"),
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
			uiTextVars("gvim.workspace.close_question", "Close workspace %{label}? This stops %{summary}", map[string]any{"label": fmt.Sprintf("%q", e.Label), "summary": workspaceRunningSummary(running)}),
			workspaceCloseRunningTag{id: e.ID},
			prompt.ConfirmOptions{Yes: uiText("gvim.workspace.close_button", "Close"), No: uiText("gvim.dialog.button.cancel", "Cancel"), DefaultYes: true}))
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
			name = uiText("gvim.workspace.noun_terminal", "terminal")
		}
		if r.Agent != "" {
			parts[i] = name + " (agent " + r.Agent + ")"
		} else {
			parts[i] = name
		}
	}
	noun := uiText("gvim.workspace.noun_terminal", "terminal")
	if len(running) != 1 {
		noun = uiText("gvim.workspace.noun_terminals", "terminals")
	}
	return uiTextVars("gvim.workspace.running_summary", "%{count} running %{noun}: %{parts}", map[string]any{"count": len(running), "noun": noun, "parts": strings.Join(parts, ", ")})
}

// extractWorkspaceTab asks the workspace layer to move the active tab into its own
// workspace. The layer captures the tab on this goroutine and returns a command for the
// persistence and materialization; a tab it cannot move (no file path, a large-file
// overlay, a folder that is already the workspace root) comes back as an error with the
// reason and the tab stays open.
func (m *Model) extractWorkspaceTab() tea.Cmd {
	if m.workspace == nil {
		m.quietNotice(uiText("gvim.workspace.extract_needs_daemon", "Extracting a tab needs the daemon workspace layer"))
		return nil
	}
	p, ok := m.workspace.(workspaceExtractPort)
	if !ok {
		m.quietNotice(uiText("gvim.workspace.extract_unsupported", "Extracting a tab is not supported by this workspace layer"))
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

// workspaceActionError shows a workspace action's error on the status row: the layer's
// errors are lower-case Go style, the status row is a sentence, so the first letter is
// capitalized for display.
func (m *Model) workspaceActionError(err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if r, size := utf8.DecodeRuneInString(msg); size > 0 {
		msg = string(unicode.ToUpper(r)) + msg[size:]
	}
	m.quietNotice(msg)
}

// extractTargetEntries returns the workspaces an extract-into-existing action can target,
// or nil when there are none (the picker then explains instead of opening empty).
func (m *Model) extractTargetEntries() []WorkspaceEntry {
	p, ok := m.workspace.(workspaceExtractTargetPort)
	if !ok {
		return nil
	}
	return p.ExtractTargets()
}

// openExtractTargetPicker lists the workspaces over this root and runs run for the chosen
// one. It reports false when there is no target (the caller shows the reason).
func (m *Model) openExtractTargetPicker(title string, run func(id string) (WorkspaceCmd, error)) bool {
	entries := m.extractTargetEntries()
	if len(entries) == 0 {
		m.quietNotice(uiText("gvim.workspace.extract_no_other", "No other workspace over this root to extract into"))
		return false
	}
	rows := make([]list.Row, 0, len(entries))
	byID := make(map[string]WorkspaceEntry, len(entries))
	for _, e := range entries {
		left, right := workspaceRowText(e)
		rows = append(rows, list.Row{Left: left, Right: right, Value: e.ID})
		byID[e.ID] = e
	}
	v := list.New(title, rows, m.listChrome(), func(_ *list.View, r list.Row) tea.Cmd {
		cmd, err := run(byID[r.Value].ID)
		if err != nil {
			m.workspaceActionError(err)
		}
		return cmd
	})
	v.Fit, v.MaxW = true, 80
	m.pushOverlay(v)
	return true
}

// extractWorkspaceTabTo moves the active file tab into an existing co-tenant workspace.
func (m *Model) extractWorkspaceTabTo() tea.Cmd {
	p, ok := m.workspace.(workspaceExtractTargetPort)
	if !ok {
		m.quietNotice(uiText("gvim.workspace.extract_into_existing_unsupported", "Extracting a tab into an existing workspace is not supported by this workspace layer"))
		return nil
	}
	m.openExtractTargetPicker(uiText("gvim.workspace.extract_title_single", "Extract Tab to Workspace"), p.ExtractTabTo)
	return nil
}

// extractWorkspaceTerminalTo moves the active dock terminal tab into an existing co-tenant
// workspace. The running process is not handed over: the target restores the saved terminal
// and revives it on the first dive.
func (m *Model) extractWorkspaceTerminalTo() tea.Cmd {
	p, ok := m.workspace.(workspaceExtractTargetPort)
	if !ok {
		m.quietNotice(uiText("gvim.workspace.extract_terminal_unsupported", "Extracting a terminal tab is not supported by this workspace layer"))
		return nil
	}
	if m.dock.Count() == 0 {
		m.quietNotice(noTerminalNotice())
		return nil
	}
	m.openExtractTargetPicker(uiText("gvim.workspace.extract_title_terminal", "Extract Terminal Tab to Workspace"), p.ExtractTerminalTabTo)
	return nil
}

// openExtractTabsPicker offers the open file tabs in a multi-select list. Space marks a tab,
// Enter extracts the marked ones (or the highlighted one) into a new co-tenant workspace.
// A terminal tab, a folder and a large-file overlay are not offered: the first has its own
// extract action, the others have no file state to carry.
func (m *Model) openExtractTabsPicker() {
	if _, ok := m.workspace.(workspaceExtractManyPort); !ok {
		m.quietNotice(uiText("gvim.workspace.extract_many_unsupported", "Extracting several tabs is not supported by this workspace layer"))
		return
	}
	tabs := m.ed.Tabs()
	rows := make([]list.Row, 0, len(tabs))
	for _, t := range tabs {
		if t.Path == "" {
			continue
		}
		left := t.Name
		if left == "" {
			left = t.Path
		}
		right := "modified"
		if !t.Modified {
			right = ""
		}
		rows = append(rows, list.Row{Left: left, Right: right, Value: t.Path})
	}
	if len(rows) == 0 {
		m.quietNotice(uiText("gvim.workspace.extract_no_file_tabs", "No file tabs to extract"))
		return
	}
	extract := func(view *list.View, paths []string) tea.Cmd {
		view.Close()
		p, ok := m.workspace.(workspaceExtractManyPort)
		if !ok {
			return nil
		}
		cmd, err := p.ExtractTabs(paths)
		if err != nil {
			m.workspaceActionError(err)
			return nil
		}
		return cmd
	}
	// Enter with no marks runs the highlighted row through this single-tab callback;
	// a marked set goes through ChooseMany. Both keep the multi-select rule "the
	// active tab alone or the marked tabs".
	v := list.New(uiText("gvim.workspace.extract_title_tabs_new", "Extract Tabs to New Workspace"), rows, m.listChrome(), func(view *list.View, r list.Row) tea.Cmd {
		return extract(view, []string{r.Value})
	})
	v.Multi = true
	v.Fit, v.MaxW = true, 80
	v.Hint = uiText("gvim.workspace.extract_hint_select", "Space mark, Enter extract, type to filter, Esc close")
	v.ChooseMany = func(view *list.View, marked []list.Row) tea.Cmd {
		paths := make([]string, 0, len(marked))
		for _, r := range marked {
			paths = append(paths, r.Value)
		}
		return extract(view, paths)
	}
	m.pushOverlay(v)
}

// The extract picker values: a file tab's path or a dock tab's index, so one
// multi-select list can carry both kinds.
const (
	extractFileValue     = "file:"
	extractTerminalValue = "term:"
)

// openMixedExtractPicker offers the open file tabs and the dock's terminal tabs in
// one multi-select list (gvim extension: Fresh extracts a single tab only). Space
// marks rows, Enter asks for the target (a new co-tenant always, plus every existing
// same-root workspace), and a file tab without a path (the welcome screen) or a
// large-file overlay refuses the whole action in the command.
func (m *Model) openMixedExtractPicker() {
	if _, ok := m.workspace.(workspaceExtractMixedPort); !ok {
		m.quietNotice(uiText("gvim.workspace.extract_mixed_unsupported", "Extracting mixed file and terminal tabs is not supported by this workspace layer"))
		return
	}
	termRefs := m.TerminalTabRefs()
	termNames := make(map[int]string, len(termRefs))
	rows := make([]list.Row, 0, len(m.ed.Tabs())+len(termRefs))
	for _, t := range m.ed.Tabs() {
		if t.Path == "" {
			continue
		}
		left := t.Name
		if left == "" {
			left = t.Path
		}
		right := "file"
		if t.Modified {
			right = "file · modified"
		}
		rows = append(rows, list.Row{Left: left, Right: right, Value: extractFileValue + t.Path})
	}
	for _, ref := range termRefs {
		termNames[ref.Index] = ref.Name
		rows = append(rows, list.Row{Left: ref.Label, Right: "terminal", Value: extractTerminalValue + strconv.Itoa(ref.Index)})
	}
	if len(rows) == 0 {
		m.quietNotice(uiText("gvim.workspace.no_tabs_to_extract", "No tabs to extract"))
		return
	}
	selectionOf := func(selected []list.Row) ExtractSelection {
		var sel ExtractSelection
		for _, r := range selected {
			switch {
			case strings.HasPrefix(r.Value, extractFileValue):
				sel.Files = append(sel.Files, strings.TrimPrefix(r.Value, extractFileValue))
			case strings.HasPrefix(r.Value, extractTerminalValue):
				idx, err := strconv.Atoi(strings.TrimPrefix(r.Value, extractTerminalValue))
				if err != nil {
					continue
				}
				sel.Terminals = append(sel.Terminals, ExtractTerminalRef{Index: idx, Name: termNames[idx]})
			}
		}
		return sel
	}
	extract := func(view *list.View, selected []list.Row) tea.Cmd {
		view.Close()
		sel := selectionOf(selected)
		return tea.Cmd(func() tea.Msg { return mixedExtractSelectionMsg{selection: sel} })
	}
	v := list.New(uiText("gvim.workspace.extract_title_tabs_existing", "Extract Tabs to Workspace"), rows, m.listChrome(), func(view *list.View, r list.Row) tea.Cmd {
		return extract(view, []list.Row{r})
	})
	v.Multi = true
	v.Fit, v.MaxW = true, 80
	v.Hint = uiText("gvim.workspace.extract_hint_target", "Space mark, Enter choose target, type to filter, Esc close")
	v.ChooseMany = func(view *list.View, marked []list.Row) tea.Cmd {
		return extract(view, marked)
	}
	m.pushOverlay(v)
}

// afterMixedExtractSelection asks for the target: "New workspace" is always offered;
// the existing same-root workspaces follow when there are any. With no existing target
// the new co-tenant is created directly.
func (m *Model) afterMixedExtractSelection(sel ExtractSelection) tea.Cmd {
	if sel.Len() == 0 {
		m.quietNotice(uiText("gvim.workspace.extract_none_selected", "No tabs selected"))
		return nil
	}
	if _, ok := m.workspace.(workspaceExtractMixedPort); !ok {
		m.quietNotice(uiText("gvim.workspace.extract_mixed_unsupported", "Extracting mixed file and terminal tabs is not supported by this workspace layer"))
		return nil
	}
	targets := m.extractTargetEntries()
	if len(targets) == 0 {
		return m.runMixedExtract(sel, "", false)
	}
	rows := []list.Row{{Left: uiText("gvim.workspace.extract_new_workspace_row", "New workspace"), Right: uiText("gvim.workspace.extract_new_workspace_detail", "new co-tenant over this root"), Value: ""}}
	for _, e := range targets {
		left, right := workspaceRowText(e)
		rows = append(rows, list.Row{Left: left, Right: right, Value: e.ID})
	}
	v := list.New(uiText("gvim.workspace.extract_title_tabs_choice", "Extract Tabs To"), rows, m.listChrome(), func(view *list.View, r list.Row) tea.Cmd {
		view.Close()
		return m.runMixedExtract(sel, r.Value, false)
	})
	v.Fit, v.MaxW = true, 80
	m.pushOverlay(v)
	return nil
}

// runMixedExtract calls the mixed extract surface. targetID "" means a new co-tenant;
// replace is set only after the conflict confirmation, which the command sends BEFORE
// any write. An immediate error is a refusal (nothing moved) and is shown as a notice.
func (m *Model) runMixedExtract(sel ExtractSelection, targetID string, replace bool) tea.Cmd {
	p, ok := m.workspace.(workspaceExtractMixedPort)
	if !ok {
		m.quietNotice(uiText("gvim.workspace.extract_mixed_unsupported", "Extracting mixed file and terminal tabs is not supported by this workspace layer"))
		return nil
	}
	cmd, err := p.ExtractMixed(sel, targetID, replace)
	if err != nil {
		m.workspaceActionError(err)
		return nil
	}
	return cmd
}

// mixedExtractSelectionMsg carries a completed multi-select picker selection to
// Update. The target chooser is opened from Update rather than from inside the list
// callback, so the picker has already been popped off the overlay stack when the
// chooser is pushed.
type mixedExtractSelectionMsg struct {
	selection ExtractSelection
}

// extractConflictAnswer is the tag of the mixed-extract conflict confirmation: Yes
// retries with replace (target file state replaced; same-name terminal tabs added
// alongside) and No cancels with everything untouched.
type extractConflictAnswer struct {
	selection ExtractSelection
	targetID  string
}

// extractConflictQuestion states exactly what the conflicts are and what Yes does,
// including the append semantics for a same-name terminal tab.
func extractConflictQuestion(msg ExtractConflictMsg) string {
	label := msg.TargetLabel
	if label == "" {
		label = msg.TargetID
	}
	conflicts := strings.Join(msg.Conflicts, ", ")
	return uiTextVars("gvim.workspace.conflict", "Workspace %{label} already has: %{conflicts}. Continue? The target's file state is replaced; same-name terminal tabs are added alongside. The source tabs stay open until the target accepts.", map[string]any{"label": fmt.Sprintf("%q", label), "conflicts": conflicts})
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
	if tag, ok := d.Tag.(extractConflictAnswer); ok {
		if d.Outcome == prompt.Yes {
			return m.runMixedExtract(tag.selection, tag.targetID, true), true
		}
		m.quietNotice("Extraction cancelled; nothing was moved")
		return nil, true
	}
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
	if _, ok := m.workspace.(workspaceExtractTargetPort); ok {
		rows = append(rows,
			commands.Entry{Name: "Extract tab to workspace…", Description: "Move the active tab into an existing workspace over the same root", Action: "workspace_extract_tab_to", Source: commands.SourceBuiltin},
			commands.Entry{Name: "Extract terminal tab to workspace…", Description: "Move the active dock terminal tab into an existing workspace over the same root (it revives on the first dive)", Action: "workspace_extract_terminal_to", Source: commands.SourceBuiltin},
		)
	}
	if _, ok := m.workspace.(workspaceExtractMixedPort); ok {
		rows = append(rows, commands.Entry{Name: "Extract tabs to workspace…", Description: "Choose file and terminal tabs and move them into a new or existing workspace over the same root", Action: "workspace_extract_tabs", Source: commands.SourceBuiltin})
	} else if _, ok := m.workspace.(workspaceExtractManyPort); ok {
		rows = append(rows, commands.Entry{Name: "Extract tabs to new workspace…", Description: "Choose several file tabs and open them in one new workspace over the same root", Action: "workspace_extract_tabs", Source: commands.SourceBuiltin})
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
