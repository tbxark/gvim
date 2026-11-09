package dock

import "github.com/tbxark/gvim/i18n"

// dockText resolves one dock chrome string through the catalogue with the English text
// as the explicit fallback (missing translations show English, never the key).
func dockText(key, english string) string { return i18n.TOr("", key, english, nil) }

// dockTextVars is dockText for a string with %{...} placeholders.
func dockTextVars(key, english string, vars map[string]any) string {
	return i18n.TOr("", key, english, vars)
}
