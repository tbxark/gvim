package tui

import (
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/explorer"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/browser"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/popup"
	"github.com/tbxark/gvim/internal/tui/sidebar"
)

// rootState is the state of the root features that the editor does not own: the
// optional ports, the git marks, the macro recorder and the last live grep query.
// Its zero value disables every feature.
type rootState struct {
	ports    RootPorts
	gitMarks map[string]rune // the explorer's status letters, keyed by absolute path
	macro    macroState
	// lastGrep is the last Live Grep session's controls: query, modes and scopes,
	// captured when the palette closes. Resume restores them and searches again;
	// hits and the selected row are deliberately not kept.
	lastGrep *palette.LiveResume
	// reload is the state of the background tree reload. See watch_reload.go.
	reload treeReload
	// watched holds the folders of the open documents that were asked of the file
	// watcher, so each one is added once. See watchSync.
	watched map[string]bool
	// recoveryFailed is set while the last write of the crash-recovery copy failed.
	recoveryFailed bool
}

// resetLiveGrepResume drops the cached Live Grep session. A project-root change or a
// configuration change makes the cached query/modes/matches belong to a different
// context, so Resume must start fresh instead of restoring them.
func (m *Model) resetLiveGrepResume() {
	m.root.lastGrep = nil
}

// rootInit returns the commands the root starts when the program starts: the git
// status of the tree, the file watch, the folders of the open documents and the
// auto-save tick.
func (m *Model) rootInit() tea.Cmd {
	cmds := []tea.Cmd{m.gitCmd(), m.autoSaveCmd(), m.watchWait(), m.watchSync(), m.revertPollCmd(), m.recoveryCmd(), m.remoteExplorerInitialCmd(), m.authorityPollCmd()}
	for _, err := range m.root.ports.FullLoadPrompts {
		cmds = append(cmds, func() tea.Msg { return fullLoadPromptMsg{err: err} })
	}
	return tea.Batch(cmds...)
}

// rootMessage handles the messages of the root features. It reports whether it
// took msg, and the command the message starts. A message the root only observes,
// such as a grep result that records the query, is not taken, so the overlay and
// the rest of the root still see it.
func (m *Model) rootMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case popup.Chosen:
		if cmd, ok := m.remoteChosen(msg); ok {
			return cmd, true
		}
	case agentRankMsg:
		if !msg.ov.p.Active() {
			return nil, true // the picker closed while the ranking ran
		}
		return msg.ov.cmdFor(msg.ov.p.Update(msg.msg)), true
	case agentPickMsg:
		return m.openAgentActionPicker(msg.name), true
	case agentActionMsg:
		return m.startAgentChoice(msg.name, msg.resume, msg.dir), true
	case agentChangeDirMsg:
		return m.openAgentDirPrompt(msg.name, msg.dir), true
	case agentRunDirMsg:
		return m.agentDirChosen(msg), true
	case completionReplyMsg:
		return m.completionReplied(msg), true
	case completionAutoMsg:
		return m.autoCompletionDue(msg), true
	case gitStatusMsg:
		m.applyGitStatus(msg)
		return nil, true
	case autoSaveTickMsg:
		return m.autoSaveTick(), true
	case autoSaveDoneMsg:
		return m.autoSaveDone(msg), true
	case recoveryTickMsg:
		return m.recoveryTick(), true
	case recoveryDoneMsg:
		return m.recoveryDone(msg), true
	case treeListingMsg:
		return m.treeListed(msg), true
	case watchMsg:
		return m.watchChanged(), true
	case browser.Choose:
		return m.switchProject(msg.Path), true
	case palette.GrepExportMsg:
		return m.openLiveGrepResultsList(msg), true
	case palette.OpenLineMsg:
		cmd := m.openFileCmd(msg.Path, false, msg.Line, msg.Col)
		m.focusEditor()
		return cmd, true
	case palette.OpenDiagnosticMsg:
		cmd := m.lspGoto(LSPLocation{Path: msg.Path, Start: buffer.Pos{Line: msg.Line - 1, Col: msg.Col - 1}})
		m.focusEditor()
		return cmd, true
	case openLoadedMsg:
		m.openLoaded(msg)
		return nil, true
	case projectOpenedMsg:
		return m.adoptProject(msg), true
	case shellDone:
		return m.finishShell(msg), true
	case openIconPreviewMsg:
		m.openIconPreview()
		return nil, true
	case keybindSavedMsg:
		m.keybindSaved(msg)
		return nil, true
	case tocRescanMsg:
		return m.tocRescan(msg), true
	}
	return nil, false
}

// rootAction runs the root feature's action named action. It reports whether it
// handled the action, and the command the action starts.
func (m *Model) rootAction(action string) (tea.Cmd, bool) {
	switch action {
	case "focus_next_sidebar_section":
		m.cycleSidebar(1)
	case "focus_prev_sidebar_section":
		m.cycleSidebar(-1)
	case "prompt_record_macro", "stop_macro_recording", "play_last_macro":
		return m.macroAction(action), true
	case "open_folder":
		m.openBrowser(true)
	case "open_live_grep":
		return m.openLiveGrep(""), true
	case "resume_live_grep":
		resume := m.root.lastGrep
		query := ""
		if resume != nil {
			query = resume.Text
		}
		return m.openLiveGrepResume(query, resume), true
	default:
		return nil, false
	}
	return nil, true
}

// ExplorerState is the state a session keeps of the explorer column: whether the
// panel is shown, the tree root, the two listing settings, and the bounded sidebar
// UI state (sections, sizes, scroll, width form and side). The zero value is the
// state of a model with no explorer.
type ExplorerState struct {
	Open        bool
	Root        string
	ShowHidden  bool
	ShowIgnored bool
	Sidebar     SidebarState
}

// SidebarState is the sidebar's per-session UI state: where the native sections
// stand (the explorer folded, the Contents and Diagnostics sections open,
// collapsed and their body rows), the explorer's width form and side, and the
// scroll positions of the explorer and Contents trees. It is a bounded snapshot —
// booleans and numbers, no file contents or diagnostics — saved in the session
// file beside the explorer state. The zero value is the state a fresh session
// starts from, so a session file without it (or an old one) keeps the config
// defaults.
type SidebarState struct {
	ExplorerCollapsed bool
	// Width and Side are the explorer's runtime width form and side: the config
	// value, then the grip drag and the side toggle. The zero width is the frame's
	// automatic width and the zero side is the left edge.
	Width explorer.Width
	Side  explorer.Side
	// ExplorerSelected is the selected tree row's path, and ExplorerScroll the
	// tree's first visible row offset; together they put the reader back where the
	// tree stood (the selection keeps the offset meaningful across frame sizes).
	ExplorerSelected string
	ExplorerScroll   int
	// ExplorerRows is the explorer section's explicit body height (0 = flex).
	ExplorerRows int
	// SectionRows is the unified bounded per-section height map (explorer, contents,
	// diagnostics, plugin:<id>); legacy readers use the fields below.
	SectionRows          map[string]int
	ContentsOpen         bool
	ContentsCollapsed    bool
	ContentsRows         int
	ContentsScroll       int
	DiagnosticsOpen      bool
	DiagnosticsCollapsed bool
	DiagnosticsRows      int
	// PluginOrder, PluginRows and PluginCollapsed are the plugin sections' stack
	// order, requested body rows and folded state, by plugin panel id (R9-4); they
	// are restored when the sections mount and collected while they are mounted.
	PluginOrder     []string
	PluginRows      map[string]int
	PluginCollapsed map[string]bool
}

// selectedTreePath is the tree's selected row path, or "" when nothing is
// selected.
func selectedTreePath(tree ExplorerTree) string {
	if tree == nil {
		return ""
	}
	row, ok := tree.Selected()
	if !ok {
		return ""
	}
	return row.Path
}

// SidebarState reports the sidebar's UI state for the session. Only the native
// sections are kept: the plugin sections and their order belong to the plugin
// runtime, and a model with no explorer has no sidebar state.
func (m *Model) SidebarState() SidebarState {
	if m.tree == nil {
		return SidebarState{}
	}
	st := SidebarState{
		ExplorerCollapsed:    m.explorerCollapsed,
		Width:                m.explorerWidth,
		Side:                 m.explorerSide,
		ExplorerSelected:     selectedTreePath(m.tree),
		ExplorerScroll:       m.tree.ScrollOffset(),
		ContentsOpen:         m.contents.Open(),
		ContentsCollapsed:    m.contents.Collapsed(),
		ContentsScroll:       m.contents.Scroll(),
		DiagnosticsOpen:      m.diag.Open(),
		DiagnosticsCollapsed: m.diag.Collapsed(),
	}
	// Capture must not apply the pending plugin restore (it would mutate the model
	// while the UI is only being snapshotted); the helper folds the pending values
	// into the snapshot without applying them.
	st.PluginOrder, st.PluginRows, st.PluginCollapsed = m.pluginSidebarCaptureState()
	// Only the reader's explicit heights belong in the session: a config default
	// contents.rows/diagnostics height must not become an override that beats a later
	// config edit on restart. The explorer is explicit only after its own drag.
	if m.explorerRows > 0 {
		st.ExplorerRows = clampSectionRows(sidebar.KindExplorer, m.explorerRows)
	}
	if m.contents.RowsOverridden() {
		st.ContentsRows = clampSectionRows(sidebar.KindContents, m.contents.Rows())
	}
	if m.diag.rows > 0 {
		st.DiagnosticsRows = clampSectionRows(sidebar.KindDiagnostics, m.diag.Rows())
	}
	rows := map[string]int{}
	if st.ExplorerRows > 0 {
		rows["explorer"] = st.ExplorerRows
	}
	if st.ContentsRows > 0 {
		rows["contents"] = st.ContentsRows
	}
	if st.DiagnosticsRows > 0 {
		rows["diagnostics"] = st.DiagnosticsRows
	}
	for id, n := range st.PluginRows {
		rows["plugin:"+id] = clampSectionRows(sidebar.KindPlugin, n)
	}
	if len(rows) > 0 {
		st.SectionRows = rows
	}
	return st
}

// ExplorerState reports the explorer state. Open is false when there is no tree.
func (m *Model) ExplorerState() ExplorerState {
	if m.tree == nil {
		return ExplorerState{}
	}
	return ExplorerState{
		Open:        m.explorerOpen,
		Root:        m.tree.Root(),
		ShowHidden:  m.tree.ShowHidden(),
		ShowIgnored: m.tree.ShowIgnored(),
		Sidebar:     m.SidebarState(),
	}
}
