package tui

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/reviewdiff"
)

const (
	reviewPreviewDebounce = 50 * time.Millisecond
	reviewRefreshDebounce = 250 * time.Millisecond
)

// reviewModeState holds the root TUI controller state for built-in Review Diff Mode.
type reviewModeState struct {
	active        bool
	startedDirect bool
	sidebarOpen   bool

	// Saved Edit Mode chrome state restored on exit.
	savedExplorerOpen  bool
	savedExplorerFocus bool
	savedTabFocus      bool
	savedContentsFocus bool
	savedDiagFocus     bool

	model        reviewdiff.Model
	styles       reviewdiff.Styles
	repoRoot     string
	headerLayout reviewdiff.HeaderLayout

	changesGen      uint64
	diffGen         uint64
	refsGen         uint64
	previewSeq      uint64
	watchRefreshSeq uint64
	pendingLastHunk bool

	gripDrag       bool
	diffScrollDrag bool
}

type (
	reviewChangesMsg struct {
		gen uint64
		res ReviewChangesResult
		err error
	}
	reviewDiffMsg struct {
		gen     uint64
		path    string
		baseRef string
		diff    *reviewdiff.FileDiff
		err     error
	}
	reviewRefsMsg struct {
		gen  uint64
		refs []string
		err  error
	}
	reviewPreviewTickMsg struct {
		seq uint64
	}
	reviewRefreshTickMsg struct {
		seq uint64
	}
	reviewBasePromptTag struct{}
)

func (m *Model) initReviewMode(startReview bool) {
	m.review.model = reviewdiff.NewModel()
	if m.tabSize > 0 {
		m.review.model.TabSize = m.tabSize
	}
	m.review.styles = reviewdiff.NewStyles(m.theme)
	if startReview {
		m.review.active = true
		m.review.startedDirect = true
		m.review.sidebarOpen = true
		m.review.model.Focus = reviewdiff.FocusFiles
	}
}

func (m *Model) reviewInitCmd() tea.Cmd {
	if !m.review.active {
		return nil
	}
	return m.reviewRefreshCmd()
}

// toggleReviewMode switches between Edit Mode and Review Diff Mode without
// disturbing open Edit Mode tabs or split panes.
func (m *Model) toggleReviewMode() tea.Cmd {
	if m.review.active {
		m.review.startedDirect = false
		return m.exitReviewMode()
	}
	return m.enterReviewMode(false)
}

func (m *Model) enterReviewMode(direct bool) tea.Cmd {
	m.menu = m.menu.Close()
	m.blurPluginPanels()
	m.drag = drag{}
	m.divider = dividerState{}

	if !m.review.active {
		m.review.savedExplorerOpen = m.explorerOpen
		m.review.savedExplorerFocus = m.explorerFocus
		m.review.savedTabFocus = m.tabFocus
		m.review.savedContentsFocus = m.contentsFocus
		m.review.savedDiagFocus = m.diag.focus
		m.review.active = true
		m.review.startedDirect = direct
		m.review.sidebarOpen = true
		if m.tabSize > 0 {
			m.review.model.TabSize = m.tabSize
		}
	}
	m.explorerFocus = false
	m.tabFocus = false
	m.contentsFocus = false
	m.contents.SetFocus(false)
	m.diag.focus = false
	return m.reviewRefreshCmd()
}

// reviewCommands returns Review Diff Mode sub-commands that are only relevant
// while Review Diff Mode is active, keeping the global Command Palette focused
// on the single "Review Diff Mode" entry when editing.
func (m *Model) reviewCommands() []commands.Entry {
	if !m.review.active {
		return nil
	}
	return []commands.Entry{
		{
			Name:        "Review Diff Mode: Refresh",
			Description: "Reload changed files and the active diff in Review Diff Mode",
			Action:      "review_diff_mode_refresh",
			I18nKey:     "gvim.command.review_diff_mode_refresh",
			DescKey:     "gvim.command.review_diff_mode_refresh_desc",
			Source:      commands.SourceBuiltin,
			Keybinding:  m.shortcutOf("review_diff_mode_refresh"),
		},
		{
			Name:        "Review Diff Mode: Compare Against...",
			Description: "Choose a git branch, tag, or commit to compare the working tree against in Review Diff Mode",
			Action:      "review_diff_mode_change_base",
			I18nKey:     "gvim.command.review_diff_mode_change_base",
			DescKey:     "gvim.command.review_diff_mode_change_base_desc",
			Source:      commands.SourceBuiltin,
			Keybinding:  m.shortcutOf("review_diff_mode_change_base"),
		},
	}
}

func (m *Model) exitReviewMode() tea.Cmd {
	if !m.review.active {
		return nil
	}
	if m.review.startedDirect {
		return m.quit()
	}
	m.leaveReviewToEdit(false)
	return nil
}

func (m *Model) leaveReviewToEdit(focusEd bool) {
	m.review.active = false
	m.review.startedDirect = false
	m.review.gripDrag = false
	m.review.diffScrollDrag = false
	m.review.model.FilterActive = false
	m.review.model.SearchActive = false
	m.review.model.HeaderHover = reviewdiff.HeaderNone

	m.explorerOpen = m.review.savedExplorerOpen
	if focusEd {
		m.focusEditor()
	} else {
		m.explorerFocus = m.review.savedExplorerFocus && m.explorerOpen && m.tree != nil
		m.tabFocus = m.review.savedTabFocus
		m.contentsFocus = m.review.savedContentsFocus && m.explorerOpen && m.contents.Open()
		m.contents.SetFocus(m.contentsFocus)
		m.diag.focus = m.review.savedDiagFocus && m.explorerOpen && m.diag.Open()
	}
	m.syncViewport()
}

func (m *Model) reviewWorkDir() string {
	if m.fileRoot != "" {
		return m.fileRoot
	}
	if root := m.rootDir(); root != "" {
		return root
	}
	if doc := m.ed.Active(); doc != nil {
		if pe, ok := doc.(pathEditor); ok && pe.FilePath() != "" {
			return filepath.Dir(pe.FilePath())
		}
	}
	return "."
}

func (m *Model) reviewRefreshCmd() tea.Cmd {
	port := m.root.ports.ReviewGit
	if port == nil {
		m.review.model.SetFiles(nil, "", "Git review backend is unavailable", false)
		return nil
	}
	m.review.changesGen++
	gen := m.review.changesGen
	dir := m.reviewWorkDir()
	baseRef := m.review.model.BaseRef
	m.review.model.LoadingFiles = true
	return func() tea.Msg {
		res, err := port.Changes(context.Background(), dir, baseRef)
		return reviewChangesMsg{gen: gen, res: res, err: err}
	}
}

func (m *Model) scheduleReviewAutoRefresh() tea.Cmd {
	if !m.review.active || m.root.ports.ReviewGit == nil {
		return nil
	}
	m.review.watchRefreshSeq++
	seq := m.review.watchRefreshSeq
	return tea.Tick(reviewRefreshDebounce, func(time.Time) tea.Msg {
		return reviewRefreshTickMsg{seq: seq}
	})
}

func (m *Model) scheduleReviewPreviewDiff() tea.Cmd {
	m.review.previewSeq++
	seq := m.review.previewSeq
	return tea.Tick(reviewPreviewDebounce, func(time.Time) tea.Msg {
		return reviewPreviewTickMsg{seq: seq}
	})
}

func (m *Model) reviewLoadSelectedDiffCmd() tea.Cmd {
	port := m.root.ports.ReviewGit
	if port == nil {
		return nil
	}
	entry, ok := m.review.model.SelectedFile()
	if !ok {
		m.review.model.ClearDiff()
		m.review.model.UnsavedInEditor = false
		return nil
	}
	repoRoot := m.review.repoRoot
	if repoRoot == "" {
		repoRoot = m.reviewWorkDir()
	}
	m.review.diffGen++
	gen := m.review.diffGen
	baseRef := m.review.model.BaseRef
	m.review.model.LoadingDiff = true
	m.syncReviewUnsavedFlag()
	return func() tea.Msg {
		fd, err := port.FileDiff(context.Background(), repoRoot, baseRef, entry)
		return reviewDiffMsg{gen: gen, path: entry.Path, baseRef: baseRef, diff: fd, err: err}
	}
}

func (m *Model) syncReviewUnsavedFlag() {
	entry, ok := m.review.model.SelectedFile()
	if !ok {
		m.review.model.UnsavedInEditor = false
		return
	}
	target := m.reviewFullPath(entry.Path)
	for _, tab := range m.ed.Tabs() {
		if !tab.Modified || tab.Path == "" {
			continue
		}
		if m.fileRoot != "" {
			if path.Clean(tab.Path) == path.Clean(target) {
				m.review.model.UnsavedInEditor = true
				return
			}
		} else if filepath.Clean(tab.Path) == filepath.Clean(target) {
			m.review.model.UnsavedInEditor = true
			return
		}
	}
	m.review.model.UnsavedInEditor = false
}

func (m *Model) reviewFullPath(rel string) string {
	root := m.review.repoRoot
	if root == "" {
		root = m.reviewWorkDir()
	}
	if m.fileRoot != "" {
		return path.Join(root, rel)
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}

func (m *Model) reviewOpenBasePickerCmd() tea.Cmd {
	port := m.root.ports.ReviewGit
	if port == nil {
		m.notice = "Git review backend is unavailable"
		return nil
	}
	m.review.refsGen++
	gen := m.review.refsGen
	dir := m.reviewWorkDir()
	return func() tea.Msg {
		refs, err := port.Refs(context.Background(), dir)
		return reviewRefsMsg{gen: gen, refs: refs, err: err}
	}
}

func (m *Model) openReviewBaseList(refs []string) {
	if len(refs) == 0 {
		refs = []string{"HEAD"}
	}
	cur := m.review.model.BaseRef
	rows := make([]list.Row, 0, len(refs)+1)
	for _, ref := range refs {
		right := ""
		if ref == cur {
			right = "current"
		}
		rows = append(rows, list.Row{Left: ref, Right: right, Value: ref})
	}
	rows = append(rows, list.Row{
		Left:  uiText("gvim.review.custom_ref", "Enter custom ref / commit…"),
		Value: "\x00custom",
	})
	v := list.New(
		uiText("gvim.review.select_base", "Review Diff: Compare Against"),
		rows,
		m.listChrome(),
		func(v *list.View, r list.Row) tea.Cmd {
			v.Close()
			if r.Value == "\x00custom" {
				m.openReviewCustomBasePrompt()
				return nil
			}
			return m.setReviewBaseRef(r.Value)
		},
	)
	v.Fit = true
	m.pushOverlay(v)
}

func (m *Model) openReviewCustomBasePrompt() {
	p := prompt.NewLine(m.styles, prompt.LineOptions{
		Title:     uiText("gvim.review.base_prompt_title", "Compare Against Git Ref"),
		Label:     "Ref:",
		Input:     m.review.model.BaseRef,
		SelectAll: true,
		Tag:       reviewBasePromptTag{},
	})
	m.pushOverlay(p)
}

func (m *Model) setReviewBaseRef(ref string) tea.Cmd {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "HEAD"
	}
	if m.review.model.BaseRef == ref {
		return nil
	}
	m.review.model.BaseRef = ref
	m.review.model.ClearDiff()
	m.notice = fmt.Sprintf("Review Diff: comparing against %s", ref)
	return m.reviewRefreshCmd()
}

// reviewMessage handles async review-mode messages and prompt submissions.
func (m *Model) reviewMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case reviewChangesMsg:
		if msg.gen != m.review.changesGen {
			return nil, true
		}
		if msg.err != nil {
			m.review.model.SetFiles(nil, msg.res.Branch, msg.err.Error(), false)
			m.review.model.ClearDiff()
			m.notice = "Review Diff: " + msg.err.Error()
			return nil, true
		}
		m.review.repoRoot = msg.res.RepoRoot
		m.review.model.SetFiles(msg.res.Files, msg.res.Branch, "", msg.res.NotRepo)
		if msg.res.NotRepo || len(m.review.model.Filtered) == 0 {
			m.review.model.ClearDiff()
			return nil, true
		}
		return m.reviewLoadSelectedDiffCmd(), true

	case reviewDiffMsg:
		if msg.gen != m.review.diffGen {
			return nil, true
		}
		// Discard if selection or base ref moved on while loading.
		cur, ok := m.review.model.SelectedFile()
		if !ok || cur.Path != msg.path || m.review.model.BaseRef != msg.baseRef {
			return nil, true
		}
		if msg.err != nil {
			m.review.model.SetDiff(nil, msg.err.Error())
			return nil, true
		}
		m.review.model.SetDiff(msg.diff, "")
		if m.review.pendingLastHunk {
			m.review.pendingLastHunk = false
			dr := m.reviewDiffRect(m.layout())
			m.review.model.JumpLastHunk(dr.W, m.review.model.DiffViewportRows(dr))
		}
		m.syncReviewUnsavedFlag()
		return nil, true

	case reviewRefsMsg:
		if msg.gen != m.review.refsGen || !m.review.active {
			return nil, true
		}
		if msg.err != nil {
			m.notice = "Review Diff refs: " + msg.err.Error()
		}
		m.openReviewBaseList(msg.refs)
		return nil, true

	case reviewPreviewTickMsg:
		if msg.seq != m.review.previewSeq || !m.review.active {
			return nil, true
		}
		return m.reviewLoadSelectedDiffCmd(), true

	case reviewRefreshTickMsg:
		if msg.seq != m.review.watchRefreshSeq || !m.review.active {
			return nil, true
		}
		return m.reviewRefreshCmd(), true

	case prompt.Done:
		if _, ok := msg.Tag.(reviewBasePromptTag); ok {
			if msg.Outcome != prompt.Submitted {
				return nil, true
			}
			return m.setReviewBaseRef(msg.Text), true
		}
	}
	return nil, false
}

// Geometry helpers derived from frame.Layout so paint and hit-testing match.

func (m *Model) reviewSidebarRect(l frame.Layout) reviewdiff.SidebarRect {
	if !l.ExplorerOpen || l.Ex <= 0 {
		return reviewdiff.SidebarRect{}
	}
	return reviewdiff.SidebarRect{
		X:      l.ExX,
		W:      l.Ex,
		Top:    l.ExTitleY,
		Bottom: l.ExBottom,
		Right:  l.ExRight,
	}
}

func (m *Model) reviewDiffRect(l frame.Layout) reviewdiff.DiffRect {
	return reviewdiff.DiffRect{
		X: l.EditorX,
		Y: l.BodyTop,
		W: l.EditorW,
		H: l.EditorRows(),
	}
}

func (m *Model) paintReviewMode(g *grid.Grid, l frame.Layout) {
	m.syncReviewUnsavedFlag()
	m.review.headerLayout = m.review.model.PaintHeaderBar(g, l.EditorX, l.StripY, l.EditorW, m.review.styles, m.view.icons)
	m.review.model.PaintDiff(g, m.reviewDiffRect(l), m.review.styles)
	m.dock.Draw(g, l, m.styles)
	if l.ExplorerOpen {
		m.review.model.PaintSidebar(g, m.reviewSidebarRect(l), m.review.styles, m.view.icons)
		if m.gripHover {
			m.paintGripHover(g, l)
		}
	}
}

func (m *Model) reviewStatusInfo(s *frame.StatusView) {
	s.ReviewMode = true
	diffW := m.width
	if m.review.sidebarOpen {
		diffW -= frame.ExplorerCols(m.width, m.explorerWidth)
	}
	var parts []string
	parts = append(parts, "vs "+m.review.model.BaseRef)
	if m.review.model.Branch != "" {
		parts = append(parts, m.review.model.Branch)
	}
	if cur, total := m.review.model.CurrentHunkPos(diffW); total > 0 {
		parts = append(parts, fmt.Sprintf("Hunk %d/%d", cur, total))
	} else if len(m.review.model.Files) > 0 {
		parts = append(parts, fmt.Sprintf("%d files", len(m.review.model.Files)))
	}
	s.ReviewText = strings.Join(parts, " · ")

	switch {
	case m.review.model.FilterActive:
		s.ReviewHints = "Type to filter  Enter:done  Esc:clear"
	case m.review.model.SearchActive:
		s.ReviewHints = "Type to search  Enter:confirm  Esc:cancel"
	case m.review.model.Focus == reviewdiff.FocusFiles:
		s.ReviewHints = "j/k:file  Tab/Enter:diff  /:filter  v:split  b:base  o:edit  q:exit"
	default:
		s.ReviewHints = "Tab:files  ]/[:hunk  v:split  f:full  w:wrap  /:find  Enter:edit  q:exit"
	}
}

func (m *Model) reviewJumpToEdit(relPath string, line int) tea.Cmd {
	full := m.reviewFullPath(relPath)
	m.leaveReviewToEdit(true)
	return m.openFileCmd(full, false, line, 1)
}

func (m *Model) reviewStepHunk(dir int) tea.Cmd {
	l := m.layout()
	dr := m.reviewDiffRect(l)
	viewRows := m.review.model.DiffViewportRows(dr)
	var jumped, cross bool
	if dir > 0 {
		jumped, cross = m.review.model.NextHunk(dr.W, viewRows)
	} else {
		jumped, cross = m.review.model.PrevHunk(dr.W, viewRows)
	}
	if jumped || !cross {
		return nil
	}
	// Cross to the adjacent file in the filtered list.
	if len(m.review.model.Filtered) <= 1 {
		if dir > 0 {
			m.notice = "Last hunk"
		} else {
			m.notice = "First hunk"
		}
		return nil
	}
	nextIdx := m.review.model.Selected + dir
	if nextIdx < 0 {
		m.notice = "First hunk"
		return nil
	}
	if nextIdx >= len(m.review.model.Filtered) {
		m.notice = "Last hunk"
		return nil
	}
	sr := m.reviewSidebarRect(l)
	if _, changed := m.review.model.SelectFile(nextIdx, sr.ListRows()); changed {
		m.review.pendingLastHunk = dir < 0
		return m.reviewLoadSelectedDiffCmd()
	}
	return nil
}

// reviewGlobalFallthrough are app-wide actions that keep their Normal keymap
// binding while Review Diff Mode is active.
var reviewGlobalFallthrough = map[string]bool{
	"toggle_review_diff_mode": true,
	"quit":                    true,
	"command_palette":         true,
	"quick_open":              true,
	"quick_open_files":        true,
	"quick_open_buffers":      true,
	"menu_activate":           true,
	"menu_open":               true,
	"open_settings":           true,
	"show_help":               true,
	"toggle_utility_dock":     true,
	"open_terminal_in_dock":   true,
	"toggle_dock_focus":       true,
	"toggle_file_explorer":    true,
	"compare_next_difference": true,
	"compare_prev_difference": true,
}

// handleReviewKey handles keyboard input while Review Diff Mode is active.
func (m *Model) handleReviewKey(k tea.Key, kp KeyPress, text string) tea.Cmd {
	// Text-entry states own printable input and Backspace/Enter/Esc first so
	// single-letter shortcuts (q, j, k, v, etc.) never fire while typing.
	if m.review.model.FilterActive {
		return m.handleReviewFilterKey(k, kp, text)
	}
	if m.review.model.SearchActive {
		return m.handleReviewSearchKey(k, kp, text)
	}

	// Consult app-wide keymap bindings (Alt+Shift+R, Ctrl+Q, Ctrl+P, Ctrl+B,
	// Alt+J/Alt+`, menus, Alt+F5 hunk jump, etc.).
	if m.keys != nil {
		if cmd, ok := m.keys.Resolve(kp, KeyContextNormal); ok && reviewGlobalFallthrough[cmd.Action] {
			if m.suppressedMnemonic(kp, cmd) {
				return nil
			}
			switch cmd.Action {
			case "toggle_file_explorer":
				m.review.sidebarOpen = !m.review.sidebarOpen
				if !m.review.sidebarOpen {
					m.review.model.Focus = reviewdiff.FocusDiff
				}
				return nil
			case "compare_next_difference":
				return m.reviewStepHunk(1)
			case "compare_prev_difference":
				return m.reviewStepHunk(-1)
			}
			return m.dispatch(cmd)
		}
	}

	l := m.layout()
	sr := m.reviewSidebarRect(l)
	dr := m.reviewDiffRect(l)
	viewRows := m.review.model.DiffViewportRows(dr)
	hasMod := k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) != 0

	// Pane focus toggle.
	if kp.Name == "Tab" || kp.Name == "BackTab" {
		if m.review.sidebarOpen {
			if m.review.model.Focus == reviewdiff.FocusFiles {
				m.review.model.Focus = reviewdiff.FocusDiff
			} else {
				m.review.model.Focus = reviewdiff.FocusFiles
			}
		} else {
			m.review.sidebarOpen = true
			m.review.model.Focus = reviewdiff.FocusFiles
		}
		return nil
	}

	if kp.Name == "Escape" {
		if m.review.model.SearchQuery != "" {
			m.review.model.ClearSearch()
			return nil
		}
		if m.review.model.FilterQuery != "" {
			m.review.model.SetFilter("")
			return m.reviewLoadSelectedDiffCmd()
		}
		if !m.review.startedDirect {
			return m.exitReviewMode()
		}
		return nil
	}

	// Single-key shortcuts (no Ctrl/Alt/Super).
	if !hasMod && text != "" {
		switch text {
		case "q":
			return m.exitReviewMode()
		case "r":
			m.notice = "Refreshing changes…"
			return m.reviewRefreshCmd()
		case "v":
			m.review.model.ToggleViewMode()
			m.notice = "Diff view: " + m.review.model.ViewMode.Label()
			return nil
		case "f":
			m.review.model.ToggleFullFile()
			if m.review.model.FullFile {
				m.notice = "Diff context: full file"
			} else {
				m.notice = "Diff context: hunks only"
			}
			return nil
		case "w":
			m.review.model.ToggleWrap()
			if m.review.model.Wrap {
				m.notice = "Diff wrap: on"
			} else {
				m.notice = "Diff wrap: off"
			}
			return nil
		case "b":
			return m.reviewOpenBasePickerCmd()
		case "]":
			return m.reviewStepHunk(1)
		case "[":
			return m.reviewStepHunk(-1)
		case "/":
			if m.review.model.Focus == reviewdiff.FocusFiles && m.review.sidebarOpen {
				m.review.model.FilterActive = true
			} else {
				m.review.model.Focus = reviewdiff.FocusDiff
				m.review.model.SearchActive = true
			}
			return nil
		case "n":
			if m.review.model.SearchQuery != "" {
				m.review.model.NextMatch(dr.W, viewRows)
				return nil
			}
		case "N":
			if m.review.model.SearchQuery != "" {
				m.review.model.PrevMatch(dr.W, viewRows)
				return nil
			}
		case "o":
			if p, line, ok := m.review.model.EditTargetAtCursor(dr.W); ok {
				return m.reviewJumpToEdit(p, line)
			}
			if f, ok := m.review.model.SelectedFile(); ok && f.Status != reviewdiff.StatusDeleted {
				return m.reviewJumpToEdit(f.Path, 1)
			}
			m.notice = "Cannot open deleted file in editor"
			return nil
		}
	}

	if m.review.model.Focus == reviewdiff.FocusFiles && m.review.sidebarOpen {
		return m.handleReviewFilesKey(kp, text, hasMod, sr.ListRows())
	}
	return m.handleReviewDiffKey(k, kp, text, hasMod, dr.W, viewRows)
}

func (m *Model) handleReviewFilesKey(kp KeyPress, text string, hasMod bool, listRows int) tea.Cmd {
	move := func(delta int) tea.Cmd {
		if _, changed := m.review.model.MoveFile(delta, listRows); changed {
			return m.scheduleReviewPreviewDiff()
		}
		return nil
	}
	switch kp.Name {
	case "Up":
		return move(-1)
	case "Down":
		return move(1)
	case "PageUp":
		return move(-max(listRows-1, 1))
	case "PageDown":
		return move(max(listRows-1, 1))
	case "Home":
		if _, changed := m.review.model.SelectFile(0, listRows); changed {
			return m.scheduleReviewPreviewDiff()
		}
		return nil
	case "End":
		if n := len(m.review.model.Filtered); n > 0 {
			if _, changed := m.review.model.SelectFile(n-1, listRows); changed {
				return m.scheduleReviewPreviewDiff()
			}
		}
		return nil
	case "Enter", "Right":
		m.review.model.Focus = reviewdiff.FocusDiff
		// If a debounced diff load is still pending, fetch immediately.
		if f, ok := m.review.model.SelectedFile(); ok {
			if m.review.model.Diff == nil || m.review.model.Diff.Path != f.Path {
				return m.reviewLoadSelectedDiffCmd()
			}
		}
		return nil
	}
	if !hasMod {
		switch text {
		case "j":
			return move(1)
		case "k":
			return move(-1)
		case "g":
			if _, changed := m.review.model.SelectFile(0, listRows); changed {
				return m.scheduleReviewPreviewDiff()
			}
		case "G":
			if n := len(m.review.model.Filtered); n > 0 {
				if _, changed := m.review.model.SelectFile(n-1, listRows); changed {
					return m.scheduleReviewPreviewDiff()
				}
			}
		case "l":
			m.review.model.Focus = reviewdiff.FocusDiff
		}
	}
	return nil
}

func (m *Model) handleReviewDiffKey(k tea.Key, kp KeyPress, text string, hasMod bool, diffW, viewRows int) tea.Cmd {
	switch kp.Name {
	case "Up":
		m.review.model.MoveCursor(-1, diffW, viewRows)
		return nil
	case "Down":
		m.review.model.MoveCursor(1, diffW, viewRows)
		return nil
	case "PageUp":
		m.review.model.PageMove(-1, diffW, viewRows)
		return nil
	case "PageDown":
		m.review.model.PageMove(1, diffW, viewRows)
		return nil
	case "Home":
		m.review.model.JumpTop()
		return nil
	case "End":
		m.review.model.JumpBottom(diffW, viewRows)
		return nil
	case "Left":
		if m.review.model.HScroll > 0 {
			m.review.model.ScrollHorizontal(-4)
		} else if m.review.sidebarOpen {
			m.review.model.Focus = reviewdiff.FocusFiles
		}
		return nil
	case "Right":
		if !m.review.model.Wrap || m.review.model.EffectiveViewMode(diffW) == reviewdiff.ViewSplit {
			m.review.model.ScrollHorizontal(4)
		}
		return nil
	case "Enter":
		if m.review.model.ExpandAtCursor(diffW) {
			return nil
		}
		if p, line, ok := m.review.model.EditTargetAtCursor(diffW); ok {
			return m.reviewJumpToEdit(p, line)
		}
		if m.review.model.Diff != nil && m.review.model.Diff.Status == reviewdiff.StatusDeleted {
			m.notice = "Cannot open deleted file in editor"
		}
		return nil
	}
	if k.Mod == tea.ModCtrl {
		switch kp.Name {
		case "d":
			m.review.model.MoveCursor(max(viewRows/2, 1), diffW, viewRows)
			return nil
		case "u":
			m.review.model.MoveCursor(-max(viewRows/2, 1), diffW, viewRows)
			return nil
		}
	}
	if !hasMod {
		switch text {
		case "j":
			m.review.model.MoveCursor(1, diffW, viewRows)
		case "k":
			m.review.model.MoveCursor(-1, diffW, viewRows)
		case " ":
			m.review.model.PageMove(1, diffW, viewRows)
		case "d":
			m.review.model.MoveCursor(max(viewRows/2, 1), diffW, viewRows)
		case "u":
			m.review.model.MoveCursor(-max(viewRows/2, 1), diffW, viewRows)
		case "g":
			m.review.model.JumpTop()
		case "G":
			m.review.model.JumpBottom(diffW, viewRows)
		case "h":
			if m.review.model.HScroll > 0 {
				m.review.model.ScrollHorizontal(-4)
			} else if m.review.sidebarOpen {
				m.review.model.Focus = reviewdiff.FocusFiles
			}
		case "l":
			if !m.review.model.Wrap || m.review.model.EffectiveViewMode(diffW) == reviewdiff.ViewSplit {
				m.review.model.ScrollHorizontal(4)
			}
		case "e":
			m.review.model.ExpandAtCursor(diffW)
		}
	}
	return nil
}

func (m *Model) handleReviewFilterKey(k tea.Key, kp KeyPress, text string) tea.Cmd {
	prevPath := m.review.model.SelectedPath
	switch kp.Name {
	case "Escape":
		m.review.model.FilterActive = false
		if m.review.model.FilterQuery != "" {
			m.review.model.SetFilter("")
			return m.reviewLoadSelectedDiffCmd()
		}
		return nil
	case "Enter", "Down":
		m.review.model.FilterActive = false
		m.review.model.Focus = reviewdiff.FocusFiles
		return nil
	case "Tab":
		m.review.model.FilterActive = false
		m.review.model.Focus = reviewdiff.FocusDiff
		return nil
	case "Backspace":
		if q := m.review.model.FilterQuery; len(q) > 0 {
			_, sz := utf8.DecodeLastRuneInString(q)
			m.review.model.SetFilter(q[:len(q)-sz])
		}
	default:
		if k.Mod == tea.ModCtrl && (kp.Name == "u" || kp.Name == "w") {
			m.review.model.SetFilter("")
		} else if text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) == 0 {
			m.review.model.SetFilter(m.review.model.FilterQuery + text)
		}
	}
	if m.review.model.SelectedPath != prevPath {
		return m.scheduleReviewPreviewDiff()
	}
	return nil
}

func (m *Model) handleReviewSearchKey(k tea.Key, kp KeyPress, text string) tea.Cmd {
	dr := m.reviewDiffRect(m.layout())
	viewRows := m.review.model.DiffViewportRows(dr)
	switch kp.Name {
	case "Escape":
		m.review.model.ClearSearch()
		return nil
	case "Enter":
		m.review.model.SearchActive = false
		return nil
	case "Backspace":
		if q := m.review.model.SearchQuery; len(q) > 0 {
			_, sz := utf8.DecodeLastRuneInString(q)
			m.review.model.SetSearch(q[:len(q)-sz], dr.W, viewRows)
		}
		return nil
	default:
		if k.Mod == tea.ModCtrl && (kp.Name == "u" || kp.Name == "w") {
			m.review.model.SetSearch("", dr.W, viewRows)
			return nil
		}
		if text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) == 0 {
			m.review.model.SetSearch(m.review.model.SearchQuery+text, dr.W, viewRows)
		}
	}
	return nil
}

// Mouse routing for Review Diff Mode.

func (m *Model) handleReviewClick(msg tea.MouseClickMsg) tea.Cmd {
	mo := tea.Mouse(msg)
	count := 0
	if mo.Button == tea.MouseLeft {
		count = m.clicks.PressWindow(m.now(), mo.X, mo.Y, m.doubleClickWindow())
	}
	if cmd, taken := m.dockMouse(msg, mo.X, mo.Y, count); taken {
		return cmd
	}
	if mo.Button != tea.MouseLeft {
		return nil
	}

	l := m.layout()

	// Status bar clicks (e.g. [REVIEW] pill or Palette button).
	if l.StatusOn && mo.Y == l.StatusY {
		t := l.Hit(mo.X, mo.Y)
		switch t.Kind {
		case frame.HitStatusPalette:
			return m.openPalette(">")
		case frame.HitStatusSegment:
			switch t.Index {
			case frame.StatusSegmentReviewMode:
				m.review.startedDirect = false
				return m.exitReviewMode()
			case frame.StatusSegmentMessages:
				return m.dispatch(Command{Action: "show_warnings"})
			}
		}
		return nil
	}

	// Diff header bar controls.
	if act := m.review.headerLayout.Hit(mo.X, mo.Y); act != reviewdiff.HeaderNone {
		m.review.model.FilterActive = false
		m.review.model.SearchActive = false
		switch act {
		case reviewdiff.HeaderBaseRef:
			return m.reviewOpenBasePickerCmd()
		case reviewdiff.HeaderViewMode:
			m.review.model.ToggleViewMode()
		case reviewdiff.HeaderFullFile:
			m.review.model.ToggleFullFile()
		case reviewdiff.HeaderWrap:
			m.review.model.ToggleWrap()
		case reviewdiff.HeaderEdit:
			dr := m.reviewDiffRect(l)
			if p, line, ok := m.review.model.EditTargetAtCursor(dr.W); ok {
				return m.reviewJumpToEdit(p, line)
			}
			if f, ok := m.review.model.SelectedFile(); ok && f.Status != reviewdiff.StatusDeleted {
				return m.reviewJumpToEdit(f.Path, 1)
			}
			m.notice = "Cannot open deleted file in editor"
		}
		return nil
	}

	// Changes sidebar clicks.
	if l.ExplorerOpen {
		sr := m.reviewSidebarRect(l)
		sh := m.review.model.HitSidebar(sr, m.view.icons, mo.X, mo.Y)
		switch sh.Kind {
		case reviewdiff.SidebarHitReload:
			return m.reviewRefreshCmd()
		case reviewdiff.SidebarHitClose:
			m.review.sidebarOpen = false
			m.review.model.Focus = reviewdiff.FocusDiff
			return nil
		case reviewdiff.SidebarHitGrip:
			m.review.gripDrag = true
			return nil
		case reviewdiff.SidebarHitFilter:
			m.review.model.Focus = reviewdiff.FocusFiles
			m.review.model.FilterActive = true
			m.review.model.SearchActive = false
			return nil
		case reviewdiff.SidebarHitFile:
			m.review.model.Focus = reviewdiff.FocusFiles
			m.review.model.FilterActive = false
			m.review.model.SearchActive = false
			entry, changed := m.review.model.SelectFile(sh.FilteredIdx, sr.ListRows())
			if count >= 2 && entry.Status != reviewdiff.StatusDeleted {
				return m.reviewJumpToEdit(entry.Path, 1)
			}
			if changed || m.review.model.Diff == nil || m.review.model.Diff.Path != entry.Path {
				return m.reviewLoadSelectedDiffCmd()
			}
			return nil
		case reviewdiff.SidebarHitBody:
			m.review.model.Focus = reviewdiff.FocusFiles
			m.review.model.FilterActive = false
			return nil
		}
	}

	// Main diff pane clicks.
	dr := m.reviewDiffRect(l)
	dh := m.review.model.HitDiff(dr, mo.X, mo.Y)
	switch dh.Kind {
	case reviewdiff.DiffHitSearchBar:
		m.review.model.Focus = reviewdiff.FocusDiff
		m.review.model.SearchActive = true
		m.review.model.FilterActive = false
		return nil
	case reviewdiff.DiffHitScrollbar:
		m.review.model.Focus = reviewdiff.FocusDiff
		m.review.diffScrollDrag = true
		m.review.model.ScrollToTrackRow(dh.ScrollRow, dr.W, m.review.model.DiffViewportRows(dr))
		return nil
	case reviewdiff.DiffHitExpand:
		m.review.model.Focus = reviewdiff.FocusDiff
		m.review.model.FilterActive = false
		m.review.model.Cursor = dh.RowIdx
		m.review.model.ExpandGap(dh.GapID)
		return nil
	case reviewdiff.DiffHitRow:
		m.review.model.Focus = reviewdiff.FocusDiff
		m.review.model.FilterActive = false
		m.review.model.SearchActive = false
		m.review.model.Cursor = dh.RowIdx
		if count >= 2 {
			if p, line, ok := m.review.model.EditTargetAtCursor(dr.W); ok {
				return m.reviewJumpToEdit(p, line)
			}
		}
		return nil
	}
	return nil
}

func (m *Model) handleReviewMotion(mo tea.Mouse) {
	l := m.layout()
	if m.review.gripDrag && mo.Button == tea.MouseLeft {
		newW := mo.X + 1
		if l.ExRight {
			newW = l.W - mo.X
		}
		cols := frame.ExplorerCols(l.W, explorer.Width{Mode: explorer.WidthColumns, Value: newW})
		m.explorerWidth = m.explorerWidth.WithCols(cols, l.W)
		return
	}
	if m.review.diffScrollDrag && mo.Button == tea.MouseLeft {
		dr := m.reviewDiffRect(l)
		viewRows := m.review.model.DiffViewportRows(dr)
		m.review.model.ScrollToTrackRow(mo.Y-dr.Y, dr.W, viewRows)
		return
	}
	// Pointer hover feedback on header bar controls, grip, and status bar.
	m.review.model.HeaderHover = m.review.headerLayout.Hit(mo.X, mo.Y)
	m.gripHover = false
	if l.ExplorerOpen {
		sh := m.review.model.HitSidebar(m.reviewSidebarRect(l), m.view.icons, mo.X, mo.Y)
		m.gripHover = sh.Kind == reviewdiff.SidebarHitGrip
	}
	m.statusHover = frame.HitTarget{}
	if l.StatusOn && mo.Y == l.StatusY {
		t := l.Hit(mo.X, mo.Y)
		if t.Kind == frame.HitStatusSegment || t.Kind == frame.HitStatusPalette {
			m.statusHover = t
		}
	}
}

func (m *Model) handleReviewRelease() {
	m.review.gripDrag = false
	m.review.diffScrollDrag = false
}

func (m *Model) handleReviewWheel(msg tea.MouseWheelMsg) tea.Cmd {
	mo := tea.Mouse(msg)
	if cmd, taken := m.dockMouse(msg, mo.X, mo.Y, 1); taken {
		return cmd
	}
	l := m.layout()
	rows := m.wheelRows()

	if l.ExplorerOpen && l.InExplorer(mo.X) {
		sr := m.reviewSidebarRect(l)
		switch mo.Button {
		case tea.MouseWheelUp:
			m.review.model.ScrollFiles(-rows, sr.ListRows())
		case tea.MouseWheelDown:
			m.review.model.ScrollFiles(rows, sr.ListRows())
		}
		return nil
	}

	dr := m.reviewDiffRect(l)
	viewRows := m.review.model.DiffViewportRows(dr)
	switch mo.Button {
	case tea.MouseWheelUp:
		if mo.Mod&tea.ModShift != 0 {
			m.review.model.ScrollHorizontal(-4)
		} else {
			m.review.model.ScrollDiff(-rows, dr.W, viewRows)
		}
	case tea.MouseWheelDown:
		if mo.Mod&tea.ModShift != 0 {
			m.review.model.ScrollHorizontal(4)
		} else {
			m.review.model.ScrollDiff(rows, dr.W, viewRows)
		}
	case tea.MouseWheelLeft:
		m.review.model.ScrollHorizontal(-4)
	case tea.MouseWheelRight:
		m.review.model.ScrollHorizontal(4)
	}
	return nil
}
