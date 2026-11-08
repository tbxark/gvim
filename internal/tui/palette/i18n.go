package palette

import "github.com/tbxark/gvim/i18n"

// paletteText resolves one palette chrome string through the catalogue with the English
// text as the explicit fallback (an untranslated key shows English, never the key).
func paletteText(key, english string) string { return i18n.TOr("", key, english, nil) }

// paletteTextVars is paletteText for a string with %{...} placeholders.
func paletteTextVars(key, english string, vars map[string]any) string {
	return i18n.TOr("", key, english, vars)
}
