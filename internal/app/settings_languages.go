package app

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
)

// The per-language structured values of the Languages page: the two string lists,
// the formatter, on_save and indent. on_save is an array of objects, which a
// single config path cannot address, so its page shows each action read-only and
// edits the whole array as JSON (config.ValidateEdits stays authoritative); the
// lists, the formatter and indent are written whole at their own paths.

// structureLanguageChild builds the structured child pages of one language entry,
// or reports false so the page falls back to the scalar fields.
func (s *settingsService) structureLanguageChild(object map[string]json.RawMessage, rest string) (tui.SettingsStructure, bool) {
	id := languageOfChild(rest)
	entry, _ := layerObjectAt(object, []string{"languages", id})
	switch {
	case strings.HasSuffix(rest, ".extensions"):
		return s.structureLanguageList(object, id, "extensions", "Extensions", "Add extension…"), true
	case strings.HasSuffix(rest, ".filenames"):
		return s.structureLanguageList(object, id, "filenames", "File Names", "Add file name…"), true
	case strings.HasSuffix(rest, ".formatter.args"):
		return s.structureLanguageStringList(object, id, []string{"languages", id, "formatter", "args"}, "Formatter Arguments", "Add argument…"), true
	case strings.HasSuffix(rest, ".formatter"):
		return s.structureLanguageFormatter(entry, id), true
	case strings.HasSuffix(rest, ".on_save"):
		return s.structureLanguageOnSave(entry, id), true
	case strings.HasSuffix(rest, ".indent"):
		return s.structureLanguageIndent(entry, id), true
	}
	return tui.SettingsStructure{}, false
}

// languageOfChild is the language id of a child structure key: everything before
// the first "." of the suffix ("go.extensions" gives go).
func languageOfChild(rest string) string {
	if i := strings.IndexByte(rest, '.'); i >= 0 {
		return rest[:i]
	}
	return rest
}

// structureLanguageList is a whole-list page of one language entry: extensions or
// filenames. The list is written back as an array at the entry's path.
func (s *settingsService) structureLanguageList(object map[string]json.RawMessage, id, key, title, addLabel string) tui.SettingsStructure {
	path := []string{"languages", id, key}
	st := tui.SettingsStructure{Key: "languages." + id + "." + key, Title: settingsLanguageTitle(id) + " · " + settingsLanguagePart(title),
		Kind: "list", Path: path, AddLabel: settingsLanguageAdd(addLabel)}
	entry, _ := layerObjectAt(object, []string{"languages", id})
	for _, value := range languageListValue(entry, key) {
		st.Rows = append(st.Rows, tui.SettingsStructureRow{Path: path, Name: value, Value: value})
	}
	return st
}

// structureLanguageStringList is the same page for a list nested in an object
// (the formatter's args); the whole array is written at path.
func (s *settingsService) structureLanguageStringList(object map[string]json.RawMessage, id string, path []string, title, addLabel string) tui.SettingsStructure {
	st := tui.SettingsStructure{Key: "languages." + id + ".formatter.args", Title: settingsLanguageTitle(id) + " · " + settingsLanguagePart(title),
		Kind: "list", Path: path, AddLabel: settingsLanguageAdd(addLabel)}
	entry, _ := layerObjectAt(object, []string{"languages", id, "formatter"})
	for _, value := range languageListValue(entry, "args") {
		st.Rows = append(st.Rows, tui.SettingsStructureRow{Path: path, Name: value, Value: value})
	}
	return st
}

// structureLanguageFormatter is the formatter page: command, args (a child list),
// stdin and timeout. The formatter object is written whole, one field at a time.
func (s *settingsService) structureLanguageFormatter(entry map[string]json.RawMessage, id string) tui.SettingsStructure {
	path := []string{"languages", id, "formatter"}
	st := tui.SettingsStructure{Key: "languages." + id + ".formatter", Title: settingsLanguageTitle(id) + " · " + settingsLanguagePart("Formatter"), Kind: "fields"}
	formatter, _ := layerObjectAt(entry, []string{"formatter"})
	rows := []struct {
		key, kind string
		min, max  int
	}{
		{"command", "string", 0, 0},
		{"stdin", "bool", 0, 0},
		{"timeout_ms", "int", 0, config.DefaultFormatterTimeoutMS * 100},
	}
	for _, row := range rows {
		r := tui.SettingsStructureRow{Path: append(slices.Clone(path), row.key), Name: row.key, Kind: row.kind, Min: row.min, Max: row.max}
		if raw, ok := layerRawAt(formatter, []string{row.key}); ok {
			r.Value = displayJSON(raw, kindOf(row.kind))
		} else {
			r.Inherited = true
			r.Note = "inherit"
			switch row.key {
			case "stdin":
				r.Value = "on"
			case "timeout_ms":
				r.Value = strconv.Itoa(config.DefaultFormatterTimeoutMS)
			}
		}
		st.Rows = append(st.Rows, r)
	}
	args := languageListValue(formatter, "args")
	st.Rows = append(st.Rows, tui.SettingsStructureRow{
		Name: "args", Child: "languages." + id + ".formatter.args",
		Value: countText(len(args), "argument") + " set", Note: "the command's arguments",
	})
	return st
}

// structureLanguageOnSave is the on_save page: the whole array as validated JSON
// (an array of objects cannot be patched item by item); each action is described
// in the page note.
func (s *settingsService) structureLanguageOnSave(entry map[string]json.RawMessage, id string) tui.SettingsStructure {
	st := tui.SettingsStructure{Key: "languages." + id + ".on_save", Title: settingsLanguageTitle(id) + " · " + settingsLanguagePart("On Save"), Kind: "fields"}
	st.Note = settingsNote("languages_on_save", "on_save is an array of actions; edit the whole array as JSON. Each action: command, args, working_dir, stdin, timeout_ms, enabled.")
	raw := json.RawMessage("[]")
	if r, ok := layerRawAt(entry, []string{"on_save"}); ok {
		raw = compactJSON(r)
		st.Note = countText(len(onSaveEntries(r)), "action") + " set. " + st.Note
	}
	st.Rows = append(st.Rows, tui.SettingsStructureRow{
		Path: []string{"languages", id, "on_save"}, Name: "actions (JSON)", Kind: "json",
		Value: string(raw), Note: "whole array",
	})
	return st
}

// onSaveEntries decodes an on_save array for its count; a malformed array counts 0.
func onSaveEntries(raw json.RawMessage) []config.OnSaveAction {
	var out []config.OnSaveAction
	_ = json.Unmarshal(raw, &out)
	return out
}

// structureLanguageIndent is the indent pattern page: the five patterns as
// strings; an unset pattern is marked inherited (a built-in language entry or the
// global default may supply it).
func (s *settingsService) structureLanguageIndent(entry map[string]json.RawMessage, id string) tui.SettingsStructure {
	path := []string{"languages", id, "indent"}
	st := tui.SettingsStructure{Key: "languages." + id + ".indent", Title: settingsLanguageTitle(id) + " · " + settingsLanguagePart("Indent"), Kind: "fields"}
	indent, _ := layerObjectAt(entry, []string{"indent"})
	for _, key := range []string{"increase_indent_pattern", "decrease_indent_pattern", "indent_next_line_pattern", "dedent_next_line_pattern", "self_close_pattern"} {
		r := tui.SettingsStructureRow{Path: append(slices.Clone(path), key), Name: key, Kind: "string"}
		if raw, ok := layerRawAt(indent, []string{key}); ok {
			var text string
			_ = json.Unmarshal(raw, &text)
			r.Value = text
		} else {
			r.Inherited = true
			r.Note = "inherit"
		}
		st.Rows = append(st.Rows, r)
	}
	return st
}

// languageListValue returns the string list a layer entry sets at key, or nil.
func languageListValue(entry map[string]json.RawMessage, key string) []string {
	raw, ok := layerRawAt(entry, []string{key})
	if !ok {
		return nil
	}
	list, _ := stringList(raw)
	return list
}

// languageListSummary is a structured child row's value: what the layer sets.
func languageListSummary(entry map[string]json.RawMessage, key string) string {
	return countText(len(languageListValue(entry, key)), "entry") + " set"
}

// languageObjectSummary is a structured child row's value for an object field.
func languageObjectSummary(entry map[string]json.RawMessage, key string) string {
	if _, ok := layerObjectAt(entry, []string{key}); ok {
		return "set"
	}
	return "inherit"
}

// languageOnSaveSummary counts the on_save actions the layer sets.
func languageOnSaveSummary(entry map[string]json.RawMessage) string {
	raw, ok := layerRawAt(entry, []string{"on_save"})
	if !ok {
		return countText(0, "action") + " set"
	}
	return countText(len(onSaveEntries(raw)), "action") + " set"
}

// countText says "1 entry" or "3 entries".
func countText(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	if strings.HasSuffix(noun, "y") {
		return strconv.Itoa(n) + " " + strings.TrimSuffix(noun, "y") + "ies"
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// kindOf maps a row's kind name to the config display kind.
func kindOf(kind string) config.SettingKind {
	switch kind {
	case "bool":
		return config.SettingBool
	case "int":
		return config.SettingInt
	case "enum":
		return config.SettingEnum
	}
	return config.SettingString
}
