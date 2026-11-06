// Package app is the composition root of gvim. It turns the parsed command
// line into a running Bubble Tea program. It is the only package that imports
// the concrete UI and domain packages together; the command-line layer in
// internal/cli sees only the Options type defined here.
package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/theme"
)

// FileTarget is one file argument. Line and Col are 1-based. Zero means the
// position was not given, and the file opens at its start.
type FileTarget struct {
	Path string
	Line int
	Col  int
}

// Options is the parsed command line. The zero value starts gvim with no
// files in the current directory, the platform keymap and the dark theme.
type Options struct {
	// Files open in order, each in its own tab. A missing file opens as an
	// empty buffer that is created on save.
	Files []FileTarget
	// Root is the directory the file explorer shows. Empty means the current
	// directory. A non-empty root is also recorded in the recent list.
	Root string
	// Config is the user config file path. Empty means the default location,
	// ~/.config/gvim/config.json (config.Dir). A non-empty value replaces that
	// default user file. The project file (.gvim/config.json at or above Root)
	// still applies on top of it. A missing file is an error that names the path,
	// so a typo does not silently drop the default user file.
	Config string
	// Keymap is a built-in keymap name. Empty means the config's keymap, or the
	// platform default when no config file sets one.
	Keymap string
	// Theme is a bundled theme name. Empty means the config's theme, which is
	// "dark" unless a config file sets another.
	Theme string
	// LogFile is the log file path. Empty means no log file. Logging is not
	// implemented yet, so the value is accepted and not written.
	LogFile string
	// SessionDir holds the saved session: the open tabs, the unsaved text and the
	// explorer state. Run saves it when the program ends. Run uses
	// os.UserCacheDir()/gvim/session when this is empty. Without a directory, the
	// session is not restored either, so a caller that builds the model for a test
	// does not read the user's session.
	SessionDir string
	// AutoSaveInterval is how often the modified, named files are saved. Zero defers to
	// the config: editor.auto_save_enabled turns auto-save on at
	// editor.auto_save_interval_secs, and it is off when the config does not enable it.
	// A non-zero value wins over the config.
	AutoSaveInterval time.Duration
	// Diff opens the two files, in tabs 0 and 1, in the compare view. It needs exactly
	// two files and no folder.
	Diff bool
}

// Validate reports the first option that cannot be used. It does not touch
// the file system: a missing file is valid, and a root is checked when Run
// opens it.
func (o Options) Validate() error {
	if o.Keymap != "" {
		if _, err := keymap.ByName(o.Keymap); err != nil {
			return fmt.Errorf("--keymap: unknown keymap %q (want default, macos, macos-gui, emacs or vscode)", o.Keymap)
		}
	}
	if o.Theme != "" && !slices.Contains(theme.Names(), o.Theme) {
		return fmt.Errorf("--theme: unknown theme %q (want %s)", o.Theme, strings.Join(theme.Names(), ", "))
	}
	if o.Diff {
		if len(o.Files) != 2 {
			return errors.New("--diff needs exactly two files")
		}
		if o.Root != "" {
			return errors.New("--diff compares two files, not a folder")
		}
	}
	for _, f := range o.Files {
		if f.Path == "" {
			return errors.New("file path is empty")
		}
		if f.Line < 0 || f.Col < 0 {
			return fmt.Errorf("%s: line and column must not be negative", f.Path)
		}
		if f.Col > 0 && f.Line == 0 {
			return fmt.Errorf("%s: a column needs a line", f.Path)
		}
	}
	return nil
}
