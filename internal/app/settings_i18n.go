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

// settingsPage is the title of a structured settings page.
func settingsPage(key, english string) string {
	return i18n.TOr("", "gvim.settings.page."+key, english, nil)
}

// settingsAdd is the add-row label of a structured settings page.
func settingsAdd(key, english string) string {
	return i18n.TOr("", "gvim.settings.add."+key, english, nil)
}

// settingsNote is a structured settings page's explanatory note.
func settingsNote(key, english string) string {
	return i18n.TOr("", "gvim.settings.note."+key, english, nil)
}

// settingsLanguageTitle is the title of one language's page: the translated Languages
// title with the language id (an identifier, not translated).
func settingsLanguageTitle(id string) string {
	return settingsPage("languages", "Languages") + " · " + id
}

// settingsLanguagePart is one translated part of a language child title: a page part
// (Extensions, ...) or a section part (Formatter, On Save, Indent).
func settingsLanguagePart(english string) string {
	key, ok := settingsLanguagePartKeys[english]
	if !ok {
		return english
	}
	return i18n.TOr("", key, english, nil)
}

// settingsLanguagePartKeys maps the English part to its catalogue key.
var settingsLanguagePartKeys = map[string]string{
	"Extensions":          "gvim.settings.page.extensions",
	"File Names":          "gvim.settings.page.file_names",
	"Formatter Arguments": "gvim.settings.page.formatter_arguments",
	"Formatter":           "gvim.settings.section.languages.formatter",
	"On Save":             "gvim.settings.section.languages.on_save",
	"Indent":              "gvim.settings.section.languages.indent",
}

// settingsLanguageAdd is the add-row label of a language child list.
func settingsLanguageAdd(english string) string {
	key, ok := settingsLanguageAddKeys[english]
	if !ok {
		return english
	}
	return i18n.TOr("", key, english, nil)
}

// settingsLanguageAddKeys maps the English add-row text to its catalogue key.
var settingsLanguageAddKeys = map[string]string{
	"Add extension…": "gvim.settings.add.extension",
	"Add file name…": "gvim.settings.add.file_name",
	"Add argument…":  "gvim.settings.add.argument",
}

// settingsValue is one structured-settings value label: translated when the catalogue
// has it, else the English text the page has always shown.
func settingsValue(key, english string) string {
	return i18n.TOr("", "gvim.settings.value."+key, english, nil)
}
