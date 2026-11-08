package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/space"
)

// Settings is the effective configuration that the read-only Settings list shows
// when no SettingsPort is wired. The app fills it from the config snapshot.
// Keymap is the name of the active keymap, and ConfigFile is the user config
// file. An empty ConfigFile means there is no such file to open.
//
// The editable page (settingsPage) does not use this struct: it reads the plain
// rows of SettingsSnapshot. Settings stays because gvim.config.get and the dump
// command read it.
type Settings struct {
	Theme               string
	Keymap              string
	TabSize             int
	InsertSpaces        bool
	AutoIndent          bool
	LineNumbers         bool
	MenuBar             bool
	ExplorerShowHidden  bool
	ExplorerShowIgnored bool
	UIIcons             string // the ui_icons setting: "auto", "plain" or "nerd"; empty reads as auto
	UIIconsFont         string // the ui_icons_font setting
	IconsMode           string // the icon set in use, "plain" or "nerd", after "auto" is resolved
	IconsWhy            string // why that set: the setting, the font or the terminal
	ConfigFile          string

	// Read by gvim.config.get only; the Settings list does not show them.
	LineEnding             string // line_ending: "auto", "lf", "crlf" or "cr"
	TrimTrailingWhitespace bool
	EnsureFinalNewline     bool
}

// settingsRows returns one row per setting for the read-only list, named as the
// config file names it. An unset text value shows as "(none)".
func settingsRows(s Settings) []list.Row {
	return []list.Row{
		{Left: "theme", Right: orNone(s.Theme)},
		{Left: "keymap", Right: orNone(s.Keymap)},
		{Left: "tab_size", Right: strconv.Itoa(s.TabSize)},
		{Left: "insert_spaces", Right: onOff(s.InsertSpaces)},
		{Left: "auto_indent", Right: onOff(s.AutoIndent)},
		{Left: "line_numbers", Right: onOff(s.LineNumbers)},
		{Left: "show_menu_bar", Right: onOff(s.MenuBar)},
		{Left: "explorer_show_hidden", Right: onOff(s.ExplorerShowHidden)},
		{Left: "explorer_show_ignored", Right: onOff(s.ExplorerShowIgnored)},
		{Left: settingsIconsKey, Right: iconsSettingText(s, iconModeOf(s))},
		{Left: "ui_icons_font", Right: orNone(s.UIIconsFont)},
		{Left: "config_file", Right: orNone(s.ConfigFile)},
	}
}

// settingsIconsKey is the row of the icon setting.
const settingsIconsKey = "ui_icons"

// iconModeOf is the icon set in use. An empty or unknown name is plain.
func iconModeOf(s Settings) icons.Mode {
	mode, _ := icons.Parse(s.IconsMode)
	return mode
}

// orNone is s, or "(none)" when s is empty.
func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// onOff is "on" or "off".
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// openSettings opens the Settings page: the editable page over the SettingsPort
// when the app wired one, and the read-only list that opens the config file when
// it did not.
func (m *Model) openSettings() {
	if m.util.settingsPort == nil {
		m.openReadOnlySettings()
		return
	}
	m.pushOverlay(newSettingsPage(m, m.util.settingsPort))
}

// openReadOnlySettings shows the effective settings without a way to change
// them. Choosing a row, or pressing Enter, opens the user config file; the
// ui_icons row opens the icon preview instead.
func (m *Model) openReadOnlySettings() {
	v := list.New("Settings", settingsRows(m.util.settings), m.listChrome(), func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		if r.Left == settingsIconsKey {
			// The list is still on the stack while it runs, so the preview opens from
			// a message, after the stack has popped the list.
			return func() tea.Msg { return openIconPreviewMsg{} }
		}
		m.openConfigFile()
		return nil
	})
	m.pushOverlay(v)
}

// openConfigFile opens the user config file in a tab of the editor, and moves the
// focus there. A new file opens empty, so saving it creates the file. With no
// config file the status row says so.
func (m *Model) openConfigFile() {
	path := m.util.settings.ConfigFile
	if path == "" {
		m.notice = "No user config file to open"
		return
	}
	m.openFile(path, false)
	m.focusEditor()
}

// settingsPage is the editable Settings overlay. It lists the plain rows of a
// SettingsSnapshot by section, filters them as the user types, and edits one
// value at a time: a bool toggles in place, an enum opens a picker, an int or a
// string opens a one-line prompt. The edits are held as pending values until
// Ctrl+S saves them to the chosen layer (User by default, Project for a trusted
// workspace), which the app writes with the config schema and reloads. Ctrl+R
// resets the pending values, Ctrl+E opens the raw config file of the layer, and
// Esc with pending edits asks before dropping them.
type settingsPage struct {
	m      *Model
	port   SettingsPort
	snap   SettingsSnapshot
	layer  string
	filter []rune
	rows   []settingsRow
	sel    int
	top    int
	page   int
	// pending holds the values the user changed but did not save yet, by setting
	// key. The page shows them in place of the snapshot's values.
	pending map[string]string
	active  bool
	box     space.Rect
}

// settingsRow is one line of the page: a section header or an editable item.
type settingsRow struct {
	header bool
	title  string // the section name of a header row
	item   SettingsItem
}

var _ Overlay = (*settingsPage)(nil)

// newSettingsPage returns the page over port, on the User layer.
func newSettingsPage(m *Model, port SettingsPort) *settingsPage {
	p := &settingsPage{
		m:       m,
		port:    port,
		snap:    port.Snapshot(),
		layer:   SettingsLayerUser,
		pending: make(map[string]string),
		active:  true,
	}
	p.rebuild()
	return p
}

// Active reports whether the page is still open.
func (p *settingsPage) Active() bool { return p.active }

// Close closes the page.
func (p *settingsPage) Close() { p.active = false }

// TakesEscape reports that the page handles Esc itself: it asks before dropping
// pending edits.
func (p *settingsPage) TakesEscape() bool { return true }

// rebuild turns the snapshot, the layer and the filter into the rows the page
// draws, with the pending values in place of the saved ones. The selection stays
// on the same setting when it is still shown.
func (p *settingsPage) rebuild() {
	selected := p.selectedKey()
	needle := strings.ToLower(string(p.filter))
	rows := make([]settingsRow, 0, len(p.snap.Sections))
	for _, sec := range p.snap.Sections {
		var items []settingsRow
		for _, it := range sec.Items {
			value := it.Value
			if pending, ok := p.pending[it.Key]; ok {
				value = pending
			}
			shown := SettingsItem{
				Key: it.Key, Name: it.Name, Section: it.Section, Kind: it.Kind,
				Value: value, Choices: it.Choices, Min: it.Min, Max: it.Max,
				Apply: it.Apply, Layers: it.Layers,
			}
			if needle != "" && !settingsMatches(shown, needle) {
				continue
			}
			items = append(items, settingsRow{item: shown})
		}
		if len(items) == 0 {
			continue
		}
		rows = append(rows, settingsRow{header: true, title: sec.Name})
		rows = append(rows, items...)
	}
	p.rows = rows
	p.sel = p.firstItem()
	if selected != "" {
		for i, r := range p.rows {
			if !r.header && r.item.Key == selected {
				p.sel = i
				break
			}
		}
	}
	p.keepVisible()
}

// settingsMatches reports whether row passes the filter.
func settingsMatches(it SettingsItem, needle string) bool {
	text := strings.ToLower(it.Name + " " + it.Key + " " + it.Section + " " + it.Value)
	return strings.Contains(text, needle)
}

// selectedKey returns the key of the highlighted row, or "".
func (p *settingsPage) selectedKey() string {
	if p.sel < 0 || p.sel >= len(p.rows) || p.rows[p.sel].header {
		return ""
	}
	return p.rows[p.sel].item.Key
}

// firstItem returns the index of the first item row, or 0.
func (p *settingsPage) firstItem() int {
	for i, r := range p.rows {
		if !r.header {
			return i
		}
	}
	return 0
}

// itemAt returns the highlighted item, and false on a header or an empty list.
func (p *settingsPage) itemAt(i int) (SettingsItem, bool) {
	if i < 0 || i >= len(p.rows) || p.rows[i].header {
		return SettingsItem{}, false
	}
	return p.rows[i].item, true
}

// dirty reports whether there are pending values.
func (p *settingsPage) dirty() bool { return len(p.pending) > 0 }

// writable reports whether the item may be edited in the current layer.
func (p *settingsPage) writable(it SettingsItem) bool {
	return p.layer == SettingsLayerUser || slices.Contains(it.Layers, SettingsLayerProject)
}

// key handles one key press. It returns the command the press starts.
func (p *settingsPage) key(msg tea.KeyPressMsg) tea.Cmd {
	switch tea.Key(msg).Keystroke() {
	case "up":
		p.move(-1)
	case "down":
		p.move(1)
	case "pgup":
		p.move(-p.pageCount())
	case "pgdown":
		p.move(p.pageCount())
	case "home":
		p.sel = p.firstItem()
	case "end":
		p.sel = p.lastItem()
	case "enter":
		return p.edit()
	case "tab":
		p.toggleLayer()
	case "ctrl+s":
		return p.save()
	case "ctrl+r":
		p.reset()
	case "ctrl+e":
		return p.editRaw()
	case "esc":
		return p.cancel()
	case "backspace":
		if n := len(p.filter); n > 0 {
			p.filter = p.filter[:n-1]
			p.rebuild()
		}
	default:
		if msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0 && msg.Text != "" {
			p.filter = append(p.filter, []rune(msg.Text)...)
			p.rebuild()
		}
	}
	return nil
}

// move shifts the highlight by d rows, stopping at the ends and skipping
// headers.
func (p *settingsPage) move(d int) {
	step, n := 1, d
	if d < 0 {
		step, n = -1, -d
	}
	for range n {
		next := p.sel + step
		for next >= 0 && next < len(p.rows) && p.rows[next].header {
			next += step
		}
		if next < 0 || next >= len(p.rows) {
			break
		}
		p.sel = next
	}
	p.keepVisible()
}

// lastItem returns the index of the last item row, or 0.
func (p *settingsPage) lastItem() int {
	for i := len(p.rows) - 1; i >= 0; i-- {
		if !p.rows[i].header {
			return i
		}
	}
	return 0
}

// pageCount is the number of item rows the last draw showed, for PageUp and
// PageDown.
func (p *settingsPage) pageCount() int {
	if p.page > 0 {
		return p.page
	}
	return list.DefaultPage
}

// keepVisible scrolls the rows so that the highlight is inside the page.
func (p *settingsPage) keepVisible() {
	page := p.pageCount()
	if p.sel < p.top {
		p.top = p.sel
	}
	if p.sel >= p.top+page {
		p.top = p.sel - page + 1
	}
	p.top = max(0, min(p.top, len(p.rows)-page))
}

// edit opens the editor of the highlighted setting: a bool toggles, an enum
// opens a picker, and an int or a string opens its one-line prompt. A setting
// the current layer cannot hold is a notice.
func (p *settingsPage) edit() tea.Cmd {
	it, ok := p.itemAt(p.sel)
	if !ok {
		return nil
	}
	if !p.writable(it) {
		p.m.notice = "Settings: " + it.Name + " can only be set in the user config"
		return nil
	}
	switch it.Kind {
	case "bool":
		if it.Value == "on" {
			p.pending[it.Key] = "off"
		} else {
			p.pending[it.Key] = "on"
		}
		p.rebuild()
	default:
		// The picker and the prompt are overlays: they are opened from a message,
		// after the overlay stack has finished routing this key. Opening them here
		// would race the stack's own update of the top overlay.
		key := it.Key
		return func() tea.Msg { return settingsEditMsg{page: p, key: key} }
	}
	return nil
}

// openEdit opens the editor a settingsEditMsg asked for: the enum picker or the
// one-line value prompt.
func (p *settingsPage) openEdit(key string) {
	for _, sec := range p.snap.Sections {
		for _, it := range sec.Items {
			if it.Key != key {
				continue
			}
			if !p.writable(it) {
				p.m.notice = "Settings: " + it.Name + " can only be set in the user config"
				return
			}
			if it.Kind == "enum" {
				p.pick(it)
			} else {
				p.prompt(it)
			}
			return
		}
	}
}

// pick opens the enum picker of it. Choosing a value makes it pending; it does
// not save it.
func (p *settingsPage) pick(it SettingsItem) tea.Cmd {
	rows := make([]list.Row, len(it.Choices))
	for i, choice := range it.Choices {
		right := ""
		if choice == it.Value {
			right = "current"
		}
		rows[i] = list.Row{Left: choice, Right: right, Value: choice}
	}
	v := list.New("Set "+it.Name, rows, p.m.listChrome(), func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		p.pending[it.Key] = r.Value
		p.rebuild()
		return nil
	})
	v.Fit = true
	v.Hint = list.Hint + ", Esc cancel"
	p.m.pushOverlay(v)
	return nil
}

// prompt opens the one-line prompt of an int or a string. The port validates the
// value while it is typed; a valid answer becomes pending. Nothing is saved.
func (p *settingsPage) prompt(it SettingsItem) tea.Cmd {
	p.m.pushOverlay(prompt.NewLine(p.m.styles, prompt.LineOptions{
		Label:     it.Name + ": ",
		Input:     it.Value,
		SelectAll: true,
		Tag:       settingsEditTag{page: p, key: it.Key},
		Validate:  func(text string) error { return p.port.Validate(it.Key, text) },
	}))
	return nil
}

// toggleLayer switches the layer the page writes: User, or Project for a
// trusted workspace. A workspace that is not trusted keeps the User layer and
// says why.
func (p *settingsPage) toggleLayer() {
	if p.layer == SettingsLayerUser {
		if !p.snap.ProjectWritable {
			p.m.notice = "Settings: the project layer can be written only in a trusted workspace"
			return
		}
		p.layer = SettingsLayerProject
		p.m.notice = "Settings: writing the project config"
		return
	}
	p.layer = SettingsLayerUser
	p.m.notice = "Settings: writing the user config"
}

// reset drops the pending values.
func (p *settingsPage) reset() {
	if !p.dirty() {
		p.m.notice = "Settings: no changes to reset"
		return
	}
	p.pending = make(map[string]string)
	p.rebuild()
	p.m.notice = "Settings: changes reset"
}

// save writes the pending values to the current layer, through a command: the
// port patches the file and reloads the config, which must not run on the update
// loop.
func (p *settingsPage) save() tea.Cmd {
	if !p.dirty() {
		p.m.notice = "Settings: no changes to save"
		return nil
	}
	var edits []SettingEdit
	for _, sec := range p.snap.Sections {
		for _, it := range sec.Items {
			if value, ok := p.pending[it.Key]; ok {
				edits = append(edits, SettingEdit{Key: it.Key, Value: value})
			}
		}
	}
	port, layer := p.port, p.layer
	return func() tea.Msg {
		change, err := port.Save(edits, layer)
		change.Layer = layer
		return settingsChangeMsg{change: change, err: err}
	}
}

// editRaw closes the page and opens the raw config file of the current layer in a
// tab, the escape hatch for the settings the page does not edit.
func (p *settingsPage) editRaw() tea.Cmd {
	path := p.snap.UserFile
	if p.layer == SettingsLayerProject {
		path = p.snap.ProjectFile
	}
	layer := p.layer
	p.Close()
	if path == "" {
		p.m.notice = "Settings: no " + layer + " config file is known"
		return nil
	}
	p.m.openFile(path, false)
	p.m.focusEditor()
	return nil
}

// cancel closes the page, asking first when there are pending values.
func (p *settingsPage) cancel() tea.Cmd {
	if !p.dirty() {
		p.Close()
		return nil
	}
	return func() tea.Msg { return settingsCancelMsg{page: p} }
}

// confirmCancel asks whether to drop the pending values.
func (p *settingsPage) confirmCancel() {
	n := len(p.pending)
	label := "Discard " + strconv.Itoa(n) + " unsaved setting"
	if n != 1 {
		label += "s"
	}
	label += "?"
	p.m.pushOverlay(prompt.NewConfirm(p.m.styles, label, settingsDiscardTag{page: p}))
}

// applied takes the result of a Save: on success the page shows the new
// snapshot with no pending values; on failure it stays put and the status row
// says why.
func (p *settingsPage) applied(change SettingsChange, err error) {
	if err != nil {
		p.m.notice = "Settings: " + err.Error()
		return
	}
	p.pending = make(map[string]string)
	p.snap = change.Snapshot
	p.rebuild()
}

// rowAt returns the index of the row drawn at (x, y) in the last draw.
func (p *settingsPage) rowAt(x, y int) (int, bool) {
	b := p.box
	if !b.Contains(x, y) || y < b.Y+2 || y >= b.Y+b.H-1 {
		return 0, false
	}
	i := p.top + (y - b.Y - 2)
	if i < 0 || i >= len(p.rows) {
		return 0, false
	}
	return i, true
}

// Update handles one message through the overlay contract.
func (p *settingsPage) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		cmd = p.key(msg)
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			mo := tea.Mouse(msg)
			if i, ok := p.rowAt(mo.X, mo.Y); ok && !p.rows[i].header {
				p.sel = i
			}
		}
	case tea.MouseWheelMsg:
		switch tea.Mouse(msg).Button {
		case tea.MouseWheelUp:
			p.move(-list.WheelRows)
		case tea.MouseWheelDown:
			p.move(list.WheelRows)
		}
	case prompt.Done:
		if tag, ok := msg.Tag.(settingsEditTag); ok && tag.page == p {
			if msg.Outcome == prompt.Submitted {
				p.pending[tag.key] = msg.Text
				p.rebuild()
			}
		}
		if tag, ok := msg.Tag.(settingsDiscardTag); ok && tag.page == p {
			if msg.Outcome == prompt.Yes {
				p.Close()
			}
		}
	}
	if !p.active {
		return nil, cmd
	}
	return p, cmd
}

// settingsEditTag is the tag of the value prompt of one setting.
type settingsEditTag struct {
	page *settingsPage
	key  string
}

// settingsDiscardTag is the tag of the discard confirmation of a page with
// pending values.
type settingsDiscardTag struct {
	page *settingsPage
}

// settingsChangeMsg is the result of the command a Save starts.
type settingsChangeMsg struct {
	change SettingsChange
	err    error
}

// settingsEditMsg asks the root to open the editor of one setting, after the
// overlay stack has routed the key that asked for it.
type settingsEditMsg struct {
	page *settingsPage
	key  string
}

// settingsCancelMsg asks the root to confirm dropping a page's pending values.
type settingsCancelMsg struct {
	page *settingsPage
}

// topSettingsPage returns the Settings page on the overlay stack, or nil.
func (m *Model) topSettingsPage() *settingsPage {
	for i := len(m.overlays) - 1; i >= 0; i-- {
		if page, ok := m.overlays[i].(*settingsPage); ok {
			return page
		}
	}
	return nil
}

// settingsMessage handles the result of a settings Save: the open page takes its
// new snapshot, and the running editor applies the live values. It reports
// whether it took msg.
func (m *Model) settingsMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch d := msg.(type) {
	case settingsChangeMsg:
		if page := m.topSettingsPage(); page != nil {
			page.applied(d.change, d.err)
		}
		if d.err != nil {
			m.notice = "Settings: " + d.err.Error()
			return nil, true
		}
		m.applySettingsChange(d.change)
		return nil, true
	case settingsEditMsg:
		if d.page.Active() {
			d.page.openEdit(d.key)
		}
		return nil, true
	case settingsCancelMsg:
		if d.page.Active() {
			d.page.confirmCancel()
		}
		return nil, true
	}
	return nil, false
}

// applySettingsChange pushes a saved or reloaded configuration into the running
// editor: the display options, the theme, the keymap, the icon set and the
// explorer toggles that the config now sets. Only the settings named in Live are
// applied; the Restart names are announced in one notice, because gvim does not
// hot-reload the parts of itself that read them at startup.
func (m *Model) applySettingsChange(change SettingsChange) {
	m.util.settings = change.Settings
	if m.util.settingsPort != nil {
		// The editor is owned by the update loop, so its setters run here and not
		// in the command that wrote and reloaded the file.
		m.util.settingsPort.Apply(change)
	}
	for _, key := range change.Live {
		m.applyLiveSetting(key, change)
	}
	var notices []string
	switch {
	case change.Layer != "":
		notices = append(notices, "Settings saved to the "+change.Layer+" config")
	case len(change.Live) > 0:
		notices = append(notices, "Settings reloaded")
	}
	if len(change.Restart) > 0 {
		notices = append(notices, "Restart gvim to apply: "+strings.Join(change.Restart, ", "))
	}
	notices = append(notices, change.Warnings...)
	if len(notices) == 0 {
		notices = append(notices, "Settings: no changes")
	}
	m.notice = strings.Join(notices, "; ")
}

// applyLiveSetting applies one setting whose new value takes effect now.
func (m *Model) applyLiveSetting(key string, change SettingsChange) {
	v := change.View
	switch key {
	case "theme":
		if change.Theme != "" {
			m.applyTheme(change.Theme)
		}
	case "keymap":
		if change.Keymap != "" {
			km, err := m.util.switchKeymap(change.Keymap)
			if err != nil {
				m.notice = "Keymap " + change.Keymap + ": " + err.Error()
				return
			}
			m.keys = km
			m.util.settings.Keymap = change.Keymap
		}
	case "ui_icons", "ui_icons_font":
		if change.UIIconsMode != "" {
			if mode, ok := icons.Parse(change.UIIconsMode); ok {
				m.view.icons = mode
			}
			m.util.settings.IconsMode = change.UIIconsMode
			m.util.settings.IconsWhy = change.IconsWhy
		}
	case "editor.show_menu_bar":
		m.hideMenuBar = !change.Settings.MenuBar
	case "editor.menu_bar_mnemonics":
		m.menu = m.menu.WithMnemonics(v.MenuBarMnemonics)
	case "editor.line_numbers":
		m.hideLineNumbers = !change.Settings.LineNumbers
	case "editor.show_tab_bar":
		m.view.tabBar = v.ShowTabBar
	case "editor.show_status_bar":
		m.view.statusBar = v.ShowStatusBar
	case "editor.show_prompt_line":
		m.view.promptLine = v.ShowPromptLine
	case "editor.show_vertical_scrollbar":
		m.view.vScroll = v.ShowVerticalScrollbar
	case "editor.show_horizontal_scrollbar":
		m.view.hScroll = v.ShowHorizontalScrollbar
	case "editor.line_wrap":
		m.view.lineWrap = v.LineWrap
	case "editor.highlight_current_line":
		m.view.currentLine = v.HighlightCurrentLine
	case "editor.highlight_occurrences":
		m.view.occurrences = v.HighlightOccurrences
	case "editor.whitespace_show":
		m.view.whitespace = v.ShowWhitespace
	case "editor.cursor_style":
		if slices.Contains([]string{cursorBlock, cursorBar, cursorUnderline}, v.CursorStyle) {
			m.view.cursor = v.CursorStyle
		}
	case "mouse_capture":
		m.view.mouseCapture = v.MouseCapture
	case "editor.mouse_hover_enabled":
		m.view.mouseHover = v.MouseHover
	case "scroll_sync":
		m.view.scrollSync = v.ScrollSync
	case "explorer_show_hidden":
		if m.tree != nil {
			m.tree.SetShowHidden(change.Settings.ExplorerShowHidden)
		}
	case "explorer_show_ignored":
		if m.tree != nil {
			m.tree.SetShowIgnored(change.Settings.ExplorerShowIgnored)
		}
	}
	// The rest of the display options are read from the view options by the
	// painting, the layout, the mouse and the find bar, so they only need the
	// option copied: the options a language can override stay in the editor.
	m.view.opts = copyViewOption(m.view.opts, v, key)
}

// copyViewOption returns opts with the one display option named by key taken
// from next. The fields that also have a toggle above (tab bar, line wrap, ...)
// are not copied here.
func copyViewOption(opts, next ViewOptions, key string) ViewOptions {
	switch key {
	case "editor.relative_line_numbers":
		opts.RelativeLineNumbers = next.RelativeLineNumbers
	case "editor.hide_current_line_on_selection":
		opts.HideCurrentLineOnSelection = next.HideCurrentLineOnSelection
	case "editor.highlight_current_column":
		opts.HighlightCurrentColumn = next.HighlightCurrentColumn
	case "editor.wrap_indent":
		opts.WrapIndent = next.WrapIndent
	case "editor.syntax_highlighting":
		opts.SyntaxHighlighting = next.SyntaxHighlighting
	case "editor.show_tilde":
		opts.ShowTilde = next.ShowTilde
	case "editor.indentation_guide":
		opts.IndentationGuideMode = next.IndentationGuideMode
	case "editor.rainbow_indentation":
		opts.RainbowIndentation = next.RainbowIndentation
	case "editor.highlight_matching_brackets":
		opts.HighlightMatchingBrackets = next.HighlightMatchingBrackets
	case "editor.rainbow_brackets":
		opts.RainbowBrackets = next.RainbowBrackets
	case "editor.smooth_scroll":
		opts.SmoothScroll = next.SmoothScroll
	case "editor.mouse_wheel_scroll_lines":
		opts.MouseWheelScrollLines = next.MouseWheelScrollLines
	case "editor.double_click_time_ms":
		opts.DoubleClickTimeMS = next.DoubleClickTimeMS
	case "editor.mouse_hover_delay_ms":
		opts.MouseHoverDelayMS = next.MouseHoverDelayMS
	case "editor.search.case_sensitive":
		opts.Search.CaseSensitive = next.Search.CaseSensitive
	case "editor.search.whole_word":
		opts.Search.WholeWord = next.Search.WholeWord
	case "editor.search.regex":
		opts.Search.Regex = next.Search.Regex
	case "editor.search.confirm_each":
		opts.Search.ConfirmEach = next.Search.ConfirmEach
	}
	return opts
}

// Draw paints the page and records its box and page size.
func (p *settingsPage) Draw(g *grid.Grid) {
	n := min(len(p.rows), list.MaxHeight-3)
	box := list.Centred(g.Width(), g.Height(), list.MaxWidth, n+3)
	p.box = box
	if box.W == 0 || box.H < 3 {
		return
	}
	p.page = box.H - 3
	p.keepVisible()
	ov := p.m.styles.Overlay
	title := "Settings — " + settingsLayerTitle(p.layer)
	if p.dirty() {
		title += " *"
	}
	ov.PaintBox(g, box.X, box.Y, box.W, box.H, title)
	end := box.X + box.W - 2
	// Filter row.
	const filterLabel = "Filter: "
	filterY := box.Y + 1
	g.Fill(box.X+1, box.X+box.W-1, filterY, ov.Box)
	labelX := box.X + 2 + g.Put(box.X+2, filterY, end, filterLabel, ov.Muted)
	g.Put(labelX, filterY, end, space.Tail(string(p.filter), end-labelX), ov.Box)
	// Item rows.
	start := p.top
	for k := 0; k < p.page && start+k < len(p.rows); k++ {
		i := start + k
		r := p.rows[i]
		y := box.Y + 2 + k
		st, rightSt := ov.Box, ov.Muted
		if i == p.sel {
			st = ov.Selected
			rightSt = st
			g.Fill(box.X+1, box.X+box.W-1, y, st)
		}
		if r.header {
			st.Bold = true
			g.Put(box.X+2, y, end, r.title, st)
			continue
		}
		it := r.item
		name := it.Name
		if _, ok := p.pending[it.Key]; ok {
			name = "* " + name
		}
		value := it.Value
		if !p.writable(it) {
			name = it.Name + " (user layer)"
		}
		valueX := end - ansi.StringWidth(value)
		g.Put(max(valueX, box.X+2), y, end, value, rightSt)
		room := max(valueX-box.X-4, 1)
		g.Put(box.X+2, y, box.X+2+room, name, st)
	}
	if len(p.rows) == 0 {
		g.Put(box.X+2, box.Y+2, end, "No matches", ov.Muted)
	}
	ov.PaintFooter(g, box.X, box.Y+box.H-1, box.W,
		"Enter edit · Tab layer · ^S save · ^R reset · ^E raw · Esc close")
}

// Cursor returns the caret in the filter row.
func (p *settingsPage) Cursor(_, _ int) (x, y int, ok bool) {
	if p.box.W == 0 {
		return 0, 0, false
	}
	const label = "Filter: "
	labelX := p.box.X + 2 + ansi.StringWidth(label)
	room := p.box.X + p.box.W - 2 - labelX
	if room < 0 {
		return 0, 0, false
	}
	return labelX + ansi.StringWidth(space.Tail(string(p.filter), room)), p.box.Y + 1, true
}

// settingsLayerTitle names a layer in the page title.
func settingsLayerTitle(layer string) string {
	if layer == SettingsLayerProject {
		return "Project"
	}
	return "User"
}
