package app

import (
	"encoding/json"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
)

func TestSettingsForPrefersFlagsOverConfig(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Theme = "dark"
	cfg.Keymap = "vscode"
	tests := []struct {
		name                  string
		opts                  Options
		wantTheme, wantKeymap string
	}{
		{"config values without flags", Options{}, "dark", "vscode"},
		{"flags win over config", Options{Theme: "light", Keymap: "emacs"}, "light", "emacs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := settingsFor(tt.opts, cfg)
			if s.Theme != tt.wantTheme || s.Keymap != tt.wantKeymap {
				t.Fatalf("settingsFor = theme %q keymap %q, want %q %q", s.Theme, s.Keymap, tt.wantTheme, tt.wantKeymap)
			}
		})
	}
	if s := settingsFor(Options{Config: "/tmp/x.json"}, config.Defaults()); s.ConfigFile != "/tmp/x.json" {
		t.Fatalf("ConfigFile = %q, want the --config path", s.ConfigFile)
	}
	if s := settingsFor(Options{}, config.Defaults()); s.Keymap != keymap.DefaultName() {
		t.Fatalf("Keymap without a name = %q, want %q", s.Keymap, keymap.DefaultName())
	}
}

func TestSettingsForCarriesTheSaveOptions(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.LineEnding = config.LineEndingCRLF
	cfg.TrimTrailingWhitespace = true
	cfg.EnsureFinalNewline = true
	s := settingsFor(Options{}, cfg)
	if s.LineEnding != "crlf" || !s.TrimTrailingWhitespace || !s.EnsureFinalNewline {
		t.Fatalf("settingsFor = %q %v %v, want crlf true true", s.LineEnding, s.TrimTrailingWhitespace, s.EnsureFinalNewline)
	}
}

// TestSettingsForCarriesTheImageProtocol checks the settings snapshot that the
// settings page shows: a session without a SettingsPort must still list the
// configured image_protocol, not fall back to auto.
func TestSettingsForCarriesTheImageProtocol(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.UIImageProtocol = config.UIImageProtocolOff
	if s := settingsFor(Options{}, cfg); s.UIImageProtocol != config.UIImageProtocolOff {
		t.Fatalf("UIImageProtocol = %q, want %q", s.UIImageProtocol, config.UIImageProtocolOff)
	}
}

func TestSwitchKeymapKeepsTheConfigKeybindings(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Keybindings = json.RawMessage(`{"bindings":[{"key":"s","modifiers":["ctrl"],"action":"noop","when":"normal"}]}`)
	switchTo := switchKeymapFor(cfg)
	km, err := switchTo("emacs")
	if err != nil || km == nil {
		t.Fatalf("switch to emacs = %v, %v; want a keymap", km, err)
	}
	if cmd, ok := km.Resolve(tui.KeyPress{Name: "s", Mods: []string{"ctrl"}}, tui.KeyContextNormal); ok && cmd.Action != "noop" {
		t.Fatalf("ctrl+s resolves to %q after the switch; the config override should unbind it", cmd.Action)
	}
	if _, err := switchTo("no-such-keymap"); err == nil {
		t.Fatal("switch to an unknown keymap did not fail")
	}
}
