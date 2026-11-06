// Package config loads gvim's settings from JSON-with-comments files.
//
// Three layers are merged, lowest first: the built-in defaults, the user file
// (Dir()/config.json (~/.config/gvim/config.json)), and the project file (.gvim/config.json
// in the nearest directory at or above the project root that has one). A layer
// sets only the keys it names. A later layer replaces the value of each key it
// sets and keeps the others.
//
// Files may contain // and /* */ comments and trailing commas (JWCC, parsed by
// github.com/tailscale/hujson). A file that cannot be read or parsed is skipped
// and reported as a Diagnostic. A key with an invalid value, or an unknown key,
// is skipped and reported the same way. Load does not panic on file content.
//
// The package does not choose the platform keymap. Keymap is empty unless a
// file sets it, and the caller applies its own default.
//
// The lsp key maps a language ID to the language server that runs for it:
// {"lsp": {"go": {"command": "gopls", "args": []}}}. No language server runs
// unless the lsp key lists its language, so a fresh install starts none. A later
// layer that sets lsp replaces the whole map.
//
// The formatters key maps a language name, or a file extension with its dot, to the
// command that formats a buffer: {"formatters": {"go": "gofmt", ".go": "gofmt"}}. Keys
// are matched without regard to case, and the language name is tried before the
// extension. A later layer that sets formatters replaces the whole map.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/tailscale/hujson"
)

// Line ending policies accepted by line_ending.
const (
	LineEndingAuto = "auto"
	LineEndingLF   = "lf"
	LineEndingCRLF = "crlf"
)

// Tab width bounds accepted by tab_size.
const (
	MinTabSize = 1
	MaxTabSize = 16
)

// Cursor shapes accepted by cursor_style.
const (
	CursorStyleBlock     = "block"
	CursorStyleBar       = "bar"
	CursorStyleUnderline = "underline"
)

// Icon sets accepted by ui_icons. Auto lets the app pick from ui_icons_font and the
// terminal.
const (
	UIIconsAuto  = "auto"
	UIIconsPlain = "plain"
	UIIconsNerd  = "nerd"
)

// DefaultPluginTimeout is the plugin_timeout_ms default. The plugin host applies the
// same default when it is given no timeout.
const DefaultPluginTimeout = 500 * time.Millisecond

// Plugin timeout bounds, in milliseconds, accepted by plugin_timeout_ms.
const (
	MinPluginTimeoutMS = 1
	MaxPluginTimeoutMS = 60000
)

// LSPServer is the language server for one language, as the lsp key writes it.
// Command is the program, and Args are passed to it in order.
type LSPServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// Config is the effective set of settings after all layers are merged.
type Config struct {
	Theme                  string // theme name, default "dark"
	Keymap                 string // keymap name as written in a file; empty when no file sets one
	TabSize                int    // columns per tab stop, default 4
	InsertSpaces           bool   // insert spaces for Tab, default true
	LineEnding             string // "auto", "lf" or "crlf"; default "auto"
	TrimTrailingWhitespace bool   // strip trailing blanks on save
	EnsureFinalNewline     bool   // end the file with a newline on save
	AutoIndent             bool   // copy indentation to new lines, default true
	LineNumbers            bool   // show the line-number gutter, default true
	ShowMenuBar            bool   // show the menu bar row, default true
	ExplorerShowHidden     bool   // list dot-files in the explorer
	ExplorerShowIgnored    bool   // list files matched by .gitignore in the explorer

	// The view keys set the starting state of the display toggles. The
	// defaults keep the layout gvim had before they existed.
	ShowTabBar              bool   // show the tab strip row, default true
	ShowStatusBar           bool   // show the status row, default true
	ShowPromptLine          bool   // show the prompt row under the status row, default false
	ShowVerticalScrollbar   bool   // draw the vertical scrollbar, default true
	ShowHorizontalScrollbar bool   // draw the horizontal scrollbar when lines are wider, default true
	LineWrap                bool   // wrap long lines, default true
	HighlightCurrentLine    bool   // tint the caret line, default true
	HighlightOccurrences    bool   // highlight the other occurrences of the word at the caret, default false
	ShowWhitespace          bool   // show spaces as dots and tabs as arrows, default false
	MouseCapture            bool   // gvim takes the mouse; false leaves it to the terminal, default true
	MouseHover              bool   // hover information on mouse movement, default false
	ScrollSync              bool   // scroll split views of one buffer together, default false
	CursorStyle             string // "block", "bar" or "underline"; default "block"
	UIIcons                 string // "auto", "plain" or "nerd" glyphs for interface controls; default "auto"
	UIIconsFont             string // font family the terminal uses, for ui_icons "auto"; default empty

	// PluginTimeout is the longest one plugin call may run, default 500ms.
	PluginTimeout time.Duration

	// Keybindings is the raw JSON object of keybinding overrides, or nil. Its
	// schema is the bindings of a keymap file: {"bindings": [...]}. A layer that
	// sets it replaces the value from the layers below it as a whole.
	Keybindings json.RawMessage

	// LSP maps a language ID to its language server, or is nil when no file sets
	// the lsp key. A language that is not in the map runs no server.
	LSP map[string]LSPServer

	// Formatters maps a language name, or a file extension with its dot, to the shell
	// command that formats a buffer, or is nil when no file sets the formatters key.
	// The command reads the text on standard input and writes the result to standard
	// output. A buffer whose language and extension are not in the map has no formatter.
	Formatters map[string]string

	// sources maps each key that a file set to the path of that file.
	sources map[string]string
}

// Defaults returns the built-in settings, the lowest layer.
func Defaults() Config {
	return Config{
		Theme:        "dark",
		TabSize:      4,
		InsertSpaces: true,
		LineEnding:   LineEndingAuto,
		AutoIndent:   true,
		LineNumbers:  true,
		ShowMenuBar:  true,

		ShowTabBar:              true,
		ShowStatusBar:           true,
		ShowVerticalScrollbar:   true,
		ShowHorizontalScrollbar: true,
		LineWrap:                true,
		HighlightCurrentLine:    true,
		MouseCapture:            true,
		CursorStyle:             CursorStyleBlock,
		UIIcons:                 UIIconsAuto,

		PluginTimeout: DefaultPluginTimeout,
	}
}

// Source returns the path of the file that last set key, or "" when no file
// sets it and the default applies. key is the setting name as the files write
// it, for example "keybindings".
func (c Config) Source(key string) string {
	return c.sources[key]
}

// Diagnostic describes one problem found while loading. Loading continues past
// it, and the value it names keeps the value from the layer below.
type Diagnostic struct {
	Path    string // file the problem is in; empty when no file is involved
	Key     string // setting name; empty when the whole file is affected
	Message string // what is wrong, for display
}

// String formats the diagnostic as "path: key: message", leaving out the parts
// that are empty.
func (d Diagnostic) String() string {
	s := d.Message
	if d.Key != "" {
		s = d.Key + ": " + s
	}
	if d.Path != "" {
		s = d.Path + ": " + s
	}
	return s
}

// Options selects the files Load reads.
type Options struct {
	// Root is the project directory. The project file is searched for from
	// Root upward, through each ancestor, and the search stops at the
	// filesystem root. An empty Root skips the project layer.
	Root string

	// UserFile is the user config file. An empty value means the default,
	// Dir()/config.json (~/.config/gvim/config.json).
	UserFile string
}

// Files lists the config files gvim reads by default.
type Files struct {
	User string // Dir()/config.json (~/.config/gvim/config.json)
}

// Paths returns the default config file locations. It fails only when
// Dir fails, which happens when no home directory is known.
func Paths() (Files, error) {
	user, err := defaultUserFile()
	if err != nil {
		return Files{}, err
	}
	return Files{User: user}, nil
}

// Dir returns gvim's per-user directory, which holds config.json, plugins and the
// recent list. It is gvim under $XDG_CONFIG_HOME when that is an absolute path, and
// ~/.config/gvim otherwise, on macOS as on Linux. Windows uses os.UserConfigDir. It
// fails only when no home directory is known.
func Dir() (string, error) {
	if runtime.GOOS == "windows" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("config: user config dir: %w", err)
		}
		return filepath.Join(base, "gvim"), nil
	}
	if base := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(base) {
		return filepath.Join(base, "gvim"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: user config dir: %w", err)
	}
	return filepath.Join(home, ".config", "gvim"), nil
}

// defaultUserFile returns the default user config file path.
func defaultUserFile() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// ProjectFile returns the nearest .gvim/config.json at or above root. ok is
// false when no such file exists between root and the filesystem root. err is
// non-nil only when root cannot be made absolute.
func ProjectFile(root string) (path string, ok bool, err error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false, fmt.Errorf("config: project root: %w", err)
	}
	path, ok = findUp(abs, isRegularFile)
	return path, ok, nil
}

// findUp checks dir/.gvim/config.json, then the same file in each parent
// directory, and returns the first that isFile reports as present. It stops
// after dir reaches the filesystem root, where filepath.Dir returns its input.
func findUp(dir string, isFile func(string) bool) (string, bool) {
	for {
		candidate := filepath.Join(dir, ".gvim", "config.json")
		if isFile(candidate) {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Load reads the three layers and returns the merged settings. The returned
// Diagnostics list every file or key that was skipped. A missing file is not a
// problem and produces no Diagnostic.
//
// The error is non-nil only when opts.Root is set and cannot be made absolute.
// In that case the settings returned are the defaults with the user layer
// applied.
func Load(opts Options) (Config, []Diagnostic, error) {
	cfg := Defaults()
	var diags []Diagnostic

	userPath := opts.UserFile
	if userPath == "" {
		p, err := defaultUserFile()
		if err != nil {
			diags = append(diags, Diagnostic{Message: err.Error()})
		} else {
			userPath = p
		}
	}
	if userPath != "" {
		diags = append(diags, applyFile(&cfg, userPath)...)
	}

	if opts.Root == "" {
		return cfg, diags, nil
	}
	projectPath, ok, err := ProjectFile(opts.Root)
	if err != nil {
		return cfg, diags, err
	}
	if ok {
		diags = append(diags, applyFile(&cfg, projectPath)...)
	}
	return cfg, diags, nil
}

// applyFile reads one layer file and applies each of its keys to cfg. It
// returns the diagnostics for the file. A missing file applies nothing.
func applyFile(cfg *Config, path string) []Diagnostic {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return []Diagnostic{{Path: path, Message: err.Error()}}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	standard, err := hujson.Standardize(data)
	if err != nil {
		return []Diagnostic{{Path: path, Message: "invalid JSON with comments: " + err.Error()}}
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(standard, &keys); err != nil {
		return []Diagnostic{{Path: path, Message: "top level must be a JSON object: " + err.Error()}}
	}

	var diags []Diagnostic
	for _, key := range sortedKeys(keys) {
		if err := cfg.set(key, keys[key]); err != nil {
			diags = append(diags, Diagnostic{Path: path, Key: key, Message: err.Error()})
			continue
		}
		cfg.note(key, path)
	}
	return diags
}

// note records that the file at path set key. A later layer that sets the key
// replaces the record.
func (c *Config) note(key, path string) {
	if c.sources == nil {
		c.sources = make(map[string]string)
	}
	c.sources[key] = path
}

// sortedKeys returns the keys in order, so diagnostics come out in a stable
// order.
func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// set applies one setting. On error the field is left unchanged.
func (c *Config) set(key string, raw json.RawMessage) error {
	if isNull(raw) {
		return fmt.Errorf("null is not a valid value")
	}
	switch key {
	case "theme":
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		if s == "" {
			return fmt.Errorf("must not be empty")
		}
		c.Theme = s
	case "keymap":
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		c.Keymap = s
	case "tab_size":
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			return fmt.Errorf("must be an integer")
		}
		if n < MinTabSize || n > MaxTabSize {
			return fmt.Errorf("must be between %d and %d, got %d", MinTabSize, MaxTabSize, n)
		}
		c.TabSize = n
	case "plugin_timeout_ms":
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			return fmt.Errorf("must be an integer")
		}
		if n < MinPluginTimeoutMS || n > MaxPluginTimeoutMS {
			return fmt.Errorf("must be between %d and %d, got %d", MinPluginTimeoutMS, MaxPluginTimeoutMS, n)
		}
		c.PluginTimeout = time.Duration(n) * time.Millisecond
	case "line_ending":
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		if !slices.Contains([]string{LineEndingAuto, LineEndingLF, LineEndingCRLF}, s) {
			return fmt.Errorf("must be %q, %q or %q, got %q", LineEndingAuto, LineEndingLF, LineEndingCRLF, s)
		}
		c.LineEnding = s
	case "cursor_style":
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		if !slices.Contains([]string{CursorStyleBlock, CursorStyleBar, CursorStyleUnderline}, s) {
			return fmt.Errorf("must be %q, %q or %q, got %q", CursorStyleBlock, CursorStyleBar, CursorStyleUnderline, s)
		}
		c.CursorStyle = s
	case "ui_icons":
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		if !slices.Contains([]string{UIIconsAuto, UIIconsPlain, UIIconsNerd}, s) {
			return fmt.Errorf("must be %q, %q or %q, got %q", UIIconsAuto, UIIconsPlain, UIIconsNerd, s)
		}
		c.UIIcons = s
	case "ui_icons_font":
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		c.UIIconsFont = strings.TrimSpace(s)
	case "keybindings":
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return fmt.Errorf("must be a JSON object")
		}
		c.Keybindings = append(json.RawMessage(nil), trimmed...)
	case "lsp":
		servers, err := decodeLSP(raw)
		if err != nil {
			return err
		}
		c.LSP = servers
	case "formatters":
		formatters, err := decodeFormatters(raw)
		if err != nil {
			return err
		}
		c.Formatters = formatters
	default:
		field, known := boolKeys[key]
		if !known {
			return fmt.Errorf("unknown setting")
		}
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("must be true or false")
		}
		*field(c) = v
	}
	return nil
}

// boolKeys maps each boolean setting to the field it sets.
var boolKeys = map[string]func(*Config) *bool{
	"insert_spaces":             func(c *Config) *bool { return &c.InsertSpaces },
	"trim_trailing_whitespace":  func(c *Config) *bool { return &c.TrimTrailingWhitespace },
	"ensure_final_newline":      func(c *Config) *bool { return &c.EnsureFinalNewline },
	"auto_indent":               func(c *Config) *bool { return &c.AutoIndent },
	"line_numbers":              func(c *Config) *bool { return &c.LineNumbers },
	"show_menu_bar":             func(c *Config) *bool { return &c.ShowMenuBar },
	"explorer_show_hidden":      func(c *Config) *bool { return &c.ExplorerShowHidden },
	"explorer_show_ignored":     func(c *Config) *bool { return &c.ExplorerShowIgnored },
	"show_tab_bar":              func(c *Config) *bool { return &c.ShowTabBar },
	"show_status_bar":           func(c *Config) *bool { return &c.ShowStatusBar },
	"show_prompt_line":          func(c *Config) *bool { return &c.ShowPromptLine },
	"show_vertical_scrollbar":   func(c *Config) *bool { return &c.ShowVerticalScrollbar },
	"show_horizontal_scrollbar": func(c *Config) *bool { return &c.ShowHorizontalScrollbar },
	"line_wrap":                 func(c *Config) *bool { return &c.LineWrap },
	"highlight_current_line":    func(c *Config) *bool { return &c.HighlightCurrentLine },
	"highlight_occurrences":     func(c *Config) *bool { return &c.HighlightOccurrences },
	"show_whitespace":           func(c *Config) *bool { return &c.ShowWhitespace },
	"mouse_capture":             func(c *Config) *bool { return &c.MouseCapture },
	"mouse_hover":               func(c *Config) *bool { return &c.MouseHover },
	"scroll_sync":               func(c *Config) *bool { return &c.ScrollSync },
}

// decodeLSP reads the lsp key: an object from language ID to LSPServer. Each
// server must name a command, and it may not have other fields.
func decodeLSP(raw json.RawMessage) (map[string]LSPServer, error) {
	trimmed := bytes.TrimSpace(raw)
	var entries map[string]json.RawMessage
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &entries) != nil {
		return nil, fmt.Errorf("must be a JSON object")
	}
	servers := make(map[string]LSPServer, len(entries))
	for _, lang := range sortedKeys(entries) {
		dec := json.NewDecoder(bytes.NewReader(entries[lang]))
		dec.DisallowUnknownFields()
		var s LSPServer
		if err := dec.Decode(&s); err != nil {
			return nil, fmt.Errorf("%s: %w", lang, err)
		}
		if s.Command == "" {
			return nil, fmt.Errorf("%s: command must not be empty", lang)
		}
		servers[lang] = s
	}
	return servers, nil
}

// decodeFormatters reads the formatters key: an object from language name to the
// command that formats it. Each command must be non-blank, and each language name
// must be non-empty.
func decodeFormatters(raw json.RawMessage) (map[string]string, error) {
	var formatters map[string]string
	if err := json.Unmarshal(raw, &formatters); err != nil {
		return nil, fmt.Errorf("must be a JSON object of strings")
	}
	for _, lang := range slices.Sorted(maps.Keys(formatters)) {
		if lang == "" {
			return nil, fmt.Errorf("language name must not be empty")
		}
		if strings.TrimSpace(formatters[lang]) == "" {
			return nil, fmt.Errorf("%s: command must not be empty", lang)
		}
	}
	return formatters, nil
}

func decodeString(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("must be a string")
	}
	return s, nil
}

func isNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}
