package config

import (
	"path/filepath"
	"testing"
)

// viewCase is one user file with view keys and what the loaded config should say.
type viewCase struct {
	name  string
	json  string
	check func(Config) bool
	diag  string // the key that must be reported, or "" for none
}

func TestLoadViewKeysSetTheirFields(t *testing.T) {
	tests := []viewCase{
		{"tab bar off", `{"show_tab_bar": false}`,
			func(c Config) bool { return !c.ShowTabBar }, ""},
		{"status bar off", `{"show_status_bar": false}`,
			func(c Config) bool { return !c.ShowStatusBar }, ""},
		{"prompt line on", `{"show_prompt_line": true}`,
			func(c Config) bool { return c.ShowPromptLine }, ""},
		{"vertical scrollbar off", `{"show_vertical_scrollbar": false}`,
			func(c Config) bool { return !c.ShowVerticalScrollbar }, ""},
		{"horizontal scrollbar off", `{"show_horizontal_scrollbar": false}`,
			func(c Config) bool { return !c.ShowHorizontalScrollbar }, ""},
		{"line wrap off", `{"line_wrap": false}`,
			func(c Config) bool { return !c.LineWrap }, ""},
		{"current line off", `{"highlight_current_line": false}`,
			func(c Config) bool { return !c.HighlightCurrentLine }, ""},
		{"occurrences on", `{"highlight_occurrences": true}`,
			func(c Config) bool { return c.HighlightOccurrences }, ""},
		{"whitespace on", `{"show_whitespace": true}`,
			func(c Config) bool { return c.ShowWhitespace }, ""},
		{"mouse capture off", `{"mouse_capture": false}`,
			func(c Config) bool { return !c.MouseCapture }, ""},
		{"mouse hover on", `{"mouse_hover": true}`,
			func(c Config) bool { return c.MouseHover }, ""},
		{"scroll sync on", `{"scroll_sync": true}`,
			func(c Config) bool { return c.ScrollSync }, ""},
		{"cursor bar", `{"cursor_style": "bar"}`,
			func(c Config) bool { return c.CursorStyle == CursorStyleBar }, ""},
		{"cursor underline", `{"cursor_style": "underline"}`,
			func(c Config) bool { return c.CursorStyle == CursorStyleUnderline }, ""},
		{"cursor block", `{"cursor_style": "block"}`,
			func(c Config) bool { return c.CursorStyle == CursorStyleBlock }, ""},
		{"cursor unknown", `{"cursor_style": "beam"}`,
			func(c Config) bool { return c.CursorStyle == CursorStyleBlock }, "cursor_style"},
		{"cursor not a string", `{"cursor_style": 1}`,
			func(c Config) bool { return c.CursorStyle == CursorStyleBlock }, "cursor_style"},
		{"icons nerd", `{"ui_icons": "nerd"}`,
			func(c Config) bool { return c.UIIcons == UIIconsNerd }, ""},
		{"icons plain", `{"ui_icons": "plain"}`,
			func(c Config) bool { return c.UIIcons == UIIconsPlain }, ""},
		{"icons auto", `{"ui_icons": "auto"}`,
			func(c Config) bool { return c.UIIcons == UIIconsAuto }, ""},
		{"icons unknown", `{"ui_icons": "emoji"}`,
			func(c Config) bool { return c.UIIcons == UIIconsAuto }, "ui_icons"},
		{"icons wrong case", `{"ui_icons": "Nerd"}`,
			func(c Config) bool { return c.UIIcons == UIIconsAuto }, "ui_icons"},
		{"icons not a string", `{"ui_icons": true}`,
			func(c Config) bool { return c.UIIcons == UIIconsAuto }, "ui_icons"},
		{"icons font", `{"ui_icons_font": "  JetBrainsMono Nerd Font Mono "}`,
			func(c Config) bool { return c.UIIconsFont == "JetBrainsMono Nerd Font Mono" }, ""},
		{"icons font not a string", `{"ui_icons_font": 12}`,
			func(c Config) bool { return c.UIIconsFont == "" }, "ui_icons_font"},
		{"line wrap not a bool", `{"line_wrap": "yes"}`,
			func(c Config) bool { return c.LineWrap }, "line_wrap"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeFile(t, path, tt.json)
			cfg, diags, err := Load(Options{UserFile: path})
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if tt.diag == "" && len(diags) != 0 {
				t.Fatalf("diagnostics %v, want none", diags)
			}
			if tt.diag != "" && (len(diags) != 1 || diags[0].Key != tt.diag) {
				t.Fatalf("diagnostics %v, want one for %s", diags, tt.diag)
			}
			if !tt.check(cfg) {
				t.Fatalf("config %+v does not carry the value from %s", cfg, tt.json)
			}
		})
	}
}

func TestViewKeysDefaultsMatchTheStartingLayout(t *testing.T) {
	d := Defaults()
	tests := []struct {
		name string
		got  bool
		want bool
	}{
		{"tab bar shown", d.ShowTabBar, true},
		{"status bar shown", d.ShowStatusBar, true},
		{"prompt line hidden", d.ShowPromptLine, false},
		{"vertical scrollbar shown", d.ShowVerticalScrollbar, true},
		{"horizontal scrollbar shown", d.ShowHorizontalScrollbar, true},
		{"line wrap on", d.LineWrap, true},
		{"current line on", d.HighlightCurrentLine, true},
		{"occurrences off", d.HighlightOccurrences, false},
		{"whitespace off", d.ShowWhitespace, false},
		{"mouse capture on", d.MouseCapture, true},
		{"mouse hover off", d.MouseHover, false},
		{"scroll sync off", d.ScrollSync, false},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: default is %v, want %v", tt.name, tt.got, tt.want)
		}
	}
	if d.CursorStyle != CursorStyleBlock {
		t.Errorf("cursor style default %q, want %q", d.CursorStyle, CursorStyleBlock)
	}
}

func TestUIIconsDefaultsToAuto(t *testing.T) {
	d := Defaults()
	if d.UIIcons != UIIconsAuto || d.UIIconsFont != "" {
		t.Fatalf("ui_icons default %q and font %q, want auto and none", d.UIIcons, d.UIIconsFont)
	}
}

func TestUIIconsLayers(t *testing.T) {
	tests := []struct {
		name          string
		user, project string
		want          string
		diagKey       string
	}{
		{"project over user", `{"ui_icons": "nerd"}`, `{"ui_icons": "plain"}`, UIIconsPlain, ""},
		{"project sets user default", `{}`, `{"ui_icons": "nerd"}`, UIIconsNerd, ""},
		{"user kept when project is silent", `{"ui_icons": "nerd"}`, `{"tab_size": 2}`, UIIconsNerd, ""},
		{"invalid project value keeps the user value", `{"ui_icons": "nerd"}`, `{"ui_icons": "bogus"}`, UIIconsNerd, "ui_icons"},
		{"invalid user value keeps the default", `{"ui_icons": "bogus"}`, `{}`, UIIconsAuto, "ui_icons"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			userFile := filepath.Join(dir, "user", "config.json")
			root := filepath.Join(dir, "proj")
			writeFile(t, userFile, tt.user)
			writeFile(t, filepath.Join(root, ".gvim", "config.json"), tt.project)
			cfg, diags, err := Load(Options{Root: root, UserFile: userFile})
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.UIIcons != tt.want {
				t.Errorf("ui_icons = %q, want %q", cfg.UIIcons, tt.want)
			}
			switch {
			case tt.diagKey == "" && len(diags) != 0:
				t.Errorf("diagnostics %v, want none", diags)
			case tt.diagKey != "" && (len(diags) != 1 || diags[0].Key != tt.diagKey):
				t.Errorf("diagnostics %v, want one for %s", diags, tt.diagKey)
			}
		})
	}
}
