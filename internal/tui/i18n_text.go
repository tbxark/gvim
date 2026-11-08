package tui

import "github.com/tbxark/gvim/i18n"

// uiText resolves one user-visible TUI string through the catalogue with the English text
// as the explicit fallback: an untranslated or gvim-only key shows English, never the bare
// key. Display code keeps its literal English next to the key, so behavior in English is
// unchanged and future owners can see both forms together.
func uiText(key, english string) string {
	return i18n.TOr("", key, english, nil)
}

// uiTextVars is uiText for a string with %{...} placeholders.
func uiTextVars(key, english string, vars map[string]any) string {
	return i18n.TOr("", key, english, vars)
}
