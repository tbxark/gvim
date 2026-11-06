package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/keymap"
)

// ConfigFile is one config file location and whether a file is there.
type ConfigFile struct {
	// Label names the layer: "user" or "project".
	Label string
	// Path is the file location. For a project with no file, it is the
	// location a project file would take under the root.
	Path   string
	Exists bool
}

// ConfigView is the effective configuration, ready to print.
type ConfigView struct {
	// JSON is the effective settings as indented JSON, using the key names of
	// the config files. It ends with a newline.
	JSON []byte
	// Warnings describe each file or key that was skipped while loading. They
	// do not stop loading.
	Warnings []string
}

// effectiveSettings is the JSON shape of ConfigView.JSON. Keys follow the
// config file names. An empty keymap in the files shows as the platform
// default keymap.
type effectiveSettings struct {
	Theme                  string          `json:"theme"`
	Keymap                 string          `json:"keymap"`
	TabSize                int             `json:"tab_size"`
	InsertSpaces           bool            `json:"insert_spaces"`
	LineEnding             string          `json:"line_ending"`
	TrimTrailingWhitespace bool            `json:"trim_trailing_whitespace"`
	EnsureFinalNewline     bool            `json:"ensure_final_newline"`
	AutoIndent             bool            `json:"auto_indent"`
	LineNumbers            bool            `json:"line_numbers"`
	ShowMenuBar            bool            `json:"show_menu_bar"`
	ExplorerShowHidden     bool            `json:"explorer_show_hidden"`
	ExplorerShowIgnored    bool            `json:"explorer_show_ignored"`
	Keybindings            json.RawMessage `json:"keybindings,omitempty"`
}

// LoadConfig merges the default, user and project config layers for root and
// returns the result as printable text. A non-empty opts.Config replaces the
// default user file, as the --config flag does. A non-empty opts.Keymap and
// opts.Theme replace the keymap and theme the files name, as --keymap and
// --theme do; the defaults apply when neither is set. Other fields of opts are
// not used. A root that is not absolute is made absolute first. The error is
// non-nil when opts is invalid, when the result cannot be built, or when
// opts.Config is set and does not exist.
func LoadConfig(root string, opts Options) (ConfigView, error) {
	if err := opts.Validate(); err != nil {
		return ConfigView{}, err
	}
	if err := checkConfigFile(opts.Config); err != nil {
		return ConfigView{}, err
	}
	cfg, diags, err := config.Load(config.Options{Root: root, UserFile: opts.Config})
	if err != nil {
		return ConfigView{}, err
	}
	keymapName := opts.Keymap
	if keymapName == "" {
		keymapName = cfg.Keymap
	}
	if keymapName == "" {
		keymapName = keymap.DefaultName()
	}
	themeName := opts.Theme
	if themeName == "" {
		themeName = cfg.Theme
	}
	data, err := json.MarshalIndent(effectiveSettings{
		Theme:                  themeName,
		Keymap:                 keymapName,
		TabSize:                cfg.TabSize,
		InsertSpaces:           cfg.InsertSpaces,
		LineEnding:             cfg.LineEnding,
		TrimTrailingWhitespace: cfg.TrimTrailingWhitespace,
		EnsureFinalNewline:     cfg.EnsureFinalNewline,
		AutoIndent:             cfg.AutoIndent,
		LineNumbers:            cfg.LineNumbers,
		ShowMenuBar:            cfg.ShowMenuBar,
		ExplorerShowHidden:     cfg.ExplorerShowHidden,
		ExplorerShowIgnored:    cfg.ExplorerShowIgnored,
		Keybindings:            cfg.Keybindings,
	}, "", "  ")
	if err != nil {
		return ConfigView{}, err
	}
	view := ConfigView{JSON: append(data, '\n')}
	for _, d := range diags {
		view.Warnings = append(view.Warnings, d.String())
	}
	return view, nil
}

// ConfigPaths returns the user config file and the project config file for
// root. A non-empty configPath stands in for the user file, as it does in
// LoadConfig. The project entry is the nearest .gvim/config.json at or above
// root. When none exists, its path is the one under root, and Exists is false.
// The error is non-nil only when configPath is empty and the user config
// directory is unknown.
func ConfigPaths(root, configPath string) ([]ConfigFile, error) {
	user := configPath
	if user == "" {
		files, err := config.Paths()
		if err != nil {
			return nil, err
		}
		user = files.User
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	proj, found, err := config.ProjectFile(abs)
	if err != nil {
		return nil, err
	}
	if !found {
		proj = filepath.Join(abs, ".gvim", "config.json")
	}
	return []ConfigFile{
		{Label: "user", Path: user, Exists: isFile(user)},
		{Label: "project", Path: proj, Exists: found},
	}, nil
}

// checkConfigFile returns an error that names path when a config file was given
// with --config and does not exist. An empty path is the default user file,
// which may be absent, so it passes. Other stat failures are left to
// config.Load, which reports them as diagnostics.
func checkConfigFile(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("config file %s does not exist", path)
	}
	return nil
}

// isFile reports whether path is an existing regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// GrammarNames returns the names and aliases of the syntax grammars chroma
// knows. The order is not specified, and names may repeat.
func GrammarNames() []string {
	return lexers.Names(true)
}
