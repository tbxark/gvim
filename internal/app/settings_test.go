package app

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
)

func TestNestedConfigReachesEditorAndUI(t *testing.T) {
	body := `{"editor": {
		"auto_close": false, "auto_surround": false, "virtual_space": "on", "scroll_offset": 7,
		"auto_read_only": false, "snapshot_interval": 10, "confirm_quit": true,
		"auto_create_empty_buffer_on_last_buffer_close": false,
		"recovery_enabled": false, "auto_recovery_save_interval_secs": 9,
		"auto_revert_poll_interval_ms": 11, "large_file_threshold_bytes": 12, "estimated_line_length": 13,
		"ephemeral_file_patterns": ["**/tmp/**"],
		"relative_line_numbers": true, "show_tilde": false, "rulers": [80, 100],
		"indentation_guide": "all", "mouse_wheel_scroll_lines": 5, "double_click_time_ms": 250,
		"wrap_indent": false, "whitespace_spaces_inner": true,
		"search": {"regex": true}, "status_bar": {"separator": "|"}
	}}`
	got, err := buildCaptured(t, projectRoot(t, body))
	if err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	o := got.editor
	if o.AutoClose || o.AutoSurround || o.VirtualSpace != "on" || o.ScrollOffset != 7 || o.AutoReadOnly ||
		o.SnapshotInterval != 10 || !o.ConfirmQuit || o.AutoCreateEmptyBufferOnLastBufferClose ||
		o.RecoveryEnabled || o.AutoRecoverySaveIntervalSecs != 9 || o.AutoRevertPollIntervalMS != 11 ||
		o.LargeFileThresholdBytes != 12 || o.EstimatedLineLength != 13 {
		t.Errorf("editor.Options = %+v", o)
	}
	v := got.deps.View
	if v == nil {
		t.Fatal("Deps.View is nil")
	}
	// The rulers are the editor's: each buffer it opens starts with them.
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := got.ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	if r := got.ed.Active().ViewSettings().Rulers; !slices.Equal(r, []int{80, 100}) {
		t.Errorf("rulers of an opened buffer = %v, want the configured [80 100]", r)
	}
	if !v.RelativeLineNumbers || v.ShowTilde ||
		v.IndentationGuideMode != "all" || v.MouseWheelScrollLines != 5 || v.DoubleClickTimeMS != 250 ||
		v.WrapIndent || !v.Whitespace.SpacesInner || !v.Search.Regex || v.StatusBar.Separator != "|" {
		t.Errorf("Deps.View = %+v", *v)
	}
}

func TestDefaultConfigGivesFreshViewDefaults(t *testing.T) {
	got, err := buildCaptured(t, projectRoot(t, ``))
	if err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	v := *got.deps.View
	if !slices.Equal(v.StatusBar.Left, config.Defaults().StatusBar.Left) || !slices.Equal(v.StatusBar.Right, config.Defaults().StatusBar.Right) {
		t.Errorf("status bar = %+v, want Fresh's elements", v.StatusBar)
	}
	// The status bar lists and the explorer width are where a config default
	// differs from DefaultViewOptions: the lists are Fresh's elements and the
	// width is file_explorer.width (30%). The icon set is resolved from the
	// terminal.
	want := tui.DefaultViewOptions()
	v.StatusBar = want.StatusBar
	v.Explorer.Width = want.Explorer.Width
	v.UIIcons = want.UIIcons
	// image_protocol has a config default ("auto") while DefaultViewOptions leaves
	// the field empty; the empty value reads as auto at runtime.
	v.ImageProtocol = want.ImageProtocol
	if !reflect.DeepEqual(v, want) {
		t.Errorf("Deps.View = %+v\nwant       %+v", v, want)
	}
}

func TestLanguageSettingsReachEditorAndUI(t *testing.T) {
	body := `{"languages": {"python": {"tab_size": 3, "line_wrap": false}}}`
	got, err := buildCaptured(t, projectRoot(t, body))
	if err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	if got.deps.Languages == nil {
		t.Fatal("Deps.Languages is nil")
	}
	tests := []struct {
		name, language, path string
		tabSize              int
		useTabs, lineWrap    bool
		autoClose            bool
		tabMarks             bool
	}{
		{"user entry", "Python", "", 3, false, false, true, true},
		{"built-in go by name", "Go", "", 8, true, true, true, false},
		{"built-in go by path", "", "x/main.go", 8, true, true, true, false},
		{"markdown", "Markdown", "", 4, false, true, false, true},
		{"unknown", "Rust", "a.rs", 4, false, true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := got.deps.Languages(tt.language, tt.path)
			if s.TabSize != tt.tabSize || s.UseTabs != tt.useTabs || s.LineWrap != tt.lineWrap ||
				s.AutoClose != tt.autoClose || s.WhitespaceTabsInner != tt.tabMarks {
				t.Errorf("settings = %+v", s)
			}
		})
	}
}

func TestAutoSaveInterval(t *testing.T) {
	enabled := config.Defaults()
	enabled.AutoSaveEnabled = true
	enabled.AutoSaveIntervalSecs = 12
	zero := enabled
	zero.AutoSaveIntervalSecs = 0
	tests := []struct {
		name    string
		flagged time.Duration
		cfg     config.Config
		want    time.Duration
	}{
		{"off by default", 0, config.Defaults(), 0},
		{"config enables", 0, enabled, 12 * time.Second},
		{"flag wins", 3 * time.Second, enabled, 3 * time.Second},
		{"flag without config", 3 * time.Second, config.Defaults(), 3 * time.Second},
		{"enabled with zero interval stays off", 0, zero, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := autoSaveInterval(tt.flagged, tt.cfg); got != tt.want {
				t.Errorf("autoSaveInterval = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigAutoSaveReachesEditorAndUI(t *testing.T) {
	got, err := buildCaptured(t, projectRoot(t, `{"editor": {"auto_save_enabled": true, "auto_save_interval_secs": 5}}`))
	if err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	if got.editor.AutoSaveInterval != 5*time.Second || got.deps.Root.AutoSave != 5*time.Second {
		t.Errorf("auto-save = editor %v, ui %v, want 5s", got.editor.AutoSaveInterval, got.deps.Root.AutoSave)
	}
}

func TestChangedSettingsDetectsNerdFontIconsAlias(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, `{}`)
	got, err := buildRecorded(t, Options{Root: dir, Config: userFile})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, userFile, `{"editor": {"nerd_font_icons": true}}`)
	change, err := got.deps.SettingsPort.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(change.Live, "ui_icons") {
		t.Fatalf("Live = %v, want ui_icons when editor.nerd_font_icons changes", change.Live)
	}
	if change.UIIconsMode != "nerd" {
		t.Fatalf("UIIconsMode = %q, want nerd", change.UIIconsMode)
	}
}

func TestLanguageSettingsMemoizationBoundedAndIsolated(t *testing.T) {
	cfg := config.Defaults()
	cfg.Languages = map[string]config.LanguageSettings{
		"go": {
			OnSave: []config.OnSaveAction{{Command: "gofmt", Enabled: true}},
		},
	}
	resolve := languageSettings(cfg)

	first := resolve("Go", "/repo/main.go")
	if len(first.OnSave) != 1 || first.OnSave[0].Command != "gofmt" {
		t.Fatalf("unexpected OnSave: %+v", first.OnSave)
	}
	first.OnSave[0].Command = "mutated"
	second := resolve("Go", "/repo/main.go")
	if len(second.OnSave) != 1 || second.OnSave[0].Command != "gofmt" {
		t.Fatalf("OnSave mutation leaked through cache: %+v", second.OnSave)
	}

	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := range 300 {
				path := fmt.Sprintf("/repo/w%d/file%d.go", worker, i)
				got := resolve("", path)
				if got.Language != "go" || len(got.OnSave) != 1 || got.OnSave[0].Command != "gofmt" {
					t.Errorf("resolve(%q) = %+v", path, got)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}
