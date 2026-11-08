package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/tailscale/hujson"
)

// SettingPage is the kind of a settings row that opens a sub-page instead of
// editing one text value: languages, formatters, the status-bar element lists
// and terminal.shell.args. Its Summary is what the row shows.
const SettingPage SettingKind = "page"

// The sections of the structured settings, in page order.
const (
	SectionLanguages    = "Languages"
	SectionFormatters   = "Formatters"
	SectionStatusBar    = "Status Bar"
	SectionFileExplorer = "File Explorer"
	SectionTerminal     = "Terminal"
)

// LanguageOverrideField is one scalar field of a languages.<id> entry that the
// Settings UI edits. The config package owns the list so the page validates a
// value through the same languageFields table a config load uses.
type LanguageOverrideField struct {
	Key     string
	Kind    SettingKind
	Choices []string // enum values, when Kind is SettingEnum
	Min     int
	Max     int
}

// languageOverrideFields is the page order of the per-language scalars. The
// structured values (extensions, filenames, formatter, on_save, indent) are not
// edited by the Settings page yet; they stay Edit-raw only.
var languageOverrideFields = []LanguageOverrideField{
	{Key: "tab_size", Kind: SettingInt, Min: MinTabSize, Max: MaxTabSize},
	{Key: "use_tabs", Kind: SettingBool},
	{Key: "auto_indent", Kind: SettingBool},
	{Key: "auto_close", Kind: SettingBool},
	{Key: "auto_surround", Kind: SettingBool},
	{Key: "line_wrap", Kind: SettingBool},
	{Key: "wrap_column", Kind: SettingInt, Min: 0},
	{Key: "page_view", Kind: SettingBool},
	{Key: "page_width", Kind: SettingInt, Min: 0},
	{Key: "show_whitespace_tabs", Kind: SettingBool},
	{Key: "format_on_save", Kind: SettingBool},
	{Key: "comment_prefix", Kind: SettingString},
	{Key: "word_characters", Kind: SettingString},
	{Key: "grammar", Kind: SettingString},
	{Key: "textmate_grammar", Kind: SettingString},
	{Key: "indentation_guide", Kind: SettingBool},
}

// LanguageOverrideFields returns the scalar per-language overrides the Settings
// page edits, in page order.
func LanguageOverrideFields() []LanguageOverrideField {
	return slices.Clone(languageOverrideFields)
}

// languageOverrideFieldKeys indexes languageOverrideFields by key.
var languageOverrideFieldKeys = func() map[string]LanguageOverrideField {
	m := make(map[string]LanguageOverrideField, len(languageOverrideFields))
	for _, f := range languageOverrideFields {
		m[f.Key] = f
	}
	return m
}()

// LanguageOverrideFieldByKey returns the scalar override field called key.
func LanguageOverrideFieldByKey(key string) (LanguageOverrideField, bool) {
	f, ok := languageOverrideFieldKeys[key]
	return f, ok
}

// LanguageInheritedValue returns the display form of the value a
// languages.<id> entry inherits for one scalar field: the built-in language entry
// applied over the global editor option (go tab_size inherits 8, auto_indent
// inherits editor.auto_indent, ...). ok is false for a field whose inherited value
// has no single display form (the grammar names).
func (c Config) LanguageInheritedValue(language, field string) (string, bool) {
	eff := c.EditorFor(language, "")
	switch field {
	case "tab_size":
		return strconv.Itoa(eff.TabSize), true
	case "use_tabs":
		return onOff(eff.UseTabs), true
	case "auto_indent":
		return onOff(eff.AutoIndent), true
	case "auto_close":
		return onOff(eff.AutoClose), true
	case "auto_surround":
		return onOff(eff.AutoSurround), true
	case "line_wrap":
		return onOff(eff.LineWrap), true
	case "wrap_column":
		return strconv.Itoa(eff.WrapColumn), true
	case "page_view":
		return onOff(eff.PageView), true
	case "page_width":
		return strconv.Itoa(eff.PageWidth), true
	case "show_whitespace_tabs":
		return onOff(eff.Whitespace.TabsLeading), true
	case "format_on_save":
		return onOff(eff.FormatOnSave), true
	case "comment_prefix":
		return eff.CommentPrefix, true
	case "word_characters":
		return eff.WordCharacters, true
	case "indentation_guide":
		return onOff(eff.IndentationGuide), true
	case "textmate_grammar":
		return eff.TextmateGrammar, true
	}
	return "", false
}

// LanguageStructuredValues are the structured per-language value names the
// Settings page edits besides the scalar overrides, in page order.
func LanguageStructuredValues() []string {
	return []string{"extensions", "filenames", "formatter", "on_save", "indent"}
}

// KnownLanguageIDs returns the language ids gvim knows by default, plus "text",
// sorted. The Settings page offers them when a language entry is added.
func KnownLanguageIDs() []string {
	ids := make([]string, 0, len(builtinLanguages)+1)
	for id := range builtinLanguages {
		ids = append(ids, id)
	}
	ids = append(ids, textLanguage)
	slices.Sort(ids)
	return ids
}

// StatusBarElementNames returns the element names editor.status_bar.left and
// .right accept, without their braces, sorted. It is the same set the config
// load validates against.
func StatusBarElementNames() []string {
	names := make([]string, 0, len(statusBarElementNames))
	for name := range statusBarElementNames {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// RemovablePath reports whether the Settings page may reset path: delete the key
// from a layer so a lower layer or the built-in default provides the value. It
// accepts the scalar schema keys and the entries of the structured sections the
// page edits (formatters, languages, terminal.shell, the status-bar lists).
func RemovablePath(path []string) bool {
	if len(path) == 0 {
		return false
	}
	if _, ok := SettingByKey(strings.Join(path, ".")); ok {
		return true
	}
	switch path[0] {
	case "formatters":
		return len(path) == 1 || len(path) == 2
	case "languages":
		if len(path) == 1 || len(path) == 2 {
			return true
		}
		if len(path) == 3 {
			if _, ok := languageOverrideFieldKeys[path[2]]; ok {
				return true
			}
			switch path[2] {
			case "extensions", "filenames", "formatter", "on_save", "indent":
				return true
			}
			return false
		}
		if len(path) == 4 && (path[2] == "formatter" || path[2] == "indent") {
			return true
		}
		return false
	case "terminal":
		return len(path) == 3 && path[1] == "shell" && (path[2] == "command" || path[2] == "args")
	}
	return false
}

// languagesSummary is the Languages page row's value: how many language entries
// the config sets.
func languagesSummary(c Config) string {
	if len(c.Languages) == 0 {
		return "none"
	}
	return countText(len(c.Languages), "entry")
}

// formattersSummary is the Formatters page row's value: how many commands the
// config sets.
func formattersSummary(c Config) string {
	if len(c.Formatters) == 0 {
		return "none"
	}
	return countText(len(c.Formatters), "command")
}

// statusBarSummary is the summary of one editor.status_bar list: "default" when
// the key is unset, "empty" when it is set to no elements.
func statusBarSummary(elements []string) string {
	if elements == nil {
		return "default"
	}
	if len(elements) == 0 {
		return "empty"
	}
	return countText(len(elements), "element")
}

// shellArgsSummary is the terminal.shell.args page row's value.
func shellArgsSummary(c Config) string {
	if len(c.Terminal.Shell.Args) == 0 {
		return "none"
	}
	return countText(len(c.Terminal.Shell.Args), "argument")
}

// countText is n and its noun, pluralized with a trailing s.
func countText(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// explorerWidthText is file_explorer.width as the page shows it: "30%" for a
// percentage, the column count otherwise.
func explorerWidthText(c Config) string {
	w := c.FileExplorer.Width
	if w.Percent {
		return strconv.Itoa(w.Value) + "%"
	}
	return strconv.Itoa(w.Value)
}

// encodeExplorerWidth parses the page's width: "30%" (or "30 %") is a
// percentage, a bare integer is a percentage too, as the config file reads it;
// a value in columns is written with a leading "=" exception? No: the page shows
// a column count as the same bare number, so the file's own form is what a
// number means (a percentage). To keep the column form explicit the page accepts
// "24" as columns only when it ends in "cols"?? Keep the config rule: a number
// is a percentage.
func encodeExplorerWidth(text string) (json.RawMessage, error) {
	s := strings.ReplaceAll(strings.TrimSpace(text), " ", "")
	if pct, ok := strings.CutSuffix(s, "%"); ok {
		if _, err := parseExplorerPercent(pct); err != nil {
			return nil, err
		}
		return json.Marshal(pct + "%")
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 100 {
		return nil, fmt.Errorf("must be a percent (\"30%%\") or a column count? use a percent 0-100: got %q", text)
	}
	return json.RawMessage(strconv.Itoa(n)), nil
}

// validStatusBarElementNamesText is a test helper? kept private.

// DecodeLayer decodes the body of one config layer file into its top-level
// keys, so the Settings page's structured sub-pages can show what that layer
// sets. Comments and trailing commas are accepted; a missing body is empty. A
// document that is not a JSON object is an error.
func DecodeLayer(data []byte) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	standard, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON with comments: %w", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(standard, &object); err != nil {
		return nil, fmt.Errorf("top level must be a JSON object: %w", err)
	}
	return object, nil
}
