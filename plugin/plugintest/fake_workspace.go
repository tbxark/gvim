package plugintest

import "github.com/tbxark/gvim/pluginapi"

// The workspace part of the fake host (stage S9): root, file index, effective editor
// settings and the theme. Methods the fake does not implement stay pluginapi.Unsupported's.

// FakeWorkspace is the state behind FakeHost.Root, Files, Config, Theme and ThemeColor.
type FakeWorkspace struct {
	RootDir   string
	FileList  []string
	Loading   bool
	Settings  map[string]any
	ThemeName string
	Dark      bool
	Colors    map[string][3]uint8 // theme key ("section.key") -> r, g, b
}

// Workspace returns the workspace state of the fake host, creating it on first use.
func (f *FakeHost) Workspace() *FakeWorkspace {
	if w, ok := f.Ext["workspace"].(*FakeWorkspace); ok {
		return w
	}
	w := &FakeWorkspace{RootDir: "/work", Settings: map[string]any{}, Colors: map[string][3]uint8{}, ThemeName: "dark", Dark: true}
	f.Ext["workspace"] = w
	return w
}

// Root is the workspace root.
func (f *FakeHost) Root() string {
	f.rec("Root")
	return f.Workspace().RootDir
}

// Files returns at most limit entries of the file index.
func (f *FakeHost) Files(limit int) ([]string, bool) {
	f.rec("Files", limit)
	w := f.Workspace()
	files := w.FileList
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	return append([]string(nil), files...), w.Loading
}

// Config returns an effective editor setting from Workspace().Settings.
func (f *FakeHost) Config(key string) (any, bool) {
	f.rec("Config", key)
	v, ok := f.Workspace().Settings[key]
	return v, ok
}

// Theme names the active theme.
func (f *FakeHost) Theme() pluginapi.ThemeInfo {
	w := f.Workspace()
	return pluginapi.ThemeInfo{Name: w.ThemeName, Dark: w.Dark}
}

// ThemeColor resolves a theme key from Workspace().Colors.
func (f *FakeHost) ThemeColor(key string) (r, g, b uint8, ok bool) {
	c, ok := f.Workspace().Colors[key]
	return c[0], c[1], c[2], ok
}
