package restore

import (
	"errors"

	"github.com/tbxark/gvim/editor"
)

// openFile opens path in ed as OpenFile does. A large file that is not UTF-8 cannot be
// read lazily, and no question can be asked before the UI is up (a command line
// argument, a restored tab), so it is decoded whole, as such a file was before large
// file mode. A file over what a full load reads is refused with an error that wraps
// editor.ErrTooLargeToTranscode.
func openFile(ed *editor.Editor, path string) error {
	err := ed.OpenFile(path)
	var need editor.ErrLargeNeedsFullLoad
	if !errors.As(err, &need) {
		return err
	}
	d, err := ed.LoadFullFunc(path)()
	if err != nil {
		return err
	}
	ed.OpenLoaded(d)
	return nil
}
