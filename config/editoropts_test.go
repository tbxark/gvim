package config

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const gvimEditorDefaults = `{
  "line_numbers": true,
  "relative_line_numbers": false,
  "highlight_current_line": true,
  "highlight_occurrences": true,
  "hide_current_line_on_selection": false,
  "highlight_current_column": false,
  "line_wrap": true,
  "wrap_indent": true,
  "wrap_column": null,
  "page_width": 80,
  "syntax_highlighting": true,
  "show_menu_bar": true,
  "menu_bar_mnemonics": true,
  "show_tab_bar": true,
  "show_status_bar": true,
  "status_bar": {
    "left": [
      "{trust}",
      "{remote}",
      "{terminal_restart}",
      "{cursor}",
      "{diagnostics}",
      "{cursor_count}",
      "{messages}"
    ],
    "right": [
      "{read_only}",
      "{line_ending}",
      "{encoding}",
      "{language}",
      "{lsp}",
      "{warnings}",
      "{update}",
      "{palette}"
    ],
    "separator": ""
  },
  "search": {
    "case_sensitive": false,
    "whole_word": false,
    "regex": false,
    "confirm_each": false
  },
  "show_vertical_scrollbar": true,
  "show_horizontal_scrollbar": false,
  "show_tilde": true,
  "nerd_font_icons": false,
  "set_window_title": true,
  "cursor_style": "default",
  "rulers": [],
  "indentation_guide": "none",
  "indentation_guide_glyph": "▏",
  "rainbow_indentation": false,
  "whitespace_show": true,
  "whitespace_spaces_leading": false,
  "whitespace_spaces_inner": false,
  "whitespace_spaces_trailing": false,
  "whitespace_tabs_leading": true,
  "whitespace_tabs_inner": true,
  "whitespace_tabs_trailing": true,
  "whitespace_newlines": false,
  "whitespace_carriage_returns": false,
  "whitespace_in_selection": true,
  "use_tabs": false,
  "tab_size": 4,
  "auto_indent": true,
  "auto_close": true,
  "auto_surround": true,
  "virtual_space": "off",
  "scroll_offset": 3,
  "default_line_ending": "lf",
  "trim_trailing_whitespace_on_save": false,
  "ensure_final_newline_on_save": false,
  "auto_read_only": true,
  "highlight_matching_brackets": true,
  "rainbow_brackets": true,
  "completion_popup_auto_show": false,
  "quick_suggestions": true,
  "quick_suggestions_delay_ms": 150,
  "suggest_on_trigger_characters": true,
  "enable_inlay_hints": true,
  "diagnostics_inline_text": false,
  "mouse_wheel_scroll_lines": 3,
  "mouse_hover_enabled": true,
  "mouse_hover_delay_ms": 500,
  "double_click_time_ms": 500,
  "auto_save_enabled": false,
  "auto_save_interval_secs": 30,
  "hot_exit": true,
  "confirm_quit": false,
  "save_session": false,
  "restore_previous_session": true,
  "skip_session_restore_when_files_passed": true,
  "ephemeral_file_patterns": [
    "**/.git/**"
  ],
  "auto_create_empty_buffer_on_last_buffer_close": false,
  "recovery_enabled": true,
  "auto_recovery_save_interval_secs": 2,
  "auto_revert_poll_interval_ms": 2000,
  "highlight_context_bytes": 10000,
  "large_file_threshold_bytes": 10485760,
  "estimated_line_length": 80
}`

const exampleConfig = `{
  "theme": "high-contrast",
  "check_for_updates": true,
  "editor": {
    "use_tabs": false,
    "tab_size": 4,
    "auto_indent": true,
    "line_numbers": true,
    "relative_line_numbers": false,
    "scroll_offset": 3,
    "syntax_highlighting": true,
    "highlight_current_line": true,
    "line_wrap": true,
    "large_file_threshold_bytes": 1048576,
    "estimated_line_length": 80,
    "enable_inlay_hints": true,
    "auto_save_enabled": true,
    "auto_save_interval_secs": 30,
    "recovery_enabled": true,
    "auto_recovery_save_interval_secs": 2,
    "highlight_context_bytes": 10000,
    "mouse_hover_enabled": true,
    "mouse_hover_delay_ms": 500,
    "double_click_time_ms": 500,
    "auto_revert_poll_interval_ms": 2000
  },
  "file_explorer": {
    "respect_gitignore": true,
    "show_hidden": false,
    "hide_gitignore": true,
    "custom_ignore_patterns": [],
    "width": 0.3
  },
  "clipboard": {
    "use_osc52": true
  },
  "terminal": {
    "jump_to_end_on_output": true
  },
  "keybindings": [],
  "keymap": "default",
  "default_language": "bash",
  "languages": {
    "toml": {
      "extensions": ["toml"],
      "filenames": ["Cargo.lock"],
      "comment_prefix": "#",
      "auto_indent": true,
      "show_whitespace_tabs": true,
      "use_tabs": false
    },
    "go": {
      "extensions": ["go"],
      "filenames": [],
      "comment_prefix": "//",
      "auto_indent": true,
      "show_whitespace_tabs": false,
      "use_tabs": true
    },
    "markdown": {
      "extensions": ["md", "markdown"],
      "filenames": ["README"],
      "comment_prefix": null,
      "auto_indent": false,
      "show_whitespace_tabs": true,
      "use_tabs": false
    },
    "makefile": {
      "extensions": ["mk"],
      "filenames": ["Makefile", "makefile", "GNUmakefile"],
      "comment_prefix": "#",
      "auto_indent": false,
      "show_whitespace_tabs": true,
      "use_tabs": true
    }
  },
  "lsp": {
    "rust": {
      "command": "rust-analyzer",
      "args": ["--log-file", "/tmp/rust-analyzer-77535.log"],
      "enabled": true,
      "auto_start": false,
      "process_limits": {"max_memory_percent": 50, "max_cpu_percent": 90, "enabled": true},
      "initialization_options": {"checkOnSave": false}
    },
    "gdscript": {
      "command": "nc",
      "args": ["127.0.0.1", "6005"],
      "enabled": false,
      "auto_start": false
    },
    "go": {
      "command": "gopls",
      "args": [],
      "enabled": true,
      "auto_start": false
    }
  }
}`

// loadBody writes body as the user config file and loads it.
func loadBody(t *testing.T, body string) (Config, []Diagnostic) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, body)
	cfg, diags, err := Load(Options{UserFile: path})
	if err != nil {
		t.Fatal(err)
	}
	return cfg, diags
}

// The settings that only existed for Fresh config compatibility are gone: a file that
// still sets one gets an "unknown setting" diagnostic naming it.
func TestRetiredFreshSettingsAreUnknown(t *testing.T) {
	tests := []struct{ body, key string }{
		{`{"sidebar": "files"}`, "sidebar"},
		{`{"$schema": "x"}`, "$schema"},
		{`{"active_keybinding_map": "default"}`, "active_keybinding_map"},
		{`{"keybinding_maps": {}}`, "keybinding_maps"},
		{`{"explorer_show_ignored": true}`, "explorer_show_ignored"},
		{`{"explorer_show_hidden": true}`, "explorer_show_hidden"},
		{`{"file_explorer": {"show_gitignored": true}}`, "file_explorer.show_gitignored"},
		{`{"file_explorer": {"tree_indicator_expanded": "v"}}`, "file_explorer.tree_indicator_expanded"},
		{`{"editor": {"smooth_scroll": true}}`, "editor.smooth_scroll"},
		{`{"editor": {"animations": true}}`, "editor.animations"},
		{`{"editor": {"snapshot_interval": 100}}`, "editor.snapshot_interval"},
		{`{"editor": {"show_prompt_line": false}}`, "editor.show_prompt_line"},
		{`{"editor": {"default_line_ending": "cr"}}`, "editor.default_line_ending"},
		{`{"languages": {"go": {"grammar": "go"}}}`, "languages.go.grammar"},
		{`{"languages": {"go": {"textmate_grammar": "x"}}}`, "languages.go.textmate_grammar"},
		{`{"languages": {"go": {"highlighter": "x"}}}`, "languages.go.highlighter"},
		{`{"clipboard": {"use_system_clipboard": true}}`, "clipboard.use_system_clipboard"},
		{`{"terminal": {"skip_app_execution_alias": true}}`, "terminal.skip_app_execution_alias"},
		{`{"plugins": {"p": {"path": "/x"}}}`, "plugins.p.path"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			_, diags := loadBody(t, tt.body)
			if len(diags) != 1 || diags[0].Key != tt.key {
				t.Fatalf("diags = %+v, want one diagnostic for %s", diags, tt.key)
			}
		})
	}
}

func TestEditorDefaultsRoundTrip(t *testing.T) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(gvimEditorDefaults), &raw); err != nil {
		t.Fatal(err)
	}
	cfg, diags := loadBody(t, `{"editor": `+gvimEditorDefaults+`}`)
	if len(diags) != 0 {
		t.Fatalf("diagnostics for editor defaults: %v", diags)
	}
	if got, want := cfg.EditorOptions, Defaults().EditorOptions; !reflect.DeepEqual(got, want) {
		t.Errorf("EditorOptions parsed from defaults = %+v\nDefaults().EditorOptions = %+v", got, want)
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if got := EditorKeys(); !slices.Equal(got, keys) {
		t.Errorf("EditorKeys() differs from expected keys\n got  %v\n want %v", got, keys)
	}
}

func TestEditorDefaultValues(t *testing.T) {
	d := Defaults()
	tests := []struct {
		name string
		got  any
		want any
	}{
		{"relative_line_numbers", d.RelativeLineNumbers, false},
		{"wrap_indent", d.WrapIndent, true},
		{"wrap_column", d.WrapColumn, 0},
		{"page_width", d.PageWidth, 80},
		{"syntax_highlighting", d.SyntaxHighlighting, true},
		{"show_tilde", d.ShowTilde, true},
		{"rulers", d.Rulers, []int(nil)},
		{"indentation_guide", d.IndentationGuide, "none"},
		{"indentation_guide_glyph", d.IndentationGuideGlyph, "\u258f"},
		{"whitespace_tabs_leading", d.WhitespaceTabsLeading, true},
		{"whitespace_spaces_inner", d.WhitespaceSpacesInner, false},
		{"whitespace_in_selection", d.WhitespaceInSelection, true},
		{"auto_close", d.AutoClose, true},
		{"auto_surround", d.AutoSurround, true},
		{"virtual_space", d.VirtualSpace, "off"},
		{"scroll_offset", d.ScrollOffset, 3},
		{"auto_read_only", d.AutoReadOnly, true},
		{"highlight_matching_brackets", d.HighlightMatchingBrackets, true},
		{"rainbow_brackets", d.RainbowBrackets, true},
		{"mouse_wheel_scroll_lines", d.MouseWheelScrollLines, 3},
		{"double_click_time_ms", d.DoubleClickTimeMS, 500},
		{"auto_save_enabled", d.AutoSaveEnabled, false},
		{"auto_save_interval_secs", d.AutoSaveIntervalSecs, 30},
		{"hot_exit", d.HotExit, true},
		{"confirm_quit", d.ConfirmQuit, false},
		{"save_session", d.SaveSession, false},
		{"restore_previous_session", d.RestorePreviousSession, true},
		{"skip_session_restore_when_files_passed", d.SkipSessionRestoreWhenFilesPassed, true},
		{"ephemeral_file_patterns", d.EphemeralFilePatterns, []string{"**/.git/**"}},
		{"auto_create_empty_buffer_on_last_buffer_close", d.AutoCreateEmptyBufferOnLastBufferClose, false},
		{"auto_revert_poll_interval_ms", d.AutoRevertPollIntervalMS, 2000},
		{"search", d.Search, SearchDefaults{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Errorf("default = %#v, want %#v", tt.got, tt.want)
			}
		})
	}
}

func TestNestedEditorParsing(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		check func(Config) bool
	}{
		{"tab_size", `{"editor": {"tab_size": 2}}`, func(c Config) bool { return c.TabSize == 2 }},
		{"use_tabs inverts insert_spaces", `{"editor": {"use_tabs": true}}`, func(c Config) bool { return !c.InsertSpaces }},
		{"line_wrap off", `{"editor": {"line_wrap": false}}`, func(c Config) bool { return !c.LineWrap }},
		{"trim on save", `{"editor": {"trim_trailing_whitespace_on_save": true}}`, func(c Config) bool { return c.TrimTrailingWhitespace }},
		{"final newline on save", `{"editor": {"ensure_final_newline_on_save": true}}`, func(c Config) bool { return c.EnsureFinalNewline }},
		{"whitespace_show", `{"editor": {"whitespace_show": true}}`, func(c Config) bool { return c.ShowWhitespace }},
		{"default_line_ending", `{"editor": {"default_line_ending": "crlf"}}`, func(c Config) bool { return c.LineEnding == LineEndingCRLF }},
		{"mouse_hover_enabled", `{"editor": {"mouse_hover_enabled": true}}`, func(c Config) bool { return c.MouseHover }},
		{"cursor_style fresh name", `{"editor": {"cursor_style": "steady_bar"}}`, func(c Config) bool { return c.CursorStyle == CursorStyleBar }},
		{"cursor_style default", `{"editor": {"cursor_style": "default"}}`, func(c Config) bool { return c.CursorStyle == CursorStyleBlock }},
		{"rulers", `{"editor": {"rulers": [80, 120]}}`, func(c Config) bool { return slices.Equal(c.Rulers, []int{80, 120}) }},
		{"wrap_column null", `{"editor": {"wrap_column": null}}`, func(c Config) bool { return c.WrapColumn == 0 }},
		{"wrap_column", `{"editor": {"wrap_column": 100}}`, func(c Config) bool { return c.WrapColumn == 100 }},
		{"page_width null", `{"editor": {"page_width": null}}`, func(c Config) bool { return c.PageWidth == 0 }},
		{"virtual_space", `{"editor": {"virtual_space": "block"}}`, func(c Config) bool { return c.VirtualSpace == VirtualSpaceBlock }},
		{"indentation_guide", `{"editor": {"indentation_guide": "active"}}`, func(c Config) bool { return c.IndentationGuide == IndentationGuideActive }},
		{"search", `{"editor": {"search": {"case_sensitive": true, "regex": true}}}`, func(c Config) bool {
			return c.Search == SearchDefaults{CaseSensitive: true, Regex: true}
		}},
		{"status_bar partial", `{"editor": {"status_bar": {"separator": " | "}}}`, func(c Config) bool {
			return c.StatusBar.Separator == " | " && len(c.StatusBar.Left) == 7 && len(c.StatusBar.Right) == 8
		}},
		{"file_explorer", `{"file_explorer": {"show_hidden": true, "hide_gitignore": true, "width": 0.3}}`, func(c Config) bool {
			return c.ExplorerShowHidden && c.FileExplorer.HideGitignore
		}},
		{"default_language", `{"default_language": "bash"}`, func(c Config) bool { return c.DefaultLanguage == "bash" }},
		{"empty keybindings list", `{"keybindings": []}`, func(c Config) bool { return c.Keybindings == nil }},
		{"check_for_updates and clipboard", `{"check_for_updates": false, "clipboard": {"use_osc52": true}}`, func(c Config) bool { return !c.Update.Check && c.Clipboard.UseOSC52 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := loadBody(t, tt.body)
			if len(diags) != 0 {
				t.Fatalf("diagnostics: %v", diags)
			}
			if !tt.check(cfg) {
				t.Errorf("config after %s is wrong: %+v", tt.body, cfg)
			}
		})
	}
}

func TestNestedEditorBadValues(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		key     string
		wantMsg string
	}{
		{"editor not an object", `{"editor": 3}`, "editor", "must be a JSON object"},
		{"unknown editor key", `{"editor": {"tabsize": 2}}`, "editor.tabsize", "unknown setting"},
		{"tab_size range", `{"editor": {"tab_size": 99}}`, "editor.tab_size", "between 1 and 16"},
		{"bool as string", `{"editor": {"line_numbers": "yes"}}`, "editor.line_numbers", "must be true or false"},
		{"null", `{"editor": {"line_numbers": null}}`, "editor.line_numbers", "null"},
		{"wheel lines negative", `{"editor": {"mouse_wheel_scroll_lines": -1}}`, "editor.mouse_wheel_scroll_lines", "at least 0"},
		{"negative interval", `{"editor": {"auto_save_interval_secs": -1}}`, "editor.auto_save_interval_secs", "at least 0"},
		{"virtual_space enum", `{"editor": {"virtual_space": "wide"}}`, "editor.virtual_space", `"off", "block" or "on"`},
		{"line ending unknown", `{"editor": {"default_line_ending": "lf2"}}`, "editor.default_line_ending", "must be"},
		{"indentation_guide enum", `{"editor": {"indentation_guide": "some"}}`, "editor.indentation_guide", "must be"},
		{"ruler zero", `{"editor": {"rulers": [0]}}`, "editor.rulers", "at least 1"},
		{"rulers not a list", `{"editor": {"rulers": 80}}`, "editor.rulers", "list of column numbers"},
		{"empty glyph", `{"editor": {"indentation_guide_glyph": ""}}`, "editor.indentation_guide_glyph", "must not be empty"},
		{"search unknown field", `{"editor": {"search": {"fuzzy": true}}}`, "editor.search", "unknown field"},
		{"file_explorer unknown", `{"file_explorer": {"depth": 1}}`, "file_explorer.depth", "unknown setting"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := loadBody(t, tt.body)
			if len(diags) != 1 || diags[0].Key != tt.key || !strings.Contains(diags[0].Message, tt.wantMsg) {
				t.Fatalf("diags = %+v, want one for key %q containing %q", diags, tt.key, tt.wantMsg)
			}
			if !reflect.DeepEqual(cfg, Defaults()) {
				t.Errorf("a rejected key changed the config: %+v", cfg)
			}
		})
	}
}

// The editing and display options are set under "editor" only. Their old flat
// top-level spellings are reported as unknown settings and change nothing.
func TestFlatEditorKeysAreUnknownSettings(t *testing.T) {
	tests := []struct {
		flat   string // the retired top-level key
		nested string // its name under "editor"
		value  string // a JSON value that differs from the default
		get    func(Config) any
		want   any // what get returns once the nested spelling is applied
	}{
		{"line_numbers", "line_numbers", "false", func(c Config) any { return c.LineNumbers }, false},
		{"highlight_current_line", "highlight_current_line", "false", func(c Config) any { return c.HighlightCurrentLine }, false},
		{"highlight_occurrences", "highlight_occurrences", "true", func(c Config) any { return c.HighlightOccurrences }, true},
		{"line_wrap", "line_wrap", "false", func(c Config) any { return c.LineWrap }, false},
		{"show_menu_bar", "show_menu_bar", "false", func(c Config) any { return c.ShowMenuBar }, false},
		{"show_tab_bar", "show_tab_bar", "false", func(c Config) any { return c.ShowTabBar }, false},
		{"show_status_bar", "show_status_bar", "false", func(c Config) any { return c.ShowStatusBar }, false},
		{"show_vertical_scrollbar", "show_vertical_scrollbar", "false", func(c Config) any { return c.ShowVerticalScrollbar }, false},
		{"show_horizontal_scrollbar", "show_horizontal_scrollbar", "false", func(c Config) any { return c.ShowHorizontalScrollbar }, false},
		{"cursor_style", "cursor_style", `"underline"`, func(c Config) any { return c.CursorStyle }, CursorStyleUnderline},
		{"show_whitespace", "whitespace_show", "true", func(c Config) any { return c.ShowWhitespace }, true},
		{"tab_size", "tab_size", "2", func(c Config) any { return c.TabSize }, 2},
		{"auto_indent", "auto_indent", "false", func(c Config) any { return c.AutoIndent }, false},
		{"line_ending", "default_line_ending", `"crlf"`, func(c Config) any { return c.LineEnding }, LineEndingCRLF},
		{"trim_trailing_whitespace", "trim_trailing_whitespace_on_save", "true", func(c Config) any { return c.TrimTrailingWhitespace }, true},
		{"ensure_final_newline", "ensure_final_newline_on_save", "true", func(c Config) any { return c.EnsureFinalNewline }, true},
		{"mouse_hover", "mouse_hover_enabled", "true", func(c Config) any { return c.MouseHover }, true},
	}
	if len(tests) != 17 {
		t.Fatalf("table has %d keys, want the 17 retired flat keys", len(tests))
	}
	def := Defaults()
	for _, tt := range tests {
		t.Run(tt.flat, func(t *testing.T) {
			if reflect.DeepEqual(tt.get(def), tt.want) {
				t.Fatalf("%v is already the default, so this case proves nothing", tt.want)
			}

			cfg, diags := loadBody(t, `{"`+tt.flat+`": `+tt.value+`}`)
			if len(diags) != 1 || diags[0].Key != tt.flat || !strings.Contains(diags[0].Message, "unknown setting") {
				t.Fatalf("diags = %+v, want one unknown setting for %q", diags, tt.flat)
			}
			if got := tt.get(cfg); !reflect.DeepEqual(got, tt.get(def)) {
				t.Errorf("flat %s changed the field to %v, want the default %v", tt.flat, got, tt.get(def))
			}
			if !reflect.DeepEqual(cfg, def) {
				t.Errorf("flat %s changed the config: %+v", tt.flat, cfg)
			}

			cfg, diags = loadBody(t, `{"editor": {"`+tt.nested+`": `+tt.value+`}}`)
			if len(diags) != 0 {
				t.Fatalf("editor.%s diagnostics: %v", tt.nested, diags)
			}
			if got := tt.get(cfg); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("editor.%s = %v, want %v", tt.nested, got, tt.want)
			}
		})
	}
}

// insert_spaces, mouse_capture, scroll_sync and lsp_enabled are real top-level
// settings, not aliases of an editor option, so they stay accepted.
func TestTopLevelBoolKeysStayAccepted(t *testing.T) {
	tests := []struct {
		key   string
		value string
		get   func(Config) bool
		want  bool
	}{
		{"insert_spaces", "false", func(c Config) bool { return c.InsertSpaces }, false},
		{"mouse_capture", "false", func(c Config) bool { return c.MouseCapture }, false},
		{"scroll_sync", "true", func(c Config) bool { return c.ScrollSync }, true},
		{"lsp_enabled", "false", func(c Config) bool { return c.LSPEnabled }, false},
	}
	def := Defaults()
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if tt.get(def) == tt.want {
				t.Fatalf("%v is already the default, so this case proves nothing", tt.want)
			}
			cfg, diags := loadBody(t, `{"`+tt.key+`": `+tt.value+`}`)
			if len(diags) != 0 {
				t.Fatalf("diagnostics: %v", diags)
			}
			if got := tt.get(cfg); got != tt.want {
				t.Errorf("%s = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// The project layer is applied after the user layer, so its editor option wins.
func TestLaterLayerWins(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	user := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, user, `{"editor": {"tab_size": 2}}`)
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"editor": {"tab_size": 8}}`)
	cfg, diags, err := Load(Options{Root: root, UserFile: user})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load: %v %v", err, diags)
	}
	if cfg.TabSize != 8 {
		t.Errorf("TabSize = %d, want 8 from the project layer", cfg.TabSize)
	}
}

func TestSourceOfNestedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"editor": {"scroll_offset": 5}, "languages": {"go": {"tab_size": 4}}}`)
	cfg, _, err := Load(Options{UserFile: path})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"editor.scroll_offset", "languages.go.tab_size"} {
		if got := cfg.Source(key); got != path {
			t.Errorf("Source(%q) = %q, want %q", key, got, path)
		}
	}
	if got := cfg.Source("editor.tab_size"); got != "" {
		t.Errorf("Source of an unset key = %q, want empty", got)
	}
}

func TestExampleConfigLoads(t *testing.T) {
	cfg, diags := loadBody(t, exampleConfig)
	if len(diags) != 0 {
		t.Fatalf("diagnostics for example config: %v", diags)
	}
	checks := []struct {
		name string
		ok   bool
	}{
		{"theme", cfg.Theme == "high-contrast"},
		{"auto_save_enabled", cfg.AutoSaveEnabled && cfg.AutoSaveIntervalSecs == 30},
		{"large file threshold", cfg.LargeFileThresholdBytes == 1048576},
		{"default_language", cfg.DefaultLanguage == "bash"},
		{"lsp rust", len(cfg.LSP["rust"]) == 1 && cfg.LSP["rust"][0].Command == "rust-analyzer"},
		{"lsp go", len(cfg.LSP["go"]) == 1 && cfg.LSP["go"][0].Command == "gopls"},
		{"lsp disabled gdscript", len(cfg.LSP["gdscript"]) == 0 && cfg.LSPDisabled["gdscript"]},
		{"go language", cfg.Editor("go").UseTabs && !cfg.Editor("go").Whitespace.TabsInner},
		{"markdown language", cfg.Editor("markdown").AutoClose == false},
		{"toml language", cfg.Editor("toml").CommentPrefix == "#"},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("check %q failed: %+v", c.name, cfg)
		}
	}
}

func TestLSPFreshShapes(t *testing.T) {
	limits := &ProcessLimits{Enabled: new(bool)}
	*limits.Enabled = true
	tests := []struct {
		name string
		body string
		want map[string][]LSPServer
	}{
		{"a list keeps the enabled servers in order", `{"lsp": {"py": [{"command": "a", "enabled": false}, {"command": "b", "args": ["x"]}, {"command": "c"}]}}`,
			map[string][]LSPServer{"py": {{Command: "b", Args: []string{"x"}}, {Command: "c"}}}},
		{"disabled without command", `{"lsp": {"py": {"enabled": false}}}`, map[string][]LSPServer{}},
		{"fresh fields", `{"lsp": {"go": {"command": "gopls", "auto_start": true, "env": {"A": "b"}, "root_markers": ["go.mod"], "only_features": null, "initialization_options": {"x": 1}, "process_limits": {"enabled": true}}}}`,
			map[string][]LSPServer{"go": {{
				Command:               "gopls",
				RootMarkers:           []string{"go.mod"},
				Env:                   map[string]string{"A": "b"},
				InitializationOptions: json.RawMessage(`{"x": 1}`),
				ProcessLimits:         limits,
			}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := loadBody(t, tt.body)
			if len(diags) != 0 {
				t.Fatalf("diagnostics: %v", diags)
			}
			if !reflect.DeepEqual(cfg.LSP, tt.want) {
				t.Errorf("LSP = %+v, want %+v", cfg.LSP, tt.want)
			}
		})
	}
}

func TestFileExplorerSchemaDefaultsLoadCleanly(t *testing.T) {
	body := `{"file_explorer": {"respect_gitignore": true, "show_hidden": false, "hide_gitignore": true,
		"custom_ignore_patterns": [], "width": 0.3, "preview_tabs": true, "side": "left",
		"follow_active_buffer": false, "auto_open_on_last_buffer_close": true,
		"compact_directories": true}}`
	if _, diags := loadBody(t, body); len(diags) != 0 {
		t.Errorf("diagnostics: %v", diags)
	}
}

func TestZeroWheelLinesAccepted(t *testing.T) {
	cfg, diags := loadBody(t, `{"editor": {"mouse_wheel_scroll_lines": 0, "default_line_ending": "lf"}}`)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	if cfg.MouseWheelScrollLines != 0 || cfg.LineEnding != LineEndingLF {
		t.Errorf("wheel lines %d, line ending %q", cfg.MouseWheelScrollLines, cfg.LineEnding)
	}
}

func TestLSPDisabledAndTurnedOnEntries(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		want         map[string][]LSPServer
		wantDisabled map[string]bool
	}{
		{"disabled without command", `{"lsp": {"go": {"enabled": false}}}`, map[string][]LSPServer{}, map[string]bool{"go": true}},
		{"disabled with command", `{"lsp": {"go": {"command": "gopls", "enabled": false}}}`, map[string][]LSPServer{}, map[string]bool{"go": true}},
		{"a later enabled entry keeps the language on",
			`{"lsp": {"py": [{"command": "a", "enabled": false}, {"command": "b"}]}}`,
			map[string][]LSPServer{"py": {{Command: "b"}}}, nil},
		{"enabled without a command turns the built-in on", `{"lsp": {"odin": {"enabled": true}}}`,
			map[string][]LSPServer{"odin": {{}}}, nil},
		{"auto_start without a command turns the built-in on", `{"lsp": {"odin": {"auto_start": true, "args": ["-x"]}}}`,
			map[string][]LSPServer{"odin": {{Args: []string{"-x"}}}}, nil},
		{"other languages are not disabled", `{"lsp": {"go": {"enabled": false}, "rust": {"command": "ra"}}}`,
			map[string][]LSPServer{"rust": {{Command: "ra"}}}, map[string]bool{"go": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := loadBody(t, tt.body)
			if len(diags) != 0 {
				t.Fatalf("diagnostics: %v", diags)
			}
			if !reflect.DeepEqual(cfg.LSP, tt.want) || !reflect.DeepEqual(cfg.LSPDisabled, tt.wantDisabled) {
				t.Errorf("LSP = %+v disabled %v, want %+v disabled %v", cfg.LSP, cfg.LSPDisabled, tt.want, tt.wantDisabled)
			}
		})
	}
}

func TestLSPEnabledMasterSwitch(t *testing.T) {
	cfg, diags := loadBody(t, `{}`)
	if len(diags) != 0 || !cfg.LSPEnabled {
		t.Fatalf("default: enabled %v diagnostics %v, want on", cfg.LSPEnabled, diags)
	}
	cfg, diags = loadBody(t, `{"lsp_enabled": false}`)
	if len(diags) != 0 || cfg.LSPEnabled {
		t.Errorf("lsp_enabled false: enabled %v diagnostics %v, want off", cfg.LSPEnabled, diags)
	}
	if _, diags = loadBody(t, `{"lsp_enabled": "no"}`); len(diags) != 1 {
		t.Errorf("a string for lsp_enabled gave %d diagnostics, want 1", len(diags))
	}
}

// EditorKeys returns the names of the editor.* options gvim knows, sorted.
func EditorKeys() []string {
	keys := make([]string, 0, len(editorOptions))
	for _, o := range editorOptions {
		keys = append(keys, o.key)
	}
	slices.Sort(keys)
	return keys
}
