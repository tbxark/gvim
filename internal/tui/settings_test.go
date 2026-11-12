package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// testSettings is a settings snapshot with every field set to a non-default
// value, so each row shows what it reads.
func testSettings() Settings {
	return Settings{
		Theme:               "light",
		Keymap:              "macos",
		TabSize:             2,
		InsertSpaces:        false,
		AutoIndent:          false,
		LineNumbers:         true,
		MenuBar:             true,
		ExplorerShowHidden:  false,
		ExplorerShowIgnored: true,
		UIIcons:             "nerd",
		UIIconsFont:         "JetBrainsMono Nerd Font Mono",
		IconsMode:           "nerd",
		IconsWhy:            "set by ui_icons",
		ConfigFile:          "/home/u/.config/gvim/config.json",
	}
}

func TestSettingsRowsShowTheEffectiveValues(t *testing.T) {
	want := []string{
		"theme=light",
		"keymap=macos",
		"tab_size=2",
		"insert_spaces=off",
		"auto_indent=off",
		"line_numbers=on",
		"show_menu_bar=on",
		"explorer_show_hidden=off",
		"explorer_show_ignored=on",
		"ui_icons=nerd",
		"ui_icons_font=JetBrainsMono Nerd Font Mono",
		"image_protocol=auto",
		"config_file=/home/u/.config/gvim/config.json",
	}
	sameLines(t, "rows", rowsOf(settingsRows(testSettings())), want)
}

func TestSettingsRowsShowWhatAutoResolvedTo(t *testing.T) {
	tests := []struct {
		name string
		s    Settings
		want string
	}{
		{"unset reads as auto", Settings{}, "auto -> plain"},
		{"auto found nerd", Settings{UIIcons: "auto", IconsMode: "nerd"}, "auto -> nerd"},
		{"plain as written", Settings{UIIcons: "plain", IconsMode: "plain"}, "plain"},
	}
	for _, tt := range tests {
		for _, row := range settingsRows(tt.s) {
			if row.Left == "ui_icons" && row.Right != tt.want {
				t.Errorf("%s: ui_icons row = %q, want %q", tt.name, row.Right, tt.want)
			}
		}
	}
}

func TestSettingsRowsSayWhenThereIsNoConfigFile(t *testing.T) {
	rows := settingsRows(Settings{})
	last := rows[len(rows)-1]
	if last.Left != "config_file" || last.Right != "(none)" {
		t.Fatalf("last row = %+v, want config_file with (none)", last)
	}
}

func TestOpenSettingsShowsRowsAndFilters(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), testSettings(), nil)
	if err := m.Run("open_settings"); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if !frameHas(m, "Settings") || !frameRowHas(m, "tab_size", "2") {
		t.Fatal("open_settings does not show the effective settings")
	}
	press(m, typed("t"))
	press(m, typed("a"))
	press(m, typed("b"))
	if !frameRowHas(m, "tab_size", "2") || frameRowHas(m, "theme", "light") {
		t.Fatal("the filter did not narrow the settings to tab_size")
	}
}

func TestSettingsEnterOpensTheConfigFileInANewTab(t *testing.T) {
	ed := &fileFake{fakeEditor: newFakeEditor("hello")}
	m := newUtilityModel(t, ed, testKeys(), testSettings(), nil)
	runAction(t, m, "open_settings")
	press(m, special(tea.KeyEnter))
	if got := strings.Join(ed.opened, ","); got != testSettings().ConfigFile {
		t.Fatalf("opened %q, want the user config file", got)
	}
	if len(m.overlays) != 0 {
		t.Fatal("the settings overlay stayed open after the config file opened")
	}
}

func TestSettingsActivateOpensTheConfigFileWithoutTheOverlay(t *testing.T) {
	ed := &fileFake{fakeEditor: newFakeEditor("hello")}
	m := newUtilityModel(t, ed, testKeys(), testSettings(), nil)
	if err := m.Run("settings_activate"); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if got := strings.Join(ed.opened, ","); got != testSettings().ConfigFile {
		t.Fatalf("opened %q, want the user config file", got)
	}
	if len(m.overlays) != 0 {
		t.Fatal("settings_activate opened an overlay")
	}
}

func TestSettingsActivateWithoutConfigFileSaysSo(t *testing.T) {
	ed := &fileFake{fakeEditor: newFakeEditor("hello")}
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	runAction(t, m, "settings_activate")
	if len(ed.opened) != 0 {
		t.Fatalf("opened %v with no config file", ed.opened)
	}
	if !frameHas(m, "No user config file") {
		t.Fatal("the status row does not say there is no config file")
	}
}

func TestSettingsEscCloses(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), testSettings(), nil)
	runAction(t, m, "open_settings")
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 0 {
		t.Fatalf("Esc left %d overlays open", len(m.overlays))
	}
}
