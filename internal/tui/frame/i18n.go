package frame

import "github.com/tbxark/gvim/i18n"

// frameText resolves one status-row label through the catalogue with the English text
// as the explicit fallback (missing translations show English, never the key).
func frameText(key, english string) string { return i18n.TOr("", key, english, nil) }
