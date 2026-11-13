// Package configview backs the `gvim config` and `gvim grammar` commands: it
// renders the effective configuration for printing and reports the config file
// locations. It is private to the executable shell: internal/cli imports it,
// and it imports config, keybindings and keymap, never app or the TUI.
package configview

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/keybindings"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
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

// Options are the command-line flags that change the effective configuration in
// a view. They are the config-relevant fields of app.Options; the zero value
// uses the config files as they are.
type Options struct {
	// Config is the user config file path. Empty means the default location.
	Config string
	// Keymap and Theme override the names the config files set.
	Keymap string
	Theme  string
	// Trusted tells Load whether the workspace's recorded trust level allows the
	// project config layer (docs/trust.md). The caller resolves it read-only; nil
	// (no trust information) keeps the project layer out, the conservative choice.
	Trusted *bool
}

// KeymapNames lists the built-in keymaps, for help text and completion. Names are
// checked by ValidateFlags.
func KeymapNames() []string { return keybindings.BuiltinKeymaps }

// validate reports the first option that cannot be used.
func (o Options) validate() error { return ValidateFlags(o.Keymap, o.Theme) }

// ValidateFlags reports whether a --keymap and a --theme value can be used: a built-in
// keymap, and a theme in a valid form that exists when it is a plain name. An empty
// value is not checked. app.Options.Validate gives the same errors, so `gvim config
// show` rejects a bad flag with the same text as `gvim`.
func ValidateFlags(keymapName, themeName string) error {
	if keymapName != "" {
		if _, err := keymap.ByName(keymapName); err != nil {
			return fmt.Errorf("--keymap: unknown keymap %q (want default, macos, macos-gui, emacs or vscode)", keymapName)
		}
	}
	if themeName != "" {
		if err := theme.ValidateForm(themeName); err != nil {
			return fmt.Errorf("--theme: %w", err)
		}
		if theme.IsNamedForm(themeName) && !slices.Contains(theme.Names(), themeName) {
			dir, dirErr := config.Dir()
			if dirErr == nil {
				registry := theme.NewRegistry(filepath.Join(dir, "themes"))
				if _, err := registry.Load(themeName); err != nil {
					return fmt.Errorf("--theme: unknown theme %q (want one of the bundled themes, a theme in %s, or a theme path)", themeName, filepath.Join(dir, "themes"))
				}
			}
		}
	}
	return nil
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

// Load merges the default, user and project config layers for root and returns
// the result as printable text. A non-empty opts.Config replaces the default
// user file, as the --config flag does. A non-empty opts.Keymap and opts.Theme
// replace the keymap and theme the files name; the defaults apply when neither
// is set. A root that is not absolute is made absolute first. The error is
// non-nil when opts is invalid, when the result cannot be built, or when
// opts.Config is set and does not exist.
//
// The project layer is trust-gated exactly as the editor's config load is
// (docs/trust.md): it is merged only when the workspace's recorded trust level
// allows project config, so an untrusted or undecided workspace shows the
// configuration the editor would actually run with. When a project file exists
// but the gate skips it, the view carries a warning naming the file.
func Load(root string, opts Options) (ConfigView, error) {
	if err := opts.validate(); err != nil {
		return ConfigView{}, err
	}
	if err := CheckFile(opts.Config); err != nil {
		return ConfigView{}, err
	}
	trusted := opts.Trusted != nil && *opts.Trusted
	projectPath, projectFound := projectConfigFile(root)
	cfg, diags, err := config.Load(config.Options{Root: root, UserFile: opts.Config, Untrusted: !trusted})
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
	if !trusted && projectFound {
		view.Warnings = append(view.Warnings, fmt.Sprintf(
			"project config layer skipped: %s (the workspace is not trusted; start gvim in it to decide trust)", projectPath))
	}
	view.Warnings = append(view.Warnings, keybindings.Warnings(cfg)...)
	return view, nil
}

// projectConfigFile returns the project config file the nearest-project search finds
// for root. The trust decision itself is the caller's (Options.Trusted): this package is
// a config/keybindings leaf and does not read the trust store.
func projectConfigFile(root string) (path string, found bool) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	path, found, err = config.ProjectFile(abs)
	if err != nil {
		return path, false
	}
	return path, found
}

// Paths returns the user config file and the project config file for root. A
// non-empty configPath stands in for the user file, as it does in Load. The
// project entry is the nearest .gvim/config.json at or above root. When none
// exists, its path is the one under root, and Exists is false. The error is
// non-nil only when configPath is empty and the user config directory is
// unknown.
func Paths(root, configPath string) ([]ConfigFile, error) {
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

// CheckFile returns an error that names path when a config file was given with
// --config and does not exist. An empty path is the default user file,
// which may be absent, so it passes. Other stat failures are left to
// config.Load, which reports them as diagnostics.
func CheckFile(path string) error {
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
