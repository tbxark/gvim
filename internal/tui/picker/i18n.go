package picker

import "github.com/tbxark/gvim/i18n"

// pickerText resolves a picker chrome string through the catalogue with the English
// text as the explicit fallback (missing translations still show English, never the key).
func pickerText(key, english string) string { return i18n.TOr("", key, english, nil) }
