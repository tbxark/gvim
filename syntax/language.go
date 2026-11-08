package syntax

import "strings"

// IsPlain reports whether a lexer name stands for "no language".
func IsPlain(name string) bool {
	return name == "fallback" || name == "plaintext" || name == "text only"
}

// FileLanguage returns the language of the file at path as the highlighter names
// it, in lower case ("go" for main.go), or "" for plain text. It reads the name
// only.
func FileLanguage(path string) string {
	if n := strings.ToLower(Detect(path, nil).Config().Name); n != "" && !IsPlain(n) {
		return n
	}
	return ""
}

// languageCache bounds the file names whose language CachedFileLanguage remembers.
const languageCache = 1024

// CachedFileLanguage returns FileLanguage with its results remembered per path,
// because the editor asks for the language of the active file on every menu
// check. The map is cleared when it grows past languageCache names. The function
// is not safe for concurrent use: the editor calls it from the update loop only.
func CachedFileLanguage() func(path string) string {
	known := make(map[string]string)
	return func(path string) string {
		if lang, ok := known[path]; ok {
			return lang
		}
		if len(known) >= languageCache {
			clear(known)
		}
		lang := FileLanguage(path)
		known[path] = lang
		return lang
	}
}
