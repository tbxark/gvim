package app

import (
	"image/color"
	"strings"
	"testing"

	"github.com/tbxark/gvim/theme"
)

// TestSettingsThemePreview checks the theme row's swatches: the app resolves the
// catalog's themes and puts background/foreground/accent/popup selection chips on
// the theme item, using the dark theme's actual colors.
func TestSettingsThemePreview(t *testing.T) {
	isolateConfig(t)
	got, err := buildCaptured(t, Options{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	snap := settingsPort(t, got).Snapshot()
	item := findItem(t, snap, "theme")
	if len(item.Preview) != 4 {
		t.Fatalf("theme preview = %+v, want four chips", item.Preview)
	}
	dark, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		label  string
		bg, fg string
	}{
		{"bg", "editor.bg", "editor.fg"},
		{"fg", "editor.bg", "editor.fg"},
		{"accent", "ui.accent_bg", "ui.accent_fg"},
		{"popup", "ui.popup_selection_bg", "ui.popup_selection_fg"},
	}
	for i, want := range want {
		sw := item.Preview[i]
		if sw.Label != want.label {
			t.Errorf("chip %d label = %q, want %q", i, sw.Label, want.label)
		}
		if got, wantColor := sw.BG, previewTestColor(t, dark, want.bg); got != wantColor {
			t.Errorf("%s chip BG = %v, want %v", want.label, got, wantColor)
		}
		if got, wantColor := sw.FG, previewTestColor(t, dark, want.fg); got != wantColor {
			t.Errorf("%s chip FG = %v, want %v", want.label, got, wantColor)
		}
	}
}

// previewTestColor resolves a theme key to the RGBA the preview should hold.
func previewTestColor(t *testing.T, th theme.Theme, key string) color.Color {
	t.Helper()
	section, name, ok := strings.Cut(key, ".")
	if !ok {
		t.Fatalf("bad key %q", key)
	}
	value, ok := th.Value(section, name)
	if !ok {
		t.Fatalf("theme has no %s", key)
	}
	rgb, ok := value.RGBA()
	if !ok {
		return nil
	}
	return color.RGBA{R: rgb.R, G: rgb.G, B: rgb.B, A: 255}
}

// TestSettingsThemePreviewURLRefused checks the other half of the URL decision:
// a URL/packaged theme value is refused by the theme row's validation with the
// loader's clear message (no network code exists).
func TestSettingsThemePreviewURLRefused(t *testing.T) {
	isolateConfig(t)
	got, err := buildCaptured(t, Options{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	port := settingsPort(t, got)
	err = port.Validate("theme", "https://github.com/foo/themes#dark")
	if err == nil {
		t.Fatal("a URL theme value passed validation")
	}
	if !strings.Contains(err.Error(), "theme") || !strings.Contains(err.Error(), "https://github.com/foo/themes#dark") {
		t.Errorf("URL refusal = %q, want the value and a theme error", err)
	}
}
