package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Info describes one theme a registry offers.
type Info struct {
	Name    string // display name, the file name without .json for a user theme
	Ref     string // the portable value a config file writes: a builtin name, or a user file relative to the user theme dir
	Builtin bool
	Path    string // absolute path of a user theme; empty for a builtin
}

// Registry is the set of available themes: the bundled ones and the JSON files
// in the user theme dir (config.Dir()/themes). The config's theme value is a ref
// this registry resolves.
//
// Fields are guarded by mu: the registry is shared by the app, the settings
// service and the TUI, and Reload is called when the user edits a theme file.
type Registry struct {
	mu      sync.Mutex
	userDir string

	loaded    bool
	builtins  map[string]Theme // resolved cache, by name
	infos     []Info           // display order, sorted by name
	userByRef map[string]Info  // ref -> user info
	warnings  []string
}

// NewRegistry returns a registry over the user theme dir userDir. An empty
// userDir means bundled themes only.
func NewRegistry(userDir string) *Registry {
	return &Registry{userDir: userDir}
}

// UserDir returns the user theme dir the registry reads.
func (r *Registry) UserDir() string { return r.userDir }

// Reload rescans the bundled names and the user theme dir and forgets the
// resolved cache, so a theme file edited on disk is read again. A broken user
// theme is a warning; it is listed but Load reports the error.
func (r *Registry) Reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scanLocked(true)
}

// ensure loads the registry once.
func (r *Registry) ensure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loaded {
		return nil
	}
	return r.scanLocked(false)
}

// scanLocked rebuilds the info list. It does not parse user themes (Load does),
// so a broken file only warns when someone opens it.
func (r *Registry) scanLocked(resetCache bool) error {
	if resetCache {
		r.builtins = nil
	}
	r.infos = nil
	r.userByRef = make(map[string]Info)
	r.warnings = nil
	for _, name := range EmbeddedNames() {
		r.infos = append(r.infos, Info{Name: name, Ref: name, Builtin: true})
	}
	if r.userDir != "" {
		paths, warnings := userThemeFiles(r.userDir)
		r.warnings = append(r.warnings, warnings...)
		for _, path := range paths {
			rel, err := filepath.Rel(r.userDir, path)
			if err != nil {
				continue
			}
			ref := filepath.ToSlash(rel)
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			info := Info{Name: name, Ref: ref, Path: path}
			r.infos = append(r.infos, info)
			r.userByRef[ref] = info
		}
	}
	sort.SliceStable(r.infos, func(i, j int) bool {
		if r.infos[i].Name != r.infos[j].Name {
			return r.infos[i].Name < r.infos[j].Name
		}
		return !r.infos[i].Builtin && r.infos[j].Builtin
	})
	r.loaded = true
	return nil
}

// userThemeFiles lists the *.json files under dir, relative sorted, and the
// warnings of directories it could not read.
func userThemeFiles(dir string) (paths []string, warnings []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			warnings = append(warnings, fmt.Sprintf("themes: read %s: %v", dir, err))
		}
		return nil, warnings
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			nested, nestedWarnings := userThemeFiles(path)
			paths = append(paths, nested...)
			warnings = append(warnings, nestedWarnings...)
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths, warnings
}

// Names returns the display names of the available themes, sorted. A user theme
// and a bundled theme may share a name; both are listed.
func (r *Registry) Names() []string {
	_ = r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.infos))
	for _, info := range r.infos {
		names = append(names, info.Name)
	}
	return names
}

// Infos returns the available themes, sorted by display name.
func (r *Registry) Infos() []Info {
	_ = r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Info(nil), r.infos...)
}

// Warnings returns the problems the registry found while scanning (a directory
// it could not read). Warnings of a theme file are on the loaded theme.
func (r *Registry) Warnings() []string {
	_ = r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.warnings...)
}

// EmbeddedOnly returns the resolved bundled theme called name, using the
// registry's cache.
func (r *Registry) EmbeddedOnly(name string) (Theme, error) {
	r.mu.Lock()
	if theme, ok := r.builtins[name]; ok {
		r.mu.Unlock()
		return theme.Clone(), nil
	}
	r.mu.Unlock()
	theme, err := EmbeddedTheme(name)
	if err != nil {
		return Theme{}, err
	}
	r.mu.Lock()
	if r.builtins == nil {
		r.builtins = make(map[string]Theme)
	}
	r.builtins[name] = theme
	r.mu.Unlock()
	return theme.Clone(), nil
}

// resolveRef maps a config theme value to a builtin name or an absolute file.
func (r *Registry) resolveRef(ref string) (builtin, path string, err error) {
	_ = r.ensure()
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", fmt.Errorf("theme: the theme value is empty")
	}
	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		return "", "", fmt.Errorf("theme: %q: URL themes are not supported yet; save the theme file locally and use a name, a path relative to %s, or file://", ref, r.UserDir())
	}
	if name, ok := strings.CutPrefix(ref, "builtin://"); ok {
		if !isEmbedded(name) {
			return "", "", unknownThemeError(ref)
		}
		return name, "", nil
	}
	if path, ok := strings.CutPrefix(ref, "file://"); ok {
		path = expandEnv(path)
		path = filepath.Clean(path)
		if !filepath.IsAbs(path) {
			return "", "", fmt.Errorf("theme: %q: file:// needs an absolute path (environment variables such as HOME and XDG_CONFIG_HOME are expanded)", ref)
		}
		return "", path, nil
	}
	if isEmbedded(ref) {
		return ref, "", nil
	}
	if r.userDir == "" {
		return "", "", unknownThemeError(ref)
	}
	rel := filepath.FromSlash(ref)
	if !strings.ContainsAny(ref, "/\\") && filepath.Ext(rel) == "" {
		rel += ".json"
	}
	rel = filepath.Clean(rel)
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("theme: %q: a theme path must stay inside %s", ref, r.userDir)
	}
	path = filepath.Join(r.userDir, rel)
	if _, err := os.Stat(path); err != nil {
		return "", "", unknownThemeError(ref)
	}
	return "", path, nil
}

// Load resolves ref (a bundled name, builtin://name, a path relative to the
// user theme dir, or file:// with environment expansion) into a resolved theme.
func (r *Registry) Load(ref string) (Theme, error) {
	builtin, path, err := r.resolveRef(ref)
	if err != nil {
		return Theme{}, err
	}
	if builtin != "" {
		return r.EmbeddedOnly(builtin)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, fmt.Errorf("theme: %s: %w", path, err)
	}
	file, err := ParseFile(data)
	if err != nil {
		return Theme{}, fmt.Errorf("theme: %s: %w", filepath.Base(path), err)
	}
	if file.Name == "" {
		file.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	theme, err := resolveWith(file, func(name string) (Theme, error) { return r.EmbeddedOnly(name) })
	if err != nil {
		return Theme{}, fmt.Errorf("theme: %s: %w", filepath.Base(path), err)
	}
	return theme, nil
}

// Canonical returns the portable ref of ref: the builtin name for a bundled
// theme, the user-dir relative slash path for a user theme.
func (r *Registry) Canonical(ref string) (string, error) {
	builtin, path, err := r.resolveRef(ref)
	if err != nil {
		return "", err
	}
	if builtin != "" {
		return builtin, nil
	}
	if r.userDir != "" {
		if rel, err := filepath.Rel(r.userDir, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel), nil
		}
	}
	return "file://" + path, nil
}

// Resolve is Canonical's older name, kept for the app's validation path.
func (r *Registry) Resolve(ref string) (string, error) { return r.Canonical(ref) }

// ThemePath returns where SaveTheme(name, ...) writes.
func (r *Registry) ThemePath(name string) (string, error) {
	if r.userDir == "" {
		return "", fmt.Errorf("theme: there is no user theme dir")
	}
	if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return "", fmt.Errorf("theme: %q is not a valid theme file name", name)
	}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		name += ".json"
	}
	return filepath.Join(r.userDir, name), nil
}

// DeleteTheme removes a user theme file. A builtin theme cannot be deleted.
func (r *Registry) DeleteTheme(ref string) error {
	builtin, path, err := r.resolveRef(ref)
	if err != nil {
		return err
	}
	if builtin != "" {
		return fmt.Errorf("theme: %q is a bundled theme and cannot be deleted", ref)
	}
	if r.userDir != "" {
		if rel, err := filepath.Rel(r.userDir, path); err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("theme: %q is not a user theme in %s", ref, r.userDir)
		}
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("theme: delete %s: %w", path, err)
	}
	return r.Reload()
}

func isEmbedded(name string) bool {
	_, err := files.ReadFile(filepath.ToSlash(filepath.Join("data", name+".json")))
	return err == nil
}

// expandEnv expands environment variables in a file:// path, as Fresh's theme
// resolver does for HOME and XDG_CONFIG_HOME.
func expandEnv(s string) string { return os.ExpandEnv(s) }

// unknownThemeError is the error of a theme ref nothing answers to, listing the
// available names.
func unknownThemeError(ref string) error {
	return fmt.Errorf("theme: unknown theme %q (want a bundled name, a file in the user theme dir, or file://); bundled: %s",
		ref, strings.Join(EmbeddedNames(), ", "))
}
