package theme

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// ValidateForm checks the syntax of a theme value from the command line or a
// config file without touching the file system: a bundled name, builtin://name,
// a path relative to the user theme dir, file:// with an absolute path, or an
// http(s) URL pointing at a theme JSON file. Whether the theme exists (or the URL
// is reachable and allowed by the transport policy) is Load's/app's business.
func ValidateForm(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("the theme value is empty")
	}
	if IsURLForm(ref) {
		u, err := url.Parse(ref)
		if err != nil || u.Host == "" {
			return fmt.Errorf("%q: a theme URL must be absolute", ref)
		}
		if u.Fragment != "" {
			return fmt.Errorf("%q: URL-packaged repository themes (#name) are not supported yet; point the theme value at the theme's JSON URL", ref)
		}
		return nil
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
	if strings.Contains(ref, "://") {
		return fmt.Errorf("%q: unsupported theme URL scheme (only http(s) URLs are supported)", ref)
	}
	if ref == "." || ref == ".." || strings.HasPrefix(ref, "../") || strings.HasPrefix(ref, "/") {
		return fmt.Errorf("%q: a theme path must stay inside the user theme dir (use file:// for an absolute path)", ref)
	}
	return nil
}

// IsURLForm reports whether ref is an http(s) URL theme value. The transport
// policy (https, or http only on the loopback interface) is applied by the app
// when it fetches the theme; the form itself only checks the syntax.
func IsURLForm(ref string) bool {
	ref = strings.TrimSpace(ref)
	return strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://")
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
