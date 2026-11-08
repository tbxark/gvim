package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/theme"
)

// writeUserTheme writes one theme JSON under the user theme dir.
func writeUserTheme(t *testing.T, name, body string) string {
	t.Helper()
	dir := userThemeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRunThemeForms checks the config theme forms resolve through the registry:
// builtin://, a user theme name, a file:// path and the plain bundled name.
func TestRunThemeForms(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	writeUserTheme(t, "my-theme", `{"name":"my-theme","extends":"builtin://dark","editor":{"cursor":[1,2,3]}}`)
	absolute := filepath.Join(dir, "outside.json")
	if err := os.WriteFile(absolute, []byte(`{"name":"outside","extends":"builtin://light"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		theme    string
		wantRef  string
		wantName string
	}{
		{name: "plain bundled name", theme: "dark", wantRef: "dark", wantName: "dark"},
		{name: "builtin form", theme: "builtin://light", wantRef: "light", wantName: "light"},
		{name: "user theme name", theme: "my-theme", wantRef: "my-theme.json", wantName: "my-theme"},
		{name: "file form", theme: "file://" + absolute, wantRef: "file://" + absolute, wantName: "outside"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildRecorded(t, Options{Root: dir, Theme: tt.theme})
			apptest.Must(t, err)
			if got.deps.ThemeRef != tt.wantRef {
				t.Errorf("ThemeRef = %q, want %q", got.deps.ThemeRef, tt.wantRef)
			}
			if got.deps.Settings.Theme != tt.wantName {
				t.Errorf("Settings.Theme = %q, want %q", got.deps.Settings.Theme, tt.wantName)
			}
			if got.deps.ThemeCatalog == nil {
				t.Fatal("Deps.ThemeCatalog is nil")
			}
		})
	}
	if _, _, err := buildModel(t.Context(), Options{Root: dir, Theme: "nope-not-a-theme"}); err == nil || !strings.Contains(err.Error(), `unknown theme "nope-not-a-theme"`) {
		t.Fatalf("unknown theme error = %v", err)
	}
	// The Settings page's theme row offers the user theme by its portable ref.
	got, err := buildRecorded(t, Options{Root: dir})
	apptest.Must(t, err)
	snap := got.deps.SettingsPort.Snapshot()
	themeItem := findItem(t, snap, "theme")
	if !containsString(themeItem.Choices, "my-theme.json") {
		t.Fatalf("theme choices = %v, want the user theme ref", themeItem.Choices)
	}
}

// containsString reports whether s contains value.
func containsString(s []string, value string) bool {
	for _, item := range s {
		if item == value {
			return true
		}
	}
	return false
}

// TestThemeCatalogSaveAndDelete checks the editor's save path: one atomic write
// under the user theme dir, then a registry reload, then delete.
func TestThemeCatalogSaveAndDelete(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	got, err := buildRecorded(t, Options{Root: dir})
	apptest.Must(t, err)
	catalog := got.deps.ThemeCatalog
	light, err := theme.Load("light")
	if err != nil {
		t.Fatal(err)
	}
	edited, err := theme.WithOverrides(light, map[string]string{"editor.cursor": "#ABCDEF"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := catalog.SaveTheme("saved-theme", edited)
	apptest.Must(t, err)
	if ref != "saved-theme.json" {
		t.Errorf("SaveTheme ref = %q, want saved-theme.json", ref)
	}
	path := filepath.Join(userThemeDir(), "saved-theme.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the saved theme file is missing: %v", err)
	}
	saved, err := catalog.Load(ref)
	apptest.Must(t, err)
	cursor, _ := saved.Value("editor", "cursor")
	if cursor.RGB != (theme.RGB{R: 0xAB, G: 0xCD, B: 0xEF}) {
		t.Errorf("saved cursor = %+v, want #ABCDEF", cursor)
	}
	found := false
	for _, info := range catalog.Infos() {
		if info.Ref == ref && info.Name == "saved-theme" {
			found = true
		}
	}
	if !found {
		t.Errorf("the saved theme is not offered: %v", catalog.Infos())
	}
	apptest.Must(t, catalog.DeleteTheme(ref))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the deleted theme file still exists: %v", err)
	}
}

// TestReloadRereadsAUserThemeFile checks the hot reload: a user theme edited on
// disk comes back changed after SettingsPort.Reload, and the theme is in Live.
func TestReloadRereadsAUserThemeFile(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, `{"theme": "hot"}`)
	writeUserTheme(t, "hot", `{"name":"hot","extends":"builtin://dark","editor":{"cursor":[1,2,3]}}`)
	got, err := buildRecorded(t, Options{Root: dir, Config: userFile})
	apptest.Must(t, err)
	port := got.deps.SettingsPort

	writeUserTheme(t, "hot", `{"name":"hot","extends":"builtin://dark","editor":{"cursor":[9,9,9]}}`)
	change, err := port.Reload()
	apptest.Must(t, err)
	if !sliceContains(change.Live, "theme") {
		t.Fatalf("Live = %v, want the re-applied theme", change.Live)
	}
	th, err := got.deps.ThemeCatalog.Load(change.Theme)
	apptest.Must(t, err)
	cursor, _ := th.Value("editor", "cursor")
	if cursor.RGB != (theme.RGB{R: 9, G: 9, B: 9}) {
		t.Fatalf("reloaded cursor = %+v, want the edited value", cursor)
	}
}

// sliceContains reports whether s contains value.
func sliceContains(s []string, value string) bool {
	for _, item := range s {
		if item == value {
			return true
		}
	}
	return false
}

var _ = config.Config{}
var _ = tui.SettingsLayerUser
