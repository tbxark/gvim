package app

import "github.com/tbxark/gvim/i18n"

// appTextVars is appText for a string with %{...} placeholders.
func appTextVars(key, english string, vars map[string]any) string {
	return i18n.TOr("", key, english, vars)
}
