package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/list"
)

// Settings is the effective configuration that open_settings lists. The app fills
// it from the config snapshot. Keymap is the name of the active keymap, and
// ConfigFile is the user config file that settings_activate opens. An empty
// ConfigFile means there is no such file to open.
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

	// Read by gvim.config.get only; open_settings does not list them.
	LineEnding             string // line_ending: "auto", "lf", "crlf" or "cr"
	TrimTrailingWhitespace bool
	EnsureFinalNewline     bool
}

// settingsRows returns one row per setting, named as the config file names it. An
// unset text value shows as "(none)".
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

// settingsIconsKey is the row of the icon setting. Choosing it opens the icon preview
// instead of the config file.
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

// openSettings shows the effective settings. Choosing a row, or pressing Enter,
// opens the user config file: it is the only way to edit settings for now. The
// ui_icons row opens the icon preview instead.
func (m *Model) openSettings() {
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
