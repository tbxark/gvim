package browser

import "github.com/tbxark/gvim/i18n"

// browserText resolves a browser chrome string through the catalogue with the English
// text as the explicit fallback (missing translations still show English, never the key).
func browserText(key, english string) string { return i18n.TOr("", key, english, nil) }

// browserTextVars is browserText for a string with %{...} placeholders.
func browserTextVars(key, english string, vars map[string]any) string {
	return i18n.TOr("", key, english, vars)
}
