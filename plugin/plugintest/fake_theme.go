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
