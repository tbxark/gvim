package theme

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateForm checks the syntax of a theme value from the command line or a
// config file without touching the file system: a bundled name, builtin://name,
// a path relative to the user theme dir, or file:// with an absolute path.
// Whether the theme exists is Load's business.
func ValidateForm(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("the theme value is empty")
	}
	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		return fmt.Errorf("%q: URL themes are not supported (save the file locally and use a name, a path relative to the user theme dir, or file://)", ref)
	}
	if name, ok := strings.CutPrefix(ref, "builtin://"); ok {
		if name == "" {
			return fmt.Errorf("%q: builtin:// needs a theme name", ref)
		}
		return nil
	}
	if path, ok := strings.CutPrefix(ref, "file://"); ok {
		path = expandEnv(path)
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%q: file:// needs an absolute path (environment variables are expanded)", ref)
		}
		return nil
	}
	if ref == "." || ref == ".." || strings.HasPrefix(ref, "../") || strings.HasPrefix(ref, "/") {
		return fmt.Errorf("%q: a theme path must stay inside the user theme dir (use file:// for an absolute path)", ref)
	}
	return nil
}

// IsNamedForm reports whether ref is a plain theme name rather than a path or a
// scheme, so a caller can check it against the bundled names.
func IsNamedForm(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.ContainsAny(ref, "/\\") || strings.Contains(ref, "://") {
		return false
	}
	return filepath.Ext(ref) == ""
}
