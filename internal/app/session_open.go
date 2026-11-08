package app

import (
	"errors"
	"path/filepath"

	"github.com/tbxark/gvim/editor"
)

// openTargets opens the file arguments in ed. With reuseOpen set, a file that a
// restored tab already shows is not opened a second time: its tab is activated
// and the caret goes to the position the argument asked for. That is the launch
// where --restore (or editor.skip_session_restore_when_files_passed=false) has
// just restored the saved tabs and the given files follow them.
//
// It returns the large files that the UI must ask about before they are loaded
// whole, and the files that could not be transcoded and were left out with a
// notice. Any other failure to open a file is returned.
func openTargets(ed *editor.Editor, targets []FileTarget, reuseOpen bool) (notices []string, fullLoads []editor.ErrLargeNeedsFullLoad, err error) {
	for _, f := range targets {
		if reuseOpen {
			if i, ok := tabForPath(ed, f.Path); ok {
				ed.ActivateTab(i)
				if f.Line > 0 {
					if err := ed.GotoLine(f.Line, f.Col); err != nil {
						return nil, nil, err
					}
				}
				continue
			}
		}
		if err := ed.OpenFile(f.Path); err != nil {
			var need editor.ErrLargeNeedsFullLoad
			if errors.As(err, &need) {
				// Decoding it whole would hold the first frame back. The UI asks.
				fullLoads = append(fullLoads, need)
				continue
			}
			if errors.Is(err, editor.ErrTooLargeToTranscode) {
				// Only this file is lost: the others open, and the status row says why.
				notices = append(notices, err.Error())
				continue
			}
			return nil, nil, err
		}
		if f.Line > 0 {
			if err := ed.GotoLine(f.Line, f.Col); err != nil {
				return nil, nil, err
			}
		}
	}
	return notices, fullLoads, nil
}

// tabForPath returns the index of the tab that shows path, and false when no tab
// does. Paths are compared absolute and cleaned, because a command-line argument
// and a restored tab spell the same file differently.
func tabForPath(ed *editor.Editor, path string) (int, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, false
	}
	abs = filepath.Clean(abs)
	for i, t := range ed.Tabs() {
		if t.Path != "" && filepath.Clean(t.Path) == abs {
			return i, true
		}
	}
	return 0, false
}
