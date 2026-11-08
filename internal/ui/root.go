package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/ui/browser"
	"github.com/tbxark/gvim/internal/ui/palette"
)

// rootState is the state of the root features that the editor does not own: the
// optional ports, the git marks, the macro recorder and the last live grep query.
// Its zero value disables every feature.
type rootState struct {
	ports    RootPorts
	gitMarks map[string]rune // the explorer's status letters, keyed by absolute path
	macro    macroState
	// lastGrep is the text of the last live grep query, which resume reopens.
	lastGrep string
	// reload is the state of the background tree reload. See watch_reload.go.
	reload treeReload
	// watched holds the folders of the open documents that were asked of the file
	// watcher, so each one is added once. See watchSync.
	watched map[string]bool
	// recoveryFailed is set while the last write of the crash-recovery copy failed.
	recoveryFailed bool
}

// rootInit returns the commands the root starts when the program starts: the git
// status of the tree, the file watch, the folders of the open documents and the
// auto-save tick.
func (m *Model) rootInit() tea.Cmd {
	cmds := []tea.Cmd{m.gitCmd(), m.autoSaveCmd(), m.watchWait(), m.watchSync(), m.revertPollCmd(), m.recoveryCmd()}
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
	case palette.OpenLineMsg:
		cmd := m.openFileCmd(msg.Path, false, msg.Line, msg.Col)
		m.focusEditor()
		return cmd, true
	case openLoadedMsg:
		m.openLoaded(msg)
		return nil, true
	case palette.GrepResultMsg:
		m.root.lastGrep = msg.Text
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
		return m.openLiveGrep(m.root.lastGrep), true
	default:
		return nil, false
	}
	return nil, true
}

// ExplorerState is the explorer state a session keeps: whether the panel is shown,
// the tree root and the two listing settings. The zero value is the state of a
// model with no explorer.
type ExplorerState struct {
	Open        bool
	Root        string
	ShowHidden  bool
	ShowIgnored bool
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
	}
}
