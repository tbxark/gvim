package config

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// freshEditorDefaults is the "default" of the editor property in Fresh's
// config-schema.json. It is copied here so that the tests do not read refs/.
const freshEditorDefaults = `{
  "animations": true,
  "cursor_jump_animation": true,
  "viewport_edge_fade": true,
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
  "screensaver_enabled": false,
  "screensaver_idle_minutes": 5,
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
  "show_prompt_line": false,
  "show_vertical_scrollbar": true,
  "show_horizontal_scrollbar": false,
  "show_tilde": true,
  "nerd_font_icons": false,
  "use_terminal_bg": false,
  "set_window_title": true,
  "terminal_auto_title": true,
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
  "enable_semantic_tokens_full": false,
  "diagnostics_inline_text": false,
  "mouse_wheel_scroll_lines": 3,
  "smooth_scroll": true,
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
  "auto_create_empty_buffer_on_last_buffer_close": true,
  "recovery_enabled": true,
  "auto_recovery_save_interval_secs": 2,
  "auto_revert_poll_interval_ms": 2000,
  "keyboard_disambiguate_escape_codes": true,
  "keyboard_report_event_types": false,
  "keyboard_report_alternate_keys": true,
  "keyboard_report_all_keys_as_escape_codes": false,
  "snapshot_interval": 100,
  "highlight_context_bytes": 10000,
  "large_file_threshold_bytes": 10485760,
  "estimated_line_length": 80,
  "read_concurrency": 64,
  "file_tree_poll_interval_ms": 3000
}`

// freshExampleConfig is Fresh's config.example.json, cut down: every top-level key
// is kept, and the languages, lsp and menu sections are shortened.
const freshExampleConfig = `{
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
    "snapshot_interval": 100,
    "large_file_threshold_bytes": 1048576,
    "estimated_line_length": 80,
    "enable_inlay_hints": true,
    "enable_semantic_tokens_full": false,
    "auto_save_enabled": true,
    "auto_save_interval_secs": 30,
    "recovery_enabled": true,
    "auto_recovery_save_interval_secs": 2,
    "highlight_context_bytes": 10000,
    "mouse_hover_enabled": true,
    "mouse_hover_delay_ms": 500,
    "double_click_time_ms": 500,
    "auto_revert_poll_interval_ms": 2000,
    "file_tree_poll_interval_ms": 3000
  },
  "file_explorer": {
    "respect_gitignore": true,
    "show_hidden": false,
    "show_gitignored": false,
    "custom_ignore_patterns": [],
    "width": 0.3
  },
  "clipboard": {
    "use_osc52": true,
    "use_system_clipboard": true
  },
  "terminal": {
    "jump_to_end_on_output": true
  },
  "keybindings": [],
  "keybinding_maps": {},
  "active_keybinding_map": "default",
  "default_language": "bash",
  "languages": {
    "toml": {
      "extensions": [
        "toml"
      ],
      "filenames": [
        "Cargo.lock"
      ],
      "grammar": "toml",
      "comment_prefix": "#",
      "auto_indent": true,
      "highlighter": "auto",
      "textmate_grammar": null,
      "show_whitespace_tabs": true,
      "use_tabs": false
    },
    "go": {
      "extensions": [
        "go"
      ],
      "filenames": [],
      "grammar": "go",
      "comment_prefix": "//",
      "auto_indent": true,
      "highlighter": "auto",
      "textmate_grammar": null,
      "show_whitespace_tabs": false,
      "use_tabs": true
    },
    "markdown": {
      "extensions": [
        "md",
        "markdown"
      ],
      "filenames": [
        "README"
      ],
      "grammar": "markdown",
      "comment_prefix": null,
      "auto_indent": false,
      "highlighter": "auto",
      "textmate_grammar": null,
      "show_whitespace_tabs": true,
      "use_tabs": false
    },
    "makefile": {
      "extensions": [
        "mk"
      ],
      "filenames": [
        "Makefile",
        "makefile",
        "GNUmakefile"
      ],
      "grammar": "make",
      "comment_prefix": "#",
      "auto_indent": false,
      "highlighter": "auto",
      "textmate_grammar": null,
      "show_whitespace_tabs": true,
      "use_tabs": true
    }
  },
  "lsp": {
    "rust": {
      "command": "rust-analyzer",
      "args": [
        "--log-file",
        "/tmp/rust-analyzer-77535.log"
      ],
      "enabled": true,
      "auto_start": false,
      "process_limits": {
        "max_memory_percent": 50,
        "max_cpu_percent": 90,
        "enabled": true
      },
      "initialization_options": {
        "checkOnSave": false,
        "cachePriming": {
          "enable": false
        },
        "procMacro": {
          "enable": false
        },
        "cargo": {
          "buildScripts": {
            "enable": false
          },
          "autoreload": false
        },
        "diagnostics": {
          "enable": true
        },
        "files": {
          "watcher": "server"
        }
      }
    },
    "gdscript": {
      "command": "nc",
      "args": [
        "127.0.0.1",
        "6005"
      ],
      "enabled": false,
      "auto_start": false,
      "process_limits": {
        "max_memory_percent": 50,
        "max_cpu_percent": 90,
        "enabled": true
      },
      "initialization_options": null
    },
    "go": {
      "command": "gopls",
      "args": [],
      "enabled": true,
      "auto_start": false,
      "process_limits": {
        "max_memory_percent": 50,
        "max_cpu_percent": 90,
        "enabled": true
      },
      "initialization_options": null
    }
  },
  "menu": {
    "menus": [
      {
        "label": "File",
        "items": [
          {
            "label": "New File",
            "action": "new",
            "args": {},
            "when": null,
            "checkbox": null
          },
          {
            "label": "Open File...",
            "action": "open",
            "args": {},
            "when": null,
            "checkbox": null
          },
          {
            "separator": true
          }
        ]
      }
    ]
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

func TestEditorDefaultsAreFreshs(t *testing.T) {
	var fresh map[string]json.RawMessage
	if err := json.Unmarshal([]byte(freshEditorDefaults), &fresh); err != nil {
		t.Fatal(err)
	}
	// Parsing Fresh's own default object must give exactly Defaults().EditorOptions.
	cfg, diags := loadBody(t, `{"editor": `+freshEditorDefaults+`}`)
	if len(diags) != 0 {
		t.Fatalf("diagnostics for Fresh's defaults: %v", diags)
	}
	if got, want := cfg.EditorOptions, Defaults().EditorOptions; !reflect.DeepEqual(got, want) {
		t.Errorf("EditorOptions parsed from Fresh's defaults = %+v\nDefaults().EditorOptions = %+v", got, want)
	}
	// Every Fresh editor key is known, and gvim knows no others.
	keys := make([]string, 0, len(fresh))
	for k := range fresh {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if got := EditorKeys(); !slices.Equal(got, keys) {
		t.Errorf("EditorKeys() differs from the schema's editor keys\n got  %v\n want %v", got, keys)
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
		{"auto_create_empty_buffer_on_last_buffer_close", d.AutoCreateEmptyBufferOnLastBufferClose, true},
		{"snapshot_interval", d.SnapshotInterval, 100},
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
		{"file_explorer", `{"file_explorer": {"show_hidden": true, "show_gitignored": true, "width": 0.3}}`, func(c Config) bool {
			return c.ExplorerShowHidden && c.ExplorerShowIgnored
		}},
		{"default_language", `{"default_language": "bash"}`, func(c Config) bool { return c.DefaultLanguage == "bash" }},
		{"empty keybindings list", `{"keybindings": []}`, func(c Config) bool { return c.Keybindings == nil }},
		{"ignored fresh keys", `{"check_for_updates": false, "clipboard": {"use_osc52": true}, "version": 1}`, func(c Config) bool { return true }},
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

func TestFlatAliasesAndPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		check func(Config) bool
	}{
		{"flat alone", `{"tab_size": 2, "insert_spaces": false, "show_whitespace": true, "line_ending": "crlf", "mouse_hover": true, "trim_trailing_whitespace": true, "ensure_final_newline": true}`,
			func(c Config) bool {
				return c.TabSize == 2 && !c.InsertSpaces && c.ShowWhitespace && c.LineEnding == LineEndingCRLF &&
					c.MouseHover && c.TrimTrailingWhitespace && c.EnsureFinalNewline
			}},
		{"nested wins over flat, flat first", `{"tab_size": 8, "editor": {"tab_size": 2}}`, func(c Config) bool { return c.TabSize == 2 }},
		{"nested use_tabs wins over flat insert_spaces", `{"insert_spaces": false, "editor": {"use_tabs": false}}`, func(c Config) bool { return c.InsertSpaces }},
		{"nested alias name wins", `{"show_whitespace": true, "editor": {"whitespace_show": false}}`, func(c Config) bool { return !c.ShowWhitespace }},
		{"file_explorer wins over flat", `{"explorer_show_hidden": true, "file_explorer": {"show_hidden": false}}`, func(c Config) bool { return !c.ExplorerShowHidden }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := loadBody(t, tt.body)
			if len(diags) != 0 {
				t.Fatalf("diagnostics: %v", diags)
			}
			if !tt.check(cfg) {
				t.Errorf("config is wrong: %+v", cfg)
			}
		})
	}
}

func TestLaterLayerWinsWhateverTheSpelling(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	user := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, user, `{"editor": {"tab_size": 2}}`)
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"tab_size": 8}`)
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

func TestFreshExampleConfigLoads(t *testing.T) {
	cfg, diags := loadBody(t, freshExampleConfig)
	if len(diags) != 0 {
		t.Fatalf("diagnostics for Fresh's example config: %v", diags)
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

func TestFreshSchemaDefaultsLoadCleanly(t *testing.T) {
	body := `{"file_explorer": {"respect_gitignore": true, "show_hidden": false, "show_gitignored": false,
		"custom_ignore_patterns": [], "width": 0.3, "preview_tabs": true, "side": "left",
		"follow_active_buffer": false, "auto_open_on_last_buffer_close": true,
		"compact_directories": true, "tree_indicator_collapsed": ">", "tree_indicator_expanded": "▼"}}`
	if _, diags := loadBody(t, body); len(diags) != 0 {
		t.Errorf("diagnostics: %v", diags)
	}
}

func TestFreshOnlyValuesAreAccepted(t *testing.T) {
	cfg, diags := loadBody(t, `{"editor": {"mouse_wheel_scroll_lines": 0, "default_line_ending": "cr"}}`)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	if cfg.MouseWheelScrollLines != 0 || cfg.LineEnding != LineEndingCR {
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
