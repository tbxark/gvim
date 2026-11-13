package uiadapter

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/dialog"
	"github.com/tbxark/gvim/internal/tui/style"
)

// dialogPort is the root's Dialogs port. It builds the dialogs of
// internal/tui/dialog and answers the Save As prompt's path questions from the
// disk, so the root does no file IO itself.
type dialogPort struct{}

// Unsaved opens the unsaved-changes dialog for the tab called name.
func (dialogPort) Unsaved(name string, tag any, ov style.Overlay) tui.Overlay {
	return dialog.NewUnsavedChanges(time.Now, name, tag, ov)
}

// Quit asks whether to quit when nothing is unsaved (editor.confirm_quit).
func (dialogPort) Quit(tag any, ov style.Overlay) tui.Overlay {
	return dialog.NewConfirmQuit(time.Now, tag, ov)
}

// Error shows message with one OK button.
func (dialogPort) Error(message string, tag any, ov style.Overlay) tui.Overlay {
	return dialog.NewErrorDialog(time.Now, message, tag, ov)
}

// SaveAs opens the Save As prompt prefilled with path. A path whose parent
// folder is missing is refused inline, and an existing file needs a confirm.
func (dialogPort) SaveAs(path string, tag any, ov style.Overlay) tui.Overlay {
	checks := dialog.PathChecks{
		Exists: func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		},
		Validate: func(p string) error {
			dir, err := os.Stat(filepath.Dir(p))
			if err != nil {
				return errors.New("folder does not exist")
			}
			if !dir.IsDir() {
				return errors.New("parent is not a folder")
			}
			return nil
		},
	}
	return dialog.NewSaveAs(time.Now, path, tag, checks, ov)
}
