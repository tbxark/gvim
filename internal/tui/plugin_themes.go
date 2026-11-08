package tui

import (
	"os"

	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// Theme-file APIs of the plugin host (plugin API tier 1b): the theme editor and the pickers a
// plugin port needs. The registry is the TUI's ThemeCatalog port; a missing catalog (a test
// frame) answers unsupported for the write paths and embedded-only for the read ones.

// pluginThemeCatalog returns the theme catalog of the frame, or nil.
func (m *Model) pluginThemeCatalog() ThemeCatalog { return m.util.themeCatalog }

// ThemeFiles lists every theme the editor offers.
func (h *pluginHost) ThemeFiles() []pluginapi.ThemeFileInfo {
	cat := h.model().pluginThemeCatalog()
	if cat == nil {
		return nil
	}
	infos := cat.Infos()
	out := make([]pluginapi.ThemeFileInfo, len(infos))
	for i, info := range infos {
		out[i] = pluginapi.ThemeFileInfo{Name: info.Name, Ref: info.Ref, Builtin: info.Builtin}
	}
	return out
}

// ThemeSchema is the theme model as a table: the color key table, the attribute-only keys
// and the named colors. It is gvim's shape of Fresh's JSON theme schema.
func (h *pluginHost) ThemeSchema() pluginapi.ThemeSchema {
	keys := theme.Keys()
	out := pluginapi.ThemeSchema{
		Keys:         make([]pluginapi.ThemeKeyInfo, len(keys)),
		ModifierOnly: theme.ModifierOnlyKeys(),
		NamedColors:  theme.NamedColorNames(),
	}
	for i, k := range keys {
		kind := "optional"
		switch k.Kind {
		case theme.RequiredKey:
			kind = "required"
		case theme.FallbackKey:
			kind = "fallback"
		}
		out.Keys[i] = pluginapi.ThemeKeyInfo{Name: k.Name(), Kind: kind, Fallback: k.Fallback}
	}
	return out
}

// ThemeUserDir is the user theme directory.
func (h *pluginHost) ThemeUserDir() string {
	if cat := h.model().pluginThemeCatalog(); cat != nil {
		return cat.UserDir()
	}
	return ""
}

// ThemeFileExists reports whether ref names a user theme file on disk.
func (h *pluginHost) ThemeFileExists(ref string) bool {
	cat := h.model().pluginThemeCatalog()
	if cat == nil || ref == "" {
		return false
	}
	path, err := cat.ThemePath(ref)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// ThemeData returns the resolved theme ref as theme-file JSON.
func (h *pluginHost) ThemeData(ref string) (string, error) {
	cat := h.model().pluginThemeCatalog()
	if cat == nil {
		return "", pluginapi.Errf(pluginapi.CodeUnsupported, "no theme catalog")
	}
	return cat.Data(ref)
}

// SaveThemeFile validates data as a theme file, saves it as name and returns its ref.
func (h *pluginHost) SaveThemeFile(name, data string) (string, error) {
	cat := h.model().pluginThemeCatalog()
	if cat == nil {
		return "", pluginapi.Errf(pluginapi.CodeUnsupported, "no theme catalog")
	}
	if name == "" {
		return "", pluginapi.Errf(pluginapi.CodeInvalid, "a theme needs a name")
	}
	file, err := theme.ParseFile([]byte(data))
	if err != nil {
		return "", pluginapi.Errf(pluginapi.CodeInvalid, "%s", err.Error())
	}
	th, err := theme.Resolve(file)
	if err != nil {
		return "", pluginapi.Errf(pluginapi.CodeInvalid, "%s", err.Error())
	}
	ref, err := cat.SaveTheme(name, th)
	if err != nil {
		return "", pluginapi.Errf(pluginapi.CodeIO, "%s", err.Error())
	}
	return ref, nil
}

// DeleteThemeFile removes a user theme file.
func (h *pluginHost) DeleteThemeFile(ref string) error {
	cat := h.model().pluginThemeCatalog()
	if cat == nil {
		return pluginapi.Errf(pluginapi.CodeUnsupported, "no theme catalog")
	}
	if err := cat.DeleteTheme(ref); err != nil {
		return pluginapi.Errf(pluginapi.CodeIO, "%s", err.Error())
	}
	return nil
}

// ReloadThemes rescans the user theme directory; a non-empty ref is applied afterwards.
func (h *pluginHost) ReloadThemes(ref string) error {
	cat := h.model().pluginThemeCatalog()
	if cat == nil {
		return pluginapi.Errf(pluginapi.CodeUnsupported, "no theme catalog")
	}
	if err := cat.Reload(); err != nil {
		return pluginapi.Errf(pluginapi.CodeIO, "%s", err.Error())
	}
	if ref != "" {
		h.model().applyTheme(ref)
	}
	return nil
}
