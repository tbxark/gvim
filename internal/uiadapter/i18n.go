package uiadapter

import "github.com/tbxark/gvim/i18n"

// uiadapterTextVars resolves a user-visible adapter string with %{...} placeholders
// through the catalogue with the English text as the explicit fallback (missing
// translations show English, never the key).
func uiadapterTextVars(key, english string, vars map[string]any) string {
	return i18n.TOr("", key, english, vars)
}
