package app

import (
	"encoding/json"
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
	ExplorerShowHidden     bool            `json:"explorer_show_hidden"`
	ExplorerShowIgnored    bool            `json:"explorer_show_ignored"`
	Keybindings            json.RawMessage `json:"keybindings,omitempty"`
}

// LoadConfig merges the default, user and project config layers for root and
// returns the result as printable text. A root that is not absolute is made
// absolute first. The error is non-nil only when the result cannot be built.
func LoadConfig(root string) (ConfigView, error) {
	cfg, diags, err := config.Load(config.Options{Root: root})
	if err != nil {
		return ConfigView{}, err
	}
	keymapName := cfg.Keymap
	if keymapName == "" {
		keymapName = keymap.DefaultName()
	}
	data, err := json.MarshalIndent(effectiveSettings{
		Theme:                  cfg.Theme,
		Keymap:                 keymapName,
		TabSize:                cfg.TabSize,
		InsertSpaces:           cfg.InsertSpaces,
		LineEnding:             cfg.LineEnding,
		TrimTrailingWhitespace: cfg.TrimTrailingWhitespace,
		EnsureFinalNewline:     cfg.EnsureFinalNewline,
		AutoIndent:             cfg.AutoIndent,
		LineNumbers:            cfg.LineNumbers,
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
// root. The project entry is the nearest .gvim/config.json at or above root.
// When none exists, its path is the one under root, and Exists is false. The
// error is non-nil only when the user config directory is unknown.
func ConfigPaths(root string) ([]ConfigFile, error) {
	files, err := config.Paths()
	if err != nil {
		return nil, err
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
		{Label: "user", Path: files.User, Exists: isFile(files.User)},
		{Label: "project", Path: proj, Exists: found},
	}, nil
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
