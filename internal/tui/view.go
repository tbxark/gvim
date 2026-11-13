package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/menubar"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/split"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
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
	// ImageProtocol is image_protocol: "auto" probes the terminal when the first
	// image tab opens, "kitty" trusts it, and "off" never draws an image. Empty
	// reads as auto.
	ImageProtocol string

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
	IndentationGuideMode       string // editor.indentation_guide: "none", "all" or "active"; empty reads as none
	IndentationGuideGlyph      string // editor.indentation_guide_glyph; empty reads as "▏"
	RainbowIndentation         bool   // editor.rainbow_indentation, default false
	Whitespace                 WhitespaceOptions
	HighlightMatchingBrackets  bool // editor.highlight_matching_brackets, default true
	RainbowBrackets            bool // editor.rainbow_brackets, default true
	Search                     SearchOptions
	StatusBar                  StatusBarOptions
	// Explorer is the file_explorer.* navigation and display settings (width,
	// side, follow-active-buffer, compact directories, auto-open on the last
	// buffer close). Zero keeps gvim's automatic width and the left side; the app
	// fills it from the config, whose defaults are Fresh's.
	Explorer          explorer.Options
	MenuBarMnemonics  bool // editor.menu_bar_mnemonics, default true
	SmoothScroll      bool // editor.smooth_scroll, default true
	UseTerminalBG     bool // editor.use_terminal_bg, default false
	SetWindowTitle    bool // editor.set_window_title, default true
	TerminalAutoTitle bool // editor.terminal_auto_title, default true
	// editor.animations, editor.cursor_jump_animation, editor.viewport_edge_fade
	// and editor.screensaver_* are not ViewOptions: gvim's cell-grid TUI draws no
	// animations, fades or screensaver, so the config keys are accepted and
	// parsed (config/editoropts.go) but not carried into the UI. The decision is
	// recorded in docs/parity.md; the keys load without diagnostics.

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
// such as "{cursor}" and "{language}", and the separator between neighbours. A nil list
// (the zero value) means Fresh's default list for that side; a non-nil empty list paints
// nothing, so a config can remove a whole group.
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
		Explorer:                  explorer.DefaultOptions(),
		MenuBarMnemonics:          true,
		SmoothScroll:              true,
		SetWindowTitle:            true,
		TerminalAutoTitle:         true,
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
	// completionAutoSeq is the generation of the pending auto-popup debounce: a
	// scheduled trigger fires only while it is still the newest one. Bumping it
	// cancels the pending trigger (a non-word character, Esc, closing the list).
	completionAutoSeq uint64
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
		return m.languageAction(args)
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
			m.notice = uiText("gvim.notice.mouse.capture_on", "Mouse capture on: gvim takes the mouse")
		} else {
			m.notice = uiText("gvim.notice.mouse.capture_off", "Mouse capture off: the terminal keeps the mouse")
		}
	case "toggle_file_explorer_side":
		m.explorerSide = m.explorerSide.Other()
		if m.explorerSide == explorer.SideRight {
			m.notice = uiText("gvim.notice.explorer.right", "File explorer moved to the right")
		} else {
			m.notice = uiText("gvim.notice.explorer.left", "File explorer moved to the left")
		}
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
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Label: label,
		Tag:   viewTag{action: action, key: key},
		Validate: func(text string) error {
			n, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil || n < 1 || n > hi {
				return fmt.Errorf("enter a whole number from 1 to %d", hi)
			}
			return nil
		},
	}))
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
	rows := make([]list.Row, len(names))
	for i, n := range names {
		right := ""
		if n == m.view.cursor {
			right = "current"
		}
		rows[i] = list.Row{Left: n, Right: right, Value: n}
	}
	v := list.New("Cursor style", rows, m.listChrome(), func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		m.setCursorStyle(r.Value)
		return nil
	})
	m.pushOverlay(v)
}

// themeInfos returns the available themes. Without a catalog only the bundled
// themes are offered, which is the behavior tests without an app get.
func (m *Model) themeInfos() []ThemeInfo {
	if m.util.themeCatalog != nil {
		return m.util.themeCatalog.Infos()
	}
	names := theme.Names()
	infos := make([]ThemeInfo, len(names))
	for i, name := range names {
		infos[i] = ThemeInfo{Name: name, Ref: name, Builtin: true}
	}
	return infos
}

// themeChoices returns the catalog's themes in menubar form, for the Copy with
// Formatting submenu.
func (m *Model) themeChoices() []menubar.ThemeChoice {
	infos := m.themeInfos()
	choices := make([]menubar.ThemeChoice, len(infos))
	for i, info := range infos {
		choices[i] = menubar.ThemeChoice{Name: info.Name, Ref: info.Ref}
	}
	return choices
}

// currentThemeRef returns the config value of the active theme.
func (m *Model) currentThemeRef() string {
	if m.themeRef != "" {
		return m.themeRef
	}
	return m.util.settings.Theme
}

// loadTheme resolves a config theme value through the catalog. Without a
// catalog it loads a bundled theme by name.
func (m *Model) loadTheme(ref string) (theme.Theme, string, error) {
	if ref == "" {
		ref = "dark"
	}
	if m.util.themeCatalog == nil {
		th, err := theme.Load(ref)
		return th, ref, err
	}
	th, err := m.util.themeCatalog.Load(ref)
	if err != nil {
		return theme.Theme{}, "", err
	}
	canonical, err := m.util.themeCatalog.Canonical(ref)
	if err != nil {
		canonical = ref
	}
	return th, canonical, nil
}

// openThemes lists the available themes: the bundled ones and the user themes.
// The current one is marked, and choosing a row applies it.
func (m *Model) openThemes() {
	infos := m.themeInfos()
	rows := make([]list.Row, len(infos))
	current := m.currentThemeRef()
	for i, info := range infos {
		right := "user"
		if info.Builtin {
			right = "builtin"
		}
		if info.Ref == current || info.Name == m.util.settings.Theme {
			right = "current"
		}
		rows[i] = list.Row{Left: info.Name, Right: right, Value: info.Ref}
	}
	v := list.New("Theme", rows, m.listChrome(), func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		m.applyTheme(r.Value)
		return nil
	})
	m.pushOverlay(v)
}

// applyTheme makes the theme a config value names the theme of the frame, at
// once: the catalog resolves it (so a theme file edited on disk is read again
// after a Reload), every style the root built from it is rebuilt, and the
// theme_changed event fires. A theme that does not load keeps the current one.
func (m *Model) applyTheme(ref string) {
	th, canonical, err := m.loadTheme(ref)
	if err != nil {
		m.notice = "Theme " + ref + ": " + err.Error()
		return
	}
	m.theme = th
	m.themeRef = canonical
	// A theme change clears the plugin session overrides, as Fresh does.
	m.themeOverrides = map[string]string{}
	m.rebuildTheme()
	m.util.settings.Theme = th.Name
	m.notice = "Theme: " + th.Name
}

// effectiveTheme returns the active theme with the session color overrides
// layered over it. It is what the plugin reads report and what the painting
// resolves, without mutating m.theme.
func (m *Model) effectiveTheme() theme.Theme {
	if len(m.themeOverrides) == 0 {
		return m.theme
	}
	over, err := theme.WithOverrides(m.theme, m.themeOverrides)
	if err != nil {
		return m.theme
	}
	return over
}

// rebuildTheme rebuilds the styles and tells the plugins the theme changed (theme_changed).
func (m *Model) rebuildTheme() {
	m.rebuildStyles()
	m.plugThemeChanged(m.theme.Name, m.theme.Dark())
}

// rebuildStyles rebuilds every style the root keeps from the active theme,
// layered under the session color overrides (plugin gvim.theme.override and the
// theme editor's live preview). It is the one place theme colors become styles,
// and it raises no event: a transient preview uses it directly.
func (m *Model) rebuildStyles() {
	effective := m.theme
	if len(m.themeOverrides) > 0 {
		over, err := theme.WithOverrides(m.theme, m.themeOverrides)
		if err != nil {
			m.notice = "Theme override: " + err.Error()
		} else {
			effective = over
		}
	}
	st := style.New(effective)
	m.styles = st
	m.panes = editorpane.NewStyles(effective)
	m.themeGen++
	m.menu = menubar.New(m, st).WithMnemonics(m.view.opts.MenuBarMnemonics).WithThemeChoices(m.themeChoices())
	if m.tree != nil {
		m.explorerPanel = expanel.New(m.tree, st, m.now).CarryCut(m.explorerPanel)
	}
}

// SetThemeOverrides replaces the session color overrides (the plugin
// gvim.theme.override map) and rebuilds the theme. An empty map clears them.
func (m *Model) SetThemeOverrides(overrides map[string]string) error {
	copied := make(map[string]string, len(overrides))
	for key, value := range overrides {
		copied[key] = value
	}
	// Validate against the active theme before touching the frame, so a bad
	// override is reported to the caller and the look stays as it was.
	if _, err := theme.WithOverrides(m.theme, copied); err != nil {
		return err
	}
	m.themeOverrides = copied
	m.rebuildTheme()
	return nil
}

// ThemeOverrides returns the current session override map.
func (m *Model) ThemeOverrides() map[string]string {
	out := make(map[string]string, len(m.themeOverrides))
	for key, value := range m.themeOverrides {
		out[key] = value
	}
	return out
}

// ClearThemeOverrides drops the session color overrides and rebuilds the theme.
func (m *Model) ClearThemeOverrides() {
	if err := m.SetThemeOverrides(nil); err != nil {
		m.notice = "Theme override: " + err.Error()
	}
}

// openLocales lists the locales built in: English and Simplified Chinese. Choosing
// one applies it immediately.
func (m *Model) openLocales() {
	rows := localeRows(i18n.Locale(), i18n.Locales())
	v := list.New("Locale", rows, m.listChrome(), func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		return m.setLocale(r.Value)
	})
	m.pushOverlay(v)
}

// localeRows lists the built-in languages, marking code as current.
func localeRows(code string, locales []i18n.Language) []list.Row {
	rows := make([]list.Row, 0, len(locales))
	for _, lang := range locales {
		right := ""
		if lang.Code == code {
			right = "current"
		}
		rows = append(rows, list.Row{Left: lang.English, Right: right, Value: lang.Code})
	}
	return rows
}

// setLocale persists a locale choice through the Settings port, whose apply step
// pushes the resolved locale into the i18n package and the plugin runtime. Without
// a port the choice applies to this session only.
func (m *Model) setLocale(code string) tea.Cmd {
	lang, ok := i18n.Find(code)
	if !ok {
		m.notice = "Locale: " + code + " is not built in"
		return nil
	}
	m.notice = "Locale: " + lang.English
	if m.util.settingsPort == nil {
		i18n.SetLocale(lang.Code)
		return nil
	}
	port := m.util.settingsPort
	return func() tea.Msg {
		change, err := port.Save([]SettingEdit{{Key: "locale", Value: lang.Code}}, SettingsLayerUser)
		return settingsChangeMsg{change: change, err: err}
	}
}

// viewLayout sets the chrome rows and the gutter of the frame from the view state. It
// runs after the layout input is gathered, so the rows it sets win.
func (m *Model) viewLayout(in *frame.Input) {
	in.HideTabBar = !m.view.tabBar
	in.HideStatus = in.HideStatus || !m.view.statusBar
	// The prompt row is reserved only while a prompt is open: with no prompt the
	// status row is the last row of the frame, so the bar keeps the same air above
	// and below it. The setting says whether an open prompt gets the row of its own.
	in.ShowPromptLine = m.view.promptLine && m.promptOpen()
	in.HideVScroll = !m.view.vScroll
	in.IconMode = m.view.icons
	if doc := m.ed.Active(); doc != nil {
		// The gutter of the layout is the focused pane's, so the layout follows the
		// active document.
		in.HideLineNumbers = !editor.OverrideOr(docViewOf(doc).LineNumbers, !m.hideLineNumbers)
	}
}

// promptOpen reports whether the top overlay is one of the bottom-row prompts: the
// built-in one-line prompt or confirm, or a plugin prompt that draws on the same row.
// Only then is the prompt row reserved.
func (m *Model) promptOpen() bool {
	if len(m.overlays) == 0 {
		return false
	}
	_, ok := m.overlays[len(m.overlays)-1].(prompt.RowOwner)
	return ok
}

// viewPane sets the display options of p, the editor pane of the split pane sp, from
// the settings of its document and the view state. Nothing else of the pane changes.
// The page view comes from the document, unless the dual view gives the pane a side
// of its own (panePageView).
func (m *Model) viewPane(sp split.Pane, p editorpane.Pane, doc Document) editorpane.Pane {
	s := docViewOf(doc)
	p.PageView = m.panePageView(sp, doc)
	if !sp.Focused && m.validDual() != nil {
		// The unfocused side of a dual view does not own the document caret, so
		// its composed rows stay concealed.
		p.PageView.NoCaretReveal = true
	}
	if p.PageView.On && p.PageView.Markdown {
		// The Markdown table's top and bottom borders and the wrapped-cell rows are
		// display rows no source line owns; the shared inserted-row hook adds them.
		p.PageExtraRows = editorpane.PageViewTableBorders(doc, p.Styles)
		p.TrackMarks = m.markdownHeadingMarks(doc, p)
	}
	// A large document wraps as the setting says: a line of more than editorpane.LongLineRunes
	// is laid out in segments, a few of them at a time, so its length costs nothing.
	p.Wrap = editor.OverrideOr(s.LineWrap, m.view.lineWrap)
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
	p.InlineDiagnostics = m.view.opts.DiagnosticsInlineText
	p.CaretShape = m.caretShape()
	// The rulers are the buffer's own. Each buffer starts with the configured ones
	// (editor.SetRulers), so one can be removed from a buffer.
	p.Rulers = slices.Clone(s.Rulers)
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

// pageViewFor returns the page view of doc. It is on when the buffer's override
// says so, or else the language's page_view (which the config layers resolve).
// The measure is the buffer's page width, else the language's, else
// editor.page_width; an unset or too wide width fills the text area. A
// large-file document falls back to the normal pane, and a non-Markdown document
// gets the margins without the inline compose.
func (m *Model) pageViewFor(doc Document) editorpane.PageView {
	if doc == nil {
		return editorpane.PageView{}
	}
	s := docViewOf(doc)
	on, ok := s.PageViewOverride()
	if !ok {
		on = m.docLanguageSettings(doc).PageView
	}
	if !on {
		return editorpane.PageView{}
	}
	if large, ok := doc.(LargeDoc); ok && large.Large() {
		return editorpane.PageView{}
	}
	measure := s.PageWidth
	if measure <= 0 {
		measure = m.docLanguageSettings(doc).PageWidth
	}
	return editorpane.PageView{On: true, Measure: measure, Markdown: m.markdownDoc(doc)}
}

// pageViewActive reports whether the focused pane is showing the page view. The
// virtual space and the virtual lines are off there: the page wraps its text, so
// there is no empty region past a line. In the dual view this follows the focused
// side, not the document.
func (m *Model) pageViewActive() bool {
	doc := m.ed.Active()
	if doc == nil {
		return false
	}
	return m.panePageView(m.layout().FocusedPane(), doc).On
}

// markdownDoc reports whether doc gets the Markdown compose: its path has a
// Markdown suffix, or the detected language is markdown.
func (m *Model) markdownDoc(doc Document) bool {
	if p, ok := doc.(pathEditor); ok {
		switch path := strings.ToLower(p.FilePath()); {
		case strings.HasSuffix(path, ".md"), strings.HasSuffix(path, ".markdown"), strings.HasSuffix(path, ".mdx"):
			return true
		}
	}
	if l, ok := doc.(interface{ Language() string }); ok {
		switch strings.ToLower(l.Language()) {
		case "markdown", "multimarkdown", "mdx":
			return true
		}
	}
	switch strings.ToLower(m.languageFor(doc.Name())) {
	case "markdown", "multimarkdown", "mdx":
		return true
	}
	return false
}

// headingMarksKey is the layout memo key of the page view's heading scrollbar
// marks, kept until the document's text changes.
type headingMarksKey struct{}

// markdownHeadingMarks returns the scrollbar track marks of the document's ATX
// heading lines, colored like a heading. The page view shows them on the
// scrollbar so the headings' places in the file stay visible.
func (m *Model) markdownHeadingMarks(doc Document, p editorpane.Pane) []editorpane.TrackMark {
	if doc == nil {
		return nil
	}
	compute := func() any {
		style := p.Styles.Syntax[syntax.KindHeading]
		var marks []editorpane.TrackMark
		for i := 0; i < doc.LineCount(); i++ {
			if !headingLine(doc.Line(i)) {
				continue
			}
			marks = append(marks, editorpane.TrackMark{From: i, To: i, Style: style})
		}
		return marks
	}
	if lm, ok := doc.(editorpane.LayoutMemo); ok {
		return lm.MemoValue(headingMarksKey{}, compute).([]editorpane.TrackMark)
	}
	return compute().([]editorpane.TrackMark)
}

// headingLine reports whether s is an ATX heading line ("#" to "######" after at
// most three blanks, then a blank or the end).
func headingLine(s string) bool {
	trimmed := strings.TrimLeft(s, " ")
	if len(s)-len(trimmed) > 3 {
		return false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return false
	}
	return n == len(trimmed) || trimmed[n] == ' ' || trimmed[n] == '\t'
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
func (m *Model) viewStatus(s *frame.StatusView) {
	// The language a set_language command named is shown, in place of the one the
	// file name gives.
	if lang, ok := m.ed.Active().(interface{ Language() string }); ok && lang.Language() != "" {
		s.Language = languageLabel(lang.Language())
	}
	if m.view.debug {
		s.Message = m.debugText()
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
