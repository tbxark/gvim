package dialog

import "github.com/tbxark/gvim/i18n"

// dialogText resolves one dialog label through the catalogue with the English text as
// the explicit fallback: a missing catalogue entry shows English, never the bare key.
// Dialogs are constructed when opened, so the active locale applies then.
func dialogText(key, english string) string {
	return i18n.TOr("", key, english, nil)
}

// dialogTextVars is dialogText for a label with %{...} placeholders.
func dialogTextVars(key, english string, vars map[string]any) string {
	return i18n.TOr("", key, english, vars)
}
