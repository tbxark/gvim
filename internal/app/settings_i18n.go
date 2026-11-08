package app

import (
	"strings"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/i18n"
)

// Settings page labels are extracted for the Interface, Display and Terminal rows plus
// the rows Fresh names in settings.field.*: the catalogue holds the current schema name
// as the English value (so English rendering is unchanged), and a translated value can
// use Fresh's human label. Sections are gvim-only keys (Fresh has no category i18n
// keys), so they stay English until a translation pass. i18n.TOr gives the English
// name when a key is missing anywhere, never the bare key.
const (
	settingsFieldKeyPrefix   = "gvim.settings.field."
	settingsSectionKeyPrefix = "gvim.settings.section."
)

// settingLabelKey is the catalogue key of one settings row label.
func settingLabelKey(setting config.Setting) string {
	return settingsFieldKeyPrefix + strings.Join(setting.Path, ".")
}

// settingsSectionKey is the catalogue key of one settings section title.
func settingsSectionKey(section string) string {
	return settingsSectionKeyPrefix + strings.ToLower(strings.ReplaceAll(section, " ", "_"))
}

// settingsLabel is the display label of one setting: the translated text when the
// catalogue has it, else the schema name the page has always shown.
func settingsLabel(setting config.Setting) string {
	return i18n.TOr("", settingLabelKey(setting), setting.Name(), nil)
}

// settingsSectionLabel is the display title of one settings section.
func settingsSectionLabel(section string) string {
	return i18n.TOr("", settingsSectionKey(section), section, nil)
}
