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

	// The fields below are the Fresh editor.* display options. The app fills them from
	// the config, whose defaults are Fresh's, and DefaultViewOptions returns the same
	// defaults; a test that sets one should start from DefaultViewOptions. None of them
	// is read by the painting yet unless the contract says so. The ones a language can
	// override (line wrap, wrap column, page view, page width, tab marks, indentation
	// guides) are in the per-document editor.LanguageSettings, not here.

	RelativeLineNumbers        bool   // editor.relative_line_numbers, default false
	HideCurrentLineOnSelection bool   // editor.hide_current_line_on_selection, default false
	HighlightCurrentColumn     bool   // editor.highlight_current_column, default false
	WrapIndent                 bool   // editor.wrap_indent, default true
	SyntaxHighlighting         bool   // editor.syntax_highlighting, default true
	ShowTilde                  bool   // editor.show_tilde, default true
	Rulers                     []int  // editor.rulers: 1-based columns, default none
	IndentationGuideMode       string // editor.indentation_guide: "none", "all" or "active"; empty reads as none
	IndentationGuideGlyph      string // editor.indentation_guide_glyph; empty reads as "▏"
	RainbowIndentation         bool   // editor.rainbow_indentation, default false
	Whitespace                 WhitespaceOptions
	HighlightMatchingBrackets  bool // editor.highlight_matching_brackets, default true
	RainbowBrackets            bool // editor.rainbow_brackets, default true
	Search                     SearchOptions
	StatusBar                  StatusBarOptions
	MenuBarMnemonics           bool // editor.menu_bar_mnemonics, default true
	Animations                 bool // editor.animations, default true
	CursorJumpAnimation        bool // editor.cursor_jump_animation, default true
	ViewportEdgeFade           bool // editor.viewport_edge_fade, default true
	SmoothScroll               bool // editor.smooth_scroll, default true
	UseTerminalBG              bool // editor.use_terminal_bg, default false
	SetWindowTitle             bool // editor.set_window_title, default true
	TerminalAutoTitle          bool // editor.terminal_auto_title, default true
	ScreensaverEnabled         bool // editor.screensaver_enabled, default false
	ScreensaverIdleMinutes     int  // editor.screensaver_idle_minutes, default 5

	// Mouse. Zero reads as the default.

	MouseWheelScrollLines int // editor.mouse_wheel_scroll_lines, default 3 (the wheelRows constant today)
	DoubleClickTimeMS     int // editor.double_click_time_ms, default 500 (doubleClickWindow today)
	MouseHoverDelayMS     int // editor.mouse_hover_delay_ms, default 500

	// Language-server display.

	EnableInlayHints           bool // editor.enable_inlay_hints, default true
	EnableSemanticTokensFull   bool // editor.enable_semantic_tokens_full, default false
	DiagnosticsInlineText      bool // editor.diagnostics_inline_text, default false
	CompletionPopupAutoShow    bool // editor.completion_popup_auto_show, default false
	QuickSuggestions           bool // editor.quick_suggestions, default true
	QuickSuggestionsDelayMS    int  // editor.quick_suggestions_delay_ms, default 150
	SuggestOnTriggerCharacters bool // editor.suggest_on_trigger_characters, default true
}

// WhitespaceOptions are the editor.whitespace_* options that are not per language.
// ShowWhitespace on ViewOptions is editor.whitespace_show, the master switch. The tab
// flags are per language and are in editor.LanguageSettings.
type WhitespaceOptions struct {
	SpacesLeading  bool // default false
	SpacesInner    bool // default false
	SpacesTrailing bool // default false
	Newlines       bool // default false
	CarriageReturn bool // default false
	InSelection    bool // marks show inside a selection even when hidden elsewhere, default true
}

// SearchOptions are editor.search: the starting state of the find bar.
type SearchOptions struct {
	CaseSensitive bool
	WholeWord     bool
	Regex         bool
	ConfirmEach   bool // ask before each replacement
}

// StatusBarOptions are editor.status_bar: the status row elements as Fresh names them,
// such as "{cursor}" and "{language}". Empty lists mean gvim's own layout.
type StatusBarOptions struct {
	Left      []string
	Right     []string
	Separator string
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

		WrapIndent:                true,
		SyntaxHighlighting:        true,
		ShowTilde:                 true,
		IndentationGuideMode:      "none",
		IndentationGuideGlyph:     "\u258f",
		Whitespace:                WhitespaceOptions{InSelection: true},
		HighlightMatchingBrackets: true,
		RainbowBrackets:           true,
		MenuBarMnemonics:          true,
		Animations:                true,
		CursorJumpAnimation:       true,
		ViewportEdgeFade:          true,
		SmoothScroll:              true,
		SetWindowTitle:            true,
		TerminalAutoTitle:         true,
		ScreensaverIdleMinutes:    5,
		MouseWheelScrollLines:     3,
		DoubleClickTimeMS:         500,
		MouseHoverDelayMS:         500,
		EnableInlayHints:          true,
		QuickSuggestions:          true,
		QuickSuggestionsDelayMS:   150,

		SuggestOnTriggerCharacters: true,
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
	// opts are the starting options, kept whole so that the Fresh display options
	// above the toggles are readable by the painting (m.view.opts.ShowTilde, ...).
	opts ViewOptions
	// languages resolves the per-language settings of a document. Nil means none.
	languages editor.SettingsResolver
}

// newViewState returns the state for the starting options o.
func newViewState(o ViewOptions) viewState {
	cursor := o.CursorStyle
	if cursor == "" {
		cursor = cursorBlock
	}
	mode, _ := icons.Parse(o.UIIcons) // an unknown name is plain
	return viewState{
		opts:         o,
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
		if doc := m.ed.Active(); doc != nil && isLargeDoc(doc) {
			// The pane never wraps a large document (see editorpane wrapOn).
			m.notice += " (not available in large file mode)"
		}
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
	case "toggle_indentation_guide_current_buffer":
		// The toggle flips what is drawn, which the mode, the buffer and the language
		// decide together, not the buffer's own override.
		return m.bufferFlag(action, args, m.guidesDrawn())
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

// guidesDrawn reports whether the active document draws indentation guides.
func (m *Model) guidesDrawn() bool {
	doc := m.ed.Active()
	if doc == nil {
		return false
	}
	ls := m.docLanguageSettings(doc)
	mode := editor.ResolveIndentGuide(m.view.opts.IndentationGuideMode, m.docViewSettings().IndentGuides, ls.IndentationGuide)
	return mode != editor.GuidesNone
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
	m.plugThemeChanged(name)
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
	// A large document is never wrapped: a wrapped line of a megabyte would be laid out
	// in full to find its rows.
	p.Wrap = editor.OverrideOr(s.LineWrap, m.view.lineWrap) && !isLargeDoc(doc)
	p.HideGutter = !editor.OverrideOr(s.LineNumbers, !m.hideLineNumbers)
	p.CurrentLine = editor.OverrideOr(s.CurrentLine, m.view.currentLine)
	p.Occurrences = editor.OverrideOr(s.Occurrences, m.view.occurrences) && !m.occurrencesOff(doc)
	p.VisibleWhitespace = editor.OverrideOr(s.Whitespace, m.view.whitespace)
	p.IndentGuides = s.IndentGuidesOn()
	p.HideFoldMarkers = !s.FoldIndicatorsOn()
	p.Icons = m.view.icons
	p.HideTabArrows = !s.TabIndicatorsOn()
	p.HideVerticalScrollbar = !m.view.vScroll
	p.HideHorizontalScrollbar = !m.view.hScroll
	p.CaretShape = m.caretShape()
	p.Rulers = s.Rulers
	// The tab width is the language's (which holds the global tab_size where the
	// language sets none), unless the buffer set its own.
	if n := m.docLanguageSettings(doc).TabSize; n > 0 {
		p.TabSize = n
	}
	if s.TabSize > 0 {
		p.TabSize = s.TabSize
	}
	return m.viewRender(p, doc, s)
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

// languageSettings returns the effective settings of a document with the given language
// name and file path: Deps.Languages applied, or the Fresh defaults when it is nil. The
// document's own ViewSettings are not applied; read them with docViewSettings.
func (m *Model) languageSettings(language, path string) editor.LanguageSettings {
	if m.view.languages != nil {
		return m.view.languages(language, path)
	}
	return editor.LanguageSettings{
		TabSize:                m.tabSize,
		LineWrap:               m.view.lineWrap,
		PageWidth:              editor.DefaultPageWidth,
		WhitespaceTabsLeading:  true,
		WhitespaceTabsInner:    true,
		WhitespaceTabsTrailing: true,
		IndentationGuide:       true,
	}
}

// viewOptionsOf returns the options o, or the defaults when o is nil.
func viewOptionsOf(o *ViewOptions) ViewOptions {
	if o == nil {
		return DefaultViewOptions()
	}
	return *o
}
