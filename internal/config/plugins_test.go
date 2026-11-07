package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// loadLayers loads a user file and a project file with the given bodies. An empty
// body writes no file for that layer.
func loadLayers(t *testing.T, user, project string) (Config, []Diagnostic) {
	t.Helper()
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.json")
	root := filepath.Join(dir, "proj")
	if user != "" {
		writeFile(t, userFile, user)
	}
	if project != "" {
		writeFile(t, filepath.Join(root, ".gvim", "config.json"), project)
	} else if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, diags, err := Load(Options{Root: root, UserFile: userFile})
	if err != nil {
		t.Fatal(err)
	}
	return cfg, diags
}

func TestProjectLayerCanOnlyLowerPluginTimeout(t *testing.T) {
	const raise = "project config cannot raise plugin_timeout_ms"
	tests := []struct {
		name      string
		user      string
		project   string
		want      time.Duration
		wantDiags []string // messages, in order
	}{
		{"project larger than user", `{"plugin_timeout_ms": 500}`, `{"plugin_timeout_ms": 60000}`, 500 * time.Millisecond, []string{raise}},
		{"project smaller than user", `{"plugin_timeout_ms": 500}`, `{"plugin_timeout_ms": 100}`, 100 * time.Millisecond, nil},
		{"project larger than the default", ``, `{"plugin_timeout_ms": 2000}`, 500 * time.Millisecond, []string{raise}},
		{"project smaller than the default", ``, `{"plugin_timeout_ms": 20}`, 20 * time.Millisecond, nil},
		{"project equal to user", `{"plugin_timeout_ms": 700}`, `{"plugin_timeout_ms": 700}`, 700 * time.Millisecond, nil},
		{"user raised, project lowers", `{"plugin_timeout_ms": 60000}`, `{"plugin_timeout_ms": 2000}`, 2 * time.Second, nil},
		{"user raised, project above", `{"plugin_timeout_ms": 3000}`, `{"plugin_timeout_ms": 3001}`, 3 * time.Second, []string{raise}},
		{"user alone may raise", `{"plugin_timeout_ms": 60000}`, ``, 60 * time.Second, nil},
		{"project out of range keeps the user value", `{"plugin_timeout_ms": 800}`, `{"plugin_timeout_ms": 0}`, 800 * time.Millisecond, []string{"must be between 1 and 60000, got 0"}},
		{"project wrong type keeps the user value", `{"plugin_timeout_ms": 800}`, `{"plugin_timeout_ms": "5"}`, 800 * time.Millisecond, []string{"must be an integer"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := loadLayers(t, tt.user, tt.project)
			if cfg.PluginTimeout != tt.want {
				t.Errorf("PluginTimeout = %v, want %v", cfg.PluginTimeout, tt.want)
			}
			var got []string
			for _, d := range diags {
				if d.Key != "plugin_timeout_ms" {
					t.Errorf("diagnostic for %q, want plugin_timeout_ms: %+v", d.Key, d)
				}
				got = append(got, d.Message)
			}
			if !reflect.DeepEqual(got, tt.wantDiags) {
				t.Errorf("diagnostics = %q, want %q", got, tt.wantDiags)
			}
		})
	}
}

func TestProjectPluginTimeoutSourceIsOnlyTheAcceptedFile(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.json")
	writeFile(t, userFile, `{"plugin_timeout_ms": 500}`)
	writeFile(t, filepath.Join(dir, "proj", ".gvim", "config.json"), `{"plugin_timeout_ms": 9000}`)
	cfg, _, err := Load(Options{Root: filepath.Join(dir, "proj"), UserFile: userFile})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Source("plugin_timeout_ms"); got != userFile {
		t.Errorf("Source = %q, want the user file %q", got, userFile)
	}
}

func TestPluginSettingsSplitByLayer(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name      string
		user      string
		project   string
		want      map[string]PluginConfig
		wantDiags []string // "key: message", in order
	}{
		{
			name: "none set",
			want: nil,
		},
		{
			name: "user layer goes to Settings",
			user: `{"plugins": {"flash": {"enabled": false, "settings": {"labels": "asdf", "n": 3, "nested": {"a": [1, "x"]}}}}}`,
			want: map[string]PluginConfig{"flash": {Enabled: &no, Settings: map[string]any{
				"labels": "asdf", "n": 3.0, "nested": map[string]any{"a": []any{1.0, "x"}},
			}}},
		},
		{
			name:    "project layer goes to ProjectSettings and is not merged",
			user:    `{"plugins": {"flash": {"settings": {"labels": "asdf", "depth": 1}}}}`,
			project: `{"plugins": {"flash": {"settings": {"depth": 9}}}}`,
			want: map[string]PluginConfig{"flash": {
				Settings:        map[string]any{"labels": "asdf", "depth": 1.0},
				ProjectSettings: map[string]any{"depth": 9.0},
			}},
		},
		{
			name:    "project settings without a user entry",
			project: `{"plugins": {"flash": {"settings": {"depth": 9}}}}`,
			want:    map[string]PluginConfig{"flash": {ProjectSettings: map[string]any{"depth": 9.0}}},
		},
		{
			name:    "project may disable",
			user:    `{"plugins": {"a": {"enabled": true}}}`,
			project: `{"plugins": {"a": {"enabled": false}}}`,
			want:    map[string]PluginConfig{"a": {Enabled: &no}},
		},
		{
			name:      "project may not enable",
			user:      `{"plugins": {"a": {"enabled": false}}}`,
			project:   `{"plugins": {"a": {"enabled": true}, "b": {"enabled": true, "settings": {"x": 1}}}}`,
			want:      map[string]PluginConfig{"a": {Enabled: &no}, "b": {ProjectSettings: map[string]any{"x": 1.0}}},
			wantDiags: []string{"plugins.a.enabled: project config cannot enable a plugin", "plugins.b.enabled: project config cannot enable a plugin"},
		},
		{
			name: "user may enable and unknown names are kept",
			user: `{"plugins": {"zzz_unknown": {"enabled": true}}}`,
			want: map[string]PluginConfig{"zzz_unknown": {Enabled: &yes}},
		},
		{
			name: "fresh path key is ignored",
			user: `{"plugins": {"a": {"path": "/x/a.lua", "enabled": true}}}`,
			want: map[string]PluginConfig{"a": {Enabled: &yes}},
		},
		{
			name:      "plugins not an object",
			user:      `{"plugins": []}`,
			wantDiags: []string{"plugins: must be a JSON object"},
		},
		{
			name:      "entry not an object",
			user:      `{"plugins": {"a": true, "b": {"enabled": true}}}`,
			want:      map[string]PluginConfig{"b": {Enabled: &yes}},
			wantDiags: []string{"plugins.a: must be a JSON object"},
		},
		{
			name:      "wrong types and unknown fields",
			user:      `{"plugins": {"a": {"enabled": "yes", "settings": [1], "other": 1}, "b": {"enabled": null, "settings": null}}}`,
			wantDiags: []string{"plugins.a.enabled: must be true or false", "plugins.a.other: unknown setting", "plugins.a.settings: must be a JSON object", "plugins.b.enabled: null is not a valid value", "plugins.b.settings: null is not a valid value"},
		},
		{
			name:      "a bad key leaves the good keys of the entry",
			user:      `{"plugins": {"a": {"enabled": 3, "settings": {"k": "v"}}}}`,
			want:      map[string]PluginConfig{"a": {Settings: map[string]any{"k": "v"}}},
			wantDiags: []string{"plugins.a.enabled: must be true or false"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := loadLayers(t, tt.user, tt.project)
			if !reflect.DeepEqual(cfg.Plugins, tt.want) {
				t.Errorf("Plugins = %+v, want %+v", cfg.Plugins, tt.want)
			}
			var got []string
			for _, d := range diags {
				got = append(got, d.Key+": "+d.Message)
			}
			if !reflect.DeepEqual(got, tt.wantDiags) {
				t.Errorf("diagnostics = %q, want %q", got, tt.wantDiags)
			}
		})
	}
}

func TestPluginKeysRecordTheirFile(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.json")
	projFile := filepath.Join(dir, "proj", ".gvim", "config.json")
	writeFile(t, userFile, `{"plugins": {"a": {"enabled": true, "settings": {}}}}`)
	writeFile(t, projFile, `{"plugins": {"a": {"enabled": false}}}`)
	cfg, diags, err := Load(Options{Root: filepath.Join(dir, "proj"), UserFile: userFile})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load = %v, %v", diags, err)
	}
	if got := cfg.Source("plugins.a.enabled"); got != projFile {
		t.Errorf("Source(plugins.a.enabled) = %q, want %q", got, projFile)
	}
	if got := cfg.Source("plugins.a.settings"); got != userFile {
		t.Errorf("Source(plugins.a.settings) = %q, want %q", got, userFile)
	}
}
