package app

import (
	"context"
	"fmt"
	"os"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/explorer"
	"github.com/tbxark/gvim/internal/finder"
	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui"
	"github.com/tbxark/gvim/internal/workspace"
)

// defaultTheme is the theme used when Options.Theme is empty.
const defaultTheme = "dark"

// Run validates opts, builds the terminal UI and runs it in the alternate
// screen until the user quits. Canceling ctx stops the program, and Run then
// returns the context's error.
func Run(ctx context.Context, opts Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	model, files, err := buildModel(ctx, opts)
	if err != nil {
		return err
	}
	// The file index stops enumerating when the program ends.
	defer files.Close()
	return ui.Run(ctx, model)
}

// buildModel opens the files and returns the UI model that shows them, with the
// file index of the root started in the background. It does not start the
// program. The caller closes the index. A directory root is recorded in the
// recent list before the files are opened.
func buildModel(ctx context.Context, opts Options) (*ui.Model, *finder.Index, error) {
	root, err := resolveRoot(opts.Root)
	if err != nil {
		return nil, nil, err
	}
	tree, err := explorer.Open(root)
	if err != nil {
		return nil, nil, err
	}
	// Without files the editor starts empty, and the welcome screen shows.
	ed := editor.New(editor.Options{Empty: len(opts.Files) == 0})
	for _, f := range opts.Files {
		if err := ed.OpenFile(f.Path); err != nil {
			return nil, nil, err
		}
		if f.Line > 0 {
			if err := ed.GotoLine(f.Line, f.Col); err != nil {
				return nil, nil, err
			}
		}
	}
	km, err := keymapFor(opts.Keymap)
	if err != nil {
		return nil, nil, err
	}
	name := opts.Theme
	if name == "" {
		name = defaultTheme
	}
	th, err := theme.Load(name)
	if err != nil {
		return nil, nil, err
	}
	files := startFiles(ctx, root)
	deps := newUIDeps(ed, km, th, tree, files)
	// A folder argument shows the explorer at startup. Without one the explorer
	// stays hidden until Ctrl+B.
	deps.ShowExplorer = opts.Root != ""
	return ui.New(deps), files, nil
}

// resolveRoot returns the absolute explorer root. An empty dir means the
// current directory. Otherwise dir is opened as a workspace and recorded in
// the recent list. A failure to record it is a warning on stderr, and the
// root is still used.
func resolveRoot(dir string) (string, error) {
	if dir == "" {
		return os.Getwd()
	}
	r, err := workspace.Switch(dir)
	if r.Path() == "" {
		if err == nil {
			err = fmt.Errorf("open workspace %s: no root", dir)
		}
		return "", err
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning:", err)
	}
	return r.Path(), nil
}

// keymapFor returns the named built-in keymap. An empty name is the platform
// default.
func keymapFor(name string) (*keymap.Map, error) {
	if name == "" {
		return keymap.Default(), nil
	}
	return keymap.ByName(name)
}
