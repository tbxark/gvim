package plugintest

import "github.com/tbxark/gvim/pluginapi"

// The theme part of the fake host (plugin API tier 1a): the session color overrides a plugin
// applies. The fake keeps the map and can be told to reject one, as the ui does for a bad
// key or value.

// FakeTheme is the theme state of the fake host.
type FakeTheme struct {
	// Themes are the built-in theme names ThemeNames reports.
	Themes []string
	// Overrides is the map in force (nil: none).
	Overrides map[string]string
	// Err, when set, is returned by SetThemeOverrides and keeps the map.
	Err error
	// Saved are the theme files SaveThemeFile wrote, in order.
	Saved []FakeThemeFile
	// Data is theme-file JSON by ref for ThemeData reads of unsaved refs.
	Data map[string]string
	// SaveErr, when set, is returned by SaveThemeFile.
	SaveErr error
	// UserDir is ThemeUserDir's answer.
	UserDir string
	// Schema is ThemeSchema's answer.
	Schema pluginapi.ThemeSchema
	// Reloads counts ReloadThemes calls and Applied is the last ref applied.
	Reloads int
	Applied string
}

// FakeTheme returns the theme state of the fake host.
func (f *FakeHost) FakeTheme() *FakeTheme {
	t, ok := f.Ext["theme"].(*FakeTheme)
	if !ok {
		t = &FakeTheme{}
		f.Ext["theme"] = t
	}
	return t
}

// ThemeNames returns the built-in theme names.
func (f *FakeHost) ThemeNames() []string {
	f.rec("ThemeNames")
	return append([]string(nil), f.FakeTheme().Themes...)
}

// ThemeOverrides returns the session overrides.
func (f *FakeHost) ThemeOverrides() (map[string]string, bool) {
	f.rec("ThemeOverrides")
	t := f.FakeTheme()
	if len(t.Overrides) == 0 {
		return nil, false
	}
	out := make(map[string]string, len(t.Overrides))
	for k, v := range t.Overrides {
		out[k] = v
	}
	return out, true
}

// SetThemeOverrides replaces the session overrides; an empty map clears them.
func (f *FakeHost) SetThemeOverrides(overrides map[string]string) error {
	f.rec("SetThemeOverrides", len(overrides))
	t := f.FakeTheme()
	if t.Err != nil {
		return t.Err
	}
	if len(overrides) == 0 {
		t.Overrides = nil
		return nil
	}
	t.Overrides = make(map[string]string, len(overrides))
	for k, v := range overrides {
		t.Overrides[k] = v
	}
	return nil
}

var _ pluginapi.Host = (*FakeHost)(nil)

// FakeThemeFile is one saved user theme of the fake host.
type FakeThemeFile struct {
	Name string
	Data string
	Ref  string
}

// ThemeFiles lists the fake's built-in and saved themes.
func (f *FakeHost) ThemeFiles() []pluginapi.ThemeFileInfo {
	f.rec("ThemeFiles")
	t := f.FakeTheme()
	out := make([]pluginapi.ThemeFileInfo, 0, len(t.Themes)+len(t.Saved))
	for _, name := range t.Themes {
		out = append(out, pluginapi.ThemeFileInfo{Name: name, Ref: name, Builtin: true})
	}
	for _, saved := range t.Saved {
		out = append(out, pluginapi.ThemeFileInfo{Name: saved.Name, Ref: saved.Ref})
	}
	return out
}

// ThemeSchema returns the fake theme schema.
func (f *FakeHost) ThemeSchema() pluginapi.ThemeSchema {
	f.rec("ThemeSchema")
	return f.FakeTheme().Schema
}

// ThemeUserDir returns the fake user theme dir.
func (f *FakeHost) ThemeUserDir() string {
	f.rec("ThemeUserDir")
	return f.FakeTheme().UserDir
}

// ThemeFileExists reports whether a saved theme has the ref.
func (f *FakeHost) ThemeFileExists(ref string) bool {
	f.rec("ThemeFileExists", ref)
	for _, saved := range f.FakeTheme().Saved {
		if saved.Ref == ref {
			return true
		}
	}
	return false
}

// ThemeData returns the saved theme's data, or the fake data of a built-in.
func (f *FakeHost) ThemeData(ref string) (string, error) {
	f.rec("ThemeData", ref)
	t := f.FakeTheme()
	for _, saved := range t.Saved {
		if saved.Ref == ref {
			return saved.Data, nil
		}
	}
	if data, ok := t.Data[ref]; ok {
		return data, nil
	}
	return "", pluginapi.Errf(pluginapi.CodeNotFound, "theme %q is not found", ref)
}

// SaveThemeFile records a saved theme.
func (f *FakeHost) SaveThemeFile(name, data string) (string, error) {
	f.rec("SaveThemeFile", name, data)
	t := f.FakeTheme()
	if t.SaveErr != nil {
		return "", t.SaveErr
	}
	ref := name + ".json"
	t.Saved = append(t.Saved, FakeThemeFile{Name: name, Data: data, Ref: ref})
	return ref, nil
}

// DeleteThemeFile forgets a saved theme.
func (f *FakeHost) DeleteThemeFile(ref string) error {
	f.rec("DeleteThemeFile", ref)
	t := f.FakeTheme()
	for i, saved := range t.Saved {
		if saved.Ref == ref {
			t.Saved = append(t.Saved[:i:i], t.Saved[i+1:]...)
			return nil
		}
	}
	return pluginapi.Errf(pluginapi.CodeNotFound, "theme %q is not found", ref)
}

// ReloadThemes records the rescan.
func (f *FakeHost) ReloadThemes(ref string) error {
	f.rec("ReloadThemes", ref)
	t := f.FakeTheme()
	t.Reloads++
	t.Applied = ref
	return nil
}
