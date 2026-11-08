package pluginhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/keybindings"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/pluginapi"
)

// newTestWiring builds the wiring of a session with no config and no keymap preset.
func newTestWiring(t *testing.T, o Options) *Wiring {
	t.Helper()
	return New(config.Config{}, editor.New(editor.Options{}), func(*editor.Document) {}, o)
}

// TestNewWiringIsCreatedForNoPlugins: --no-plugins still creates the runtime, with its
// loader disabled, so the manager can say why nothing is loaded (FINAL 4.4).
func TestNewWiringIsCreatedForNoPlugins(t *testing.T) {
	apptest.IsolateConfig(t)
	tests := []struct {
		name     string
		opts     Options
		disabled bool
	}{
		{"default", Options{}, false},
		{"--no-plugins", Options{Disabled: true}, true},
		{"--no-plugins --plugin", Options{Disabled: true, Paths: []string{"x.lua"}}, true},
		{"--plugin", Options{Paths: []string{"x.lua"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pw := newTestWiring(t, Options{Paths: tt.opts.Paths, Disabled: tt.opts.Disabled, Preset: keymap.Default()})
			if pw == nil || pw.Runtime == nil || pw.Editor == nil || pw.Keys == nil {
				t.Fatalf("wiring = %+v, want the runtime, the editor and the key builder", pw)
			}
			_, notices := pw.Runtime.Control(pluginapi.Control{Op: "reload_all"})
			got := len(notices) == 1 && strings.Contains(notices[0].Text, "disabled by --no-plugins")
			if got != tt.disabled {
				t.Fatalf("reload_all notices = %+v, disabled = %v, want %v", notices, got, tt.disabled)
			}
		})
	}
}

// TestPluginDirsFollowTheWorkspaceRoot: the directories the plugins get report the current
// root, which a project switch changes, and the config directory and the home.
func TestPluginDirsFollowTheWorkspaceRoot(t *testing.T) {
	apptest.IsolateConfig(t)
	root := "/first"
	dirs := pluginDirs("/cfg/gvim", func() string { return root })
	d := dirs()
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	if d.Root != "/first" || d.Config != "/cfg/gvim" || d.Home != home || d.Cwd != cwd || d.Temp != os.TempDir() {
		t.Fatalf("dirs = %+v", d)
	}
	root = "/second"
	if got := dirs().Root; got != "/second" {
		t.Fatalf("Root after the switch = %q, want /second", got)
	}
}

// TestPluginKeysPortBuildsALayeredKeymap: the Wiring.Keys function returns a keymap the
// root can resolve with once it is wrapped as the UI's keymap.
func TestPluginKeysPortBuildsALayeredKeymap(t *testing.T) {
	pw := newTestWiring(t, Options{Preset: keymap.Default()})
	km, err := pw.Keys("", []pluginapi.Binding{{Chord: "ctrl+alt+j", Action: "hello.hello", Plugin: "hello"}})
	apptest.Must(t, err)
	k := keybindings.NewKeymap(km)
	cmd, ok := k.Resolve(tui.KeyPress{Name: "j", Mods: []string{"ctrl", "alt"}}, tui.KeyContextNormal)
	if !ok || cmd.Action != "hello.hello" {
		t.Fatalf("resolved %+v, %v", cmd, ok)
	}
	if cmd, ok := k.Resolve(tui.KeyPress{Name: "s", Mods: []string{"ctrl"}}, tui.KeyContextNormal); !ok || cmd.Action != "save" {
		t.Fatalf("the preset bindings are lost: %+v, %v", cmd, ok)
	}
	// The error of a partial failure comes with a usable keymap; a total one with none.
	km, err = pw.Keys("", []pluginapi.Binding{{Chord: "bogus+", Action: "x", Plugin: "p"}})
	if err == nil || km == nil {
		t.Fatalf("keys = %v, %v, want the keymap and the error about the bad chord", km, err)
	}
}

var _ = filepath.Join
