package list

import "github.com/tbxark/gvim/i18n"

// listText resolves a list chrome string through the catalogue with the English text as
// the explicit fallback (missing translations still show English, never the key).
func listText(key, english string) string { return i18n.TOr("", key, english, nil) }
