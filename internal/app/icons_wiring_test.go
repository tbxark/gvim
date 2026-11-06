package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/ui/icons"
	"github.com/tbxark/gvim/internal/uitest"
)

// TestIconSetFollowsTheConfigAndTheTerminal starts the UI with a config and a
// terminal environment, and checks which close glyph the explorer title draws.
func TestIconSetFollowsTheConfigAndTheTerminal(t *testing.T) {
	root := shotProject(t)
	tests := []struct {
		name   string
		config string
		env    map[string]string
		want   icons.Mode
	}{
		{"auto in an unknown terminal", `{}`, map[string]string{"TERM": "xterm-256color"}, icons.Plain},
		{"auto in kitty", `{}`, map[string]string{"TERM": "xterm-kitty"}, icons.Nerd},
		{"auto in Ghostty", `{"ui_icons": "auto"}`, map[string]string{"TERM_PROGRAM": "ghostty"}, icons.Nerd},
		{"auto with a Nerd Font declared", `{"ui_icons_font": "FiraCode Nerd Font Mono"}`, nil, icons.Nerd},
		{"auto with another font declared in kitty", `{"ui_icons_font": "Menlo"}`, map[string]string{"TERM": "xterm-kitty"}, icons.Plain},
		{"plain in kitty", `{"ui_icons": "plain"}`, map[string]string{"TERM": "xterm-kitty"}, icons.Plain},
		{"nerd in an unknown terminal", `{"ui_icons": "nerd"}`, nil, icons.Nerd},
		{"invalid value falls back to auto", `{"ui_icons": "emoji"}`, map[string]string{"TERM": "xterm-kitty"}, icons.Nerd},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			cfg := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(cfg, []byte(tt.config), 0o600); err != nil {
				t.Fatal(err)
			}
			m, files, err := buildModel(t.Context(), Options{Config: cfg, Root: root})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { files.Close() })
			s := uitest.Start(t, m, 100, 30)
			if got := s.Screen().Contains(icons.Nerd.Glyph(icons.Close)); got != (tt.want == icons.Nerd) {
				t.Fatalf("nerd close glyph on screen = %v, want mode %v", got, tt.want)
			}
			if got := s.Screen().Contains(icons.Plain.Glyph(icons.Close)); got != (tt.want == icons.Plain) {
				t.Fatalf("plain close glyph on screen = %v, want mode %v", got, tt.want)
			}
		})
	}
}
