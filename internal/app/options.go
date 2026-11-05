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
	// Config is the config file path. Empty means the default location.
	// Config loading is not implemented yet, so the value is accepted and
	// not read.
	Config string
	// Keymap is a built-in keymap name. Empty means the platform default.
	Keymap string
	// Theme is a bundled theme name. Empty means "dark".
	Theme string
	// LogFile is the log file path. Empty means no log file. Logging is not
	// implemented yet, so the value is accepted and not written.
	LogFile string
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
