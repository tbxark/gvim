package theme

// Ported from refs/fresh crates/fresh-editor-core/src/theme/types.rs, the
// theme_color_keys! table, at Fresh commit 2338640274915a37786810bd8b59439c6633ba64.
// keys_table_test.go checks the required set, the fallback chains and the
// count against that source revision; update both together.

// KeyKind is what a theme key names.
type KeyKind int

const (
	// RequiredKey is a color every standalone theme must name.
	RequiredKey KeyKind = iota
	// FallbackKey is a color that names another key it falls back to.
	FallbackKey
	// OptionalKey is a color a theme may leave unset with no fallback.
	OptionalKey
)

// Key is one color key of the theme model.
type Key struct {
	Section  string
	Key      string
	Kind     KeyKind
	Fallback string // the key this one falls back to; empty for required and optional
}

// Name is the canonical "section.key" spelling.
func (k Key) Name() string { return k.Section + "." + k.Key }

// themeKeys is the ported table, in Fresh table order.
var themeKeys = []Key{
	// editor
	{Section: "editor", Key: "after_eof_bg", Kind: FallbackKey, Fallback: "editor.bg"},
	{Section: "editor", Key: "bg", Kind: RequiredKey, Fallback: ""},
	{Section: "editor", Key: "current_line_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "editor", Key: "cursor", Kind: RequiredKey, Fallback: ""},
	{Section: "editor", Key: "diff_add_bg", Kind: FallbackKey, Fallback: "diagnostic.info_bg"},
	{Section: "editor", Key: "diff_add_collision_fg", Kind: OptionalKey, Fallback: ""},
	{Section: "editor", Key: "diff_add_highlight_bg", Kind: FallbackKey, Fallback: "editor.diff_add_bg"},
	{Section: "editor", Key: "diff_modify_bg", Kind: FallbackKey, Fallback: "diagnostic.warning_bg"},
	{Section: "editor", Key: "diff_modify_collision_fg", Kind: OptionalKey, Fallback: ""},
	{Section: "editor", Key: "diff_remove_bg", Kind: FallbackKey, Fallback: "diagnostic.error_bg"},
	{Section: "editor", Key: "diff_remove_collision_fg", Kind: OptionalKey, Fallback: ""},
	{Section: "editor", Key: "diff_remove_highlight_bg", Kind: FallbackKey, Fallback: "editor.diff_remove_bg"},
	{Section: "editor", Key: "fg", Kind: RequiredKey, Fallback: ""},
	{Section: "editor", Key: "inactive_cursor", Kind: FallbackKey, Fallback: "editor.line_number_fg"},
	{Section: "editor", Key: "indentation_guide_fg", Kind: FallbackKey, Fallback: "editor.whitespace_indicator_fg"},
	{Section: "editor", Key: "indent_rainbow_1", Kind: FallbackKey, Fallback: "editor.indentation_guide_fg"},
	{Section: "editor", Key: "indent_rainbow_2", Kind: FallbackKey, Fallback: "editor.indentation_guide_fg"},
	{Section: "editor", Key: "indent_rainbow_3", Kind: FallbackKey, Fallback: "editor.indentation_guide_fg"},
	{Section: "editor", Key: "indent_rainbow_4", Kind: FallbackKey, Fallback: "editor.indentation_guide_fg"},
	{Section: "editor", Key: "indent_rainbow_5", Kind: FallbackKey, Fallback: "editor.indentation_guide_fg"},
	{Section: "editor", Key: "indent_rainbow_6", Kind: FallbackKey, Fallback: "editor.indentation_guide_fg"},
	{Section: "editor", Key: "line_number_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "editor", Key: "line_number_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "editor", Key: "ruler_bg", Kind: FallbackKey, Fallback: "editor.current_line_bg"},
	{Section: "editor", Key: "selection_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "editor", Key: "whitespace_indicator_fg", Kind: FallbackKey, Fallback: "editor.line_number_fg"},
	{Section: "editor", Key: "whitespace_indicator_selected_fg", Kind: FallbackKey, Fallback: "editor.whitespace_indicator_fg"},
	{Section: "editor", Key: "bracket_match_fg", Kind: FallbackKey, Fallback: "editor.cursor"},
	{Section: "editor", Key: "bracket_rainbow_1", Kind: FallbackKey, Fallback: "syntax.keyword"},
	{Section: "editor", Key: "bracket_rainbow_2", Kind: FallbackKey, Fallback: "syntax.function"},
	{Section: "editor", Key: "bracket_rainbow_3", Kind: FallbackKey, Fallback: "syntax.type"},
	{Section: "editor", Key: "bracket_rainbow_4", Kind: FallbackKey, Fallback: "syntax.string"},
	{Section: "editor", Key: "bracket_rainbow_5", Kind: FallbackKey, Fallback: "syntax.constant"},
	{Section: "editor", Key: "bracket_rainbow_6", Kind: FallbackKey, Fallback: "syntax.variable"},
	// ui
	{Section: "ui", Key: "blame_header_bg", Kind: FallbackKey, Fallback: "ui.menu_bg"},
	{Section: "ui", Key: "blame_header_fg", Kind: FallbackKey, Fallback: "ui.menu_fg"},
	{Section: "ui", Key: "compose_margin_bg", Kind: FallbackKey, Fallback: "editor.after_eof_bg"},
	{Section: "ui", Key: "file_explorer_pinned_bg", Kind: FallbackKey, Fallback: "editor.current_line_bg"},
	{Section: "ui", Key: "file_status_added_fg", Kind: FallbackKey, Fallback: "diagnostic.info_fg"},
	{Section: "ui", Key: "file_status_conflicted_fg", Kind: FallbackKey, Fallback: "diagnostic.error_fg"},
	{Section: "ui", Key: "file_status_deleted_fg", Kind: FallbackKey, Fallback: "diagnostic.error_fg"},
	{Section: "ui", Key: "file_status_modified_fg", Kind: FallbackKey, Fallback: "diagnostic.warning_fg"},
	{Section: "ui", Key: "file_status_renamed_fg", Kind: FallbackKey, Fallback: "diagnostic.info_fg"},
	{Section: "ui", Key: "file_status_untracked_fg", Kind: FallbackKey, Fallback: "diagnostic.hint_fg"},
	{Section: "ui", Key: "help_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "help_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "help_indicator_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "help_indicator_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "help_key_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "help_separator_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "inline_code_bg", Kind: FallbackKey, Fallback: "editor.current_line_bg"},
	{Section: "ui", Key: "list_drop_target_bg", Kind: FallbackKey, Fallback: "ui.tab_drop_zone_bg"},
	{Section: "ui", Key: "menu_active_bg", Kind: FallbackKey, Fallback: "ui.popup_selection_bg"},
	{Section: "ui", Key: "menu_active_fg", Kind: FallbackKey, Fallback: "ui.popup_selection_fg"},
	{Section: "ui", Key: "menu_bg", Kind: FallbackKey, Fallback: "ui.tab_inactive_bg"},
	{Section: "ui", Key: "menu_border_fg", Kind: FallbackKey, Fallback: "ui.popup_border_fg"},
	{Section: "ui", Key: "menu_disabled_bg", Kind: FallbackKey, Fallback: "ui.menu_dropdown_bg"},
	{Section: "ui", Key: "menu_disabled_fg", Kind: FallbackKey, Fallback: "editor.line_number_fg"},
	{Section: "ui", Key: "menu_dropdown_bg", Kind: FallbackKey, Fallback: "ui.popup_bg"},
	{Section: "ui", Key: "menu_dropdown_fg", Kind: FallbackKey, Fallback: "ui.popup_text_fg"},
	{Section: "ui", Key: "menu_fg", Kind: FallbackKey, Fallback: "ui.tab_inactive_fg"},
	{Section: "ui", Key: "menu_highlight_bg", Kind: FallbackKey, Fallback: "ui.popup_selection_bg"},
	{Section: "ui", Key: "menu_highlight_fg", Kind: FallbackKey, Fallback: "ui.popup_selection_fg"},
	{Section: "ui", Key: "menu_hover_bg", Kind: FallbackKey, Fallback: "ui.menu_highlight_bg"},
	{Section: "ui", Key: "menu_hover_fg", Kind: FallbackKey, Fallback: "ui.menu_highlight_fg"},
	{Section: "ui", Key: "menu_separator_fg", Kind: FallbackKey, Fallback: "ui.menu_border_fg"},
	{Section: "ui", Key: "popup_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "popup_border_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "popup_selection_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "popup_selection_fg", Kind: FallbackKey, Fallback: "ui.popup_text_fg"},
	{Section: "ui", Key: "popup_text_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "prompt_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "prompt_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "prompt_selection_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "prompt_selection_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "scrollbar_thumb_fg", Kind: FallbackKey, Fallback: "editor.line_number_fg"},
	{Section: "ui", Key: "scrollbar_thumb_hover_fg", Kind: FallbackKey, Fallback: "ui.scrollbar_thumb_fg"},
	{Section: "ui", Key: "scrollbar_track_fg", Kind: FallbackKey, Fallback: "editor.current_line_bg"},
	{Section: "ui", Key: "scrollbar_track_hover_fg", Kind: FallbackKey, Fallback: "ui.scrollbar_track_fg"},
	{Section: "ui", Key: "semantic_highlight_bg", Kind: FallbackKey, Fallback: "editor.current_line_bg"},
	{Section: "ui", Key: "settings_selected_bg", Kind: FallbackKey, Fallback: "ui.popup_selection_bg"},
	{Section: "ui", Key: "settings_selected_fg", Kind: FallbackKey, Fallback: "ui.popup_selection_fg"},
	{Section: "ui", Key: "split_separator_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "split_separator_hover_fg", Kind: FallbackKey, Fallback: "ui.split_separator_fg"},
	{Section: "ui", Key: "status_bar_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "status_bar_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "status_error_indicator_bg", Kind: FallbackKey, Fallback: "diagnostic.error_bg"},
	{Section: "ui", Key: "status_error_indicator_fg", Kind: FallbackKey, Fallback: "diagnostic.error_fg"},
	{Section: "ui", Key: "status_error_indicator_hover_bg", Kind: FallbackKey, Fallback: "ui.status_error_indicator_bg"},
	{Section: "ui", Key: "status_error_indicator_hover_fg", Kind: FallbackKey, Fallback: "ui.status_error_indicator_fg"},
	{Section: "ui", Key: "status_lsp_actionable_bg", Kind: FallbackKey, Fallback: "ui.status_warning_indicator_bg"},
	{Section: "ui", Key: "status_lsp_actionable_fg", Kind: FallbackKey, Fallback: "ui.status_warning_indicator_fg"},
	{Section: "ui", Key: "status_lsp_on_bg", Kind: FallbackKey, Fallback: "ui.status_bar_bg"},
	{Section: "ui", Key: "status_lsp_on_fg", Kind: FallbackKey, Fallback: "ui.status_bar_fg"},
	{Section: "ui", Key: "status_palette_bg", Kind: FallbackKey, Fallback: "ui.status_bar_bg"},
	{Section: "ui", Key: "status_palette_fg", Kind: FallbackKey, Fallback: "ui.status_bar_fg"},
	{Section: "ui", Key: "status_separator_bg", Kind: FallbackKey, Fallback: "ui.status_bar_bg"},
	{Section: "ui", Key: "status_separator_fg", Kind: FallbackKey, Fallback: "ui.status_bar_fg"},
	{Section: "ui", Key: "status_warning_indicator_bg", Kind: FallbackKey, Fallback: "diagnostic.warning_bg"},
	{Section: "ui", Key: "status_warning_indicator_fg", Kind: FallbackKey, Fallback: "diagnostic.warning_fg"},
	{Section: "ui", Key: "status_warning_indicator_hover_bg", Kind: FallbackKey, Fallback: "ui.status_warning_indicator_bg"},
	{Section: "ui", Key: "status_warning_indicator_hover_fg", Kind: FallbackKey, Fallback: "ui.status_warning_indicator_fg"},
	{Section: "ui", Key: "suggestion_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "suggestion_fg", Kind: FallbackKey, Fallback: "ui.popup_text_fg"},
	{Section: "ui", Key: "suggestion_selected_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "tab_active_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "tab_active_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "tab_close_hover_fg", Kind: FallbackKey, Fallback: "diagnostic.error_fg"},
	{Section: "ui", Key: "tab_drop_zone_bg", Kind: FallbackKey, Fallback: "editor.selection_bg"},
	{Section: "ui", Key: "tab_drop_zone_border", Kind: FallbackKey, Fallback: "ui.tab_active_fg"},
	{Section: "ui", Key: "tab_hover_bg", Kind: FallbackKey, Fallback: "ui.tab_inactive_bg"},
	{Section: "ui", Key: "tab_inactive_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "tab_inactive_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "tab_separator_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "ui", Key: "terminal_bg", Kind: FallbackKey, Fallback: "editor.bg"},
	{Section: "ui", Key: "terminal_fg", Kind: FallbackKey, Fallback: "editor.fg"},
	{Section: "ui", Key: "text_input_selection_bg", Kind: FallbackKey, Fallback: "ui.popup_selection_bg"},
	{Section: "ui", Key: "tour_step_bg", Kind: FallbackKey, Fallback: "ui.popup_selection_bg"},
	// syntax
	{Section: "syntax", Key: "comment", Kind: RequiredKey, Fallback: ""},
	{Section: "syntax", Key: "constant", Kind: RequiredKey, Fallback: ""},
	{Section: "syntax", Key: "function", Kind: RequiredKey, Fallback: ""},
	{Section: "syntax", Key: "keyword", Kind: RequiredKey, Fallback: ""},
	{Section: "syntax", Key: "operator", Kind: RequiredKey, Fallback: ""},
	{Section: "syntax", Key: "punctuation_bracket", Kind: FallbackKey, Fallback: "syntax.operator"},
	{Section: "syntax", Key: "punctuation_delimiter", Kind: FallbackKey, Fallback: "syntax.operator"},
	{Section: "syntax", Key: "string", Kind: RequiredKey, Fallback: ""},
	{Section: "syntax", Key: "type", Kind: RequiredKey, Fallback: ""},
	{Section: "syntax", Key: "variable", Kind: RequiredKey, Fallback: ""},
	{Section: "syntax", Key: "variable_builtin", Kind: FallbackKey, Fallback: "syntax.keyword"},
	// diagnostic
	{Section: "diagnostic", Key: "error_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "diagnostic", Key: "error_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "diagnostic", Key: "hint_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "diagnostic", Key: "hint_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "diagnostic", Key: "info_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "diagnostic", Key: "info_fg", Kind: RequiredKey, Fallback: ""},
	{Section: "diagnostic", Key: "warning_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "diagnostic", Key: "warning_fg", Kind: RequiredKey, Fallback: ""},
	// search
	{Section: "search", Key: "current_match_bg", Kind: FallbackKey, Fallback: "editor.selection_bg"},
	{Section: "search", Key: "current_match_fg", Kind: FallbackKey, Fallback: "editor.fg"},
	{Section: "search", Key: "label_bg", Kind: FallbackKey, Fallback: "syntax.keyword"},
	{Section: "search", Key: "label_fg", Kind: FallbackKey, Fallback: "editor.bg"},
	{Section: "search", Key: "match_bg", Kind: RequiredKey, Fallback: ""},
	{Section: "search", Key: "match_fg", Kind: RequiredKey, Fallback: ""},

	// gvim extension keys: not in Fresh's table. They name the one accent color
	// the gvim chrome uses; a theme without them takes the popup selection
	// colors, so a Fresh theme loads and paints.
	{Section: "ui", Key: "accent_bg", Kind: FallbackKey, Fallback: "ui.popup_selection_bg"},
	{Section: "ui", Key: "accent_fg", Kind: FallbackKey, Fallback: "ui.popup_selection_fg"},
}

// themeKeysByName indexes themeKeys by canonical name.
var themeKeysByName = func() map[string]Key {
	m := make(map[string]Key, len(themeKeys))
	for _, k := range themeKeys {
		m[k.Name()] = k
	}
	return m
}()

// modifierOnlyKeys are the keys that name text attributes alone: they have no
// color of their own.
var modifierOnlyKeys = []string{
	"editor.selection_modifier",
	"ui.semantic_highlight_modifier",
	"editor.bracket_rainbow_match_modifier",
}

// keyAliases are Fresh's historical spellings mapped to their canonical key.
var keyAliases = map[string]string{
	"ui.popup_fg": "ui.popup_text_fg",
}

// defaultModifierOnly is the default attribute list of a modifier-only key a
// theme leaves unset.
var defaultModifierOnly = map[string][]string{
	"editor.bracket_rainbow_match_modifier": {"bold", "underlined"},
}

// Keys returns every color key, in Fresh table order.
func Keys() []Key { return append([]Key(nil), themeKeys...) }

// KeyByName returns the color key called name. It accepts the canonical
// "section.key" spelling and Fresh's aliases.
func KeyByName(name string) (Key, bool) {
	if alias, ok := keyAliases[name]; ok {
		name = alias
	}
	k, ok := themeKeysByName[name]
	return k, ok
}

// RequiredKeys returns the names of the required color keys, in table order.
func RequiredKeys() []string {
	out := make([]string, 0, 49)
	for _, k := range themeKeys {
		if k.Kind == RequiredKey {
			out = append(out, k.Name())
		}
	}
	return out
}

// ModifierOnlyKeys returns the attribute-only theme keys.
func ModifierOnlyKeys() []string { return append([]string(nil), modifierOnlyKeys...) }

// CanonicalKey returns the canonical spelling of name and whether it is a
// theme key of either kind.
func CanonicalKey(name string) (string, bool) {
	if alias, ok := keyAliases[name]; ok {
		name = alias
	}
	if _, ok := themeKeysByName[name]; ok {
		return name, true
	}
	for _, k := range modifierOnlyKeys {
		if k == name {
			return name, true
		}
	}
	return name, false
}
