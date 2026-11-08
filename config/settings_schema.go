package config

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// SettingKind is the type of value one Settings UI row edits.
type SettingKind string

// The kinds of setting the Settings UI edits.
const (
	SettingBool   SettingKind = "bool"
	SettingInt    SettingKind = "int"
	SettingEnum   SettingKind = "enum"
	SettingString SettingKind = "string"
)

// SettingApply says when a saved value takes effect.
type SettingApply string

// The apply times. A live value is pushed into the running editor at save; a
// restart value is read only while the editor starts, so the row says so and the
// status row asks for a restart.
const (
	ApplyLive    SettingApply = "live"
	ApplyRestart SettingApply = "restart"
)

// SettingLayers is the set of config layers a setting may be written in.
type SettingLayers uint8

// The writable config layers of the Settings UI.
const (
	LayerUser SettingLayers = 1 << iota
	LayerProject
)

// Has reports whether layer is in the set.
func (l SettingLayers) Has(layer SettingLayers) bool { return l&layer != 0 }

// Setting is one scalar setting of the Settings UI: where it lives in the config
// file, how the page edits it, the layers it may be written in, and whether a
// saved value applies live or needs a restart. The config package keeps the
// table so a value is validated by the same option tables a real config load
// uses; the page itself gets plain data from the app (see internal/tui/ports.go).
type Setting struct {
	Path    []string // the keys the value is written under, for example {"editor","tab_size"}
	Section string   // the group the page lists the row in
	Kind    SettingKind
	Choices []string // the values of an enum
	// ChoicesFrom names the values the app supplies for an enum whose choices are
	// not config's: "themes" and "keymaps". The config package is a leaf, so it
	// cannot import theme or keymap; the app fills those two in.
	ChoicesFrom string
	Min, Max    int // the bounds of an int; Max 0 means no upper bound
	Layers      SettingLayers
	Apply       SettingApply

	value  func(Config) string
	encode func(string) (json.RawMessage, error)
}

// Key is the dotted name of the setting, for example "editor.tab_size". The
// Settings UI and the app's settings service exchange the key, not the path.
func (s Setting) Key() string { return strings.Join(s.Path, ".") }

// Name is the last element of the path: the label the Settings page shows.
func (s Setting) Name() string { return s.Path[len(s.Path)-1] }

// Value is the setting's current value, as the page shows it: "on"/"off" for a
// bool, the decimal for an int, and the bare text for an enum or a string.
func (s Setting) Value(c Config) string { return s.value(c) }

// Encode parses a value the page produced into the JSON to write. It checks the
// bounds and the choices; ValidateEdits checks the result again through the
// config option tables.
func (s Setting) Encode(text string) (json.RawMessage, error) { return s.encode(text) }

// The sections, in the order the page lists them.
const (
	SectionInterface  = "Interface"
	SectionDisplay    = "Display"
	SectionWhitespace = "Whitespace"
	SectionEditing    = "Editing"
	SectionSearch     = "Search"
	SectionExplorer   = "Explorer"
	SectionSession    = "Session"
	SectionMouse      = "Mouse"
)

// settingsTable is the Settings UI's schema, in section order. It covers the
// scalar settings gvim reads today except the ones with no live effect yet (the
// tool entries such as the editors listed in docs/settings.md). The page shows
// the effective value and writes the edited keys to the chosen layer.
var settingsTable = []Setting{
	// Interface.
	{Path: []string{"theme"}, Section: SectionInterface, Kind: SettingEnum, ChoicesFrom: "themes",
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: func(c Config) string { return c.Theme }, encode: encodeText},
	{Path: []string{"keymap"}, Section: SectionInterface, Kind: SettingEnum, ChoicesFrom: "keymaps",
		Layers: LayerUser, Apply: ApplyLive, // the project layer cannot set the keymap (C11)
		value: func(c Config) string { return c.Keymap }, encode: encodeText},
	{Path: []string{"ui_icons"}, Section: SectionInterface, Kind: SettingEnum,
		Choices: []string{UIIconsAuto, UIIconsPlain, UIIconsNerd},
		Layers:  LayerUser | LayerProject, Apply: ApplyLive,
		value: func(c Config) string { return c.UIIcons }, encode: encodeEnum([]string{UIIconsAuto, UIIconsPlain, UIIconsNerd})},
	{Path: []string{"ui_icons_font"}, Section: SectionInterface, Kind: SettingString,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: func(c Config) string { return c.UIIconsFont }, encode: encodeString(true)},
	{Path: []string{"editor", "show_menu_bar"}, Section: SectionInterface, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.ShowMenuBar }), encode: encodeBool},
	{Path: []string{"editor", "menu_bar_mnemonics"}, Section: SectionInterface, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.MenuBarMnemonics }), encode: encodeBool},
	{Path: []string{"editor", "show_tab_bar"}, Section: SectionInterface, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.ShowTabBar }), encode: encodeBool},
	{Path: []string{"editor", "show_status_bar"}, Section: SectionInterface, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.ShowStatusBar }), encode: encodeBool},
	{Path: []string{"editor", "show_prompt_line"}, Section: SectionInterface, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.ShowPromptLine }), encode: encodeBool},

	// Display.
	{Path: []string{"editor", "line_numbers"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.LineNumbers }), encode: encodeBool},
	{Path: []string{"editor", "relative_line_numbers"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.RelativeLineNumbers }), encode: encodeBool},
	{Path: []string{"editor", "highlight_current_line"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.HighlightCurrentLine }), encode: encodeBool},
	{Path: []string{"editor", "highlight_occurrences"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.HighlightOccurrences }), encode: encodeBool},
	{Path: []string{"editor", "hide_current_line_on_selection"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.HideCurrentLineOnSelection }), encode: encodeBool},
	{Path: []string{"editor", "highlight_current_column"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.HighlightCurrentColumn }), encode: encodeBool},
	{Path: []string{"editor", "line_wrap"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.LineWrap }), encode: encodeBool},
	{Path: []string{"editor", "wrap_indent"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.WrapIndent }), encode: encodeBool},
	{Path: []string{"editor", "syntax_highlighting"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.SyntaxHighlighting }), encode: encodeBool},
	{Path: []string{"editor", "show_tilde"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.ShowTilde }), encode: encodeBool},
	{Path: []string{"editor", "indentation_guide"}, Section: SectionDisplay, Kind: SettingEnum,
		Choices: []string{IndentationGuideNone, IndentationGuideAll, IndentationGuideActive},
		Layers:  LayerUser | LayerProject, Apply: ApplyLive,
		value:  func(c Config) string { return c.IndentationGuide },
		encode: encodeEnum([]string{IndentationGuideNone, IndentationGuideAll, IndentationGuideActive})},
	{Path: []string{"editor", "rainbow_indentation"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.RainbowIndentation }), encode: encodeBool},
	{Path: []string{"editor", "cursor_style"}, Section: SectionDisplay, Kind: SettingEnum,
		Choices: []string{CursorStyleBlock, CursorStyleBar, CursorStyleUnderline},
		Layers:  LayerUser | LayerProject, Apply: ApplyLive,
		value:  func(c Config) string { return c.CursorStyle },
		encode: encodeEnum([]string{CursorStyleBlock, CursorStyleBar, CursorStyleUnderline})},
	{Path: []string{"editor", "show_vertical_scrollbar"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.ShowVerticalScrollbar }), encode: encodeBool},
	{Path: []string{"editor", "show_horizontal_scrollbar"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.ShowHorizontalScrollbar }), encode: encodeBool},
	{Path: []string{"editor", "smooth_scroll"}, Section: SectionDisplay, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.SmoothScroll }), encode: encodeBool},

	// Whitespace.
	{Path: []string{"editor", "whitespace_show"}, Section: SectionWhitespace, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.ShowWhitespace }), encode: encodeBool},

	// Editing.
	{Path: []string{"editor", "use_tabs"}, Section: SectionEditing, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return !c.InsertSpaces }), encode: encodeBool},
	{Path: []string{"editor", "tab_size"}, Section: SectionEditing, Kind: SettingInt,
		Min: MinTabSize, Max: MaxTabSize,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: intValue(func(c Config) int { return c.TabSize }), encode: encodeInt(MinTabSize, MaxTabSize)},
	{Path: []string{"editor", "auto_indent"}, Section: SectionEditing, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.AutoIndent }), encode: encodeBool},
	{Path: []string{"editor", "auto_close"}, Section: SectionEditing, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.AutoClose }), encode: encodeBool},
	{Path: []string{"editor", "auto_surround"}, Section: SectionEditing, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.AutoSurround }), encode: encodeBool},
	{Path: []string{"editor", "virtual_space"}, Section: SectionEditing, Kind: SettingEnum,
		Choices: []string{VirtualSpaceOff, VirtualSpaceBlock, VirtualSpaceOn},
		Layers:  LayerUser | LayerProject, Apply: ApplyRestart,
		value:  func(c Config) string { return c.VirtualSpace },
		encode: encodeEnum([]string{VirtualSpaceOff, VirtualSpaceBlock, VirtualSpaceOn})},
	{Path: []string{"editor", "scroll_offset"}, Section: SectionEditing, Kind: SettingInt, Min: 0,
		Layers: LayerUser | LayerProject, Apply: ApplyRestart,
		value: intValue(func(c Config) int { return c.ScrollOffset }), encode: encodeInt(0, 0)},
	{Path: []string{"editor", "default_line_ending"}, Section: SectionEditing, Kind: SettingEnum,
		Choices: []string{LineEndingAuto, LineEndingLF, LineEndingCRLF, LineEndingCR},
		Layers:  LayerUser | LayerProject, Apply: ApplyRestart,
		value:  func(c Config) string { return c.LineEnding },
		encode: encodeEnum([]string{LineEndingAuto, LineEndingLF, LineEndingCRLF, LineEndingCR})},
	{Path: []string{"editor", "trim_trailing_whitespace_on_save"}, Section: SectionEditing, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyRestart,
		value: boolValue(func(c Config) bool { return c.TrimTrailingWhitespace }), encode: encodeBool},
	{Path: []string{"editor", "ensure_final_newline_on_save"}, Section: SectionEditing, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyRestart,
		value: boolValue(func(c Config) bool { return c.EnsureFinalNewline }), encode: encodeBool},
	{Path: []string{"editor", "auto_read_only"}, Section: SectionEditing, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyRestart,
		value: boolValue(func(c Config) bool { return c.AutoReadOnly }), encode: encodeBool},

	// Search.
	{Path: []string{"editor", "search", "case_sensitive"}, Section: SectionSearch, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.Search.CaseSensitive }), encode: encodeBool},
	{Path: []string{"editor", "search", "whole_word"}, Section: SectionSearch, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.Search.WholeWord }), encode: encodeBool},
	{Path: []string{"editor", "search", "regex"}, Section: SectionSearch, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.Search.Regex }), encode: encodeBool},
	{Path: []string{"editor", "search", "confirm_each"}, Section: SectionSearch, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.Search.ConfirmEach }), encode: encodeBool},

	// Explorer.
	{Path: []string{"explorer_show_hidden"}, Section: SectionExplorer, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.ExplorerShowHidden }), encode: encodeBool},
	{Path: []string{"explorer_show_ignored"}, Section: SectionExplorer, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.ExplorerShowIgnored }), encode: encodeBool},

	// Session.
	{Path: []string{"editor", "hot_exit"}, Section: SectionSession, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyRestart,
		value: boolValue(func(c Config) bool { return c.HotExit }), encode: encodeBool},
	{Path: []string{"editor", "restore_previous_session"}, Section: SectionSession, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyRestart,
		value: boolValue(func(c Config) bool { return c.RestorePreviousSession }), encode: encodeBool},
	{Path: []string{"editor", "auto_save_enabled"}, Section: SectionSession, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyRestart,
		value: boolValue(func(c Config) bool { return c.AutoSaveEnabled }), encode: encodeBool},
	{Path: []string{"editor", "auto_save_interval_secs"}, Section: SectionSession, Kind: SettingInt, Min: 0,
		Layers: LayerUser | LayerProject, Apply: ApplyRestart,
		value: intValue(func(c Config) int { return c.AutoSaveIntervalSecs }), encode: encodeInt(0, 0)},
	{Path: []string{"editor", "recovery_enabled"}, Section: SectionSession, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyRestart,
		value: boolValue(func(c Config) bool { return c.RecoveryEnabled }), encode: encodeBool},

	// Mouse.
	{Path: []string{"mouse_capture"}, Section: SectionMouse, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyRestart,
		value: boolValue(func(c Config) bool { return c.MouseCapture }), encode: encodeBool},
	{Path: []string{"editor", "mouse_hover_enabled"}, Section: SectionMouse, Kind: SettingBool,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: boolValue(func(c Config) bool { return c.MouseHover }), encode: encodeBool},
	{Path: []string{"editor", "mouse_wheel_scroll_lines"}, Section: SectionMouse, Kind: SettingInt, Min: 0,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: intValue(func(c Config) int { return c.MouseWheelScrollLines }), encode: encodeInt(0, 0)},
	{Path: []string{"editor", "mouse_hover_delay_ms"}, Section: SectionMouse, Kind: SettingInt, Min: 0,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: intValue(func(c Config) int { return c.MouseHoverDelayMS }), encode: encodeInt(0, 0)},
	{Path: []string{"editor", "double_click_time_ms"}, Section: SectionMouse, Kind: SettingInt, Min: 0,
		Layers: LayerUser | LayerProject, Apply: ApplyLive,
		value: intValue(func(c Config) int { return c.DoubleClickTimeMS }), encode: encodeInt(0, 0)},
}

// settingsByKey indexes settingsTable by dotted key.
var settingsByKey = func() map[string]Setting {
	m := make(map[string]Setting, len(settingsTable))
	for _, s := range settingsTable {
		m[s.Key()] = s
	}
	return m
}()

// Settings returns the schema of the Settings UI, in section order.
func Settings() []Setting { return slices.Clone(settingsTable) }

// SettingByKey returns the setting whose dotted key is key.
func SettingByKey(key string) (Setting, bool) {
	s, ok := settingsByKey[key]
	return s, ok
}

// boolValue returns a Value func for a boolean setting.
func boolValue(get func(Config) bool) func(Config) string {
	return func(c Config) string { return onOff(get(c)) }
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// intValue returns a Value func for an integer setting.
func intValue(get func(Config) int) func(Config) string {
	return func(c Config) string { return strconv.Itoa(get(c)) }
}

// encodeBool parses the page's "on"/"off" (or true/false) into a JSON bool.
func encodeBool(text string) (json.RawMessage, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "on", "true", "yes", "1":
		return json.RawMessage("true"), nil
	case "off", "false", "no", "0":
		return json.RawMessage("false"), nil
	}
	return nil, fmt.Errorf("must be on or off, got %q", text)
}

// encodeInt parses an integer in [lo, hi]; hi 0 means no upper bound.
func encodeInt(lo, hi int) func(string) (json.RawMessage, error) {
	return func(text string) (json.RawMessage, error) {
		n, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil {
			return nil, fmt.Errorf("must be an integer, got %q", text)
		}
		if n < lo || (hi > 0 && n > hi) {
			if hi > 0 {
				return nil, fmt.Errorf("must be between %d and %d, got %d", lo, hi, n)
			}
			return nil, fmt.Errorf("must be at least %d, got %d", lo, n)
		}
		return json.RawMessage(strconv.Itoa(n)), nil
	}
}

// encodeEnum parses one of choices.
func encodeEnum(choices []string) func(string) (json.RawMessage, error) {
	return func(text string) (json.RawMessage, error) {
		for _, choice := range choices {
			if text == choice {
				return json.Marshal(choice)
			}
		}
		return nil, fmt.Errorf("must be %s, got %q", quotedList(choices), text)
	}
}

// encodeText parses a string value. An empty value is refused unless emptyOK.
func encodeText(text string) (json.RawMessage, error) { return encodeString(false)(text) }

func encodeString(emptyOK bool) func(string) (json.RawMessage, error) {
	return func(text string) (json.RawMessage, error) {
		text = strings.TrimSpace(text)
		if text == "" && !emptyOK {
			return nil, fmt.Errorf("must not be empty")
		}
		return json.Marshal(text)
	}
}
