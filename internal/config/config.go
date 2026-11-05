// Package config loads gvim's settings from JSON-with-comments files.
//
// Three layers are merged, lowest first: the built-in defaults, the user file
// (os.UserConfigDir()/gvim/config.json), and the project file (.gvim/config.json
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
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

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
	ExplorerShowHidden     bool   // list dot-files in the explorer
	ExplorerShowIgnored    bool   // list files matched by .gitignore in the explorer

	// Keybindings is the raw JSON object of keybinding overrides, or nil. A
	// layer that sets it replaces the value from the layers below it as a whole.
	Keybindings json.RawMessage
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
	}
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
	// os.UserConfigDir()/gvim/config.json.
	UserFile string
}

// Files lists the config files gvim reads by default.
type Files struct {
	User string // os.UserConfigDir()/gvim/config.json
}

// Paths returns the default config file locations. It fails only when
// os.UserConfigDir fails, which happens when no home directory is known.
func Paths() (Files, error) {
	user, err := defaultUserFile()
	if err != nil {
		return Files{}, err
	}
	return Files{User: user}, nil
}

// defaultUserFile returns the default user config file path.
func defaultUserFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: user config dir: %w", err)
	}
	return filepath.Join(dir, "gvim", "config.json"), nil
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
		}
	}
	return diags
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
	case "line_ending":
		s, err := decodeString(raw)
		if err != nil {
			return err
		}
		if !slices.Contains([]string{LineEndingAuto, LineEndingLF, LineEndingCRLF}, s) {
			return fmt.Errorf("must be %q, %q or %q, got %q", LineEndingAuto, LineEndingLF, LineEndingCRLF, s)
		}
		c.LineEnding = s
	case "keybindings":
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return fmt.Errorf("must be a JSON object")
		}
		c.Keybindings = append(json.RawMessage(nil), trimmed...)
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
	"insert_spaces":            func(c *Config) *bool { return &c.InsertSpaces },
	"trim_trailing_whitespace": func(c *Config) *bool { return &c.TrimTrailingWhitespace },
	"ensure_final_newline":     func(c *Config) *bool { return &c.EnsureFinalNewline },
	"auto_indent":              func(c *Config) *bool { return &c.AutoIndent },
	"line_numbers":             func(c *Config) *bool { return &c.LineNumbers },
	"explorer_show_hidden":     func(c *Config) *bool { return &c.ExplorerShowHidden },
	"explorer_show_ignored":    func(c *Config) *bool { return &c.ExplorerShowIgnored },
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
