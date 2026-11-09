package app

import "github.com/tbxark/gvim/i18n"

// appText resolves a user-visible app string through the catalogue with the English
// text as the explicit fallback (missing translations show English, never the key).
func appText(key, english string) string { return i18n.TOr("", key, english, nil) }

// appTextVars is appText for a string with %{...} placeholders.
func appTextVars(key, english string, vars map[string]any) string {
	return i18n.TOr("", key, english, vars)
}
