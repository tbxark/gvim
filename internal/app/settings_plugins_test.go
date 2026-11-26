package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/pluginapi"
)

// fakeSettingsPlugins is the Settings page's plugin runtime: a fixed inventory and
// the Control requests it received.
type fakeSettingsPlugins struct {
	infos    []pluginapi.PluginInfo
	controls []pluginapi.Control
	schemas  map[string][]pluginapi.PluginSettingSpec
	values   map[string][]pluginapi.PluginSettingValue
}

func (f *fakeSettingsPlugins) SettingsSchema(name string) ([]pluginapi.PluginSettingSpec, bool) {
	s, ok := f.schemas[name]
	return s, ok
}

func (f *fakeSettingsPlugins) SettingsValues(name string) ([]pluginapi.PluginSettingValue, bool) {
	v, ok := f.values[name]
	return v, ok
}

func (f *fakeSettingsPlugins) Plugins() []pluginapi.PluginInfo { return f.infos }
func (f *fakeSettingsPlugins) Control(c pluginapi.Control) ([]pluginapi.Work, []pluginapi.Notice) {
	f.controls = append(f.controls, c)
	return nil, nil
}

// pluginSettingsService builds the page service with a fake plugin runtime and a
// user config file holding userJSON.
func pluginSettingsService(t *testing.T, userJSON string, cfg config.Config, plugins settingsPlugins) (*settingsService, string) {
	t.Helper()
	path := t.TempDir() + "/config.json"
	if userJSON != "" {
		if err := os.WriteFile(path, []byte(userJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := newSettingsService("", Options{Config: path}, editor.New(editor.Options{}), func() bool { return true }, cfg, nil, settingsRuntime{plugins: plugins})
	return s, path
}

// TestSettingsPluginsPage checks the inventory rows: enabled/disabled state, the
// source and status note, the toggle action, the locked plugin (no action), the
// reload row and the read-only settings keys.
func TestSettingsPluginsPage(t *testing.T) {
	t.Parallel()
	user := `{"plugins": {"git": {"enabled": false, "settings": {"status": true, "debounce": 50}}}}`
	cfg, diags := loadLayerConfig(t, user)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	rt := &fakeSettingsPlugins{infos: []pluginapi.PluginInfo{
		{Name: "git", Source: "bundled", Status: "loaded", Enabled: false},
		{Name: "flash", Source: "user", Status: "disabled", Enabled: false, Locked: "set in config"},
		{Name: "hello", Source: "user", Status: "loaded", Enabled: true},
	}}
	s, _ := pluginSettingsService(t, user, cfg, rt)
	st, err := s.Structure("plugins", tui.SettingsLayerUser)
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != "fields" || st.AddLabel != "" {
		t.Fatalf("structure = %+v, want the read-only fields page", st)
	}
	rows := map[string]tui.SettingsStructureRow{}
	for _, r := range st.Rows {
		rows[r.Name] = r
	}
	git, ok := rows["git"]
	if !ok || git.Value != "disabled" || git.Action != "act:plugin-enable" || git.Locked {
		t.Errorf("git row = %+v (ok %v), want the disabled toggle row", git, ok)
	}
	if !strings.Contains(git.Note, "bundled") || !strings.Contains(git.Note, "settings: debounce, status") {
		t.Errorf("git note = %q, want source and the read-only settings keys", git.Note)
	}
	if flash := rows["flash"]; flash.Action != "" || !flash.Locked || !strings.Contains(flash.Note, "locked") {
		t.Errorf("flash row = %+v, want the locked plugin with no action", flash)
	}
	if hello := rows["hello"]; hello.Action != "act:plugin-disable" || hello.Value != "enabled" {
		t.Errorf("hello row = %+v, want the enabled toggle row", hello)
	}
	if reload := rows["Reload plugins"]; reload.Action != "act:plugin-reload" {
		t.Errorf("reload row = %+v, want the reload action", reload)
	}

	// The project layer ignores plugin enablement: every row is locked and no row
	// has an action.
	project, err := s.Structure("plugins", tui.SettingsLayerProject)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range project.Rows {
		if r.Name == "Reload plugins" {
			continue // reloading is layer-independent and stays available
		}
		if !r.Locked || r.Action != "" {
			t.Errorf("project row = %+v, want it locked and actionless", r)
		}
	}
}

// TestSettingsPluginToggleWritesUserLayer checks the toggle: the user config gains
// plugins.<name>.enabled, the service reloads the config, and Act does not call the
// runtime's Control (the root does that from Update; the plugin runtime is
// single-goroutine).
func TestSettingsPluginToggleWritesUserLayer(t *testing.T) {
	t.Parallel()
	cfg, _ := loadLayerConfig(t, `{}`)
	rt := &fakeSettingsPlugins{}
	s, path := pluginSettingsService(t, "", cfg, rt)
	if msg, err := s.Act("plugin-enable", "git"); err != nil || !strings.Contains(msg, "enabled") {
		t.Fatalf("enable = %q, %v", msg, err)
	}
	if _, ok := s.cfg.Plugins["git"]; !ok {
		t.Fatalf("reloaded config has no git plugin: %+v", s.cfg.Plugins)
	}
	if rt.controls != nil {
		t.Errorf("Act called Control: %+v, want the root to do it from Update", rt.controls)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"git"`) || !strings.Contains(string(body), "true") {
		t.Errorf("user config = %s, want plugins.git.enabled true", body)
	}
	// Disabling writes false.
	if msg, err := s.Act("plugin-disable", "git"); err != nil || !strings.Contains(msg, "disabled") {
		t.Fatalf("disable = %q, %v", msg, err)
	}
	if pc, ok := s.cfg.Plugins["git"]; !ok || pc.Enabled == nil || *pc.Enabled {
		t.Errorf("reloaded plugins = %+v, want git disabled", s.cfg.Plugins)
	}
}

// TestSettingsPluginToggleToleratesRetiredFlatKeys: a user file that still has the
// retired flat spelling of an editor option loads with an "unknown setting"
// diagnostic. That diagnostic was there before the toggle, so the toggle must not
// treat it as damage it caused: it writes the flag, keeps the file as the user had
// it and does not roll back.
func TestSettingsPluginToggleToleratesRetiredFlatKeys(t *testing.T) {
	t.Parallel()
	user := `{"tab_size": 2, "line_numbers": false, "theme": "dark"}`
	cfg, diags := loadLayerConfig(t, user)
	if len(diags) != 2 {
		t.Fatalf("diagnostics = %v, want the two unknown flat keys", diags)
	}
	s, path := pluginSettingsService(t, user, cfg, &fakeSettingsPlugins{})
	if msg, err := s.Act("plugin-enable", "git"); err != nil || !strings.Contains(msg, "enabled") {
		t.Fatalf("enable = %q, %v, want the toggle to succeed", msg, err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"tab_size": 2`, `"line_numbers": false`, `"git"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("user config = %s, want it to contain %s", body, want)
		}
	}
	if pc, ok := s.cfg.Plugins["git"]; !ok || pc.Enabled == nil || !*pc.Enabled {
		t.Errorf("reloaded plugins = %+v, want git enabled", s.cfg.Plugins)
	}
}

// TestSettingsPluginToggleRollback checks both failure paths: a failed file write
// and a failed reload restore the previous file bytes, so a toggle never
// half-applies.
// The rollback test swaps the package-level patchConfigFileFn, so it must not run
// in parallel with the other plugin tests (which would see the failing stub).
func TestSettingsPluginToggleRollback(t *testing.T) {
	for _, tt := range []struct {
		name  string
		patch func(path string, edits []config.Edit) error
	}{
		{"failed write", func(string, []config.Edit) error { return errors.New("disk full") }},
		{"failed reload", func(path string, _ []config.Edit) error {
			return os.WriteFile(path, []byte("{not json"), 0o600)
		}},
		{"diagnostic after reload", func(path string, _ []config.Edit) error {
			return os.WriteFile(path, []byte(`{"editor": {"tab_size": 0}, "plugins": {"git": {"enabled": true}}}`), 0o600)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			user := `{"editor": {"tab_size": 3}}`
			cfg, _ := loadLayerConfig(t, user)
			s, path := pluginSettingsService(t, user, cfg, &fakeSettingsPlugins{})
			original := patchConfigFileFn
			patchConfigFileFn = tt.patch
			t.Cleanup(func() { patchConfigFileFn = original })
			if _, err := s.Act("plugin-enable", "git"); err == nil {
				t.Fatal("the toggle succeeded despite the failure")
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != user {
				t.Errorf("config after a failed toggle = %q, want the previous bytes", body)
			}
			if s.currentConfig().TabSize != 3 {
				t.Errorf("in-memory TabSize = %d after rollback, want 3", s.currentConfig().TabSize)
			}
			if pc, ok := s.currentConfig().Plugins["git"]; ok && pc.Enabled != nil {
				t.Errorf("in-memory git.Enabled = %+v after rollback, want cleared", pc)
			}
		})
	}
	t.Run("non-not-exist read error keeps target untouched", func(t *testing.T) {
		cfg, _ := loadLayerConfig(t, `{}`)
		s, path := pluginSettingsService(t, "", cfg, &fakeSettingsPlugins{})
		_ = os.Remove(path)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Act("plugin-enable", "git"); err == nil {
			t.Fatal("Act succeeded when userFile is a directory")
		}
		if st, err := os.Stat(path); err != nil || !st.IsDir() {
			t.Fatalf("directory userFile was removed/altered on error: %v, %v", st, err)
		}
	})
}

// TestSettingsPluginReloadAction checks the reload row: it writes no config, it just
// reports that the root should reload (Control runs from Update).
func TestSettingsPluginReloadAction(t *testing.T) {
	t.Parallel()
	cfg, _ := loadLayerConfig(t, `{}`)
	rt := &fakeSettingsPlugins{}
	s, path := pluginSettingsService(t, "", cfg, rt)
	before, _ := os.Stat(path)
	if msg, err := s.Act("plugin-reload", ""); err != nil || !strings.Contains(msg, "Reload") {
		t.Fatalf("reload = %q, %v", msg, err)
	}
	if rt.controls != nil {
		t.Errorf("Act called Control: %+v", rt.controls)
	}
	if after, err := os.Stat(path); err == nil && before != nil && after.Size() != before.Size() {
		t.Error("the reload action wrote the config file")
	}
}

// TestPluginsPageShowsDeclaredSettings checks the declared-settings rows: one typed,
// read-only row per declared setting with the effective value and type/default/choices in
// the note.
func TestPluginsPageShowsDeclaredSettings(t *testing.T) {
	plugins := &fakeSettingsPlugins{
		infos: []pluginapi.PluginInfo{{Name: "hello", Source: "user", Status: "loaded", Enabled: true}},
		schemas: map[string][]pluginapi.PluginSettingSpec{"hello": {
			{Key: "mode", Type: "enum", Default: "fast", Choices: []string{"fast", "slow"}, Description: "pace"},
			{Key: "count", Type: "integer", Default: float64(3), Min: new(float64(1)), Max: new(float64(10)), Project: true},
		}},
		values: map[string][]pluginapi.PluginSettingValue{"hello": {
			{Key: "mode", Value: "slow"},
			{Key: "count", Value: float64(9), Project: true},
		}},
	}
	s, _ := pluginSettingsService(t, "", config.Config{}, plugins)
	st := s.structurePlugins(config.Config{}, false)
	rows := map[string]tui.SettingsStructureRow{}
	for _, r := range st.Rows {
		rows[r.Name] = r
	}
	mode, ok := rows["hello.mode"]
	if !ok {
		t.Fatalf("no hello.mode row in %+v", st.Rows)
	}
	if mode.Value != "slow" && mode.Value != `"slow"` {
		t.Fatalf("mode row value = %q", mode.Value)
	}
	if !mode.Locked || !strings.Contains(mode.Note, "enum") || !strings.Contains(mode.Note, "values fast|slow") || !strings.Contains(mode.Note, "pace") {
		t.Fatalf("mode row note = %q", mode.Note)
	}
	count := rows["hello.count"]
	if count.Value != "9" || !strings.Contains(count.Note, "range 1..10") || !strings.Contains(count.Note, "from project") || !strings.Contains(count.Note, "project") {
		t.Fatalf("count row = %+v", count)
	}
}

// racyPlugins is a plugin runtime as the real one is: not safe for concurrent use. Control
// rewrites its inventory, and every read walks it.
type racyPlugins struct {
	pluginRuntime
	infos  []pluginapi.PluginInfo
	schema map[string][]pluginapi.PluginSettingSpec
}

func (r *racyPlugins) Plugins() []pluginapi.PluginInfo { return slices.Clone(r.infos) }

func (r *racyPlugins) SettingsSchema(name string) ([]pluginapi.PluginSettingSpec, bool) {
	s, ok := r.schema[name]
	return s, ok
}

func (r *racyPlugins) SettingsValues(name string) ([]pluginapi.PluginSettingValue, bool) {
	if _, ok := r.schema[name]; !ok {
		return nil, false
	}
	return []pluginapi.PluginSettingValue{{Key: "k", Value: len(r.infos)}}, true
}

func (r *racyPlugins) Control(c pluginapi.Control) ([]pluginapi.Work, []pluginapi.Notice) {
	r.infos = append(r.infos, pluginapi.PluginInfo{Name: c.Name, Enabled: true, Source: "user"})
	r.schema[c.Name] = []pluginapi.PluginSettingSpec{{Key: "k", Type: "int"}}
	return nil, nil
}

// TestSettingsServiceNeverReadsThePluginRuntimeConcurrently drives the runtime through the
// mirror on one goroutine, as the update loop does, while Snapshot, Reload and the Plugins
// Structure run on others, as tea.Cmds do. Under -race it fails when the service reads the
// runtime (or its icon facts) without the mirror's copy. The page then lists what the
// runtime held after the last control.
func TestSettingsServiceNeverReadsThePluginRuntimeConcurrently(t *testing.T) {
	isolateConfig(t)
	userFile := filepath.Join(t.TempDir(), "config.json")
	writeTestFile(t, userFile, `{"ui_icons": "nerd"}`)
	rt := &racyPlugins{schema: map[string][]pluginapi.PluginSettingSpec{}}
	mirror, inv := newPluginMirror(rt)
	svc := newSettingsService("", Options{Config: userFile}, editor.New(editor.Options{}), func() bool { return true }, config.Defaults(), nil, settingsRuntime{plugins: inv})

	const controls = 50
	var wg sync.WaitGroup
	done := make(chan struct{})
	readers := []func(){
		func() { _ = svc.Snapshot() },
		func() { _, _ = svc.Reload() },
		func() { _, _ = svc.Structure("plugins", tui.SettingsLayerUser) },
	}
	for _, read := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					read()
				}
			}
		}()
	}
	for i := range controls {
		mirror.Control(pluginapi.Control{Op: "enable", Name: fmt.Sprintf("p%02d", i)})
	}
	close(done)
	wg.Wait()

	st, err := svc.Structure("plugins", tui.SettingsLayerUser)
	if err != nil {
		t.Fatal(err)
	}
	listed := 0
	for _, row := range st.Rows {
		if strings.HasPrefix(row.Name, "p") && row.Value == "enabled" && row.Group == "" {
			listed++
		}
	}
	if listed != controls {
		t.Fatalf("the page lists %d enabled plugins, want %d", listed, controls)
	}
}
