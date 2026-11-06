package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/editorpane"
	expanel "github.com/tbxark/gvim/internal/ui/explorer"
	"github.com/tbxark/gvim/internal/ui/icons"
	"github.com/tbxark/gvim/internal/ui/menubar"
	"github.com/tbxark/gvim/internal/ui/prompt"
	"github.com/tbxark/gvim/internal/ui/style"
)

// Cursor styles, as the config and select_cursor_style name them.
const (
	cursorBlock     = "block"
	cursorBar       = "bar"
	cursorUnderline = "underline"
)

// ViewOptions are the starting display settings of the root, from the config. The app
// sets Deps.View. A nil Deps.View is DefaultViewOptions.
type ViewOptions struct {
	ShowTabBar              bool
	ShowStatusBar           bool
	ShowPromptLine          bool
	ShowVerticalScrollbar   bool
	ShowHorizontalScrollbar bool
	LineWrap                bool
	HighlightCurrentLine    bool
	HighlightOccurrences    bool
	ShowWhitespace          bool
	CursorStyle             string // "block", "bar" or "underline"
	MouseCapture            bool
	MouseHover              bool
	ScrollSync              bool
	UIIcons                 string // "plain" or "nerd"; empty and unknown names are plain
}

// DefaultViewOptions returns the settings gvim starts with when no config sets them.
// They match the config defaults, and the layout of gvim before these settings existed.
func DefaultViewOptions() ViewOptions {
	return ViewOptions{
		ShowTabBar:              true,
		ShowStatusBar:           true,
		ShowVerticalScrollbar:   true,
		ShowHorizontalScrollbar: true,
		LineWrap:                true,
		HighlightCurrentLine:    true,
		MouseCapture:            true,
		CursorStyle:             cursorBlock,
	}
}

// viewState is the display state of the root that the view commands change. The
// menu bar and the line numbers keep their fields on Model, as the layout reads them.
type viewState struct {
	tabBar, statusBar, promptLine bool
	vScroll, hScroll              bool
	lineWrap, currentLine         bool
	occurrences, whitespace       bool
	cursor                        string
	mouseCapture, mouseHover      bool
	scrollSync                    bool
	debug                         bool // the status row shows the highlight ranges
	icons                         icons.Mode
}

// newViewState returns the state for the starting options o.
func newViewState(o ViewOptions) viewState {
	cursor := o.CursorStyle
	if cursor == "" {
		cursor = cursorBlock
	}
	mode, _ := icons.Parse(o.UIIcons) // an unknown name is plain
	return viewState{
		icons:        mode,
		tabBar:       o.ShowTabBar,
		statusBar:    o.ShowStatusBar,
		promptLine:   o.ShowPromptLine,
		vScroll:      o.ShowVerticalScrollbar,
		hScroll:      o.ShowHorizontalScrollbar,
		lineWrap:     o.LineWrap,
		currentLine:  o.HighlightCurrentLine,
		occurrences:  o.HighlightOccurrences,
		whitespace:   o.ShowWhitespace,
		cursor:       cursor,
		mouseCapture: o.MouseCapture,
		mouseHover:   o.MouseHover,
		scrollSync:   o.ScrollSync,
	}
}

// viewDocument is the optional view of a document's display settings. The app adapter
// forwards it from the editor document. A document without it has the global settings
// only.
type viewDocument interface {
	ViewSettings() editor.ViewSettings
}

// docViewOf returns the display settings of doc, or none when doc does not keep them.
func docViewOf(doc Document) editor.ViewSettings {
	if v, ok := doc.(viewDocument); ok {
		return v.ViewSettings()
	}
	return editor.ViewSettings{}
}

// docViewSettings returns the display settings of the active document.
func (m *Model) docViewSettings() editor.ViewSettings {
	doc := m.ed.Active()
	if doc == nil {
		return editor.ViewSettings{}
	}
	return docViewOf(doc)
}

// viewTag identifies the prompt that asks for the argument of a view command. text
// marks a prompt whose answer is a name, not a number.
type viewTag struct {
	action string
	key    string
	text   bool
}

// viewAction runs a view command that the root handles. It reports whether it did, and
// the command the action starts, if any. The per-buffer commands with a global setting
// behind them send the editor the value the UI computed. The commands that only the
// editor knows, and the ones given their argument, go on to the editor.
func (m *Model) viewAction(action string, args map[string]any) (tea.Cmd, bool) {
	switch action {
	case "toggle_menu_bar":
		m.hideMenuBar = !m.hideMenuBar
		m.notice = shownNotice("Menu bar", !m.hideMenuBar)
	case "toggle_tab_bar":
		m.view.tabBar = !m.view.tabBar
		// A hidden strip cannot take the keys, so the focus leaves it.
		m.tabFocus = m.tabFocus && m.view.tabBar
		m.notice = shownNotice("Tab bar", m.view.tabBar)
	case "toggle_status_bar":
		m.view.statusBar = !m.view.statusBar
		m.notice = shownNotice("Status bar", m.view.statusBar)
	case "toggle_prompt_line":
		m.view.promptLine = !m.view.promptLine
		m.notice = shownNotice("Prompt line", m.view.promptLine)
	case "toggle_vertical_scrollbar":
		m.view.vScroll = !m.view.vScroll
		m.notice = shownNotice("Vertical scrollbar", m.view.vScroll)
	case "toggle_horizontal_scrollbar":
		m.view.hScroll = !m.view.hScroll
		m.notice = shownNotice("Horizontal scrollbar", m.view.hScroll)
	case "toggle_line_wrap":
		m.view.lineWrap = !m.view.lineWrap
		m.notice = onOffNotice("Line wrap", m.view.lineWrap)
	case "toggle_current_line_highlight":
		m.view.currentLine = !m.view.currentLine
		m.notice = onOffNotice("Current line highlight", m.view.currentLine)
	case "toggle_occurrence_highlight":
		m.view.occurrences = !m.view.occurrences
		m.notice = onOffNotice("Occurrence highlight", m.view.occurrences)
	case "toggle_line_numbers_current_buffer":
		return m.bufferFlag(action, args, editor.OverrideOr(m.docViewSettings().LineNumbers, !m.hideLineNumbers))
	case "toggle_line_wrap_current_buffer":
		return m.bufferFlag(action, args, editor.OverrideOr(m.docViewSettings().LineWrap, m.view.lineWrap))
	case "toggle_current_line_highlight_current_buffer":
		return m.bufferFlag(action, args, editor.OverrideOr(m.docViewSettings().CurrentLine, m.view.currentLine))
	case "toggle_occurrence_highlight_current_buffer":
		return m.bufferFlag(action, args, editor.OverrideOr(m.docViewSettings().Occurrences, m.view.occurrences))
	case "toggle_whitespace_indicators":
		return m.bufferFlag(action, args, editor.OverrideOr(m.docViewSettings().Whitespace, m.view.whitespace))
	case "add_ruler", "remove_ruler":
		if _, ok := args["column"]; ok {
			return nil, false
		}
		m.askNumber(action, "column", "Ruler column:", editor.MaxViewColumn)
	case "set_tab_size":
		if _, ok := args["size"]; ok {
			return nil, false
		}
		m.askNumber(action, "size", "Tab size:", editor.MaxViewTabSize)
	case "set_page_width":
		if _, ok := args["width"]; ok {
			return nil, false
		}
		m.askNumber(action, "width", "Page width:", editor.MaxViewColumn)
	case "set_language":
		if _, ok := args["name"]; ok {
			return nil, false
		}
		m.askText(action, "name", "Language:")
	case "select_cursor_style":
		if s, ok := args["style"].(string); ok {
			m.setCursorStyle(s)
			return nil, true
		}
		m.openCursorStyles()
	case "select_theme":
		if name, ok := args["name"].(string); ok {
			m.applyTheme(name)
			return nil, true
		}
		m.openThemes()
	case "select_locale":
		m.openLocales()
	case "toggle_scroll_sync":
		m.view.scrollSync = !m.view.scrollSync
		m.notice = onOffNotice("Scroll sync", m.view.scrollSync) + " (the compare view does not read it yet)"
	case "toggle_mouse_hover":
		m.view.mouseHover = !m.view.mouseHover
		m.notice = onOffNotice("Mouse hover", m.view.mouseHover) + " (hover on mouse movement is not built in this port)"
	case "toggle_mouse_capture":
		m.view.mouseCapture = !m.view.mouseCapture
		if m.view.mouseCapture {
			m.notice = "Mouse capture on: gvim takes the mouse"
		} else {
			m.notice = "Mouse capture off: the terminal keeps the mouse"
		}
	case "toggle_file_explorer_side":
		m.notice = "Moving the file explorer to the other side is not built in this port yet"
	case "toggle_debug_highlights":
		m.view.debug = !m.view.debug
		m.notice = onOffNotice("Highlight debug", m.view.debug)
	case "set_background", "set_background_blend":
		m.notice = backgroundNotice
	default:
		return nil, false
	}
	return nil, true
}

// backgroundNotice says why the background commands do nothing in a terminal.
const backgroundNotice = "Not applicable in a terminal: text cells are drawn over the terminal's own background, which has no image layer to fade"

// shownNotice is the status text of a shown or hidden part.
func shownNotice(what string, shown bool) string {
	if shown {
		return what + " shown"
	}
	return what + " hidden"
}

// onOffNotice is the status text of a setting that is on or off.
func onOffNotice(what string, on bool) string {
	return what + ": " + onOff(on)
}

// bufferFlag sets a per-buffer flag to the opposite of cur, through the editor. A call
// that already gives a value keeps it, so the editor reads it.
func (m *Model) bufferFlag(action string, args map[string]any, cur bool) (tea.Cmd, bool) {
	if _, ok := args["value"]; ok {
		return nil, false
	}
	m.editorDo(editor.Action(action), map[string]any{"value": !cur})
	return nil, true
}

// askNumber opens the prompt for the number a view command takes as its argument.
func (m *Model) askNumber(action, key, label string, hi int) {
	m.pushOverlay(lineOverlay{l: prompt.NewLine(m.styles, prompt.LineOptions{
		Label: label,
		Tag:   viewTag{action: action, key: key},
		Validate: func(text string) error {
			n, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil || n < 1 || n > hi {
				return fmt.Errorf("enter a whole number from 1 to %d", hi)
			}
			return nil
		},
	})})
}

// askText opens the prompt for the name a view command takes as its argument.
func (m *Model) askText(action, key, label string) {
	m.pushOverlay(lineOverlay{l: prompt.NewLine(m.styles, prompt.LineOptions{
		Label: label,
		Tag:   viewTag{action: action, key: key, text: true},
	})})
}

// viewAnswer runs the view command of a prompt that was submitted. A prompt that was
// closed does nothing.
func (m *Model) viewAnswer(tag viewTag, d prompt.Done) tea.Cmd {
	if d.Outcome != prompt.Submitted {
		return nil
	}
	text := strings.TrimSpace(d.Text)
	var value any = text
	if !tag.text {
		n, err := strconv.Atoi(text)
		if err != nil {
			m.notice = "Not a number: " + text
			return nil
		}
		value = n
	}
	return m.dispatch(Command{Action: tag.action, Args: map[string]any{tag.key: value}})
}

// setCursorStyle sets the caret shape, and reports a name that is not one.
func (m *Model) setCursorStyle(s string) {
	if !slices.Contains([]string{cursorBlock, cursorBar, cursorUnderline}, s) {
		m.notice = "Cursor style must be block, bar or underline, not " + s
		return
	}
	m.view.cursor = s
	m.notice = "Cursor style: " + s
}

// openCursorStyles lists the caret shapes. The current one is marked.
func (m *Model) openCursorStyles() {
	names := []string{cursorBlock, cursorBar, cursorUnderline}
	rows := make([]listRow, len(names))
	for i, n := range names {
		right := ""
		if n == m.view.cursor {
			right = "current"
		}
		rows[i] = listRow{left: n, right: right, value: n}
	}
	v := newListView("Cursor style", rows, m.listChrome(), func(v *listView, r listRow) tea.Cmd {
		v.active = false
		m.setCursorStyle(r.value)
		return nil
	})
	m.pushOverlay(listOverlay{v: v})
}

// openThemes lists the bundled themes. The current one is marked, and choosing a row
// applies it.
func (m *Model) openThemes() {
	names := theme.Names()
	rows := make([]listRow, len(names))
	for i, n := range names {
		right := ""
		if n == m.util.settings.Theme {
			right = "current"
		}
		rows[i] = listRow{left: n, right: right, value: n}
	}
	v := newListView("Theme", rows, m.listChrome(), func(v *listView, r listRow) tea.Cmd {
		v.active = false
		m.applyTheme(r.value)
		return nil
	})
	m.pushOverlay(listOverlay{v: v})
}

// applyTheme makes the bundled theme called name the theme of the frame, at once. Every
// style that the root built from the theme is built again. A theme that does not load
// keeps the current one.
func (m *Model) applyTheme(name string) {
	th, err := theme.Load(name)
	if err != nil {
		m.notice = "Theme " + name + ": " + err.Error()
		return
	}
	st := style.New(th)
	m.theme = th
	m.styles = st
	m.panes = editorpane.NewStyles(th)
	m.menu = menubar.New(m, st)
	if m.tree != nil {
		m.explorerPanel = expanel.New(m.tree, st, m.now).CarryCut(m.explorerPanel)
	}
	m.util.settings.Theme = name
	m.notice = "Theme: " + name
}

// openLocales lists the locales built in. English is the only one, so the list has
// one row, and choosing it says that the others are not built in.
func (m *Model) openLocales() {
	rows := []listRow{{left: "English", right: "current", value: "en"}}
	v := newListView("Locale", rows, m.listChrome(), func(v *listView, r listRow) tea.Cmd {
		v.active = false
		m.notice = "Other locales are not built in: English is the only locale"
		return nil
	})
	m.pushOverlay(listOverlay{v: v})
}

// viewLayout sets the chrome rows and the gutter of the frame from the view state. It
// runs after the layout input is gathered, so the rows it sets win.
func (m *Model) viewLayout(in *layoutInput) {
	in.hideTabBar = !m.view.tabBar
	in.hideStatus = in.hideStatus || !m.view.statusBar
	in.showPromptLine = m.view.promptLine
	in.hideVScroll = !m.view.vScroll
	in.iconMode = m.view.icons
	if doc := m.ed.Active(); doc != nil {
		// The gutter of the layout is the focused pane's, so the layout follows the
		// active document.
		in.hideLineNumbers = !editor.OverrideOr(docViewOf(doc).LineNumbers, !m.hideLineNumbers)
	}
}

// viewPane sets the display options of p from the settings of its document and the
// view state. Nothing else of the pane changes.
func (m *Model) viewPane(p editorpane.Pane, doc Document) editorpane.Pane {
	s := docViewOf(doc)
	p.Wrap = editor.OverrideOr(s.LineWrap, m.view.lineWrap)
	p.HideGutter = !editor.OverrideOr(s.LineNumbers, !m.hideLineNumbers)
	p.CurrentLine = editor.OverrideOr(s.CurrentLine, m.view.currentLine)
	p.Occurrences = editor.OverrideOr(s.Occurrences, m.view.occurrences)
	p.VisibleWhitespace = editor.OverrideOr(s.Whitespace, m.view.whitespace)
	p.IndentGuides = s.IndentGuidesOn()
	p.HideFoldMarkers = !s.FoldIndicatorsOn()
	p.Icons = m.view.icons
	p.HideTabArrows = !s.TabIndicatorsOn()
	p.HideVerticalScrollbar = !m.view.vScroll
	p.HideHorizontalScrollbar = !m.view.hScroll
	p.CaretShape = m.caretShape()
	p.Rulers = s.Rulers
	if s.TabSize > 0 {
		p.TabSize = s.TabSize
	}
	return p
}

// caretShape is the caret shape of the pane for the cursor style.
func (m *Model) caretShape() editorpane.CaretShape {
	switch m.view.cursor {
	case cursorBar:
		return editorpane.CaretBar
	case cursorUnderline:
		return editorpane.CaretUnderline
	}
	return editorpane.CaretBlock
}

// cursorShape is the terminal caret shape for the cursor style.
func (m *Model) cursorShape() tea.CursorShape {
	switch m.view.cursor {
	case cursorBar:
		return tea.CursorBar
	case cursorUnderline:
		return tea.CursorUnderline
	}
	return tea.CursorBlock
}

// viewTerminal sets the terminal settings of the frame: the mouse mode, and the shape of
// the terminal caret.
func (m *Model) viewTerminal(v *tea.View) {
	if !m.view.mouseCapture {
		v.MouseMode = tea.MouseModeNone
	}
	if v.Cursor != nil {
		v.Cursor.Shape = m.cursorShape()
	}
}

// viewStatus replaces the status message with the highlight ranges of the caret line
// while the highlight debug is on.
func (m *Model) viewStatus(s *statusView) {
	// The language a set_language command named is shown, in place of the one the
	// file name gives.
	if lang, ok := m.ed.Active().(interface{ Language() string }); ok && lang.Language() != "" {
		s.language = lang.Language()
	}
	if m.view.debug {
		s.message = m.debugText()
	}
}

// debugText lists the highlight ranges of the caret line: the find matches, the
// diagnostics and the selection, in columns.
func (m *Model) debugText() string {
	doc := m.ed.Active()
	if doc == nil {
		return "hl: no document"
	}
	line := doc.Cursor().Line
	var parts []string
	if hl := m.findLineHighlights(); hl != nil {
		for _, r := range hl(line) {
			parts = append(parts, fmt.Sprintf("find %d-%d", r.Start, r.End))
		}
	}
	if spans := m.lspSpans(doc); spans != nil {
		for _, s := range spans(line) {
			parts = append(parts, fmt.Sprintf("diag %d-%d", s.Start, s.End))
		}
	}
	if a, h, ok := doc.Selection(); ok {
		parts = append(parts, fmt.Sprintf("sel %d:%d-%d:%d", a.Line+1, a.Col+1, h.Line+1, h.Col+1))
	}
	if len(parts) == 0 {
		parts = []string{"none"}
	}
	return fmt.Sprintf("hl Ln %d: %s", line+1, strings.Join(parts, ", "))
}

// viewHolds reports the checked state of a menu row that a view setting drives. The
// second result is false for a name the view state does not drive.
func (m *Model) viewHolds(name string) (bool, bool) {
	switch name {
	case "line_numbers":
		return !m.hideLineNumbers, true
	case "line_wrap":
		return m.view.lineWrap, true
	case "vertical_scrollbar":
		return m.view.vScroll, true
	case "horizontal_scrollbar":
		return m.view.hScroll, true
	case "mouse_capture":
		return m.view.mouseCapture, true
	case "mouse_hover":
		return m.view.mouseHover, true
	case "scroll_sync":
		return m.view.scrollSync, true
	}
	return false, false
}

// viewOptionsOf returns the options o, or the defaults when o is nil.
func viewOptionsOf(o *ViewOptions) ViewOptions {
	if o == nil {
		return DefaultViewOptions()
	}
	return *o
}
