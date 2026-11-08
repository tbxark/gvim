package app

import (
	"image/color"

	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/theme"
)

// Theme previews are the Settings theme row's colour chips. They are resolved
// when the settings service is built and on every Reload (both are file-IO
// paths: a user theme may be read), never from Snapshot or the page's update
// loop, which only read the cache.

// themePreviewSwatches builds the four chips of one resolved theme: its editor
// background and foreground, gvim's accent, and Fresh's popup selection.
func themePreviewSwatches(th theme.Theme) []tui.SettingsSwatch {
	return []tui.SettingsSwatch{
		{Label: "bg", BG: previewColor(th, "editor", "bg"), FG: previewColor(th, "editor", "fg")},
		{Label: "fg", BG: previewColor(th, "editor", "bg"), FG: previewColor(th, "editor", "fg")},
		{Label: "accent", BG: previewColor(th, "ui", "accent_bg"), FG: previewColor(th, "ui", "accent_fg")},
		{Label: "popup", BG: previewColor(th, "ui", "popup_selection_bg"), FG: previewColor(th, "ui", "popup_selection_fg")},
	}
}

// previewColor is a theme value as an RGB colour for a preview chip. The
// terminal default and unknown names have no RGB and draw as nil (the cell keeps
// the box's colour).
func previewColor(th theme.Theme, section, key string) color.Color {
	value, ok := th.Value(section, key)
	if !ok {
		return nil
	}
	rgb, ok := value.RGBA()
	if !ok {
		return nil
	}
	return color.RGBA{R: rgb.R, G: rgb.G, B: rgb.B, A: 255}
}

// previewFor returns the preview of ref from the cache: the catalogue refs and
// the themes' display names are both keys, so a config's "dark" and a theme
// list's "builtin://dark" find the same chips. A value with no resolved theme
// (a URL, a bad name) has no preview.
func previewFor(previews map[string][]tui.SettingsSwatch, ref string) []tui.SettingsSwatch {
	if swatches, ok := previews[ref]; ok {
		return swatches
	}
	return nil
}

// buildThemePreviews resolves the previews of every catalog theme. themeLoader
// takes a portable ref (the app catalog when wired, the embedded themes
// otherwise); an unreadable or malformed theme simply has no preview.
func (s *settingsService) buildThemePreviews() map[string][]tui.SettingsSwatch {
	previews := map[string][]tui.SettingsSwatch{}
	add := func(name string, th theme.Theme) {
		swatches := themePreviewSwatches(th)
		previews[name] = swatches
		if th.Name != "" {
			previews[th.Name] = swatches
		}
	}
	if s.theme != nil {
		for _, info := range s.theme.Infos() {
			if th, err := s.theme.Load(info.Ref); err == nil {
				add(info.Ref, th)
			}
		}
		return previews
	}
	for _, name := range theme.Names() {
		if th, err := theme.Load(name); err == nil {
			add(name, th)
		}
	}
	return previews
}
