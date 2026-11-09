package uiadapter

import "github.com/tbxark/gvim/i18n"

// uiadapterText resolves a user-visible adapter string through the catalogue with the
// English text as the explicit fallback (missing translations show English, never the key).
func uiadapterText(key, english string) string { return i18n.TOr("", key, english, nil) }

// uiadapterTextVars is uiadapterText for a string with %{...} placeholders.
func uiadapterTextVars(key, english string, vars map[string]any) string {
	return i18n.TOr("", key, english, vars)
}
